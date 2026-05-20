package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// helper: spin up an App with a fresh on-disk DB and migration applied.
func newTestApp(t *testing.T) *App {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	app, err := NewApp(cfg, db, clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// runHandler exercises a handler built by a *App.WorkerCommands /
// ProjectCommands / ConversationCommands entry. We register the handler's
// flags into a fresh FlagSet, permissive-parse args, then invoke.
func runHandler(t *testing.T, cmd *Command, args []string) (stdout, stderr string, code ExitCode) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	if cmd.Run != nil {
		code = cmd.Run(context.Background(), args, &outBuf, &errBuf)
		return outBuf.String(), errBuf.String(), code
	}
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	handler := cmd.Flags(fs)
	positionals, err := permissiveParse(fs, args)
	if err != nil {
		errBuf.WriteString("usage: " + err.Error())
		return outBuf.String(), errBuf.String(), ExitUsage
	}
	code = handler(context.Background(), positionals, &outBuf, &errBuf)
	return outBuf.String(), errBuf.String(), code
}

// runByName resolves a leaf command by name within a list (or its
// sub-tree) and runs it.
func runByName(t *testing.T, app *App, group string, names ...string) func(args []string) (string, string, ExitCode) {
	t.Helper()
	return func(args []string) (string, string, ExitCode) {
		var cmds []*Command
		switch group {
		case "worker":
			cmds = app.WorkerCommands()
		case "project":
			cmds = app.ProjectCommands()
		case "conversation":
			cmds = app.ConversationCommands()
		}
		cmd := findCmd(cmds, names[0])
		for _, n := range names[1:] {
			if cmd == nil {
				t.Fatalf("not found: %v", names)
			}
			cmd = findCmd(cmd.Subcommands, n)
		}
		if cmd == nil {
			t.Fatalf("not found: %v", names)
		}
		return runHandler(t, cmd, args)
	}
}

// =============================================================================
// worker enroll
// =============================================================================

func TestCLI_WorkerEnroll_Happy(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	out, _, code := run([]string{"--worker-id=W-1", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("json: %v\nout: %s", err, out)
	}
	if m["worker_id"] != "W-1" {
		t.Fatalf("worker_id: %v", m["worker_id"])
	}
}

func TestCLI_WorkerEnroll_MissingID(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	_, errOut, code := run([]string{"--format=json"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "usage_error") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_WorkerEnroll_Duplicate(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	_, _, _ = run([]string{"--worker-id=W-1"})
	_, errOut, code := run([]string{"--worker-id=W-1", "--format=json"})
	if code != ExitBusinessError {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "worker_already_exists") {
		t.Fatalf("err: %s", errOut)
	}
}

// =============================================================================
// worker list / status
// =============================================================================

func TestCLI_WorkerList_Empty(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "list")
	out, _, code := run([]string{"--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if strings.TrimSpace(out) != "null" && strings.TrimSpace(out) != "[]" {
		t.Fatalf("expected null/[], got %q", out)
	}
}

func TestCLI_WorkerStatus_NotFound(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "status")
	_, errOut, code := run([]string{"W-MISSING", "--format=json"})
	if code != ExitNotFound {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "worker_not_found") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_WorkerStatus_MissingArg(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "status")
	_, _, code := run([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_WorkerList_InvalidStatus(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "list")
	_, _, code := run([]string{"--status=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

// =============================================================================
// proposal flow
// =============================================================================

func enrollAndPropose(t *testing.T, app *App) string {
	t.Helper()
	run := runByName(t, app, "worker", "enroll")
	if _, _, c := run([]string{"--worker-id=W-1"}); c != ExitOK {
		t.Fatal("enroll failed")
	}
	propose := runByName(t, app, "worker", "proposal", "propose")
	out, _, c := propose([]string{"--worker-id=W-1", "--candidate-path=/x/y", "--suggested-kind=coding", "--format=json"})
	if c != ExitOK {
		t.Fatalf("propose failed: %s", out)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	return r["proposal_id"].(string)
}

func TestCLI_ProposalAccept_Happy(t *testing.T) {
	app := newTestApp(t)
	id := enrollAndPropose(t, app)
	accept := runByName(t, app, "worker", "proposal", "accept")
	out, _, code := accept([]string{id, "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d out: %s", code, out)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	if r["project_id"] == "" || r["mapping_id"] == "" {
		t.Fatalf("missing fields: %v", r)
	}
}

func TestCLI_ProposalAccept_AlreadyAccepted(t *testing.T) {
	app := newTestApp(t)
	id := enrollAndPropose(t, app)
	accept := runByName(t, app, "worker", "proposal", "accept")
	_, _, _ = accept([]string{id})
	_, errOut, code := accept([]string{id, "--format=json"})
	if code != ExitInvalidTransition {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "proposal_already_terminated") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_ProposalIgnore_NotPending(t *testing.T) {
	app := newTestApp(t)
	id := enrollAndPropose(t, app)
	accept := runByName(t, app, "worker", "proposal", "accept")
	_, _, _ = accept([]string{id})
	ign := runByName(t, app, "worker", "proposal", "ignore")
	_, errOut, code := ign([]string{id})
	if code != ExitInvalidTransition {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "proposal_already_terminated") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_ProposalUnignore_Happy(t *testing.T) {
	app := newTestApp(t)
	id := enrollAndPropose(t, app)
	ign := runByName(t, app, "worker", "proposal", "ignore")
	_, _, _ = ign([]string{id})
	un := runByName(t, app, "worker", "proposal", "unignore")
	_, _, code := un([]string{id})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalList_Pending(t *testing.T) {
	app := newTestApp(t)
	enrollAndPropose(t, app)
	list := runByName(t, app, "worker", "proposal", "list")
	out, _, code := list([]string{"--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatal(err)
	}
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

func TestCLI_ProposalShow_Happy(t *testing.T) {
	app := newTestApp(t)
	id := enrollAndPropose(t, app)
	show := runByName(t, app, "worker", "proposal", "show")
	out, _, code := show([]string{id, "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	if r["proposal_id"] != id {
		t.Fatalf("got %v", r)
	}
}

// =============================================================================
// project CRUD
// =============================================================================

func TestCLI_ProjectAdd_Happy(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	out, _, code := add([]string{"my-proj", "--kind=coding", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d err out: %s", code, out)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	if r["project_id"] != "my-proj" {
		t.Fatalf("got %v", r)
	}
}

func TestCLI_ProjectAdd_DupSlug(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding"})
	_, errOut, code := add([]string{"p", "--kind=coding", "--format=json"})
	if code != ExitBusinessError {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "project_already_exists") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_ProjectRemove_HasActiveDeps(t *testing.T) {
	app := newTestApp(t)
	// enroll + propose + accept → creates project + mapping
	id := enrollAndPropose(t, app)
	accept := runByName(t, app, "worker", "proposal", "accept")
	_, _, _ = accept([]string{id, "--project-id=p"})
	// Now p has an active mapping; remove should fail.
	rm := runByName(t, app, "project", "remove")
	_, errOut, code := rm([]string{"p"})
	if code != ExitInvariantViolation {
		t.Fatalf("code: %d errOut: %s", code, errOut)
	}
	if !strings.Contains(errOut, "project_has_active_deps") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_ProjectUpdate_VersionConflict(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding"})
	upd := runByName(t, app, "project", "update")
	_, errOut, code := upd([]string{"p", "--name=Renamed", "--version=99"})
	if code != ExitVersionConflict {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "project_version_conflict") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_ProjectUpdate_NoVersion(t *testing.T) {
	app := newTestApp(t)
	upd := runByName(t, app, "project", "update")
	_, _, code := upd([]string{"p", "--name=x"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectUpdate_NoFields(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding"})
	upd := runByName(t, app, "project", "update")
	_, _, code := upd([]string{"p", "--version=1"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectList_FilterKind(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"a", "--kind=coding"})
	_, _, _ = add([]string{"b", "--kind=writing"})
	list := runByName(t, app, "project", "list")
	out, _, _ := list([]string{"--kind=coding", "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(out), &arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

// =============================================================================
// conversation
// =============================================================================

func TestCLI_ConvAddMessage_Happy(t *testing.T) {
	app := newTestApp(t)
	open := runByName(t, app, "conversation", "open")
	out, _, code := open([]string{"--kind=dm", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	convID := r["conversation_id"].(string)
	add := runByName(t, app, "conversation", "add-message")
	out, _, code = add([]string{convID, "--kind=text", "--content=hello", "--direction=internal", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d out: %s", code, out)
	}
}

func TestCLI_ConvAddMessage_Closed(t *testing.T) {
	app := newTestApp(t)
	open := runByName(t, app, "conversation", "open")
	out, _, _ := open([]string{"--kind=dm", "--format=json"})
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	convID := r["conversation_id"].(string)
	closeC := runByName(t, app, "conversation", "close")
	_, _, _ = closeC([]string{convID, "--reason=done", "--message=ok", "--version=1"})
	add := runByName(t, app, "conversation", "add-message")
	_, errOut, code := add([]string{convID, "--kind=text", "--content=x", "--direction=internal", "--format=json"})
	if code != ExitBusinessError {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "conversation_closed") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_ConvList_TaskKind_Empty(t *testing.T) {
	app := newTestApp(t)
	list := runByName(t, app, "conversation", "list")
	out, _, code := list([]string{"--kind=task", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if strings.TrimSpace(out) != "null" && strings.TrimSpace(out) != "[]" {
		t.Fatalf("expected empty: %s", out)
	}
}

func TestCLI_ConvOpen_BadKind(t *testing.T) {
	app := newTestApp(t)
	open := runByName(t, app, "conversation", "open")
	_, _, code := open([]string{"--kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvOpen_TaskKind_Rejected(t *testing.T) {
	app := newTestApp(t)
	open := runByName(t, app, "conversation", "open")
	_, errOut, code := open([]string{"--kind=task"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "conversation_invalid_kind") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestCLI_ConvRead_Tail(t *testing.T) {
	app := newTestApp(t)
	open := runByName(t, app, "conversation", "open")
	out, _, _ := open([]string{"--kind=dm", "--format=json"})
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	convID := r["conversation_id"].(string)
	add := runByName(t, app, "conversation", "add-message")
	for i := 0; i < 3; i++ {
		_, _, _ = add([]string{convID, "--kind=text", "--content=msg", "--direction=internal"})
	}
	read := runByName(t, app, "conversation", "read")
	out, _, _ = read([]string{convID, "--tail=2", "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(out), &arr)
	if len(arr) != 2 {
		t.Fatalf("got %d", len(arr))
	}
}

// =============================================================================
// error mapping coverage
// =============================================================================

func TestMapDomainError_AllSentinels(t *testing.T) {
	cases := []error{
		workforce.ErrWorkerNotFound,
		workforce.ErrWorkerAlreadyExists,
		workforce.ErrWorkerVersionConflict,
		workforce.ErrWorkerInvalidStatus,
		workforce.ErrMappingNotFound,
		workforce.ErrMappingAlreadyActive,
		workforce.ErrMappingNotActive,
		workforce.ErrProposalNotFound,
		workforce.ErrProposalAlreadyTerminated,
		workforce.ErrProposalInvalidTransition,
		workforce.ErrProposalAlreadyExists,
		workforce.ErrProposalVersionConflict,
		workforce.ErrProjectNotFound,
		workforce.ErrProjectAlreadyExists,
		workforce.ErrProjectVersionConflict,
		workforce.ErrProjectHasActiveDeps,
		workforce.ErrProjectInvalidSlug,
		workforce.ErrProjectInvalidKind,
		conversation.ErrConversationNotFound,
		conversation.ErrConversationAlreadyExists,
		conversation.ErrConversationClosed,
		conversation.ErrConversationInvalidKind,
		conversation.ErrConversationInvalidStatus,
		conversation.ErrConversationVersionConflict,
		conversation.ErrMessageNotFound,
		conversation.ErrMessageDuplicate,
		conversation.ErrMessageImmutable,
		conversation.ErrMessageInvalidSender,
	}
	for _, e := range cases {
		reason, code, ok := MapDomainError(e)
		if !ok || reason == "" || code == 0 {
			t.Fatalf("missing mapping for %v: ok=%v reason=%s code=%d", e, ok, reason, code)
		}
	}
}

func TestMapDomainError_Unknown(t *testing.T) {
	_, _, ok := MapDomainError(errors.New("unknown"))
	if ok {
		t.Fatal()
	}
}

func TestHandleDomainError_Nil(t *testing.T) {
	var buf bytes.Buffer
	if c := HandleDomainError(&buf, "human", nil); c != ExitOK {
		t.Fatal()
	}
}

func TestHandleDomainError_Sentinel(t *testing.T) {
	var buf bytes.Buffer
	c := HandleDomainError(&buf, "json", workforce.ErrWorkerNotFound)
	if c != ExitNotFound {
		t.Fatal()
	}
	if !strings.Contains(buf.String(), "worker_not_found") {
		t.Fatalf("got %s", buf.String())
	}
}

func TestHandleDomainError_Internal(t *testing.T) {
	var buf bytes.Buffer
	c := HandleDomainError(&buf, "human", errors.New("oops"))
	if c != ExitBusinessError {
		t.Fatal()
	}
	if !strings.Contains(buf.String(), "internal_error") {
		t.Fatalf("got %s", buf.String())
	}
}

// =============================================================================
// permissiveParse
// =============================================================================

func TestPermissiveParse_FlagsAfterPositional(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	format := fs.String("format", "human", "")
	pos, err := permissiveParse(fs, []string{"ARG1", "--format=json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 1 || pos[0] != "ARG1" {
		t.Fatalf("positional: %v", pos)
	}
	if *format != "json" {
		t.Fatalf("format: %s", *format)
	}
}

func TestPermissiveParse_DoubleDashEndsFlags(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("format", "human", "")
	pos, err := permissiveParse(fs, []string{"A", "--", "--format=json"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 2 {
		t.Fatalf("positional: %v", pos)
	}
}

func TestPermissiveParse_OnlyFlags(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	flag := fs.String("name", "", "")
	pos, err := permissiveParse(fs, []string{"--name=x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pos) != 0 {
		t.Fatal()
	}
	if *flag != "x" {
		t.Fatal()
	}
}

func TestPermissiveParse_ParseError(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Int("n", 0, "")
	_, err := permissiveParse(fs, []string{"--n=bogus"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// system commands
// =============================================================================

func TestSystemCommands_Version(t *testing.T) {
	cmds := SystemCommands("v1.0", "abc")
	if len(cmds) == 0 {
		t.Fatal()
	}
	var buf bytes.Buffer
	code := cmds[0].Run(context.Background(), nil, &buf, io.Discard)
	if code != ExitOK {
		t.Fatal()
	}
	if !strings.Contains(buf.String(), "v1.0") {
		t.Fatalf("got %s", buf.String())
	}
}

func TestSystemCommands_VersionDevFallback(t *testing.T) {
	cmds := SystemCommands("", "")
	var buf bytes.Buffer
	cmds[0].Run(context.Background(), nil, &buf, io.Discard)
	if buf.Len() == 0 {
		t.Fatal()
	}
}

func TestSupervisorPlaceholder_Stub(t *testing.T) {
	cmd := SupervisorPlaceholder()
	var buf bytes.Buffer
	code := cmd.Run(context.Background(), nil, io.Discard, &buf)
	if code != ExitNotImplemented {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(buf.String(), "not_implemented") {
		t.Fatalf("err: %s", buf.String())
	}
}

func TestWorkerRunPlaceholder_Stub(t *testing.T) {
	cmd := WorkerRunPlaceholder()
	var buf bytes.Buffer
	code := cmd.Run(context.Background(), nil, io.Discard, &buf)
	if code != ExitNotImplemented {
		t.Fatalf("code: %d", code)
	}
}

func TestAdminBlobMigratePlaceholder_Stub(t *testing.T) {
	cmd := AdminBlobMigratePlaceholder()
	var buf bytes.Buffer
	code := cmd.Run(context.Background(), nil, io.Discard, &buf)
	if code != ExitNotImplemented {
		t.Fatalf("code: %d", code)
	}
}

// =============================================================================
// router build smoke
// =============================================================================

func TestBuildRouter_AddsAllSubcommands(t *testing.T) {
	router, _, err := BuildRouter("v", "c", []string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"version", "server", "migrate", "supervisor", "admin", "worker", "project", "conversation"} {
		if findSubcommand(router.Root, name) == nil {
			t.Fatalf("missing top-level command: %s", name)
		}
	}
}

func TestStripGlobalFlags(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"--config", "x.yaml", "worker", "list"}, []string{"worker", "list"}},
		{[]string{"--config=x.yaml", "worker", "list"}, []string{"worker", "list"}},
		{[]string{"-c", "x.yaml", "worker"}, []string{"worker"}},
		{[]string{"-c=x.yaml", "worker"}, []string{"worker"}},
		{[]string{"worker", "list"}, []string{"worker", "list"}},
	}
	for i, c := range cases {
		got := StripGlobalFlags(c.in, "")
		if !sliceEqual(got, c.want) {
			t.Fatalf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestExtractConfigFlag(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"--config", "a.yaml", "x"}, "a.yaml"},
		{[]string{"--config=b.yaml"}, "b.yaml"},
		{[]string{"-c", "c.yaml"}, "c.yaml"},
		{[]string{"-c=d.yaml"}, "d.yaml"},
		{[]string{"x"}, ""},
	}
	for i, c := range cases {
		if got := extractConfigFlag(c.in); got != c.want {
			t.Fatalf("case %d: got %q want %q", i, got, c.want)
		}
	}
}

func TestPathBasename(t *testing.T) {
	if pathBasename("/a/b/c") != "c" {
		t.Fatal()
	}
	if pathBasename("c") != "c" {
		t.Fatal()
	}
	if pathBasename(`a\b\c`) != "c" {
		t.Fatal()
	}
}

func TestSplitNonEmpty(t *testing.T) {
	got := splitNonEmpty("a,b, ,c", ",")
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if splitNonEmpty("", ",") != nil {
		t.Fatal()
	}
}

func TestBuildPlaceholderApp(t *testing.T) {
	app, err := buildPlaceholderApp()
	if err != nil {
		t.Fatal(err)
	}
	if app == nil || app.DB == nil {
		t.Fatal()
	}
	_ = app.DB.Close()
}

func TestApp_DefaultActor(t *testing.T) {
	app := newTestApp(t)
	if string(app.DefaultActor()) != "user:hayang" {
		t.Fatalf("got %v", app.DefaultActor())
	}
}

func TestApp_NewApp_NilDB(t *testing.T) {
	_, err := NewApp(config.DefaultConfig(), nil, nil)
	if err == nil {
		t.Fatal()
	}
}

func TestApp_NewApp_NoTables(t *testing.T) {
	// Open empty DB without migrations → NewApp should fail (event repo
	// init needs the events table).
	db, _ := persistence.Open(t.TempDir() + "/empty.db")
	t.Cleanup(func() { _ = db.Close() })
	_, err := NewApp(config.DefaultConfig(), db, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAndMigrate_BadPath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.SqlitePath = "/proc/cannot/write/here.db"
	_, err := OpenAndMigrate(cfg)
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// Migrate / Server commands smoke
// =============================================================================

func TestMigrateCommand_RunsUp(t *testing.T) {
	cmd := MigrateCommand()
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/test.db"
	if err := writeFile(t, cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\nidentity:\n  default_user: hayang\n"); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := runHandler(t, cmd, []string{"--config=" + cfgPath})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "version") {
		t.Fatalf("got %s", stdout)
	}
	// Verify DB has events table.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var c int
	_ = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'`).Scan(&c)
	if c != 1 {
		t.Fatal("events table missing")
	}
}

func TestMigrateCommand_BadConfig(t *testing.T) {
	cmd := MigrateCommand()
	_, errOut, code := runHandler(t, cmd, []string{"--config=/nonexistent.yaml"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(errOut, "config") {
		t.Fatalf("err: %s", errOut)
	}
}

func TestServerCommand_MigrateOnly(t *testing.T) {
	cmd := ServerCommand()
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/test.db"
	_ = writeFile(t, cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\nidentity:\n  default_user: hayang\n")
	stdout, _, code := runHandler(t, cmd, []string{"--config=" + cfgPath, "--migrate-only"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "migrate") {
		t.Fatalf("got %s", stdout)
	}
}

func TestServerCommand_BadConfig(t *testing.T) {
	cmd := ServerCommand()
	_, _, code := runHandler(t, cmd, []string{"--config=/nonexistent.yaml"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	return writeFileImpl(path, content)
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

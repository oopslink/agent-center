package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// seedProject creates a pm.Project directly via the pm project repo. The CLI
// project READ commands (list/show) were repointed off the retired
// workforce.Project model to the new pm.Project model in #131 PR-3, so the
// list/show tests must seed pm projects. Used by tests whose real assertion is
// on the read-only project commands (list/show).
func seedProject(t *testing.T, app *App, id, name string) {
	t.Helper()
	p, err := pm.NewProject(pm.NewProjectInput{
		ID:             pm.ProjectID(id),
		OrganizationID: "org-test",
		Name:           name,
		CreatedBy:      pm.IdentityRef("user:tester"),
		CreatedAt:      time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("seed project (pm.NewProject): %v", err)
	}
	if err := app.PMProjectRepo.Save(context.Background(), p); err != nil {
		t.Fatalf("seed project (Save): %v", err)
	}
}

// helper: spin up an App with a fresh on-disk DB and migration applied.
func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	path := dir + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	// v2.6: master_key_file is required for Identity BC auth + UserSecret.
	// Tests need a 32-byte key file so AuthSvc / SigninSvc get wired.
	mkPath := dir + "/master.key"
	if err := writeTestMasterKey(mkPath); err != nil {
		t.Fatal(err)
	}
	cfg.SecretManagement.MasterKeyFile = mkPath
	cfg.SecretManagement.SkipPermsCheck = true
	app, err := NewApp(cfg, db, clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	return app
}

// writeTestMasterKey writes a deterministic base64-encoded 32-byte AES key
// to path for tests. LoadMasterKey expects base64 (matches install flow).
func writeTestMasterKey(path string) error {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	b64 := base64.StdEncoding.EncodeToString(key)
	return os.WriteFile(path, []byte(b64+"\n"), 0600)
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

// v2.5-B1: Re-enrolling a worker whose row is offline now takes the
// idempotent claim path (matches the post-mint flow where Add()
// pre-creates the row). The "duplicate enroll → already_exists" CLI
// branch only fires once the worker is actually online; that path
// is exercised at the service layer in TestEnroll_RejectsOnlineWorker.
func TestCLI_WorkerEnroll_IdempotentOnOffline(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	_, _, _ = run([]string{"--worker-id=W-1"})
	_, _, code := run([]string{"--worker-id=W-1", "--format=json"})
	if code != ExitOK {
		t.Fatalf("expected ExitOK on re-enroll of offline worker, got %d", code)
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
// project CRUD
// =============================================================================

func TestCLI_ProjectList_All(t *testing.T) {
	app := newTestApp(t)
	// Seed two projects directly via the workforce service (the `project add`
	// CLI command was removed in #132); the read assertion is on `project list`.
	seedProject(t, app, "a", "a")
	seedProject(t, app, "b", "b")
	list := runByName(t, app, "project", "list")
	out, _, _ := list([]string{"--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(out), &arr)
	if len(arr) != 2 {
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
		conversation.ErrConversationNotFound,
		conversation.ErrConversationAlreadyExists,
		conversation.ErrConversationClosed,
		conversation.ErrConversationInvalidKind,
		conversation.ErrConversationInvalidStatus,
		conversation.ErrConversationVersionConflict,
		conversation.ErrMessageNotFound,
		conversation.ErrMessageImmutable,
		conversation.ErrMessageInvalidSender,
		conversation.ErrConversationArchived,
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

// TestWorkerRunCommand_RequiresWorkerID pins the usage guard: `worker run` with no
// --worker-id returns ExitUsage BEFORE attempting any daemon bootstrap (so the test
// is hermetic — no config/network).
func TestWorkerRunCommand_RequiresWorkerID(t *testing.T) {
	_, errOut, code := runHandler(t, WorkerRunCommand(), nil)
	if code != ExitUsage {
		t.Fatalf("missing --worker-id: code=%d want ExitUsage", code)
	}
	if !strings.Contains(errOut, "--worker-id is required") {
		t.Fatalf("stderr=%q, want the --worker-id required message", errOut)
	}
}

// TestResolveWorkerConfigPath_GlobalFallback regression-guards the slice-1 parity
// break (Tester msg 601b01a3): the unified router strips the global --config before
// the `worker run` FlagSet, so the subcommand --config is empty in real routing and
// the handler MUST fall back to the global config path (else worker run ignores the
// operator config and uses /var/lib defaults).
func TestResolveWorkerConfigPath_GlobalFallback(t *testing.T) {
	prev := GlobalConfigPath()
	defer SetGlobalConfigPath(prev)

	SetGlobalConfigPath("/tmp/operator-config.yaml")
	if got := resolveWorkerConfigPath(""); got != "/tmp/operator-config.yaml" {
		t.Fatalf("empty subcommand --config must fall back to the global config, got %q", got)
	}
	// An explicit subcommand --config (e.g. via runHandler / direct invocation) wins.
	if got := resolveWorkerConfigPath("/sub/explicit.yaml"); got != "/sub/explicit.yaml" {
		t.Fatalf("explicit subcommand --config must win, got %q", got)
	}
	SetGlobalConfigPath("")
	if got := resolveWorkerConfigPath(""); got != "" {
		t.Fatalf("both empty → empty, got %q", got)
	}
}

// TestWorkerRunCommand_FlagParity guards the `worker run` flag set against the
// (retiring) standalone daemon — no silent drop / rename / default change (the
// PM+Tester parity watch, v2.7 (b) cutover). Complements Tester's --help diff.
func TestWorkerRunCommand_FlagParity(t *testing.T) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	_ = WorkerRunCommand().Flags(fs)
	want := []string{
		"config", "worker-id", "worker-name", "fake-agent", "poll-interval",
		"capabilities", "admin-token", "admin-target", "server-fingerprint",
		"skills-dir",
	}
	for _, name := range want {
		if fs.Lookup(name) == nil {
			t.Errorf("worker run missing flag --%s (parity with standalone daemon)", name)
		}
	}
	// Behavior-critical default parity.
	if f := fs.Lookup("poll-interval"); f != nil && f.DefValue != "1s" {
		t.Errorf("--poll-interval default = %q, want 1s", f.DefValue)
	}
	// #107 slice-2: --use-control-loop was removed (control-stream path is now
	// unconditional). Guard that it is not reintroduced.
	if fs.Lookup("use-control-loop") != nil {
		t.Errorf("--use-control-loop should be removed (control-stream path is unconditional)")
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
	for _, name := range []string{"version", "server", "migrate", "admin", "worker", "project", "conversation"} {
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

// TestOpenAndMigrate_Happy exercises the happy path: opens an on-disk
// SQLite DB and runs migrations up.
func TestOpenAndMigrate_Happy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.SqlitePath = t.TempDir() + "/test.db"
	db, err := OpenAndMigrate(cfg)
	if err != nil {
		t.Fatalf("OpenAndMigrate: %v", err)
	}
	defer db.Close()
	if db == nil {
		t.Fatal("nil db")
	}
}

// =============================================================================
// Migrate / Server commands smoke
// =============================================================================

func TestMigrateCommand_RunsUp(t *testing.T) {
	cmd := MigrateUpCommand()
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

// Covers handlers_system.go:131-135 — the `--target=N` branch of the
// migrate handler (rollback to version N). The default RunsUp test only
// exercises the auto-up branch.
func TestMigrateCommand_TargetDown(t *testing.T) {
	cmd := MigrateUpCommand()
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/test.db"
	if err := writeFile(t, cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\nidentity:\n  default_user: hayang\n"); err != nil {
		t.Fatal(err)
	}
	// First run --up to populate to head.
	if _, _, code := runHandler(t, cmd, []string{"--config=" + cfgPath}); code != ExitOK {
		t.Fatalf("up: %d", code)
	}
	// Now run --target=0 (rollback to baseline).
	stdout, _, code := runHandler(t, cmd, []string{"--config=" + cfgPath, "--target=0"})
	if code != ExitOK {
		t.Fatalf("down: %d", code)
	}
	if !strings.Contains(stdout, "version") {
		t.Fatalf("got %s", stdout)
	}
}

func TestMigrateCommand_BadConfig(t *testing.T) {
	cmd := MigrateUpCommand()
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

// Covers handlers_system.go:88-99 — the idle-then-select block that prints
// the listen banner and waits on SIGINT/SIGTERM/ctx.Done. We can't easily
// raise SIGINT in-process, but driving the handler with an already-cancelled
// ctx triggers the `<-ctx.Done()` arm immediately.
func TestServerCommand_CtxCancelExitsCleanly(t *testing.T) {
	cmd := ServerCommand()
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/test.db"
	mkPath := dir + "/master.key"
	if err := writeTestMasterKey(mkPath); err != nil {
		t.Fatal(err)
	}
	_ = writeFile(t, cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\nidentity:\n  default_user: hayang\nsecret_management:\n  master_key_file: '"+mkPath+"'\n  skip_perms_check: true\n")

	// Build the handler directly so we can pass our own ctx.
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	handler := cmd.Flags(fs)
	if err := fs.Parse([]string{"--config=" + cfgPath}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invoking so the select observes Done() immediately
	code := handler(ctx, fs.Args(), &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("code: %d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "context canceled") {
		t.Fatalf("expected 'context canceled' banner, got %q", stdout.String())
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

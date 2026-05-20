package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestCLI_WorkerEnroll_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	stdout, _, code := run([]string{"--worker-id=W-1"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "enrolled worker W-1") {
		t.Fatalf("stdout: %s", stdout)
	}
}

func TestCLI_WorkerList_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	run := runByName(t, app, "worker", "enroll")
	_, _, _ = run([]string{"--worker-id=W-1"})
	listRun := runByName(t, app, "worker", "list")
	stdout, _, code := listRun([]string{})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "W-1") {
		t.Fatalf("stdout: %s", stdout)
	}
}

func TestCLI_WorkerList_StatusFilter(t *testing.T) {
	app := newTestApp(t)
	enroll := runByName(t, app, "worker", "enroll")
	_, _, _ = enroll([]string{"--worker-id=W-1"})
	listRun := runByName(t, app, "worker", "list")
	stdout, _, _ := listRun([]string{"--status=offline", "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(stdout), &arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

func TestCLI_WorkerStatus_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	enroll := runByName(t, app, "worker", "enroll")
	_, _, _ = enroll([]string{"--worker-id=W-1"})
	status := runByName(t, app, "worker", "status")
	stdout, _, code := status([]string{"W-1"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	if !strings.Contains(stdout, "W-1") || !strings.Contains(stdout, "offline") {
		t.Fatalf("stdout: %s", stdout)
	}
}

func TestCLI_ProposalPropose_NoSuggestedID(t *testing.T) {
	app := newTestApp(t)
	enroll := runByName(t, app, "worker", "enroll")
	_, _, _ = enroll([]string{"--worker-id=W-1"})
	propose := runByName(t, app, "worker", "proposal", "propose")
	out, _, code := propose([]string{"--worker-id=W-1", "--candidate-path=/home/u/dirname", "--suggested-kind=coding", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	if r["proposal_id"] == "" {
		t.Fatalf("got %v", r)
	}
}

func TestCLI_ProposalPropose_MissingArgs(t *testing.T) {
	app := newTestApp(t)
	propose := runByName(t, app, "worker", "proposal", "propose")
	_, _, code := propose([]string{"--worker-id=W-1"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalPropose_BadKind(t *testing.T) {
	app := newTestApp(t)
	propose := runByName(t, app, "worker", "proposal", "propose")
	_, _, code := propose([]string{"--worker-id=W-1", "--candidate-path=/x", "--suggested-kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalIgnore_MissingID(t *testing.T) {
	app := newTestApp(t)
	ign := runByName(t, app, "worker", "proposal", "ignore")
	_, _, code := ign([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalUnignore_MissingID(t *testing.T) {
	app := newTestApp(t)
	un := runByName(t, app, "worker", "proposal", "unignore")
	_, _, code := un([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalShow_MissingID(t *testing.T) {
	app := newTestApp(t)
	show := runByName(t, app, "worker", "proposal", "show")
	_, _, code := show([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalAccept_MissingID(t *testing.T) {
	app := newTestApp(t)
	accept := runByName(t, app, "worker", "proposal", "accept")
	_, _, code := accept([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalAccept_BadKind(t *testing.T) {
	app := newTestApp(t)
	accept := runByName(t, app, "worker", "proposal", "accept")
	_, _, code := accept([]string{"PR-X", "--kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalList_StatusFilterPending(t *testing.T) {
	app := newTestApp(t)
	id := enrollAndPropose(t, app)
	list := runByName(t, app, "worker", "proposal", "list")
	stdout, _, _ := list([]string{"--status=pending", "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(stdout), &arr)
	if len(arr) != 1 || arr[0]["proposal_id"] != id {
		t.Fatalf("got %v", arr)
	}
}

func TestCLI_ProposalList_StatusFilterRejected(t *testing.T) {
	app := newTestApp(t)
	list := runByName(t, app, "worker", "proposal", "list")
	_, _, code := list([]string{"--status=accepted"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProposalList_WorkerFilter(t *testing.T) {
	app := newTestApp(t)
	enrollAndPropose(t, app)
	list := runByName(t, app, "worker", "proposal", "list")
	stdout, _, _ := list([]string{"--worker-id=W-1", "--format=json"})
	var arr []map[string]any
	_ = json.Unmarshal([]byte(stdout), &arr)
	if len(arr) != 1 {
		t.Fatalf("got %d", len(arr))
	}
}

func TestCLI_ProposalList_WorkerFilter_BadStatus(t *testing.T) {
	app := newTestApp(t)
	list := runByName(t, app, "worker", "proposal", "list")
	_, _, code := list([]string{"--worker-id=W-1", "--status=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectShow(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding"})
	show := runByName(t, app, "project", "show")
	out, _, code := show([]string{"p", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	if r["project_id"] != "p" {
		t.Fatalf("got %v", r)
	}
}

func TestCLI_ProjectShow_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding", "--name=Pname"})
	show := runByName(t, app, "project", "show")
	stdout, _, _ := show([]string{"p"})
	if !strings.Contains(stdout, "Pname") {
		t.Fatalf("got %s", stdout)
	}
}

func TestCLI_ProjectShow_NotFound(t *testing.T) {
	app := newTestApp(t)
	show := runByName(t, app, "project", "show")
	_, _, code := show([]string{"nope"})
	if code != ExitNotFound {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectShow_MissingArg(t *testing.T) {
	app := newTestApp(t)
	show := runByName(t, app, "project", "show")
	_, _, code := show([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectAdd_BadSlug(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, code := add([]string{"BAD SLUG", "--kind=coding"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectAdd_BadKind(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, code := add([]string{"p", "--kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectAdd_MissingArg(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, code := add([]string{"--kind=coding"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectRemove_MissingArg(t *testing.T) {
	app := newTestApp(t)
	rm := runByName(t, app, "project", "remove")
	_, _, code := rm([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectRemove_NotFound(t *testing.T) {
	app := newTestApp(t)
	rm := runByName(t, app, "project", "remove")
	_, _, code := rm([]string{"nope"})
	if code != ExitNotFound {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectUpdate_BadKind(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding"})
	upd := runByName(t, app, "project", "update")
	_, _, code := upd([]string{"p", "--version=1", "--kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectUpdate_NoArg(t *testing.T) {
	app := newTestApp(t)
	upd := runByName(t, app, "project", "update")
	_, _, code := upd([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectUpdate_Happy(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding"})
	upd := runByName(t, app, "project", "update")
	out, _, code := upd([]string{"p", "--name=Renamed", "--version=1", "--format=json"})
	if code != ExitOK {
		t.Fatalf("code: %d out: %s", code, out)
	}
	var r map[string]any
	_ = json.Unmarshal([]byte(out), &r)
	if r["name"] != "Renamed" {
		t.Fatalf("got %v", r)
	}
}

func TestCLI_ProjectList_BadKind(t *testing.T) {
	app := newTestApp(t)
	list := runByName(t, app, "project", "list")
	_, _, code := list([]string{"--kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ProjectList_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "project", "add")
	_, _, _ = add([]string{"p", "--kind=coding", "--name=PName"})
	list := runByName(t, app, "project", "list")
	stdout, _, _ := list([]string{})
	if !strings.Contains(stdout, "PName") {
		t.Fatalf("got %s", stdout)
	}
}

func TestCLI_ConvAddMessage_MissingID(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "conversation", "add-message")
	_, _, code := add([]string{"--kind=text"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvAddMessage_BadKind(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "conversation", "add-message")
	_, _, code := add([]string{"C-1", "--kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvAddMessage_BadDirection(t *testing.T) {
	app := newTestApp(t)
	add := runByName(t, app, "conversation", "add-message")
	_, _, code := add([]string{"C-1", "--direction=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvList_BadKind(t *testing.T) {
	app := newTestApp(t)
	list := runByName(t, app, "conversation", "list")
	_, _, code := list([]string{"--kind=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvList_BadStatus(t *testing.T) {
	app := newTestApp(t)
	list := runByName(t, app, "conversation", "list")
	_, _, code := list([]string{"--status=bogus"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvList_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	open := runByName(t, app, "conversation", "open")
	_, _, _ = open([]string{"--kind=dm", "--title=My Title"})
	list := runByName(t, app, "conversation", "list")
	stdout, _, _ := list([]string{})
	if !strings.Contains(stdout, "My Title") {
		t.Fatalf("got %s", stdout)
	}
}

func TestCLI_ConvRead_MissingID(t *testing.T) {
	app := newTestApp(t)
	read := runByName(t, app, "conversation", "read")
	_, _, code := read([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvRead_Since_BadFormat(t *testing.T) {
	app := newTestApp(t)
	read := runByName(t, app, "conversation", "read")
	_, _, code := read([]string{"C-X", "--since=notatime"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvRead_HumanFormat(t *testing.T) {
	app := newTestApp(t)
	open := runByName(t, app, "conversation", "open")
	stdout, _, _ := open([]string{"--kind=dm", "--format=json"})
	var r map[string]any
	_ = json.Unmarshal([]byte(stdout), &r)
	convID := r["conversation_id"].(string)
	add := runByName(t, app, "conversation", "add-message")
	_, _, _ = add([]string{convID, "--kind=text", "--content=hello", "--direction=internal"})
	read := runByName(t, app, "conversation", "read")
	stdout, _, _ = read([]string{convID})
	if !strings.Contains(stdout, "hello") {
		t.Fatalf("got %s", stdout)
	}
}

func TestCLI_ConvClose_Missing(t *testing.T) {
	app := newTestApp(t)
	closeC := runByName(t, app, "conversation", "close")
	_, _, code := closeC([]string{})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvClose_MissingReason(t *testing.T) {
	app := newTestApp(t)
	closeC := runByName(t, app, "conversation", "close")
	_, _, code := closeC([]string{"C-X", "--version=1"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestCLI_ConvClose_MissingVersion(t *testing.T) {
	app := newTestApp(t)
	closeC := runByName(t, app, "conversation", "close")
	_, _, code := closeC([]string{"C-X", "--reason=x", "--message=y"})
	if code != ExitUsage {
		t.Fatalf("code: %d", code)
	}
}

func TestWorkerToMap_WithHeartbeat(t *testing.T) {
	w, _ := workforce.NewWorker(workforce.NewWorkerInput{
		ID: "W-1", EnrolledAt: timeNow(),
	})
	_ = w.Heartbeat(timeNow(), 30)
	m := workerToMap(w)
	if _, ok := m["last_heartbeat_at"]; !ok {
		t.Fatal("expected last_heartbeat_at field")
	}
}

func TestConvToMap(t *testing.T) {
	c, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "C-1", Kind: conversation.ConversationKindDM, OpenedAt: timeNow(),
	})
	m := convToMap(c)
	if m["conversation_id"] != "C-1" {
		t.Fatal()
	}
}

func TestMsgToMap_Roundtrip(t *testing.T) {
	mm, _ := conversation.NewMessage(conversation.NewMessageInput{
		ID: "M-1", ConversationID: "C-1", SenderIdentityID: "user:x",
		ContentKind: conversation.MessageContentText,
		Direction:   conversation.DirectionInbound,
		Content:     "hi",
		PostedAt:    timeNow(),
	})
	m := msgToMap(mm)
	if m["message_id"] != "M-1" {
		t.Fatal()
	}
}

// TestLazyApp_Build_BadConfig confirms the lazy build path surfaces a
// config error (covers withApp + build).
func TestLazyApp_Build_BadConfig(t *testing.T) {
	provider := &lazyApp{cfgPath: "/nonexistent.yaml"}
	if _, err := provider.build(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLazyApp_BuildOK(t *testing.T) {
	// Write a valid config + non-existent DB path → lazy.build opens
	// and migrates successfully.
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/x.db"
	_ = writeFileImpl(cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\nidentity:\n  default_user: hayang\n")
	provider := &lazyApp{cfgPath: cfgPath}
	app, err := provider.build()
	if err != nil {
		t.Fatal(err)
	}
	defer app.DB.Close()
	// Quick health check.
	if app.WorkerRepo == nil {
		t.Fatal("WorkerRepo nil")
	}
}

// End-to-end through BuildRouter + Run, using a real config + DB. Covers
// withApp wrapper's runtime path.
func TestBuildRouter_LazyResourceCmd(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	dbPath := dir + "/x.db"
	_ = writeFileImpl(cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '"+dbPath+"'\nidentity:\n  default_user: hayang\n")
	args := []string{"--config=" + cfgPath, "worker", "enroll", "--worker-id=W-1", "--format=json"}
	router, _, err := BuildRouter("v", "c", args)
	if err != nil {
		t.Fatal(err)
	}
	code := router.Run(context.Background(), StripGlobalFlags(args, cfgPath))
	if code != ExitOK {
		t.Fatalf("code: %d", code)
	}
}

func TestBuildRouter_LazyResourceCmd_BadConfig(t *testing.T) {
	args := []string{"--config=/nonexistent.yaml", "worker", "enroll", "--worker-id=W-1"}
	router, _, _ := BuildRouter("v", "c", args)
	code := router.Run(context.Background(), StripGlobalFlags(args, ""))
	if code == ExitOK {
		t.Fatal("expected non-zero exit")
	}
}

func TestLazyApp_Build_BadDBPath(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/cfg.yaml"
	_ = writeFileImpl(cfgPath, "server:\n  listen_addr: ':7000'\n  sqlite_path: '/proc/no/such/file'\nidentity:\n  default_user: hayang\n")
	provider := &lazyApp{cfgPath: cfgPath}
	_, err := provider.build()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFindCmd_NotFound(t *testing.T) {
	cs := []*Command{{Name: "a"}, {Name: "b"}}
	if findCmd(cs, "c") != nil {
		t.Fatal()
	}
	if findCmd(cs, "a") == nil {
		t.Fatal()
	}
}

func TestEmitConfigErrors_PlainErr(t *testing.T) {
	var buf strings.Builder
	emitConfigErrors(&buf, errIO("io fail"))
	if !strings.Contains(buf.String(), "io fail") {
		t.Fatalf("got %s", buf.String())
	}
}

type errIO string

func (e errIO) Error() string { return string(e) }

func timeNow() time.Time {
	return time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
}

var _ = io.Discard
var _ = context.Background

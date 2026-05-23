package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

func newDMOpenCmd(title string, actor observability.Actor) convservice.OpenCommand {
	return convservice.OpenCommand{
		Kind:      conversation.ConversationKindDM,
		Name:      title,
		CreatedBy: conversation.IdentityRef(actor),
		Actor:     actor,
	}
}

func seedProjectP1(t *testing.T, app *App) {
	t.Helper()
	if _, err := app.ProjectSvc.Add(context.Background(), wfservice.AddCommand{
		ID:    workforce.ProjectID("p-1"),
		Name:  "Phase 3 Project",
		Actor: app.DefaultActor(),
	}); err != nil {
		t.Fatal(err)
	}
}

// openIssueViaCLI is a fixture that drives `issue open` and returns the
// issue_id (CLI / lazy-create origin).
func openIssueViaCLI(t *testing.T, app *App, extraArgs ...string) string {
	t.Helper()
	args := append([]string{"p-1", "hello", "--format=json"}, extraArgs...)
	cmd := findCmd(app.IssueCommands(), "open")
	out, _, code := runHandler(t, cmd, args)
	if code != ExitOK {
		t.Fatalf("open: code=%d out=%s", code, out)
	}
	var p map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &p); err != nil {
		t.Fatalf("not json: %s", out)
	}
	return p["issue_id"]
}

func TestCLI_IssueOpen_HappyAndJSON(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	cmd := findCmd(app.IssueCommands(), "open")
	out, _, code := runHandler(t, cmd, []string{"p-1", "hello", "world", "--format=json"})
	if code != ExitOK {
		t.Fatalf("open: code=%d", code)
	}
	var p map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &p); err != nil {
		t.Fatalf("not json: %s", out)
	}
	if p["issue_id"] == "" {
		t.Fatal("no issue_id")
	}
	if p["conversation_id"] != "" {
		t.Fatal("CLI path should not auto-create conv")
	}
}

func TestCLI_IssueOpen_HumanOutput(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	cmd := findCmd(app.IssueCommands(), "open")
	out, _, code := runHandler(t, cmd, []string{"p-1", "hello"})
	if code != ExitOK {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "opened issue") {
		t.Fatalf("expected human-readable: %s", out)
	}
}

func TestCLI_IssueOpen_UsageAndBadOrigin(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	cmd := findCmd(app.IssueCommands(), "open")
	cases := []struct {
		args []string
		want ExitCode
	}{
		{[]string{}, ExitUsage},
		{[]string{"p-1"}, ExitUsage},
		{[]string{"p-missing", "t"}, ExitNotFound},
		{[]string{"p-1", "t", "--origin=weird"}, ExitUsage},
	}
	for i, c := range cases {
		_, _, code := runHandler(t, cmd, c.args)
		if code != c.want {
			t.Errorf("case %d args=%v: code=%d want %d", i, c.args, code, c.want)
		}
	}
}

func TestCLI_IssueComment_NoConvBound(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app)
	cmd := findCmd(app.IssueCommands(), "comment")
	_, _, code := runHandler(t, cmd, []string{id, "--content=hi"})
	if code != ExitInvariantViolation {
		t.Fatalf("expected invariant violation, got %d", code)
	}
}

func TestCLI_IssueBindConversation_AutoThenComment(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app)
	bindCmd := findCmd(app.IssueCommands(), "bind-conversation")
	_, _, code := runHandler(t, bindCmd, []string{id, "--auto"})
	if code != ExitOK {
		t.Fatalf("bind: %d", code)
	}
	commentCmd := findCmd(app.IssueCommands(), "comment")
	out, _, code := runHandler(t, commentCmd, []string{id, "--content=hi", "--format=json"})
	if code != ExitOK {
		t.Fatalf("comment: %d", code)
	}
	var p map[string]string
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &p)
	if p["message_id"] == "" {
		t.Fatal("no message_id")
	}
}

func TestCLI_IssueBindConversation_FlagMutexAndUsage(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	cmd := findCmd(app.IssueCommands(), "bind-conversation")
	cases := []struct {
		args []string
		want ExitCode
	}{
		{[]string{}, ExitUsage},
		{[]string{"X"}, ExitUsage},
		{[]string{"X", "--auto", "--to=Y"}, ExitUsage},
	}
	for i, c := range cases {
		_, _, code := runHandler(t, cmd, c.args)
		if code != c.want {
			t.Errorf("case %d: code=%d want %d", i, code, c.want)
		}
	}
}

func TestCLI_IssueConclude_ClosedNoAction(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app)
	cmd := findCmd(app.IssueCommands(), "conclude")
	out, _, code := runHandler(t, cmd, []string{id, "--resolution=closed_no_action", "--summary=skip", "--format=json"})
	if code != ExitOK {
		t.Fatalf("conclude: %d out=%s", code, out)
	}
	var p map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &p)
	if p["resolution"] != "closed_no_action" {
		t.Fatalf("payload: %+v", p)
	}
}

func TestCLI_IssueConclude_ClosedWithTasks_Inline(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app, "--origin=web_console")
	inline := `[{"local_id":"a","title":"T1"},{"local_id":"b","title":"T2","depends_on":["a"]}]`
	cmd := findCmd(app.IssueCommands(), "conclude")
	out, _, code := runHandler(t, cmd,
		[]string{id, "--resolution=closed_with_tasks", "--summary=go",
			"--spawn-tasks=" + inline, "--format=json"})
	if code != ExitOK {
		t.Fatalf("conclude: %d out=%s", code, out)
	}
	var p struct {
		TaskIDs []string `json:"task_ids"`
	}
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &p)
	if len(p.TaskIDs) != 2 {
		t.Fatalf("expected 2 tasks: %v", p.TaskIDs)
	}
}

func TestCLI_IssueConclude_ClosedWithTasks_File(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app)
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte(`[{"local_id":"a","title":"X","priority":"high"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := findCmd(app.IssueCommands(), "conclude")
	_, _, code := runHandler(t, cmd,
		[]string{id, "--resolution=closed_with_tasks", "--summary=g",
			"--spawn-tasks=@" + path})
	if code != ExitOK {
		t.Fatalf("conclude: %d", code)
	}
}

func TestCLI_IssueConclude_FlagValidation(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app)
	cmd := findCmd(app.IssueCommands(), "conclude")
	cases := []struct {
		args []string
		want ExitCode
	}{
		{[]string{}, ExitUsage},
		{[]string{id}, ExitUsage},
		{[]string{id, "--resolution=closed_no_action"}, ExitUsage},
		{[]string{id, "--resolution=bogus", "--summary=s"}, ExitUsage},
		{[]string{id, "--resolution=closed_with_tasks", "--summary=s"}, ExitUsage},
		{[]string{id, "--resolution=closed_no_action", "--summary=s",
			"--spawn-tasks=[{\"local_id\":\"a\",\"title\":\"x\"}]"}, ExitUsage},
		{[]string{id, "--resolution=closed_with_tasks", "--summary=s",
			"--spawn-tasks=not-json"}, ExitUsage},
		{[]string{id, "--resolution=closed_with_tasks", "--summary=s",
			"--spawn-tasks=@/no/such/file.json"}, ExitUsage},
		{[]string{id, "--resolution=closed_with_tasks", "--summary=s",
			"--spawn-tasks=[{\"local_id\":\"a\",\"title\":\"x\",\"priority\":\"weird\"}]"}, ExitUsage},
	}
	for i, c := range cases {
		_, _, code := runHandler(t, cmd, c.args)
		if code != c.want {
			t.Errorf("case %d args=%v: code=%d want %d", i, c.args, code, c.want)
		}
	}
}

func TestCLI_IssueWithdraw_HappyAndErrs(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app)
	cmd := findCmd(app.IssueCommands(), "withdraw")
	if _, _, code := runHandler(t, cmd, []string{id, "--reason=dup", "--message=of #5"}); code != ExitOK {
		t.Fatalf("withdraw happy: %d", code)
	}
	cases := []struct {
		args []string
		want ExitCode
	}{
		{[]string{}, ExitUsage},
		{[]string{"X", "--message=x"}, ExitUsage}, // no reason
		{[]string{"X", "--reason=x"}, ExitUsage},  // no message
	}
	for i, c := range cases {
		_, _, code := runHandler(t, cmd, c.args)
		if code != c.want {
			t.Errorf("case %d args=%v: code=%d want %d", i, c.args, code, c.want)
		}
	}
}

func TestCLI_IssueLinkConversation(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	id := openIssueViaCLI(t, app)
	// stand-alone DM conv to link — use MessageWriter facade to get a proper RFC3339Nano row
	openRes, err := app.MessageWriter.OpenConversation(context.Background(),
		newDMOpenCmd("link-target", app.DefaultActor()))
	if err != nil {
		t.Fatal(err)
	}
	cmd := findCmd(app.IssueCommands(), "link-conversation")
	if _, _, code := runHandler(t, cmd, []string{id, "--conversation=" + string(openRes.ConversationID), "--format=json"}); code != ExitOK {
		t.Fatalf("link: %d", code)
	}
	cases := []struct {
		args []string
		want ExitCode
	}{
		{[]string{}, ExitUsage},
		{[]string{id}, ExitUsage},
		{[]string{"--conversation=X"}, ExitUsage},
		{[]string{id, "--conversation=ghost"}, ExitNotFound},
	}
	for i, c := range cases {
		_, _, code := runHandler(t, cmd, c.args)
		if code != c.want {
			t.Errorf("case %d args=%v: code=%d want %d", i, c.args, code, c.want)
		}
	}
}

func TestCLI_OpenIssueAgentVerb(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	cmd := app.OpenIssueCommand()
	// happy
	out, _, code := runHandler(t, cmd, []string{"p-1", "from-agent", "--opened-by=agent:sess-1", "--format=json"})
	if code != ExitOK {
		t.Fatalf("open-issue: %d", code)
	}
	var p map[string]string
	_ = json.Unmarshal([]byte(strings.TrimSpace(out)), &p)
	if p["issue_id"] == "" {
		t.Fatal("no issue_id")
	}
	// fewer than 2 args
	_, _, code = runHandler(t, cmd, []string{"p-1"})
	if code != ExitUsage {
		t.Fatalf("expected usage, got %d", code)
	}
	// bad actor
	_, _, code = runHandler(t, cmd, []string{"p-1", "x", "--opened-by=BAD"})
	if code != ExitUsage {
		t.Fatalf("expected usage from bad actor, got %d", code)
	}
}

func TestCLI_OpenIssueAgentVerb_DefaultOpener(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	cmd := app.OpenIssueCommand()
	// default opener becomes agent:<defaultUser> — still must be valid prefix shape
	out, _, code := runHandler(t, cmd, []string{"p-1", "from-agent"})
	if code != ExitOK {
		t.Fatalf("default opener: %d out=%s", code, out)
	}
}

func TestCLI_IssueErrorMapping(t *testing.T) {
	// Verify the new sentinel mappings landed; exercise issue_not_found etc.
	app := newTestApp(t)
	seedProjectP1(t, app)
	// conclude on ghost id → issue_not_found → ExitNotFound
	cmd := findCmd(app.IssueCommands(), "conclude")
	_, _, code := runHandler(t, cmd, []string{"GHOST",
		"--resolution=closed_no_action", "--summary=s"})
	if code != ExitNotFound {
		t.Fatalf("expected not_found, got %d", code)
	}
	// already concluded
	id := openIssueViaCLI(t, app)
	_, _, code = runHandler(t, cmd, []string{id, "--resolution=closed_no_action", "--summary=s"})
	if code != ExitOK {
		t.Fatal("first conclude")
	}
	_, _, code = runHandler(t, cmd, []string{id, "--resolution=closed_no_action", "--summary=s"})
	if code != ExitInvalidTransition {
		t.Fatalf("expected invalid transition, got %d", code)
	}
}

func TestCLI_IssueErrorMapping_AllSentinels(t *testing.T) {
	// Direct unit check that MapDomainError handles each new sentinel.
	tests := []struct {
		err  error
		want string
	}{
		{discussion.ErrIssueNotFound, "issue_not_found"},
		{discussion.ErrIssueAlreadyExists, "issue_already_exists"},
		{discussion.ErrIssueInvalidTransition, "issue_invalid_transition"},
		{discussion.ErrIssueVersionConflict, "issue_version_conflict"},
		{discussion.ErrIssueAlreadyConcluded, "issue_already_concluded"},
		{discussion.ErrIssueWithdrawn, "issue_withdrawn"},
		{discussion.ErrIssueNoConversationBound, "issue_no_conversation_bound"},
		{discussion.ErrInvalidOrigin, "issue_invalid_origin"},
		{discussion.ErrResolutionInvalid, "issue_invalid_resolution"},
	}
	for _, tc := range tests {
		reason, _, ok := MapDomainError(tc.err)
		if !ok || reason != tc.want {
			t.Errorf("want %s, got reason=%s ok=%v", tc.want, reason, ok)
		}
	}
	// unknown error not mapped
	if _, _, ok := MapDomainError(errors.New("random")); ok {
		t.Fatal("random err should not match")
	}
}

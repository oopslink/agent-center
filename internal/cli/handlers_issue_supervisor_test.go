package cli

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition"
)

// TestADR0014_IssueOpen_SupervisorWritesDecision verifies the same-tx
// invariant for `issue open` with a supervisor caller.
func TestADR0014_IssueOpen_SupervisorWritesDecision(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-IOPEN")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")

	cmd := findCmd(app.IssueCommands(), "open")
	out, _, code := runHandler(t, cmd, []string{
		"p-1", "supervised opening",
		"--rationale=worker reports stuck dependency",
		"--format=json",
	})
	if code != ExitOK {
		t.Fatalf("open: code=%d out=%s", code, out)
	}
	rows, err := app.DecisionRepo.FindByInvocationID(context.Background(), "INV-IOPEN")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("decisions: got %d, want 1", len(rows))
	}
	if rows[0].Kind() != cognition.DecisionOpenIssue {
		t.Errorf("kind: got %s", rows[0].Kind())
	}
}

// TestADR0014_IssueOpen_SupervisorMissingRationaleRejected verifies the
// supervisor caller is rejected with usage error when --rationale is
// missing.
func TestADR0014_IssueOpen_SupervisorMissingRationaleRejected(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-NOREASON")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")

	cmd := findCmd(app.IssueCommands(), "open")
	_, errw, code := runHandler(t, cmd, []string{
		"p-1", "no rationale",
		"--format=json",
	})
	if code != ExitUsage {
		t.Errorf("expected ExitUsage, got %d (errw=%s)", code, errw)
	}
	if !strings.Contains(errw, "rationale_required") {
		t.Errorf("expected rationale_required diagnostic: %s", errw)
	}
}

// TestADR0014_IssueConclude_WithdrawnVariantUsesCloseIssueKind verifies
// the conclude→withdrawn path picks DecisionCloseIssue.
func TestADR0014_IssueConclude_WithdrawnVariantUsesCloseIssueKind(t *testing.T) {
	app := newTestApp(t)
	seedProjectP1(t, app)
	// open as user first
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	issueID := openIssueViaCLI(t, app)

	// then conclude with resolution=withdrawn as supervisor
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-WITHDRAW")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")

	cmd := findCmd(app.IssueCommands(), "conclude")
	_, errw, code := runHandler(t, cmd, []string{
		issueID,
		"--resolution=withdrawn",
		"--summary=no longer needed",
		"--rationale=rules changed",
		"--format=json",
	})
	if code != ExitOK {
		t.Fatalf("conclude code=%d errw=%s", code, errw)
	}
	rows, _ := app.DecisionRepo.FindByInvocationID(context.Background(), "INV-WITHDRAW")
	if len(rows) != 1 {
		t.Fatalf("decisions: %d", len(rows))
	}
	if rows[0].Kind() != cognition.DecisionCloseIssue {
		t.Errorf("kind: got %s, want close_issue", rows[0].Kind())
	}
}

// TestADR0014_ConversationAddMessage_SupervisorRecord exercises the
// supervisor-driven path for conversation add-message.
func TestADR0014_ConversationAddMessage_SupervisorRecord(t *testing.T) {
	app := newTestApp(t)
	// Open a conversation as user.
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	openCmd := findCmd(app.ConversationCommands(), "open")
	out, _, code := runHandler(t, openCmd, []string{
		"--kind=dm", "--name=test", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("open conv: %d", code)
	}
	var open struct {
		ConversationID string `json:"conversation_id"`
	}
	_ = json.Unmarshal([]byte(out), &open)

	// add-message as supervisor.
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-ADDMSG")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	addCmd := findCmd(app.ConversationCommands(), "add-message")
	_, errw, code := runHandler(t, addCmd, []string{
		open.ConversationID,
		"--content=auto-reply",
		"--rationale=supervisor seeded the conversation",
		"--format=json",
	})
	if code != ExitOK {
		t.Fatalf("add-message code=%d errw=%s", code, errw)
	}
	rows, _ := app.DecisionRepo.FindByInvocationID(context.Background(), "INV-ADDMSG")
	if len(rows) != 1 {
		t.Fatalf("decisions: %d", len(rows))
	}
	if rows[0].Kind() != cognition.DecisionConversationMessage {
		t.Errorf("kind: got %s", rows[0].Kind())
	}
}

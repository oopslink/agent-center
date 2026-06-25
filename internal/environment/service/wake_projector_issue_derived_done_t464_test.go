package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/outbox"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// saveIssueConv persists an issue-owned conversation (owner_ref pm://issues/{issueID}).
func (f *wakeFixture) saveIssueConv(t *testing.T, convID, issueID string) {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID(convID),
		Kind:           conversation.ConversationKindIssue,
		OwnerRef:       conversation.NewIssueOwnerRef(issueID),
		Name:           "issue " + issueID,
		OrganizationID: "org-1",
		CreatedBy:      conversation.IdentityRef("user:alice"),
		OpenedAt:       f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("new issue conv: %v", err)
	}
	if err := f.convs.Save(f.ctx, c); err != nil {
		t.Fatalf("save issue conv: %v", err)
	}
}

// issueDerivedDoneEvent builds an EvtIssueDerivedTasksDone outbox event (T464).
func issueDerivedDoneEvent(id, issueID, ownerIdentity string, total, completed, discarded int) outbox.Event {
	pl, err := json.Marshal(map[string]any{
		"issue_id":       issueID,
		"project_id":     "proj-1",
		"owner_ref":      "pm://issues/" + issueID,
		"owner_identity": ownerIdentity,
		"total":          total,
		"completed":      completed,
		"discarded":      discarded,
	})
	if err != nil {
		panic(err)
	}
	return outbox.Event{
		ID:        id,
		EventType: pmservice.EvtIssueDerivedTasksDone,
		Payload:   string(pl),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

// Headline: an AGENT owner gets the visible @owner message into the issue conversation
// AND an agent.converse wake to review + close.
func TestWakeProjector_IssueDerivedDone_AgentOwner_MessageAndWake(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")
	f.saveIssueConv(t, "issue-conv-1", "I1")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := issueDerivedDoneEvent("EVI1", "I1", "agent:BOT", 2, 2, 0)
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}

	if len(sysNotes) != 0 {
		t.Fatalf("issue-derived-done must use ordinary system message, not systemNotify: %q", sysNotes)
	}
	if len(sysMessages) != 1 || !strings.Contains(sysMessages[0], "issue-conv-1: ") ||
		!strings.Contains(sysMessages[0], "@A BOT") || !strings.Contains(sysMessages[0], "complete.") ||
		!strings.Contains(sysMessages[0], "close_issue") {
		t.Fatalf("owner message wrong: %q", sysMessages)
	}
	cmds := f.commandsFor(t, "W7")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse, got %d (%v)", len(cmds), cmds)
	}
	if cmds[0].IdempotencyKey() != "agent.converse:issue-conv-1:issue-derived-done:EVI1:BOT" {
		t.Fatalf("idempotency_key = %q", cmds[0].IdempotencyKey())
	}
}

// A HUMAN owner gets the @owner message only (no converse — humans are notified via the
// conversation @mention / UI unread, not woken).
func TestWakeProjector_IssueDerivedDone_HumanOwner_MessageOnly(t *testing.T) {
	f := newWakeFixture(t)
	f.saveIssueConv(t, "issue-conv-1", "I1")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(map[string]string{"user:alice": "Alice"}, &sysNotes, &sysMessages)

	e := issueDerivedDoneEvent("EVI1", "I1", "user:alice", 3, 1, 2)
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if len(sysNotes) != 0 {
		t.Fatalf("issue-derived-done must use ordinary system message, not systemNotify: %q", sysNotes)
	}
	if len(sysMessages) != 1 || !strings.Contains(sysMessages[0], "@Alice") ||
		!strings.Contains(sysMessages[0], "concluded (1 completed, 2 discarded)") {
		t.Fatalf("human-owner message wrong: %q", sysMessages)
	}
	// No agent.converse anywhere (human owner → nothing to wake).
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 0 {
		t.Fatalf("human owner must enqueue no converse, got %d", len(cmds))
	}
}

// Replaying the SAME event posts the message + wakes exactly once.
func TestWakeProjector_IssueDerivedDone_ReplayOnce(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")
	f.saveIssueConv(t, "issue-conv-1", "I1")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := issueDerivedDoneEvent("EVI1", "I1", "agent:BOT", 1, 1, 0)
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 1: %v", err)
	}
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project 2: %v", err)
	}
	if len(sysNotes) != 0 {
		t.Fatalf("issue-derived-done must use ordinary system message, not systemNotify: %q", sysNotes)
	}
	if len(sysMessages) != 1 {
		t.Fatalf("replay must not duplicate the message: got %d", len(sysMessages))
	}
	if cmds := f.commandsFor(t, "W7"); len(cmds) != 1 {
		t.Fatalf("replay must not duplicate the converse: got %d", len(cmds))
	}
}

// No bound issue conversation → drain the event (no message, no converse, no error).
func TestWakeProjector_IssueDerivedDone_NoConversation_NoOp(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "BOT", "W7")
	var sysNotes []string
	var sysMessages []string
	p := f.projWithSystemMessages(nil, &sysNotes, &sysMessages)

	e := issueDerivedDoneEvent("EVI1", "I-missing", "agent:BOT", 1, 1, 0)
	if err := p.Project(f.ctx, e); err != nil {
		t.Fatalf("Project must not fail with no conversation: %v", err)
	}
	if len(sysNotes) != 0 || len(sysMessages) != 0 || len(f.commandsFor(t, "W7")) != 0 {
		t.Fatalf("no conversation → no message, no converse")
	}
}

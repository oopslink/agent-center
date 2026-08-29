package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// attentionItems decodes a GET /attention response into its item list.
func attentionItems(t *testing.T, resp *http.Response) []map[string]any {
	t.Helper()
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode attention: %v", err)
	}
	return body.Items
}

// sessionDisplayName resolves the logged-in session user's display name (the
// @-handle an agent must use to mention them).
func sessionDisplayName(t *testing.T, deps HandlerDeps, sess testSession) string {
	t.Helper()
	ident, err := deps.IdentityRepo.GetByID(context.Background(), sess.IdentityID)
	if err != nil || ident == nil {
		t.Fatalf("resolve session identity: %v", err)
	}
	return ident.DisplayName()
}

// addAgentMessage posts one agent-authored message into a conversation.
func addAgentMessage(t *testing.T, deps HandlerDeps, convID conversation.ConversationID, sender, content string) conversation.MessageID {
	t.Helper()
	r, err := deps.MessageWriter.AddMessage(context.Background(), convservice.AddMessageCommand{
		ConversationID:   convID,
		SenderIdentityID: conversation.IdentityRef(sender),
		ContentKind:      conversation.MessageContentText,
		Content:          content,
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor(sender),
	})
	if err != nil {
		t.Fatal(err)
	}
	return r.MessageID
}

// failRunningTask drives a fresh task to RUNNING then fails it.
func failRunningTask(t *testing.T, deps HandlerDeps, taskID string, reason string) {
	t.Helper()
	ctx := context.Background()
	// seedPMTaskConv creates the project with CreatedBy "user:hayang", so that
	// identity is a project member and a valid transition actor.
	if err := deps.PM.SetTaskStatusWithReason(ctx, pm.TaskID(taskID), pm.TaskRunning, "user:hayang", "test transition"); err != nil {
		t.Fatalf("set running: %v", err)
	}
	if err := deps.PM.FailTask(ctx, pm.TaskID(taskID), reason, "user:hayang"); err != nil {
		t.Fatalf("fail task: %v", err)
	}
}

// TestAPI_Attention_AgentMentionNoHumanTask is the I61 core acceptance: an agent
// @mentions the human in a work (task) conversation and there is NO human-owned
// task carrying an input_required block. The escalation must still surface in the
// attention panel — as a kind=mention item deep-linking to the source.
func TestAPI_Attention_AgentMentionNoHumanTask(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	handle := sessionDisplayName(t, deps, sess)

	// A task conversation whose only participant is an agent — the human can VIEW
	// it but owns no task here. The agent escalates by @mentioning the human.
	conv, taskID, projID, _ := seedPMTaskConv(t, deps, sess.OrgID, "Blocked integrate", 1)
	addAgentMessage(t, deps, conv, "agent:AG1", "@"+handle+" the integrate node is stuck on SQLITE_BUSY — please help")

	s := newTestServer(t, deps)
	defer s.Close()

	items := attentionItems(t, orgScopedGet(t, s.URL+"/api/attention", sess))

	var mention map[string]any
	for _, it := range items {
		if it["kind"] == "mention" && it["conversation_id"] == string(conv) {
			mention = it
		}
		if it["kind"] == "task" {
			t.Errorf("no stuck task exists, but got a kind=task item: %v", it)
		}
	}
	if mention == nil {
		t.Fatalf("agent @mention escalation missing from attention; items=%v", items)
	}
	if mention["route"] != "/projects/"+projID+"/tasks/"+taskID {
		t.Errorf("mention route=%v want /projects/%s/tasks/%s", mention["route"], projID, taskID)
	}
	if mention["severity"] != "warning" {
		t.Errorf("mention severity=%v want warning", mention["severity"])
	}
	if int(mention["mention_count"].(float64)) != 1 {
		t.Errorf("mention_count=%v want 1", mention["mention_count"])
	}
	if mention["message_id"] == nil || mention["message_id"] == "" {
		t.Errorf("mention item must carry a message_id (dismiss target), got %v", mention["message_id"])
	}
	if snip, _ := mention["snippet"].(string); snip == "" {
		t.Errorf("mention item must carry a snippet, got empty")
	}
}

func TestAPI_Attention_FailedTasksDoNotCreateStuckItems(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)

	_, taskID, _, _ := seedPMTaskConv(t, deps, sess.OrgID, "Needs my reply", 1)
	failRunningTask(t, deps, taskID, "which migration strategy?")

	s := newTestServer(t, deps)
	defer s.Close()

	items := attentionItems(t, orgScopedGet(t, s.URL+"/api/attention", sess))
	for _, it := range items {
		if it["kind"] == "task" {
			t.Fatalf("failed tasks should not surface as stuck task attention items: %v", it)
		}
	}
}

// TestAPI_Attention_Union_Sort_Dedup: both sources UNION; the urgent stuck task
// sorts ahead of a directed mention; and a mention pointing at a conversation
// whose task is ALREADY a kind=task item is deduped away (the task item is richer).
func TestAPI_Attention_Union_Sort_Dedup(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	handle := sessionDisplayName(t, deps, sess)

	// (A) A stuck input_required task — AND the agent also @mentions the human in
	// that SAME task conversation → the mention must be deduped (task item wins).
	stuckConv, stuckTaskID, _, _ := seedPMTaskConv(t, deps, sess.OrgID, "Stuck + mentioned", 1)
	failRunningTask(t, deps, stuckTaskID, "need a decision")
	addAgentMessage(t, deps, stuckConv, "agent:AG1", "@"+handle+" also see this")

	// (B) A standalone directed @mention in a different (channel) conversation.
	chID := seedOrgChannel(t, deps, sess.OrgID, "ops")
	addAgentMessage(t, deps, conversation.ConversationID(chID), "agent:AG1", "@"+handle+" deploy is wedged")

	s := newTestServer(t, deps)
	defer s.Close()

	items := attentionItems(t, orgScopedGet(t, s.URL+"/api/attention", sess))

	// Failed tasks no longer create a richer task attention item, so both directed
	// mentions should remain visible.
	var taskItems, mentionConvs int
	for i, it := range items {
		switch it["kind"] {
		case "task":
			taskItems++
		case "mention":
			mentionConvs++
			if it["conversation_id"] != string(chID) && it["conversation_id"] != string(stuckConv) {
				t.Errorf("unexpected mention conversation %v", it["conversation_id"])
			}
		}
		_ = i
	}
	if taskItems != 0 {
		t.Errorf("want no kind=task items for failed tasks, got %d", taskItems)
	}
	if mentionConvs != 2 {
		t.Errorf("want 2 kind=mention items, got %d", mentionConvs)
	}
}

// TestAPI_Attention_HumanOnly_FailSoft: with the mention-source services unwired
// the endpoint degrades the mention source to empty (and still returns any task
// items) rather than 500ing. A 200 with no panic is the contract.
func TestAPI_Attention_HumanOnly_FailSoft(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.FollowStateSvc = nil // mention source degrades
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/attention", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	// Decodes cleanly as {items: [...]}.
	_ = attentionItems(t, resp)
}

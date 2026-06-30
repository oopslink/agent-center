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

// blockRunningTask drives a fresh task to RUNNING then records a stuck-reason
// annotation on it (ADR-0046: blocked is an annotation on a running task).
func blockRunningTask(t *testing.T, deps HandlerDeps, taskID string, reason string, rt pm.BlockReasonType) {
	t.Helper()
	ctx := context.Background()
	// seedPMTaskConv creates the project with CreatedBy "user:hayang", so that
	// identity is a project member and a valid transition actor.
	if err := deps.PM.SetTaskStatus(ctx, pm.TaskID(taskID), pm.TaskRunning, "user:hayang"); err != nil {
		t.Fatalf("set running: %v", err)
	}
	if err := deps.PM.BlockTask(ctx, pm.TaskID(taskID), reason, rt, "user:hayang"); err != nil {
		t.Fatalf("block task: %v", err)
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

// TestAPI_Attention_StuckTaskSource_NoRegression: the pre-existing panel source
// (actionable stuck tasks) is preserved — an input_required block ranks urgent,
// an obstacle block ranks warning. (input_required/assigned 来源不回归)
func TestAPI_Attention_StuckTaskSource_NoRegression(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)

	_, irTaskID, irProj, _ := seedPMTaskConv(t, deps, sess.OrgID, "Needs my reply", 1)
	blockRunningTask(t, deps, irTaskID, "which migration strategy?", pm.BlockReasonInputRequired)

	_, obsTaskID, _, _ := seedPMTaskConv(t, deps, sess.OrgID, "Branch not pushed", 1)
	blockRunningTask(t, deps, obsTaskID, "dev branch missing on origin", pm.BlockReasonObstacle)

	s := newTestServer(t, deps)
	defer s.Close()

	items := attentionItems(t, orgScopedGet(t, s.URL+"/api/attention", sess))
	byTask := map[string]map[string]any{}
	for _, it := range items {
		if it["kind"] == "task" {
			byTask[it["task_id"].(string)] = it
		}
	}
	ir := byTask[irTaskID]
	if ir == nil {
		t.Fatalf("input_required stuck task missing; items=%v", items)
	}
	if ir["severity"] != "urgent" || ir["reason_type"] != "input_required" {
		t.Errorf("input_required item severity/reason=%v/%v want urgent/input_required", ir["severity"], ir["reason_type"])
	}
	if ir["route"] != "/projects/"+irProj+"/tasks/"+irTaskID {
		t.Errorf("task route=%v", ir["route"])
	}
	if ir["snippet"] != "which migration strategy?" {
		t.Errorf("task snippet=%v want the block reason", ir["snippet"])
	}
	obs := byTask[obsTaskID]
	if obs == nil {
		t.Fatalf("obstacle stuck task missing; items=%v", items)
	}
	if obs["severity"] != "warning" || obs["reason_type"] != "obstacle" {
		t.Errorf("obstacle item severity/reason=%v/%v want warning/obstacle", obs["severity"], obs["reason_type"])
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
	blockRunningTask(t, deps, stuckTaskID, "need a decision", pm.BlockReasonInputRequired)
	addAgentMessage(t, deps, stuckConv, "agent:AG1", "@"+handle+" also see this")

	// (B) A standalone directed @mention in a different (channel) conversation.
	chID := seedOrgChannel(t, deps, sess.OrgID, "ops")
	addAgentMessage(t, deps, conversation.ConversationID(chID), "agent:AG1", "@"+handle+" deploy is wedged")

	s := newTestServer(t, deps)
	defer s.Close()

	items := attentionItems(t, orgScopedGet(t, s.URL+"/api/attention", sess))

	// Dedup: the stuck task's conversation must NOT also appear as a kind=mention.
	var taskItems, mentionConvs int
	firstTaskIdx, firstMentionIdx := -1, -1
	for i, it := range items {
		switch it["kind"] {
		case "task":
			taskItems++
			if firstTaskIdx == -1 {
				firstTaskIdx = i
			}
			if it["task_id"] != stuckTaskID {
				t.Errorf("unexpected task item %v", it)
			}
		case "mention":
			mentionConvs++
			if firstMentionIdx == -1 {
				firstMentionIdx = i
			}
			if it["conversation_id"] == string(stuckConv) {
				t.Errorf("mention on the stuck task's own conversation should be deduped, got %v", it)
			}
			if it["conversation_id"] != string(chID) {
				t.Errorf("unexpected mention conversation %v want channel %s", it["conversation_id"], chID)
			}
		}
	}
	if taskItems != 1 {
		t.Errorf("want exactly 1 kind=task item (the stuck task), got %d", taskItems)
	}
	if mentionConvs != 1 {
		t.Errorf("want exactly 1 kind=mention item (the channel, dedup dropped the task-conv mention), got %d", mentionConvs)
	}
	// Sort: urgent task ahead of the warning mention.
	if firstTaskIdx != -1 && firstMentionIdx != -1 && firstTaskIdx > firstMentionIdx {
		t.Errorf("urgent stuck task (idx %d) must sort before the directed mention (idx %d)", firstTaskIdx, firstMentionIdx)
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

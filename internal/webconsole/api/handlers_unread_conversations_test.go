package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// seedPMTaskConv creates a PM project + task and a task-kind conversation pinned
// to it (owner_ref pm://tasks/{id}) with n agent-authored messages. The sole
// participant is an agent — so the session human can VIEW it but is NOT a
// participant (mirrors the real task ParticipantProjector). Returns the conv id,
// task id, project id, and message ids.
func seedPMTaskConv(t *testing.T, deps HandlerDeps, orgID, title string, n int) (
	conversation.ConversationID, string, string, []conversation.MessageID,
) {
	t.Helper()
	ctx := context.Background()
	projID, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: orgID, Name: title + " proj", CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{
		ProjectID: projID, Title: title, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:             conversation.ConversationID("conv-" + string(taskID)),
		Kind:           conversation.ConversationKindTask,
		OwnerRef:       conversation.NewTaskOwnerRef(string(taskID)),
		OrganizationID: orgID,
		CreatedBy:      conversation.IdentityRef("system"),
		OpenedAt:       time.Now().UTC(),
		Participants: []conversation.ParticipantElement{
			{IdentityID: "agent:AG1", Role: "member", JoinedAt: "t", JoinedBy: conversation.IdentityRef("system")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.ConvRepo.Save(ctx, conv); err != nil {
		t.Fatal(err)
	}
	ids := make([]conversation.MessageID, n)
	for i := 0; i < n; i++ {
		r, aerr := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   conv.ID(),
			SenderIdentityID: "agent:AG1",
			ContentKind:      conversation.MessageContentText,
			Content:          "task update",
			Direction:        conversation.DirectionInbound,
			Actor:            observability.Actor("agent:AG1"),
		})
		if aerr != nil {
			t.Fatal(aerr)
		}
		ids[i] = r.MessageID
	}
	return conv.ID(), string(taskID), string(projID), ids
}

// digestByConvID indexes a GET /unread-conversations response by conversation_id.
func digestByConvID(t *testing.T, resp *http.Response) map[string]map[string]any {
	t.Helper()
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode digest: %v", err)
	}
	out := make(map[string]map[string]any, len(arr))
	for _, row := range arr {
		out[row["conversation_id"].(string)] = row
	}
	return out
}

func TestAPI_UnreadConversations_Digest(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	self := conversation.IdentityRef("user:" + sess.IdentityID)

	// (1) Channel with unread (default-follow, session user has no read-state) →
	// included as a channel row.
	chID, _ := seedConvAndMessages(t, deps, sess.OrgID, "research-room", 2)

	// (2) A task conversation the session user has NOT engaged with (no read-state
	// row, not a participant, no @mention) → EXCLUDED by the engaged gate.
	untouchedConv, _, _, _ := seedPMTaskConv(t, deps, sess.OrgID, "Untouched task", 2)

	// (3) A task conversation the session user HAS engaged with (a read-state row
	// from opening it once) → INCLUDED, carrying project_id + a navigable route.
	engConv, engTaskID, engProjID, engIDs := seedPMTaskConv(t, deps, sess.OrgID, "My churn fix", 2)
	// Open it: mark seen up to the first message, leaving 1 unread.
	if _, err := deps.ReadStateSvc.MarkSeen(context.Background(), convservice.MarkSeenCommand{
		UserID: self, ConversationID: engConv, LastSeenMessageID: engIDs[0], Actor: observability.Actor(self),
	}); err != nil {
		t.Fatal(err)
	}

	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/unread-conversations", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	rows := digestByConvID(t, resp)

	// Channel row.
	ch := rows[string(chID)]
	if ch == nil {
		t.Fatalf("channel %s missing from digest", chID)
	}
	if ch["source_type"] != "channel" {
		t.Errorf("channel source_type=%v want channel", ch["source_type"])
	}
	if ch["route"] != "/channels/"+string(chID) {
		t.Errorf("channel route=%v want /channels/%s", ch["route"], chID)
	}
	if int(ch["unread_count"].(float64)) != 2 {
		t.Errorf("channel unread_count=%v want 2", ch["unread_count"])
	}
	if ch["title"] != "research-room" {
		t.Errorf("channel title=%v want research-room", ch["title"])
	}

	// Engaged task row carries project_id + the project-scoped route + title.
	eng := rows[string(engConv)]
	if eng == nil {
		t.Fatalf("engaged task conv %s missing from digest", engConv)
	}
	if eng["source_type"] != "task" {
		t.Errorf("task source_type=%v want task", eng["source_type"])
	}
	if eng["source_id"] != engTaskID {
		t.Errorf("task source_id=%v want %s", eng["source_id"], engTaskID)
	}
	if eng["project_id"] != engProjID {
		t.Errorf("task project_id=%v want %s", eng["project_id"], engProjID)
	}
	if eng["route"] != "/projects/"+engProjID+"/tasks/"+engTaskID {
		t.Errorf("task route=%v want /projects/%s/tasks/%s", eng["route"], engProjID, engTaskID)
	}
	if eng["title"] != "My churn fix" {
		t.Errorf("task title=%v want 'My churn fix'", eng["title"])
	}
	if int(eng["unread_count"].(float64)) != 1 {
		t.Errorf("task unread_count=%v want 1 (marked seen to first of two)", eng["unread_count"])
	}

	// The non-engaged task conversation must NOT appear (the firehose gate).
	if _, present := rows[string(untouchedConv)]; present {
		t.Errorf("non-engaged task conv %s should be excluded from digest", untouchedConv)
	}
}

func TestAPI_UnreadConversations_HumanOnly_EmptyForUnwired(t *testing.T) {
	// Services unwired (no follow/read-state) → fail-soft empty list, not 500.
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps.FollowStateSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/unread-conversations", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(arr) != 0 {
		t.Fatalf("want empty digest, got %d rows", len(arr))
	}
}

func TestPMRoute(t *testing.T) {
	cases := []struct {
		kind conversation.ConversationKind
		want string
	}{
		{conversation.ConversationKindTask, "/projects/P1/tasks/T1"},
		{conversation.ConversationKindIssue, "/projects/P1/issues/T1"},
		{conversation.ConversationKindPlan, "/projects/P1/plans/T1"},
		{conversation.ConversationKindChannel, ""},
	}
	for _, c := range cases {
		if got := pmRoute(c.kind, "P1", "T1"); got != c.want {
			t.Errorf("pmRoute(%s)=%q want %q", c.kind, got, c.want)
		}
	}
}

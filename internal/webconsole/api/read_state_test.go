package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// seedConvAndMessages opens a channel (in orgID) and posts n messages,
// returning the conv id + message ids.
func seedConvAndMessages(t *testing.T, deps HandlerDeps, orgID, name string, n int) (conversation.ConversationID, []conversation.MessageID) {
	t.Helper()
	ctx := context.Background()
	res, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: name, OrganizationID: orgID, CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]conversation.MessageID, n)
	for i := 0; i < n; i++ {
		r, err := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
			ConversationID:   res.ConversationID,
			SenderIdentityID: "user:hayang",
			ContentKind:      conversation.MessageContentText,
			Content:          "m",
			Direction:        conversation.DirectionInbound,
			Actor:            observability.Actor("user:hayang"),
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = r.MessageID
	}
	return res.ConversationID, ids
}

func TestAPI_Unread_HappyPath(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, ids := seedConvAndMessages(t, deps, sess.OrgID, "unread-happy", 3)
	// Mark seen up to msg 0.
	if _, err := deps.ReadStateSvc.MarkSeen(context.Background(), convservice.MarkSeenCommand{
		UserID: "user:hayang", ConversationID: convID,
		LastSeenMessageID: ids[0], Actor: observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations/"+string(convID)+"/unread", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["last_seen_message_id"] != string(ids[0]) {
		t.Fatalf("last_seen=%v want %s", body["last_seen_message_id"], ids[0])
	}
	if int(body["unread_count"].(float64)) != 2 {
		t.Fatalf("unread_count=%v want 2", body["unread_count"])
	}
}

func TestAPI_Unread_AbsentRow(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "unread-empty", 4)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations/"+string(convID)+"/unread", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["last_seen_message_id"] != "" {
		t.Fatalf("expected empty last_seen, got %v", body["last_seen_message_id"])
	}
	if int(body["unread_count"].(float64)) != 4 {
		t.Fatalf("unread_count=%v want 4", body["unread_count"])
	}
}

func TestAPI_Unread_NotFoundConv(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL + "/api/conversations/nope/unread", sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Unread_InvalidUserID(t *testing.T) {
	deps, _ := setupAPI(t)
	convID, _ := seedConvAndMessages(t, deps, "", "unread-bad", 1)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/conversations/" + string(convID) + "/unread?user_id=no-prefix")
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Unread_RepoUnwired_501(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.ReadStateSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Get(s.URL + "/api/conversations/any/unread")
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Unread_QueryStringUserID(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "unread-qs", 2)
	s := newTestServer(t, deps)
	defer s.Close()
	resp := orgScopedGet(t, s.URL+"/api/conversations/"+string(convID)+"/unread?user_id=user:other", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["user_id"] != "user:other" {
		t.Fatalf("user_id=%v want user:other", body["user_id"])
	}
}

func TestAPI_Seen_HappyPath(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, ids := seedConvAndMessages(t, deps, sess.OrgID, "seen-happy", 2)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"last_seen_message_id":"` + string(ids[1]) + `"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convID)+"/seen", body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["bumped"] != true {
		t.Fatalf("bumped=%v", out["bumped"])
	}
	if out["event_id"] == nil || out["event_id"] == "" {
		t.Fatalf("missing event_id: %v", out["event_id"])
	}
	if int(out["version"].(float64)) != 1 {
		t.Fatalf("version=%v want 1", out["version"])
	}
}

func TestAPI_Seen_NoOpBackward(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, ids := seedConvAndMessages(t, deps, sess.OrgID, "seen-noop", 2)
	// Pre-seed cursor at msg[1].
	if _, err := deps.ReadStateSvc.MarkSeen(context.Background(), convservice.MarkSeenCommand{
		UserID: "user:hayang", ConversationID: convID,
		LastSeenMessageID: ids[1], Actor: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"last_seen_message_id":"` + string(ids[0]) + `"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convID)+"/seen", body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["bumped"] != false {
		t.Fatalf("bumped=%v want false", out["bumped"])
	}
	if out["event_id"] != "" {
		t.Fatalf("no-op should not return event_id: %v", out["event_id"])
	}
}

func TestAPI_Seen_InvalidJSON(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/x/seen",
		"application/json", strings.NewReader(`{not json`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Seen_MissingMessageID(t *testing.T) {
	deps, _ := setupAPI(t)
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/x/seen",
		"application/json", strings.NewReader(`{"user_id":"user:hayang"}`))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Seen_InvalidUserID(t *testing.T) {
	deps, _ := setupAPI(t)
	convID, ids := seedConvAndMessages(t, deps, "", "seen-baduser", 1)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"user_id":"no-prefix","last_seen_message_id":"` + string(ids[0]) + `"}`
	resp, _ := http.Post(s.URL+"/api/conversations/"+string(convID)+"/seen",
		"application/json", strings.NewReader(body))
	if resp.StatusCode != 400 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Seen_MessageInWrongConv(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	_, idsA := seedConvAndMessages(t, deps, sess.OrgID, "seen-wronga", 1)
	convB, _ := seedConvAndMessages(t, deps, sess.OrgID, "seen-wrongb", 1)
	s := newTestServer(t, deps)
	defer s.Close()
	// Post convA's message id against convB (both in the org) → the message
	// is not in convB → 422 message_not_in_conversation.
	body := `{"last_seen_message_id":"` + string(idsA[0]) + `"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convB)+"/seen", body, sess)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("got %d want 422", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["error"] != "message_not_in_conversation" {
		t.Fatalf("error=%v", out["error"])
	}
}

func TestAPI_Seen_MessageNotFound(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "seen-msg404", 0)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"last_seen_message_id":"missing-msg"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convID)+"/seen", body, sess)
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Seen_RepoUnwired_501(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.ReadStateSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/x/seen",
		"application/json", strings.NewReader(`{"last_seen_message_id":"x"}`))
	if resp.StatusCode != 501 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func TestAPI_Seen_DefaultsUserIDToActor(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, ids := seedConvAndMessages(t, deps, sess.OrgID, "seen-default-user", 1)
	s := newTestServer(t, deps)
	defer s.Close()
	body := `{"last_seen_message_id":"` + string(ids[0]) + `"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convID)+"/seen", body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	// Repo should now have a row for user:hayang (the default actor).
	row, err := deps.ReadStateRepo.FindByUserAndConv(context.Background(),
		"user:hayang", convID)
	if err != nil {
		t.Fatalf("expected row for actor: %v", err)
	}
	if row.LastSeenMessageID != ids[0] {
		t.Fatalf("row last_seen=%s want %s", row.LastSeenMessageID, ids[0])
	}
}

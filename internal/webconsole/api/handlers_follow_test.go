package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
)

func TestAPI_Follow_AndUnfollow(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "follow-1", 1)
	s := newTestServer(t, deps)
	defer s.Close()

	// POST /follow → followed=true.
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convID)+"/follow", "", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("follow got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["followed"] != true {
		t.Fatalf("followed=%v want true", out["followed"])
	}

	// DELETE /follow → followed=false.
	resp = orgScopedDelete(t, s.URL+"/api/conversations/"+string(convID)+"/follow", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("unfollow got %d", resp.StatusCode)
	}
	out = map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out["followed"] != false {
		t.Fatalf("followed=%v want false", out["followed"])
	}
}

func TestAPI_Follow_Unwired_501(t *testing.T) {
	deps, _ := setupAPI(t)
	deps.FollowStateSvc = nil
	s := newTestServer(t, deps)
	defer s.Close()
	resp, _ := http.Post(s.URL+"/api/conversations/any/follow", "application/json", nil)
	if resp.StatusCode != 501 {
		t.Fatalf("got %d want 501", resp.StatusCode)
	}
}

func TestAPI_Seen_ReturnsRecomputedCounts(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, ids := seedConvAndMessages(t, deps, sess.OrgID, "seen-counts", 3)
	s := newTestServer(t, deps)
	defer s.Close()

	// Mark seen up to msg 0 → 2 unread remain.
	body := `{"last_seen_message_id":"` + string(ids[0]) + `"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convID)+"/seen", body, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if int(out["unread_count"].(float64)) != 2 {
		t.Fatalf("unread_count=%v want 2 (recomputed N−K)", out["unread_count"])
	}
	if int(out["mention_count"].(float64)) != 0 {
		t.Fatalf("mention_count=%v want 0", out["mention_count"])
	}
}

// findConvRow returns the conversation DTO row with the given id from a
// GET /api/conversations array response.
func findConvRow(t *testing.T, resp *http.Response, convID conversation.ConversationID) map[string]any {
	t.Helper()
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, row := range arr {
		if row["id"] == string(convID) {
			return row
		}
	}
	t.Fatalf("conversation %s not in list of %d", convID, len(arr))
	return nil
}

func TestAPI_ListConversations_EmbedsBadges(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "list-badges", 3)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/conversations", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	row := findConvRow(t, resp, convID)
	// Top-level channel → followed by default → badges counted.
	if row["followed"] != true {
		t.Fatalf("followed=%v want true (top-level default)", row["followed"])
	}
	if int(row["unread_count"].(float64)) != 3 {
		t.Fatalf("unread_count=%v want 3", row["unread_count"])
	}
	if _, ok := row["mention_count"]; !ok {
		t.Fatal("mention_count field missing from DTO")
	}
}

func TestAPI_ShowConversation_EmbedsBadges(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "show-badges", 2)
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+string(convID), sess)
	if resp.StatusCode != 200 {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var row map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&row)
	if row["followed"] != true {
		t.Fatalf("followed=%v want true", row["followed"])
	}
	if int(row["unread_count"].(float64)) != 2 {
		t.Fatalf("unread_count=%v want 2", row["unread_count"])
	}
}

func TestAPI_Unfollow_SuppressesBadges(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "unfollow-suppress", 3)
	s := newTestServer(t, deps)
	defer s.Close()

	// Unfollow → list row shows followed=false + unread suppressed to 0.
	if resp := orgScopedDelete(t, s.URL+"/api/conversations/"+string(convID)+"/follow", sess); resp.StatusCode != 200 {
		t.Fatalf("unfollow got %d", resp.StatusCode)
	}
	resp := orgScopedGet(t, s.URL+"/api/conversations", sess)
	row := findConvRow(t, resp, convID)
	if row["followed"] != false {
		t.Fatalf("followed=%v want false after unfollow", row["followed"])
	}
	if int(row["unread_count"].(float64)) != 0 {
		t.Fatalf("unread_count=%v want 0 (unfollowed → badges stop)", row["unread_count"])
	}
}

func TestAPI_Send_AutoAdvancesAuthorAndAutoFollows(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	// Empty conv (no pre-seeded messages); the session user will post one.
	convID, _ := seedConvAndMessages(t, deps, sess.OrgID, "send-advance", 0)
	s := newTestServer(t, deps)
	defer s.Close()

	// Post a message as the session user via the HTTP handler.
	body := `{"content":"hello world","content_kind":"text"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+string(convID)+"/messages", body, sess)
	if resp.StatusCode != 201 {
		t.Fatalf("send got %d", resp.StatusCode)
	}
	// The sender is caught up to their own message → unread 0.
	sref := conversation.IdentityRef("user:" + sess.IdentityID)
	sum, err := deps.ReadStateSvc.UnreadWithMentions(context.Background(), sref, convID, "")
	if err != nil {
		t.Fatal(err)
	}
	if sum.UnreadCount != 0 {
		t.Fatalf("author unread=%d want 0 (auto-advance-on-send)", sum.UnreadCount)
	}
	// And the sender auto-follows (participate). For a top-level channel this is
	// the default anyway, so assert the read cursor advanced (the catch-up) and
	// the conversation resolves followed.
	followed, err := deps.FollowStateSvc.IsFollowed(context.Background(), sref, convID)
	if err != nil {
		t.Fatal(err)
	}
	if !followed {
		t.Fatal("sender should follow the conversation they posted in")
	}
}

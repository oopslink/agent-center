package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

// v2.9.1 P3: the has-new-activity derivation is a pure ULID compare against the
// user's last_seen cursor. Covers all branches deterministically.
func TestThreadHasNewActivity(t *testing.T) {
	cases := []struct {
		name     string
		dg       conversation.ThreadDigest
		lastSeen string
		applies  bool
		want     bool
	}{
		{"newer reply than cursor → new", conversation.ThreadDigest{ReplyCount: 1, LastReplyID: "0200"}, "0100", true, true},
		{"reply at cursor → seen", conversation.ThreadDigest{ReplyCount: 1, LastReplyID: "0100"}, "0100", true, false},
		{"reply older than cursor → seen", conversation.ThreadDigest{ReplyCount: 1, LastReplyID: "0100"}, "0200", true, false},
		{"never seen (empty cursor) + replies → new", conversation.ThreadDigest{ReplyCount: 1, LastReplyID: "0001"}, "", true, true},
		{"no replies → never new", conversation.ThreadDigest{ReplyCount: 0, LastReplyID: ""}, "", true, false},
		{"does not apply (agent / no read-state) → false", conversation.ThreadDigest{ReplyCount: 1, LastReplyID: "0200"}, "0100", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := threadHasNewActivity(c.dg, c.lastSeen, c.applies); got != c.want {
				t.Fatalf("threadHasNewActivity = %v want %v", got, c.want)
			}
		})
	}
}

// End-to-end wiring: the thread list + message-list badge carry has_new_activity.
// The session user posts the root + reply; posting advances the sender's read
// cursor (sendMessageHandler marks-seen), so the sender has already "seen" their
// own reply → has_new_activity is present and false. (The true path — a reply
// from another participant — is covered by the pure-fn + digest tests and by
// Tester2 run-real with two real participants.)
func TestAPI_ThreadListAndBadge_HasNewActivity_SeenBySender(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	rootID := postReply(t, s.URL, cid, "root msg", "", sess)
	postReply(t, s.URL, cid, "a reply", rootID, sess)

	// Thread list summary carries the boolean (false: sender saw own reply).
	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/threads", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get threads: got %d want 200", resp.StatusCode)
	}
	var threads []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&threads); err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads len = %d want 1", len(threads))
	}
	hna, present := threads[0]["has_new_activity"]
	if !present {
		t.Fatalf("thread summary missing has_new_activity: %v", threads[0])
	}
	if hna != false {
		t.Fatalf("has_new_activity = %v want false (sender saw own reply)", hna)
	}

	// Message-list root row carries the same badge.
	mresp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/messages", sess)
	var msgs []map[string]any
	if err := json.NewDecoder(mresp.Body).Decode(&msgs); err != nil {
		t.Fatal(err)
	}
	var rootRow map[string]any
	for _, m := range msgs {
		if m["id"] == rootID {
			rootRow = m
		}
	}
	if rootRow == nil {
		t.Fatalf("root row not found in message list")
	}
	if _, ok := rootRow["has_new_activity"]; !ok {
		t.Fatalf("root row missing has_new_activity badge: %v", rootRow)
	}
}

// postReply POSTs a message (optionally a thread reply) and returns its id.
func postReply(t *testing.T, srvURL, cid, content, parentID string, sess testSession) string {
	t.Helper()
	body := `{"content":"` + content + `"}`
	if parentID != "" {
		body = `{"content":"` + content + `","parent_message_id":"` + parentID + `"}`
	}
	resp := orgScopedPost(t, srvURL+"/api/conversations/"+cid+"/messages", body, sess)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post %q: got %d want 201", content, resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	id, _ := out["message_id"].(string)
	if id == "" {
		t.Fatalf("post %q: missing message_id: %v", content, out)
	}
	return id
}

// GET .../messages/{rootId}/replies returns ONLY the thread's replies (children),
// in order. Exercises the POST parent_message_id wiring end-to-end.
func TestAPI_ListThreadReplies_ChildrenOrdered(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	rootID := postReply(t, s.URL, cid, "root msg", "", sess)
	postReply(t, s.URL, cid, "first reply", rootID, sess)
	postReply(t, s.URL, cid, "second reply", rootID, sess)

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/messages/"+rootID+"/replies", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get replies: got %d want 200", resp.StatusCode)
	}
	var replies []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&replies); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 {
		t.Fatalf("replies len = %d want 2 (children only, no root)", len(replies))
	}
	if replies[0]["content"] != "first reply" || replies[1]["content"] != "second reply" {
		t.Fatalf("replies out of order: %v / %v", replies[0]["content"], replies[1]["content"])
	}
	// The root itself must NOT appear in the replies payload.
	for _, m := range replies {
		if m["id"] == rootID {
			t.Fatalf("root leaked into replies payload: %v", m)
		}
	}
	// Each reply carries its thread linkage.
	if replies[0]["root_message_id"] != rootID || replies[0]["parent_message_id"] != rootID {
		t.Fatalf("reply thread refs wrong: %v", replies[0])
	}
}

// The message-list endpoint shows top-level messages only (replies excluded) and
// annotates a root with reply_count + thread_last_activity_at.
func TestAPI_ListMessages_TopLevelOnly_ThreadBadge(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	rootID := postReply(t, s.URL, cid, "root", "", sess)
	replyID := postReply(t, s.URL, cid, "reply", rootID, sess)
	plainID := postReply(t, s.URL, cid, "plain", "", sess)

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/messages", sess)
	var msgs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, m := range msgs {
		byID[m["id"].(string)] = m
	}
	// Reply is excluded from the main flow.
	if _, ok := byID[replyID]; ok {
		t.Fatalf("reply must be excluded from the main message list")
	}
	// Root carries the badge.
	if byID[rootID]["reply_count"].(float64) != 1 {
		t.Fatalf("root reply_count wrong: %v", byID[rootID])
	}
	if byID[rootID]["thread_last_activity_at"] == nil {
		t.Fatalf("root must carry thread_last_activity_at: %v", byID[rootID])
	}
	// Plain top-level message carries no badge.
	if _, ok := byID[plainID]["reply_count"]; ok {
		t.Fatalf("plain message must not carry reply_count: %v", byID[plainID])
	}
}

// A non-existent root id in a reachable conversation → 404 (non-disclosure).
func TestAPI_ListThreadReplies_UnknownRoot_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/messages/ghost/replies", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown root: got %d want 404", resp.StatusCode)
	}
}

// Passing a REPLY id as the root → 404 (a thread is addressed by its root only).
func TestAPI_ListThreadReplies_ReplyAsRoot_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	rootID := postReply(t, s.URL, cid, "root", "", sess)
	replyID := postReply(t, s.URL, cid, "reply", rootID, sess)

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/messages/"+replyID+"/replies", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("reply-as-root: got %d want 404", resp.StatusCode)
	}
}

// Cross-org: reading a thread in another org's conversation → 404 (§5.7).
func TestAPI_ListThreadReplies_CrossOrg_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // caller's org
	ctx := context.Background()

	other, err := deps.ChannelMgmtSvc.CreateChannel(ctx, convservice.CreateChannelCommand{
		Name: "other-org-ch", OrganizationID: "organization-other",
		CreatedBy: "user:other", Actor: "user:other",
	})
	if err != nil {
		t.Fatal(err)
	}
	rootRes, err := deps.MessageWriter.AddMessage(ctx, convservice.AddMessageCommand{
		ConversationID:   other.ConversationID,
		SenderIdentityID: conversation.IdentityRef("user:other"),
		ContentKind:      conversation.MessageContentText,
		Content:          "secret root",
		Direction:        conversation.DirectionInbound,
		Actor:            "user:other",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+string(other.ConversationID)+"/messages/"+string(rootRes.MessageID)+"/replies", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org thread read: got %d want 404", resp.StatusCode)
	}
}

// POST a reply whose parent lives in ANOTHER conversation → 404 (ErrMessageParentMismatch).
func TestAPI_SendReply_ParentInOtherConversation_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cidA := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	cidB := seedOrgChannel(t, deps, sess.OrgID, "beta")
	s := newTestServer(t, deps)
	defer s.Close()

	rootInA := postReply(t, s.URL, cidA, "root in A", "", sess)
	// Reply in B targeting a parent in A → rejected.
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cidB+"/messages",
		`{"content":"x","parent_message_id":"`+rootInA+`"}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-conversation parent: got %d want 404", resp.StatusCode)
	}
}

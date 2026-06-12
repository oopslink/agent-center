package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

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

// GET /threads/{root} returns the root + ordered replies, with reply_count +
// has_activity derived on the root. Exercises the POST parent_message_id wiring
// end-to-end (write a reply → read the thread).
func TestAPI_GetThread_RootPlusOrderedReplies(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	rootID := postReply(t, s.URL, cid, "root msg", "", sess)
	postReply(t, s.URL, cid, "first reply", rootID, sess)
	postReply(t, s.URL, cid, "second reply", rootID, sess)

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/threads/"+rootID, sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get thread: got %d want 200", resp.StatusCode)
	}
	var out struct {
		Root    map[string]any   `json:"root"`
		Replies []map[string]any `json:"replies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Root["id"] != rootID {
		t.Fatalf("root id = %v want %v", out.Root["id"], rootID)
	}
	if out.Root["reply_count"].(float64) != 2 {
		t.Fatalf("reply_count = %v want 2", out.Root["reply_count"])
	}
	if out.Root["has_activity"] != true {
		t.Fatalf("has_activity = %v want true", out.Root["has_activity"])
	}
	if len(out.Replies) != 2 {
		t.Fatalf("replies len = %d want 2", len(out.Replies))
	}
	if out.Replies[0]["content"] != "first reply" || out.Replies[1]["content"] != "second reply" {
		t.Fatalf("replies out of order: %v / %v", out.Replies[0]["content"], out.Replies[1]["content"])
	}
	// Each reply carries its thread linkage.
	if out.Replies[0]["root_message_id"] != rootID || out.Replies[0]["parent_message_id"] != rootID {
		t.Fatalf("reply thread refs wrong: %v", out.Replies[0])
	}
}

// The message-list endpoint annotates a root message with reply_count +
// has_activity (the thread-button badge foundation); plain messages omit them.
func TestAPI_ListMessages_ThreadBadge(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	rootID := postReply(t, s.URL, cid, "root", "", sess)
	postReply(t, s.URL, cid, "reply", rootID, sess)
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
	if byID[rootID]["reply_count"].(float64) != 1 || byID[rootID]["has_activity"] != true {
		t.Fatalf("root badge wrong: %v", byID[rootID])
	}
	if _, ok := byID[plainID]["reply_count"]; ok {
		t.Fatalf("plain message must not carry reply_count: %v", byID[plainID])
	}
}

// A non-existent root id in a reachable conversation → 404 (non-disclosure).
func TestAPI_GetThread_UnknownRoot_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/threads/ghost", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown root: got %d want 404", resp.StatusCode)
	}
}

// Cross-org: reading a thread in another org's conversation → 404 (§5.7, existence
// non-disclosure), regardless of whether the root message exists there.
func TestAPI_GetThread_CrossOrg_404(t *testing.T) {
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

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+string(other.ConversationID)+"/threads/"+string(rootRes.MessageID), sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org thread read: got %d want 404", resp.StatusCode)
	}
}

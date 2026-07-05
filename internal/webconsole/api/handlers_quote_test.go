package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// postQuote POSTs a message that quotes quotedID (empty = no quote) and returns
// its id. Mirrors postReply but drives the quoted_message_id wiring.
func postQuote(t *testing.T, srvURL, cid, content, quotedID string, sess testSession) string {
	t.Helper()
	body := `{"content":"` + content + `"}`
	if quotedID != "" {
		body = `{"content":"` + content + `","quoted_message_id":"` + quotedID + `"}`
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

func listMessages(t *testing.T, srvURL, cid string, sess testSession) map[string]map[string]any {
	t.Helper()
	resp := orgScopedGet(t, srvURL+"/api/conversations/"+cid+"/messages", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list messages: got %d want 200", resp.StatusCode)
	}
	var msgs []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, m := range msgs {
		byID[m["id"].(string)] = m
	}
	return byID
}

// 引用 (quote): a message quoting an earlier one carries quoted_message_id plus a
// resolved quoted_message preview (sender + snippet, is_deleted=false). A plain
// message carries neither key.
func TestAPI_ListMessages_QuotePreview(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	targetID := postQuote(t, s.URL, cid, "the original message", "", sess)
	quotingID := postQuote(t, s.URL, cid, "replying with a quote", targetID, sess)

	byID := listMessages(t, s.URL, cid, sess)

	q := byID[quotingID]
	if q["quoted_message_id"] != targetID {
		t.Fatalf("quoting row quoted_message_id = %v want %q", q["quoted_message_id"], targetID)
	}
	preview, ok := q["quoted_message"].(map[string]any)
	if !ok {
		t.Fatalf("quoting row missing quoted_message preview: %v", q)
	}
	if preview["id"] != targetID {
		t.Fatalf("preview id = %v want %q", preview["id"], targetID)
	}
	if preview["is_deleted"] != false {
		t.Fatalf("preview is_deleted = %v want false", preview["is_deleted"])
	}
	if preview["content_snippet"] != "the original message" {
		t.Fatalf("preview snippet = %v want %q", preview["content_snippet"], "the original message")
	}
	if preview["sender_identity_id"] == "" || preview["sender_identity_id"] == nil {
		t.Fatalf("preview missing sender_identity_id: %v", preview)
	}

	// The plain target carries no quote keys.
	target := byID[targetID]
	if _, ok := target["quoted_message_id"]; ok {
		t.Fatalf("plain message must not carry quoted_message_id: %v", target)
	}
	if _, ok := target["quoted_message"]; ok {
		t.Fatalf("plain message must not carry quoted_message: %v", target)
	}
}

// A quote pointing at a message in ANOTHER conversation is rejected 404
// (existence non-disclosure — same posture as a cross-conversation reply parent).
func TestAPI_SendQuote_CrossConversation_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cidA := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	cidB := seedOrgChannel(t, deps, sess.OrgID, "beta")
	s := newTestServer(t, deps)
	defer s.Close()

	inA := postQuote(t, s.URL, cidA, "root in A", "", sess)
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cidB+"/messages",
		`{"content":"x","quoted_message_id":"`+inA+`"}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-conversation quote: got %d want 404", resp.StatusCode)
	}
}

// 引用 soft-reference degradation: if the quoted target is later removed (its
// row deleted, e.g. conversation cleared), the read side returns a deleted stub
// { id, is_deleted:true } — no sender/snippet — instead of failing the list.
func TestAPI_ListMessages_QuotePreview_DeletedTarget(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	targetID := postQuote(t, s.URL, cid, "doomed original", "", sess)
	quotingID := postQuote(t, s.URL, cid, "quoting the doomed one", targetID, sess)

	// Remove ONLY the quoted target row, simulating a soft-ref that outlived its
	// referent. The quoting message stays.
	if _, err := db.ExecContext(context.Background(),
		`DELETE FROM messages WHERE id = ?`, targetID); err != nil {
		t.Fatal(err)
	}

	byID := listMessages(t, s.URL, cid, sess)
	q := byID[quotingID]
	preview, ok := q["quoted_message"].(map[string]any)
	if !ok {
		t.Fatalf("quoting row missing quoted_message stub: %v", q)
	}
	if preview["id"] != targetID {
		t.Fatalf("stub id = %v want %q", preview["id"], targetID)
	}
	if preview["is_deleted"] != true {
		t.Fatalf("stub is_deleted = %v want true", preview["is_deleted"])
	}
	if _, ok := preview["content_snippet"]; ok {
		t.Fatalf("deleted stub must not carry a snippet: %v", preview)
	}
}

// quoteSnippet collapses whitespace to one line and truncates on rune boundaries.
func TestQuoteSnippet(t *testing.T) {
	if got := quoteSnippet("hello   world\nsecond line"); got != "hello world second line" {
		t.Fatalf("whitespace collapse = %q", got)
	}
	long := strings.Repeat("あ", quoteSnippetMaxRunes+50) // multibyte, over the cap
	got := quoteSnippet(long)
	if r := []rune(got); len(r) != quoteSnippetMaxRunes+1 || string(r[len(r)-1]) != "…" {
		t.Fatalf("truncation len = %d (want %d incl. ellipsis)", len([]rune(got)), quoteSnippetMaxRunes+1)
	}
	if got := quoteSnippet(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

// A quote pointing at a non-existent message is rejected 404.
func TestAPI_SendQuote_UnknownTarget_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedPost(t, s.URL+"/api/conversations/"+cid+"/messages",
		`{"content":"x","quoted_message_id":"ghost"}`, sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown quote target: got %d want 404", resp.StatusCode)
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
)

// Tester lane — v2.9.1 Thread P3 has-new-activity data/API class-guard.
//
// Dev's P3 coverage is the pure-fn (TestThreadHasNewActivity) + the sender-saw-own-
// reply case, and dev explicitly DEFERS the true set→clear path ("a reply from
// another participant") to Tester2 run-real. This guard closes that at the data/API
// level: the full per-user lifecycle SET → CLEAR → RE-SET driven over the real HTTP
// stack, with the new reply coming from ANOTHER sender (so it is NOT auto-seen by the
// session user). One guard for two defect classes — "marker doesn't refresh"
// (set/re-set fails) and "marker doesn't clear" (view fails) — and it asserts the
// §5.4 parity that both render sites (message-list badge + thread-list summary) agree
// at every step.

// hnaBadge reads has_new_activity for a root from the message-list badge.
func hnaBadge(t *testing.T, srvURL, cid, rootID string, sess testSession) (val bool, present bool) {
	t.Helper()
	resp := orgScopedGet(t, srvURL+"/api/conversations/"+cid+"/messages", sess)
	defer resp.Body.Close()
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	for _, m := range arr {
		if m["id"] == rootID {
			v, ok := m["has_new_activity"].(bool)
			return v, ok
		}
	}
	t.Fatalf("root %s not in message list", rootID)
	return false, false
}

// hnaSummary reads has_new_activity for a root from the thread-list summary.
func hnaSummary(t *testing.T, srvURL, cid, rootID string, sess testSession) (val bool, present bool) {
	t.Helper()
	resp := orgScopedGet(t, srvURL+"/api/conversations/"+cid+"/threads", sess)
	defer resp.Body.Close()
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode threads: %v", err)
	}
	for _, sm := range arr {
		root, _ := sm["root"].(map[string]any)
		if root != nil && root["id"] == rootID {
			v, ok := sm["has_new_activity"].(bool)
			return v, ok
		}
	}
	t.Fatalf("root %s not in thread list", rootID)
	return false, false
}

// assertHNA checks both render sites agree on the expected has_new_activity (§5.4).
func assertHNA(t *testing.T, srvURL, cid, rootID string, sess testSession, want bool, phase string) {
	t.Helper()
	badge, bOK := hnaBadge(t, srvURL, cid, rootID, sess)
	summary, sOK := hnaSummary(t, srvURL, cid, rootID, sess)
	if !bOK || !sOK {
		t.Fatalf("%s: has_new_activity absent (badge present=%v, summary present=%v)", phase, bOK, sOK)
	}
	if badge != want || summary != want {
		t.Fatalf("%s: has_new_activity DIVERGENCE/wrong: badge=%v summary=%v want %v", phase, badge, summary, want)
	}
}

// postSeen advances the session user's conversation read cursor to lastSeenID.
func postSeen(t *testing.T, srvURL, cid, lastSeenID string, sess testSession) {
	t.Helper()
	resp := orgScopedPost(t, srvURL+"/api/conversations/"+cid+"/seen",
		`{"last_seen_message_id":"`+lastSeenID+`"}`, sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mark seen %s: got %d want 200", lastSeenID, resp.StatusCode)
	}
}

func TestClassGuard_HasNewActivity_SetClearReset_Lifecycle(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	// Session user posts the root → sending auto-advances the sender's cursor to the
	// root, so no thread/replies yet → no badge state to read until a reply exists.
	rootID := postReply(t, s.URL, cid, "root msg", "", sess)

	// A reply from ANOTHER sender (backdoor write; NOT the session user) → it is not
	// auto-seen by the session user. Its ULID sorts after the root cursor → NEW.
	reply1 := otherSenderReply(t, deps, cid, rootID, "first reply from someone else")
	assertHNA(t, s.URL, cid, rootID, sess, true, "SET (other's reply, unseen)")

	// The session user views — marks the conversation seen up to that reply. CLEAR.
	postSeen(t, s.URL, cid, reply1, sess)
	assertHNA(t, s.URL, cid, rootID, sess, false, "CLEAR (after mark-seen)")

	// Another new reply from someone else arrives → the marker must light up again.
	// Guards the "marker doesn't refresh after a clear" class.
	reply2 := otherSenderReply(t, deps, cid, rootID, "second reply from someone else")
	assertHNA(t, s.URL, cid, rootID, sess, true, "RE-SET (newer reply after clear)")

	// View again → CLEAR again.
	postSeen(t, s.URL, cid, reply2, sess)
	assertHNA(t, s.URL, cid, rootID, sess, false, "CLEAR again")
}

// otherSenderReply posts a thread reply authored by a DIFFERENT identity than the
// test session, via the service (so the HTTP sender-auto-mark-seen does not fire for
// the session user). Returns the reply id.
func otherSenderReply(t *testing.T, deps HandlerDeps, cid, rootID, content string) string {
	t.Helper()
	res, err := deps.MessageWriter.AddMessage(context.Background(), convservice.AddMessageCommand{
		ConversationID:   conversation.ConversationID(cid),
		SenderIdentityID: conversation.IdentityRef("user:someone-else"),
		ContentKind:      conversation.MessageContentText,
		Content:          content,
		Direction:        conversation.DirectionInbound,
		ParentMessageID:  conversation.MessageID(rootID),
		Actor:            observability.Actor("user:someone-else"),
	})
	if err != nil {
		t.Fatalf("other-sender reply: %v", err)
	}
	return string(res.MessageID)
}

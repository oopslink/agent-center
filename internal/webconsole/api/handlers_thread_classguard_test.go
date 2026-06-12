package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Tester lane — v2.9.1 Thread P1 data/API class-guards.
//
// These are INVERSE-MUTATION class-guards: each protects a whole DEFECT CLASS,
// not a single example. They are deliberately end-to-end (HTTP edge) because that
// is the product-truth surface a user/agent actually hits. They complement (do not
// duplicate) the dev example tests in handlers_thread_test.go.

// threadView is the GET /threads/{root} response shape.
type threadView struct {
	Root    map[string]any   `json:"root"`
	Replies []map[string]any `json:"replies"`
}

func getThread(t *testing.T, srvURL, cid, rootID string, sess testSession) (int, threadView) {
	t.Helper()
	resp := orgScopedGet(t, srvURL+"/api/conversations/"+cid+"/threads/"+rootID, sess)
	defer resp.Body.Close()
	var out threadView
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode thread: %v", err)
		}
	}
	return resp.StatusCode, out
}

// listMessages returns the message-list array (each element is msgPublicMap).
func listMessages(t *testing.T, srvURL, cid string, sess testSession) []map[string]any {
	t.Helper()
	resp := orgScopedGet(t, srvURL+"/api/conversations/"+cid+"/messages", sess)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list messages: got %d want 200", resp.StatusCode)
	}
	var arr []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return arr
}

func badgeReplyCount(t *testing.T, arr []map[string]any, msgID string) (int, bool) {
	t.Helper()
	for _, m := range arr {
		if m["id"] == msgID {
			n, ok := m["reply_count"].(float64)
			return int(n), ok
		}
	}
	t.Fatalf("message %s not found in list", msgID)
	return 0, false
}

// CLASS GUARD 1 — depth-1 is a UNIVERSAL invariant over an arbitrary-depth reply
// chain. Dev tests one level (reply→reply merges to root). This builds a long
// chain — each new reply targets the PREVIOUS reply — and asserts that NO message
// ever ends up nested below the root: every reply has parent==root==rootID. This
// guards the whole "produces a second (or deeper) level" regression class. If
// ResolveReplyPlacement ever used target.ID instead of target.ThreadID(), this
// goes red.
func TestClassGuard_Depth1_ArbitraryChain_NeverNests(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	rootID := postReply(t, s.URL, cid, "root", "", sess)

	const chain = 6
	target := rootID
	for i := 0; i < chain; i++ {
		// Always reply to the LAST created message — i.e. reply to a reply, deeper
		// each iteration. Depth-1 must flatten every one onto the root.
		target = postReply(t, s.URL, cid, fmt.Sprintf("reply-%d", i), target, sess)
	}

	// Read side: the thread must hold the root + exactly `chain` replies, and EVERY
	// reply must point parent==root==rootID (never at an intermediate reply).
	code, tv := getThread(t, s.URL, cid, rootID, sess)
	if code != http.StatusOK {
		t.Fatalf("get thread: got %d want 200", code)
	}
	if got := len(tv.Replies); got != chain {
		t.Fatalf("thread reply count = %d want %d (a nested level would hide replies from the root)", got, chain)
	}
	for i, r := range tv.Replies {
		if r["root_message_id"] != rootID {
			t.Errorf("reply[%d] root_message_id = %v want %v (DEPTH VIOLATION)", i, r["root_message_id"], rootID)
		}
		if r["parent_message_id"] != rootID {
			t.Errorf("reply[%d] parent_message_id = %v want %v (depth-1: parent must be the root)", i, r["parent_message_id"], rootID)
		}
	}
	// And no reply may itself appear as a thread root (i.e. addressing any reply id
	// as a root must 404 — it is not a thread head).
	for i, r := range tv.Replies {
		rid, _ := r["id"].(string)
		if code, _ := getThread(t, s.URL, cid, rid, sess); code != http.StatusNotFound {
			t.Errorf("reply[%d] id addressed as root: got %d want 404 (a reply is not a thread head)", i, code)
		}
	}
}

// CLASS GUARD 2 — reply_count PARITY across the two independent render sites
// (§5.4: a derived field correct in one DTO but divergent in a parallel DTO). The
// message-list badge is fed by ThreadReplyCounts (one grouped query); the
// thread-detail count is len(replies) from FindThread. They MUST agree with each
// other AND with the true number of replies posted — for every root, including a
// zero-reply root (which must carry no badge). Two producers, one truth.
func TestClassGuard_ReplyCountParity_AcrossRenderSites(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	// Two independent threads with different reply counts + one zero-reply root.
	rootA := postReply(t, s.URL, cid, "rootA", "", sess)
	rootB := postReply(t, s.URL, cid, "rootB", "", sess)
	rootZero := postReply(t, s.URL, cid, "rootZero", "", sess)
	const nA, nB = 4, 2
	for i := 0; i < nA; i++ {
		postReply(t, s.URL, cid, fmt.Sprintf("a%d", i), rootA, sess)
	}
	for i := 0; i < nB; i++ {
		postReply(t, s.URL, cid, fmt.Sprintf("b%d", i), rootB, sess)
	}

	arr := listMessages(t, s.URL, cid, sess)

	for _, tc := range []struct {
		root string
		want int
	}{{rootA, nA}, {rootB, nB}} {
		// thread-detail producer
		code, tv := getThread(t, s.URL, cid, tc.root, sess)
		if code != http.StatusOK {
			t.Fatalf("get thread %s: got %d want 200", tc.root, code)
		}
		detailLen := len(tv.Replies)
		detailField, _ := tv.Root["reply_count"].(float64)
		// list-badge producer
		badge, hasBadge := badgeReplyCount(t, arr, tc.root)
		if !hasBadge {
			t.Fatalf("root %s missing list badge reply_count", tc.root)
		}
		// All three views must equal the truth.
		if detailLen != tc.want || int(detailField) != tc.want || badge != tc.want {
			t.Errorf("root %s reply_count DIVERGENCE: detail.len=%d detail.field=%d list.badge=%d want %d",
				tc.root, detailLen, int(detailField), badge, tc.want)
		}
		// has_activity must be true wherever there are replies (P1 semantics).
		if act, _ := tv.Root["has_activity"].(bool); !act {
			t.Errorf("root %s has_activity=false want true (has %d replies)", tc.root, tc.want)
		}
	}

	// Zero-reply root: NO badge keys at all, and thread detail reports 0 / inactive.
	if n, ok := badgeReplyCount(t, arr, rootZero); ok {
		t.Errorf("zero-reply root carries a list badge reply_count=%d want absent", n)
	}
	code, tv := getThread(t, s.URL, cid, rootZero, sess)
	if code != http.StatusOK {
		t.Fatalf("get zero-reply thread: got %d want 200", code)
	}
	if rc, _ := tv.Root["reply_count"].(float64); int(rc) != 0 {
		t.Errorf("zero-reply thread reply_count=%v want 0", tv.Root["reply_count"])
	}
	if act, _ := tv.Root["has_activity"].(bool); act {
		t.Errorf("zero-reply thread has_activity=true want false")
	}
}

// CLASS GUARD 3 — thread isolation is existence-non-disclosure (§5.7): every way
// of addressing a thread that the caller should not see resolves to 404, never 403
// and never 200/leak. One guard for the whole class:
//   (a) a reply id addressed as a root        → 404 (a reply is not a thread head)
//   (b) a real root from ANOTHER conversation  → 404 (FindThread is conv-scoped;
//       conversations are org-scoped so this also blocks cross-org stitching)
//   (c) a reply whose parent lives in another conversation → POST 404 (not 422/409)
// plus a positive control so the guard can't pass by always-404-ing.
func TestClassGuard_ThreadIsolation_404_Class(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	chA := seedOrgChannel(t, deps, sess.OrgID, "chA")
	chB := seedOrgChannel(t, deps, sess.OrgID, "chB")
	s := newTestServer(t, deps)
	defer s.Close()

	rootA := postReply(t, s.URL, chA, "rootA", "", sess)
	replyA := postReply(t, s.URL, chA, "replyA", rootA, sess)
	rootB := postReply(t, s.URL, chB, "rootB", "", sess)

	// (a) reply id as root → 404
	if code, _ := getThread(t, s.URL, chA, replyA, sess); code != http.StatusNotFound {
		t.Errorf("(a) reply-as-root: got %d want 404", code)
	}
	// (b) chB's root addressed under chA → 404 (cross-conversation read does not leak)
	if code, _ := getThread(t, s.URL, chA, rootB, sess); code != http.StatusNotFound {
		t.Errorf("(b) foreign-conversation root: got %d want 404", code)
	}
	// (c) reply in chA whose parent is rootB (another conversation) → 404 at the edge
	body := `{"content":"stitch","parent_message_id":"` + rootB + `"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+chA+"/messages", body, sess)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("(c) cross-conversation parent: got %d want 404 (existence non-disclosure, not 409/422)", resp.StatusCode)
	}
	// positive control — legit root in its own conversation resolves 200
	if code, _ := getThread(t, s.URL, chA, rootA, sess); code != http.StatusOK {
		t.Errorf("control: legit root got %d want 200", code)
	}
}

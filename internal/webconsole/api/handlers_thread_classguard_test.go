package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	convservice "github.com/oopslink/agent-center/internal/conversation/service"
)

// Tester lane — v2.9.1 Thread P1+P2 data/API class-guards (aligned to the FE
// contract at dev/v291-p2-be-thread-list: main list is TopLevelOnly + badge
// {reply_count, thread_last_activity_at}; replies via GET .../messages/{rootId}/replies;
// thread list via GET .../threads → [{root, reply_count, thread_last_activity_at}]).
//
// INVERSE-MUTATION class-guards: each protects a whole DEFECT CLASS, end-to-end at
// the HTTP edge, and is verified RED under a deliberate mutation of the feature code.
// They complement (not duplicate) the per-site example tests in handlers_thread_test.go
// and handlers_threadlist_test.go.

// --- helpers over the aligned endpoints ---

func listReplies(t *testing.T, srvURL, cid, rootID string, sess testSession) (int, []map[string]any) {
	t.Helper()
	resp := orgScopedGet(t, srvURL+"/api/conversations/"+cid+"/messages/"+rootID+"/replies", sess)
	defer resp.Body.Close()
	var arr []map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
			t.Fatalf("decode replies: %v", err)
		}
	}
	return resp.StatusCode, arr
}

type threadSummary struct {
	Root                 map[string]any `json:"root"`
	ReplyCount           int            `json:"reply_count"`
	ThreadLastActivityAt string         `json:"thread_last_activity_at"`
}

func listThreads(t *testing.T, srvURL, cid string, sess testSession) (int, []threadSummary) {
	t.Helper()
	resp := orgScopedGet(t, srvURL+"/api/conversations/"+cid+"/threads", sess)
	defer resp.Body.Close()
	var arr []threadSummary
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
			t.Fatalf("decode threads: %v", err)
		}
	}
	return resp.StatusCode, arr
}

func mainListBadge(t *testing.T, srvURL, cid, msgID string, sess testSession) (replyCount int, lastActivity string, present bool) {
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
	for _, m := range arr {
		if m["id"] == msgID {
			n, ok := m["reply_count"].(float64)
			la, _ := m["thread_last_activity_at"].(string)
			return int(n), la, ok
		}
	}
	// A reply is excluded from the TopLevelOnly main list — report not-present.
	return 0, "", false
}

// CLASS GUARD 1 — depth-1 is a UNIVERSAL invariant over an arbitrary-depth reply
// chain. Each new reply targets the PREVIOUS reply; depth-1 must flatten every one
// onto the root. Guards the whole "produces a 2nd+ level" class. (mutation:
// ResolveReplyPlacement→target.ID nests → replies hidden from the root.)
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
		target = postReply(t, s.URL, cid, fmt.Sprintf("reply-%d", i), target, sess)
	}

	code, replies := listReplies(t, s.URL, cid, rootID, sess)
	if code != http.StatusOK {
		t.Fatalf("list replies: got %d want 200", code)
	}
	if len(replies) != chain {
		t.Fatalf("reply count = %d want %d (a nested level would hide replies from the root)", len(replies), chain)
	}
	for i, r := range replies {
		if r["root_message_id"] != rootID {
			t.Errorf("reply[%d] root_message_id=%v want %v (DEPTH VIOLATION)", i, r["root_message_id"], rootID)
		}
		if r["parent_message_id"] != rootID {
			t.Errorf("reply[%d] parent_message_id=%v want %v (depth-1: parent must be the root)", i, r["parent_message_id"], rootID)
		}
	}
	// No reply may itself be addressable as a thread head.
	for i, r := range replies {
		rid, _ := r["id"].(string)
		if code, _ := listReplies(t, s.URL, cid, rid, sess); code != http.StatusNotFound {
			t.Errorf("reply[%d] addressed as root: got %d want 404 (a reply is not a thread head)", i, code)
		}
	}
}

// CLASS GUARD 2 — reply_count + thread_last_activity_at PARITY across the THREE
// render sites (§5.4: a derived field correct in one DTO but divergent in a parallel
// DTO). The main-list badge and the thread-list summary both read ThreadReplyDigests;
// the replies endpoint returns the rows themselves. All must agree with each other
// AND with the truth — for every root, including a zero-reply root (no badge / not a
// thread). thread_last_activity_at must equal the LAST reply's posted_at everywhere.
func TestClassGuard_ThreadDigestParity_AcrossRenderSites(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

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

	_, threads := listThreads(t, s.URL, cid, sess)
	summary := map[string]threadSummary{}
	for _, ts := range threads {
		id, _ := ts.Root["id"].(string)
		summary[id] = ts
	}

	for _, tc := range []struct {
		root string
		want int
	}{{rootA, nA}, {rootB, nB}} {
		code, replies := listReplies(t, s.URL, cid, tc.root, sess)
		if code != http.StatusOK {
			t.Fatalf("replies %s: got %d want 200", tc.root, code)
		}
		repliesLen := len(replies)
		lastPostedAt, _ := replies[len(replies)-1]["posted_at"].(string)

		badgeN, badgeLA, hasBadge := mainListBadge(t, s.URL, cid, tc.root, sess)
		if !hasBadge {
			t.Fatalf("root %s missing main-list badge", tc.root)
		}
		sm, inThreads := summary[tc.root]
		if !inThreads {
			t.Fatalf("root %s missing from thread list", tc.root)
		}

		// reply_count: replies-endpoint len == main badge == thread summary == truth.
		if repliesLen != tc.want || badgeN != tc.want || sm.ReplyCount != tc.want {
			t.Errorf("root %s reply_count DIVERGENCE: replies.len=%d badge=%d summary=%d want %d",
				tc.root, repliesLen, badgeN, sm.ReplyCount, tc.want)
		}
		// thread_last_activity_at: badge == summary == last reply posted_at.
		if badgeLA != lastPostedAt || sm.ThreadLastActivityAt != lastPostedAt {
			t.Errorf("root %s last_activity DIVERGENCE: badge=%q summary=%q lastReply.posted_at=%q",
				tc.root, badgeLA, sm.ThreadLastActivityAt, lastPostedAt)
		}
	}

	// Zero-reply root: no main-list badge, absent from thread list, replies endpoint
	// 200 with an empty array (it IS a valid root, just no replies).
	if _, _, ok := mainListBadge(t, s.URL, cid, rootZero, sess); ok {
		t.Errorf("zero-reply root carries a main-list badge, want none")
	}
	if _, in := summary[rootZero]; in {
		t.Errorf("zero-reply root appears in thread list, want excluded (not a thread)")
	}
	if code, replies := listReplies(t, s.URL, cid, rootZero, sess); code != http.StatusOK || len(replies) != 0 {
		t.Errorf("zero-reply root replies: got (%d,%d) want (200,0)", code, len(replies))
	}
}

// CLASS GUARD 3 — thread addressing is existence-non-disclosure (§5.7): every way of
// addressing a thread the caller should not see resolves to 404, never 403/409/200.
// One guard for the whole class, across BOTH thread read endpoints:
//   (a) reply id as a root on /replies            → 404
//   (b) foreign-conversation root on /replies      → 404 (FindThreadReplies conv-scoped)
//   (c) cross-conversation parent on POST          → 404 (not 409/422)
//   (d) cross-org conversation on /threads list    → 404
// plus positive controls so the guard can't pass by always-404-ing.
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
	if code, _ := listReplies(t, s.URL, chA, replyA, sess); code != http.StatusNotFound {
		t.Errorf("(a) reply-as-root: got %d want 404", code)
	}
	// (b) chB's root addressed under chA → 404 (cross-conversation read does not leak)
	if code, _ := listReplies(t, s.URL, chA, rootB, sess); code != http.StatusNotFound {
		t.Errorf("(b) foreign-conversation root: got %d want 404", code)
	}
	// (c) reply in chA whose parent is rootB (another conversation) → 404 at the edge
	body := `{"content":"stitch","parent_message_id":"` + rootB + `"}`
	resp := orgScopedPost(t, s.URL+"/api/conversations/"+chA+"/messages", body, sess)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("(c) cross-conversation parent: got %d want 404 (existence non-disclosure)", resp.StatusCode)
	}
	// (d) thread list on a conversation outside the caller's org → 404.
	// Build a foreign-org conversation directly via the service (mirrors dev's pattern).
	other, err := deps.ChannelMgmtSvc.CreateChannel(context.Background(), convservice.CreateChannelCommand{
		Name: "cg-other-org-ch", OrganizationID: "organization-other",
		CreatedBy: "user:other", Actor: "user:other",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := listThreads(t, s.URL, string(other.ConversationID), sess); code != http.StatusNotFound {
		t.Errorf("(d) cross-org thread list: got %d want 404", code)
	}

	// positive controls — own conversation resolves 200 on both read endpoints.
	if code, _ := listReplies(t, s.URL, chA, rootA, sess); code != http.StatusOK {
		t.Errorf("control: legit replies got %d want 200", code)
	}
	if code, _ := listThreads(t, s.URL, chA, sess); code != http.StatusOK {
		t.Errorf("control: own thread list got %d want 200", code)
	}
}

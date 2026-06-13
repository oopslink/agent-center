package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// GET .../conversations/{id}/threads → ThreadSummary[] (one per thread = a root
// WITH replies): {root: Message, reply_count, thread_last_activity_at}. Roots with
// no replies (plain messages) are excluded. (v2.9.1 Thread P2)
func TestAPI_ListThreads_Summaries(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	root1 := postReply(t, s.URL, cid, "thread one root", "", sess)
	postReply(t, s.URL, cid, "t1 r1", root1, sess)
	postReply(t, s.URL, cid, "t1 r2", root1, sess)
	root2 := postReply(t, s.URL, cid, "thread two root", "", sess)
	postReply(t, s.URL, cid, "t2 r1", root2, sess)
	plain := postReply(t, s.URL, cid, "no replies here", "", sess)

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/threads", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list threads: got %d want 200", resp.StatusCode)
	}
	var summaries []struct {
		Root                 map[string]any `json:"root"`
		ReplyCount           int            `json:"reply_count"`
		ThreadLastActivityAt string         `json:"thread_last_activity_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries want 2 (only roots with replies)", len(summaries))
	}
	byRoot := map[string]int{}
	for _, sm := range summaries {
		id, _ := sm.Root["id"].(string)
		byRoot[id] = sm.ReplyCount
		if id == plain {
			t.Fatalf("plain message (no replies) must not appear as a thread")
		}
		if sm.ThreadLastActivityAt == "" {
			t.Fatalf("thread %s missing thread_last_activity_at", id)
		}
		if sm.Root["content"] == nil {
			t.Fatalf("summary.root must be a full message: %v", sm.Root)
		}
	}
	if byRoot[root1] != 2 {
		t.Fatalf("root1 reply_count = %d want 2", byRoot[root1])
	}
	if byRoot[root2] != 1 {
		t.Fatalf("root2 reply_count = %d want 1", byRoot[root2])
	}
}

// An empty conversation (no threads) returns an empty array, not 404/null.
func TestAPI_ListThreads_Empty(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	cid := seedOrgChannel(t, deps, sess.OrgID, "alpha")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+cid+"/threads", sess)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d want 200", resp.StatusCode)
	}
	var summaries []any
	if err := json.NewDecoder(resp.Body).Decode(&summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("empty conversation should have no threads, got %d", len(summaries))
	}
}

// Cross-org: listing threads of another org's conversation → 404 (§5.7).
func TestAPI_ListThreads_CrossOrg_404(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	otherCid := seedOrgChannel(t, deps, "organization-other", "other-ch")
	s := newTestServer(t, deps)
	defer s.Close()

	resp := orgScopedGet(t, s.URL+"/api/conversations/"+otherCid+"/threads", sess)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org thread list: got %d want 404", resp.StatusCode)
	}
}

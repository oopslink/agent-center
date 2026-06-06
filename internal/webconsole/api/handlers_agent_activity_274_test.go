package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
)

func TestAPI_AgentActivity_CursorPagination_274(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	srv := newTestServer(t, deps)
	defer srv.Close()
	memberID := createAgentViaAPI(t, srv, sess, "w-1")

	a, err := deps.AgentSvc.ResolveAgent(context.Background(), memberID)
	if err != nil {
		t.Fatal(err)
	}
	// Seed 5 events ev-01..ev-05 (lexical id order = chronological); id DESC →
	// ev-05 newest.
	repo := agentsql.NewActivityEventRepo(db)
	base := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		ev, e := agentbc.NewActivityEvent(agentbc.NewActivityEventInput{
			ID: "ev-0" + string(rune('0'+i)), AgentID: a.ID(), EventType: "status",
			Payload: `{}`, OccurredAt: base.Add(time.Duration(i) * time.Minute),
		})
		if e != nil {
			t.Fatal(e)
		}
		if e := repo.Append(context.Background(), ev); e != nil {
			t.Fatal(e)
		}
	}

	get := func(query string) (ids []string, next any, code int) {
		resp := orgScopedGet(t, srv.URL+"/api/agents/"+memberID+"/activity"+query, sess)
		code = resp.StatusCode
		var body struct {
			Activity   []map[string]any `json:"activity"`
			NextCursor any              `json:"next_cursor"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		for _, e := range body.Activity {
			if v, ok := e["id"].(string); ok {
				ids = append(ids, v)
			}
		}
		return ids, body.NextCursor, code
	}

	// Page 1: limit=2 → newest 2 (ev-05, ev-04) + next_cursor = ev-04 (oldest of page).
	ids, next, code := get("?limit=2")
	if code != 200 {
		t.Fatalf("page1 code=%d", code)
	}
	if len(ids) != 2 || ids[0] != "ev-05" || ids[1] != "ev-04" {
		t.Fatalf("page1 ids=%v want [ev-05 ev-04]", ids)
	}
	if next != "ev-04" {
		t.Fatalf("page1 next_cursor=%v want ev-04", next)
	}

	// Page 2: before=ev-04 limit=2 → ev-03, ev-02 + next_cursor=ev-02.
	ids, next, _ = get("?limit=2&before=ev-04")
	if len(ids) != 2 || ids[0] != "ev-03" || ids[1] != "ev-02" {
		t.Fatalf("page2 ids=%v want [ev-03 ev-02]", ids)
	}
	if next != "ev-02" {
		t.Fatalf("page2 next_cursor=%v want ev-02", next)
	}

	// Page 3 (last): before=ev-02 limit=2 → ev-01 only + next_cursor=null.
	ids, next, _ = get("?limit=2&before=ev-02")
	if len(ids) != 1 || ids[0] != "ev-01" {
		t.Fatalf("page3 ids=%v want [ev-01]", ids)
	}
	if next != nil {
		t.Fatalf("page3 next_cursor=%v want null (last page)", next)
	}
}

func TestAPI_AgentActivity_LimitSemantics_274(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	srv := newTestServer(t, deps)
	defer srv.Close()
	memberID := createAgentViaAPI(t, srv, sess, "w-1")
	a, _ := deps.AgentSvc.ResolveAgent(context.Background(), memberID)
	repo := agentsql.NewActivityEventRepo(db)
	for i := 1; i <= 3; i++ {
		ev, _ := agentbc.NewActivityEvent(agentbc.NewActivityEventInput{
			ID: "ev-0" + string(rune('0'+i)), AgentID: a.ID(), EventType: "status",
			Payload: `{}`, OccurredAt: time.Now().UTC(),
		})
		_ = repo.Append(context.Background(), ev)
	}
	body := func(query string) (n int, next any, code int) {
		resp := orgScopedGet(t, srv.URL+"/api/agents/"+memberID+"/activity"+query, sess)
		var b struct {
			Activity   []map[string]any `json:"activity"`
			NextCursor any              `json:"next_cursor"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&b)
		return len(b.Activity), b.NextCursor, resp.StatusCode
	}

	// limit omitted → default 50 → all 3 + next_cursor null.
	if n, next, code := body(""); code != 200 || n != 3 || next != nil {
		t.Fatalf("default: n=%d next=%v code=%d want 3/null/200", n, next, code)
	}
	// explicit limit=0 → unlimited → all 3 + next_cursor null.
	if n, next, code := body("?limit=0"); code != 200 || n != 3 || next != nil {
		t.Fatalf("limit=0 unlimited: n=%d next=%v code=%d want 3/null/200", n, next, code)
	}
	// negative limit → 400 invalid_limit.
	resp := orgScopedGet(t, srv.URL+"/api/agents/"+memberID+"/activity?limit=-1", sess)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("limit=-1 code=%d want 400", resp.StatusCode)
	}
	var e map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if e["error"] != "invalid_limit" {
		t.Fatalf("error=%v want invalid_limit", e["error"])
	}
	// non-integer limit → 400.
	if resp := orgScopedGet(t, srv.URL+"/api/agents/"+memberID+"/activity?limit=abc", sess); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("limit=abc code=%d want 400", resp.StatusCode)
	}
}

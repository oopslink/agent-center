package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
)

// TestAPI_AgentForceDelete_AuditEvent — v2.8.1 #234: a force agent-delete emits
// agent.force_deleted (with the agent ref + force payload); a non-force delete
// emits nothing (the `if force` guard). Locks the handler-layer audit contract
// (Tester #234 test-gap finding). Best-effort (nil EventSink → no emit, delete
// still succeeds) is covered by every other force-delete test, which run with a
// nil deps.EventSink.
func TestAPI_AgentForceDelete_AuditEvent(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps) // RoleOwner → admin-capable (delete is admin-only)
	ctx := context.Background()
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC))
	deps.EventSink = observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	saveWorkerInOrg(t, db, sess.OrgID, "w-1")
	s := newTestServer(t, deps)
	defer s.Close()

	forceDeletedEvents := func() []*observability.Event {
		t.Helper()
		typ := observability.EventType("agent.force_deleted")
		evs, ferr := er.Find(ctx, observability.EventQueryFilter{EventType: &typ})
		if ferr != nil {
			t.Fatal(ferr)
		}
		return evs
	}

	// force-delete → emits agent.force_deleted with the agent ref + force payload.
	id1 := createAgentViaAPI(t, s, sess, "w-1")
	resp := orgScopedDelete(t, s.URL+"/api/agents/"+id1+"?force=true", sess)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("force delete: got %d, want 200", resp.StatusCode)
	}
	evs := forceDeletedEvents()
	if len(evs) != 1 {
		t.Fatalf("force delete must emit exactly 1 agent.force_deleted, got %d", len(evs))
	}
	if evs[0].Refs().AgentID == "" {
		t.Errorf("agent.force_deleted must carry the agent ref, got empty AgentID")
	}
	if evs[0].Payload()["force"] != true {
		t.Errorf("agent.force_deleted payload force = %v, want true", evs[0].Payload()["force"])
	}

	// non-force delete (stopped idle agent) → succeeds but emits NO event (force guard).
	id2 := createAgentViaAPI(t, s, sess, "w-1")
	resp2 := orgScopedDelete(t, s.URL+"/api/agents/"+id2, sess)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("non-force delete: got %d, want 200", resp2.StatusCode)
	}
	if n := len(forceDeletedEvents()); n != 1 {
		t.Fatalf("non-force delete must NOT emit force_deleted; want still 1, got %d", n)
	}
}

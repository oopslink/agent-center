package api

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// setupAuditAPI wires deps.PM with an audit-capable pm Service over the test DB.
func setupAuditAPI(t *testing.T, deps HandlerDeps) HandlerDeps {
	t.Helper()
	db := deps.DB
	clk := clock.SystemClock{}
	gen := idgen.NewGenerator(clk)
	deps.PM = pmservice.New(pmservice.Deps{
		DB:           db,
		Projects:     pmsql.NewProjectRepo(db),
		Members:      pmsql.NewProjectMemberRepo(db),
		Issues:       pmsql.NewIssueRepo(db),
		Tasks:        pmsql.NewTaskRepo(db),
		TaskSubs:     pmsql.NewTaskSubscriberRepo(db),
		IssueSubs:    pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db),
		Plans:        pmsql.NewPlanRepo(db),
		Outbox:       outboxsql.NewOutboxRepo(db),
		IDGen:        gen,
		Clock:        clk,
		AgentDir:     allAgentsDir{},
		Audit:        pmsql.NewAuditLogRepo(db, gen),
	})
	return deps
}

// TestTaskAuditAPI_ReturnsLedger drives real task mutations then GETs the audit
// endpoint, asserting the change ledger is returned newest-first with structured
// fields (design §6).
func TestTaskAuditAPI_ReturnsLedger(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	deps = setupAuditAPI(t, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: sess.OrgID, Name: "P", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := deps.PM.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: caller})
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.PM.AssignTask(ctx, tid, caller, caller); err != nil {
		t.Fatal(err)
	}
	if err := deps.PM.StartTask(ctx, tid, caller); err != nil {
		t.Fatal(err)
	}

	resp := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/tasks/"+string(tid)+"/audit", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("audit GET status=%d", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	entries, ok := body["entries"].([]any)
	if !ok {
		t.Fatalf("response missing entries array: %v", body)
	}
	if len(entries) < 3 {
		t.Fatalf("want >=3 entries (created+assigned+status_changed), got %d", len(entries))
	}
	// Newest-first: the most recent op was StartTask (status_changed open→running).
	first := entries[0].(map[string]any)
	if first["change_type"] != "status_changed" || first["to"] != "running" {
		t.Fatalf("newest entry wrong: %v", first)
	}
	if first["actor"] != string(caller) {
		t.Fatalf("actor = %v, want %v", first["actor"], caller)
	}
	if _, hasNext := body["next_cursor"]; !hasNext {
		t.Fatal("response missing next_cursor")
	}

	// A non-member of the project must not read the ledger (404 via require helper).
	// Simulate by requesting a bogus task id in the same project → not_found.
	resp2 := orgScopedGet(t, s.URL+"/api/projects/"+string(pid)+"/tasks/nope/audit", sess)
	if resp2.StatusCode != 404 {
		t.Fatalf("bogus task audit want 404, got %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

package api

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation"
	convsqlite "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// TestPM_NestedTaskFlow_EndToEnd is the B3 spot-check path: POST a nested task
// → the participant projector creates the task Conversation + syncs the creator
// participant; POST assign agent → the work-item projector creates a queued
// AgentWorkItem. All over the real HTTP handlers + outbox relay.
func TestPM_NestedTaskFlow_EndToEnd(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()
	caller := pm.IdentityRef("user:" + sess.IdentityID)

	// Seed a pm project owned by the session caller (creator → owner member).
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Acme", CreatedBy: caller,
	})
	if err != nil {
		t.Fatal(err)
	}

	// POST a nested task via HTTP.
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks", `{"title":"do the thing"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create task status=%d body=%s", resp.StatusCode, b)
	}
	var task map[string]any
	json.NewDecoder(resp.Body).Decode(&task)
	tid, _ := task["id"].(string)
	if tid == "" {
		t.Fatal("no task id returned")
	}

	// Assign the task to an Agent via HTTP.
	resp = orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks/"+tid+"/assign", `{"assignee":"agent:AG1"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("assign status=%d body=%s", resp.StatusCode, b)
	}

	// Drain the outbox (the server runs this as the Pump; here we run it once).
	applied := outboxsql.NewAppliedRepo(db)
	convRepo := convsqlite.NewConversationRepo(db)
	wiRepo := agentsql.NewWorkItemRepo(db)
	gen := idgen.NewGenerator(clock.SystemClock{})
	relay := outbox.NewRelay(outboxsql.NewOutboxRepo(db), applied, clock.SystemClock{},
		pmservice.NewParticipantProjector(db, convRepo, applied, gen, clock.SystemClock{}),
		pmservice.NewWorkItemProjector(db, wiRepo, applied, gen, clock.SystemClock{}))
	for i := 0; i < 5; i++ {
		n, err := relay.RunOnce(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}

	// The participant projector created the task Conversation by owner_ref with
	// the creator as a participant.
	conv, err := convRepo.FindByOwnerRef(ctx, conversation.NewTaskOwnerRef(tid))
	if err != nil {
		t.Fatalf("task Conversation not created by projector: %v", err)
	}
	if conv.Kind() != conversation.ConversationKindTask {
		t.Fatalf("conv kind = %s, want task", conv.Kind())
	}
	foundCreator := false
	for _, p := range conv.Participants() {
		if string(p.IdentityID) == string(caller) {
			foundCreator = true
		}
	}
	if !foundCreator {
		t.Fatalf("creator %s not synced as participant: %v", caller, conv.Participants())
	}

	// The work-item projector created a queued WorkItem for the agent.
	items, _ := wiRepo.ListByTask(ctx, "pm://tasks/"+tid)
	if len(items) != 1 || items[0].AgentID() != "AG1" {
		t.Fatalf("expected 1 WorkItem for AG1, got %+v", items)
	}
}

// TestPM_FlatProjectLifecycle covers the flat /api/projects surface that the
// retired Workforce project routes were repointed to in B3-c: create → list →
// get → update (rename/describe) → archive (DELETE = lifecycle, not hard
// delete). This keeps the previously-removed ListProjects/ShowProject coverage
// from going naked now that those routes serve the pm Service.
func TestPM_FlatProjectLifecycle(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()

	// Create via HTTP (caller becomes owner member).
	resp := orgScopedPost(t, s.URL+"/api/projects", `{"name":"Acme","description":"d1"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status=%d body=%s", resp.StatusCode, b)
	}
	var created map[string]any
	json.NewDecoder(resp.Body).Decode(&created)
	pid, _ := created["id"].(string)
	if pid == "" || created["status"] != "active" {
		t.Fatalf("unexpected create body: %+v", created)
	}

	// List → contains the new project.
	resp = orgScopedGet(t, s.URL+"/api/projects", sess)
	if resp.StatusCode != 200 {
		t.Fatalf("list status=%d", resp.StatusCode)
	}
	var listed struct {
		Projects []map[string]any `json:"projects"`
	}
	json.NewDecoder(resp.Body).Decode(&listed)
	if len(listed.Projects) != 1 || listed.Projects[0]["id"] != pid {
		t.Fatalf("list did not return created project: %+v", listed.Projects)
	}

	// Get by id.
	resp = orgScopedGet(t, s.URL+"/api/projects/"+pid, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("get status=%d", resp.StatusCode)
	}

	// Update (rename + describe).
	resp = orgScopedPatch(t, s.URL+"/api/projects/"+pid, `{"name":"Acme2","description":"d2"}`, sess)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("update status=%d body=%s", resp.StatusCode, b)
	}
	var updated map[string]any
	json.NewDecoder(resp.Body).Decode(&updated)
	if updated["name"] != "Acme2" || updated["description"] != "d2" {
		t.Fatalf("update not applied: %+v", updated)
	}

	// Archive (DELETE = lifecycle active→archived).
	resp = orgScopedDelete(t, s.URL+"/api/projects/"+pid, sess)
	if resp.StatusCode != 200 {
		t.Fatalf("archive status=%d", resp.StatusCode)
	}
	resp = orgScopedGet(t, s.URL+"/api/projects/"+pid, sess)
	json.NewDecoder(resp.Body).Decode(&created)
	if created["status"] != "archived" {
		t.Fatalf("project not archived: %+v", created)
	}
}

// TestPM_Gating covers the org+project membership gate: an org member who is
// NOT a project member is rejected (403), and an unknown/foreign project is 404.
func TestPM_Gating(t *testing.T) {
	deps, db := setupAPIWithAuth(t)
	sess := setupTestSession(t, db, deps)
	s := newTestServer(t, deps)
	defer s.Close()
	ctx := context.Background()

	// Project owned by a DIFFERENT identity (caller is an org member but not a
	// project member).
	pid, err := deps.PM.CreateProject(ctx, pmservice.CreateProjectCommand{
		OrganizationID: sess.OrgID, Name: "Other", CreatedBy: "user:someone-else",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := orgScopedPost(t, s.URL+"/api/projects/"+string(pid)+"/tasks", `{"title":"x"}`, sess)
	if resp.StatusCode != 403 {
		t.Fatalf("non-project-member create task: status=%d, want 403", resp.StatusCode)
	}

	// Unknown project id → 404 (cross-org / non-existent hidden).
	resp = orgScopedPost(t, s.URL+"/api/projects/ghost/tasks", `{"title":"x"}`, sess)
	if resp.StatusCode != 404 {
		t.Fatalf("unknown project: status=%d, want 404", resp.StatusCode)
	}
}

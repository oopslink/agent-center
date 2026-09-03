package service

import (
	"testing"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/collaborationeffect"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestCollaborationEffectDependencyReleaseUsesDependentToUpstreamDirection(t *testing.T) {
	h := orchestratorSetup(t)
	ctx := h.ctx
	events, err := obsqlite.NewEventRepo(ctx, h.svc.db)
	if err != nil {
		t.Fatalf("event repo: %v", err)
	}
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "dep-release", CreatedBy: "user:a"})
	h.drain(t)
	a := h.seedAssignedTask(t, pid, planID, "A", "user:a1")
	b := h.seedAssignedTask(t, pid, planID, "B", "user:b1")

	if err := h.svc.AddPlanDependency(ctx, planID, b, a, "user:a"); err != nil {
		t.Fatalf("AddPlanDependency(B,A): %v", err)
	}
	auditType := observability.EventType("pm.audit_recorded")
	auditEvents, err := events.Find(ctx, observability.EventQueryFilter{
		EventType: &auditType,
		Refs:      observability.EventRefsFilter{PlanID: string(planID)},
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("find audit mirror events: %v", err)
	}
	depMirror := findDependencyAddedMirror(auditEvents)
	if depMirror == nil {
		t.Fatal("AddPlanDependency did not mirror dependency_added into Observability")
	}
	detail := depMirror.Payload()["detail"].(map[string]any)
	if detail["from"] != string(b) || detail["to"] != string(a) {
		t.Fatalf("dependency_added detail direction = from %q to %q, want dependent B %q to upstream A %q",
			detail["from"], detail["to"], b, a)
	}

	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)
	if dispatchCount(h.dispatchRecords(t, planID), b) != 0 {
		t.Fatal("B dispatched before A completed")
	}

	h.setTaskStatus(t, a, pm.TaskCompleted)
	h.drain(t)
	if dispatchCount(h.dispatchRecords(t, planID), b) != 1 {
		t.Fatal("B was not dispatched after A completed")
	}

	auditEvents, err = events.Find(ctx, observability.EventQueryFilter{
		EventType: &auditType,
		Refs:      observability.EventRefsFilter{ProjectID: string(pid)},
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("find audit mirror events after completion: %v", err)
	}
	repo, err := collaborationeffect.NewSQLiteRepository(h.svc.db)
	if err != nil {
		t.Fatalf("collaboration effect repo: %v", err)
	}
	projector := collaborationeffect.NewProjector(repo, collaborationeffect.NewEngine(""))
	for _, ev := range auditEvents {
		if err := projector.ProjectEvent(ctx, ev); err != nil {
			t.Fatalf("project audit event %s: %v", ev.ID(), err)
		}
	}
	effects, _, err := repo.List(ctx, collaborationeffect.Filter{ProjectID: string(pid), TaskID: string(b), RelationType: collaborationeffect.RelationDependencyRelease})
	if err != nil {
		t.Fatalf("list effects: %v", err)
	}
	if len(effects) != 1 {
		t.Fatalf("effects len=%d want 1: %+v", len(effects), effects)
	}
	got := effects[0]
	if got.RelationType != collaborationeffect.RelationDependencyRelease || got.TargetTaskID != string(b) {
		t.Fatalf("effect relation/target = %s/%s, want dependency_release on B %s", got.RelationType, got.TargetTaskID, b)
	}
	if len(got.EvidenceEventIDs) != 2 {
		t.Fatalf("effect evidence ids = %v, want dependency mirror then completion", got.EvidenceEventIDs)
	}
}

func findDependencyAddedMirror(events []*observability.Event) *observability.Event {
	for _, ev := range events {
		pl := ev.Payload()
		if pl["object_type"] == "plan" && pl["change_type"] == "dependency_added" {
			return ev
		}
	}
	return nil
}

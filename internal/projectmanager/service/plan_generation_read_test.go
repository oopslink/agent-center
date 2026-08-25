package service

import (
	"context"
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestGetPlanGenerations_ReadsPersistedG0GnSnapshotsAndOwnership(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, _ := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	planID, _ := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "generations", CreatedBy: "user:a", OwnerRef: "user:a"})
	h.drain(t)
	a, b := h.startRunningPlanAB(t, pid, planID)
	p, g0 := activePlanGeneration(t, h, planID)

	result, err := h.svc.EvolvePlanGeneration(h.ctx, EvolvePlanGenerationCommand{
		PlanID: planID, ParentGenerationID: g0.ID, BaseVersion: p.Version(),
		IdempotencyKey: "generation-read-g1", Reason: "replace blocked work", Evidence: "review requires C instead of B", Creator: "user:a",
		Diff: pm.PlanGenerationDiff{
			NodeDecisions: []pm.PlanGenerationNodeDecision{
				{TaskID: a, Action: pm.EvolutionPreserve, Reason: "already dispatched"},
				{TaskID: b, Action: pm.EvolutionSupersede, Reason: "obsolete before dispatch"},
			},
			Tasks: []pm.PlanGenerationTaskDraft{{Ref: "c", Title: "C", AssigneeRef: "user:c1", DeliveryContract: pm.DeliveryCodeChange}},
			Edges: []pm.PlanGenerationEdgeDraft{{From: "c", To: string(a), Kind: pm.EdgeSeq}},
		},
	})
	if err != nil {
		t.Fatalf("EvolvePlanGeneration: %v", err)
	}
	c := generationTaskByTitle(t, result.Generation, "C").TaskID

	read, err := h.svc.GetPlanGenerations(h.ctx, planID)
	if err != nil {
		t.Fatalf("GetPlanGenerations: %v", err)
	}
	if read.ActiveGenerationID != result.Generation.ID || read.PlanVersion != result.Generation.Snapshot.PlanVersion {
		t.Fatalf("active/version=%s/%d want %s/%d", read.ActiveGenerationID, read.PlanVersion, result.Generation.ID, result.Generation.Snapshot.PlanVersion)
	}
	if len(read.Generations) != 2 {
		t.Fatalf("generations=%d want G0/G1: %+v", len(read.Generations), read.Generations)
	}
	base, evolved := read.Generations[0], read.Generations[1]
	if base.Generation.ID != g0.ID || base.Generation.ParentGenerationID != "" || base.Revision != 0 || base.Active {
		t.Fatalf("G0 projection=%+v", base)
	}
	if evolved.Generation.ID != result.Generation.ID || evolved.Generation.ParentGenerationID != g0.ID || evolved.Revision != 1 || !evolved.Active {
		t.Fatalf("G1 projection=%+v", evolved)
	}
	if len(base.Generation.Snapshot.Tasks) != 2 || len(base.Generation.Snapshot.Edges) != 1 {
		t.Fatalf("G0 historical snapshot drifted: %+v", base.Generation.Snapshot)
	}
	if len(evolved.Generation.Snapshot.Tasks) != 2 || len(evolved.Generation.Diff.Tasks) != 1 || len(evolved.Generation.Diff.NodeDecisions) != 2 || len(evolved.Generation.Diff.Edges) != 1 {
		t.Fatalf("G1 snapshot/diff incomplete: snapshot=%+v diff=%+v", evolved.Generation.Snapshot, evolved.Generation.Diff)
	}

	owners := make(map[pm.TaskID]PlanGenerationNodeOwnership)
	for _, node := range read.Nodes {
		owners[node.TaskID] = node
	}
	if owners[a].GenerationID != g0.ID || owners[a].Revision != 0 || !owners[a].PresentInActive {
		t.Fatalf("A ownership=%+v want active G0", owners[a])
	}
	if owners[b].GenerationID != g0.ID || owners[b].PresentInActive {
		t.Fatalf("superseded B ownership=%+v want historical G0", owners[b])
	}
	if owners[c].GenerationID != result.Generation.ID || owners[c].Revision != 1 || !owners[c].PresentInActive {
		t.Fatalf("C ownership=%+v want active G1", owners[c])
	}
}

type generationFinderStub struct {
	generations map[pm.PlanGenerationID]*pm.PlanGeneration
}

func (f generationFinderStub) FindGenerationByID(_ context.Context, id pm.PlanGenerationID) (*pm.PlanGeneration, error) {
	generation := f.generations[id]
	if generation == nil {
		return nil, pm.ErrPlanGenerationNotFound
	}
	return generation, nil
}

func TestLoadPlanGenerationLineage_FailsClosed(t *testing.T) {
	plan, err := pm.RehydratePlan(pm.RehydratePlanInput{
		ID: "plan-1", ProjectID: "project-1", Name: "P", Status: pm.PlanRunning,
		CreatorRef: "user:a", Version: 4, ActiveGenerationID: "g2",
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := func(id, parent pm.PlanGenerationID) *pm.PlanGeneration {
		return &pm.PlanGeneration{ID: id, PlanID: plan.ID(), ParentGenerationID: parent, Snapshot: pm.PlanGenerationSnapshot{
			PlanID: plan.ID(), ActiveGenerationID: id,
		}}
	}

	tests := []struct {
		name        string
		generations map[pm.PlanGenerationID]*pm.PlanGeneration
	}{
		{name: "missing parent", generations: map[pm.PlanGenerationID]*pm.PlanGeneration{"g2": valid("g2", "missing")}},
		{name: "cycle", generations: map[pm.PlanGenerationID]*pm.PlanGeneration{"g2": valid("g2", "g1"), "g1": valid("g1", "g2")}},
		{name: "cross plan", generations: map[pm.PlanGenerationID]*pm.PlanGeneration{"g2": {ID: "g2", PlanID: "plan-2", Snapshot: pm.PlanGenerationSnapshot{PlanID: "plan-2", ActiveGenerationID: "g2"}}}},
		{name: "snapshot mismatch", generations: map[pm.PlanGenerationID]*pm.PlanGeneration{"g2": {ID: "g2", PlanID: plan.ID(), Snapshot: pm.PlanGenerationSnapshot{PlanID: plan.ID(), ActiveGenerationID: "other"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadPlanGenerationLineage(context.Background(), plan, generationFinderStub{generations: tt.generations})
			if !errors.Is(err, pm.ErrPlanGenerationConflict) {
				t.Fatalf("err=%v want ErrPlanGenerationConflict", err)
			}
		})
	}
}

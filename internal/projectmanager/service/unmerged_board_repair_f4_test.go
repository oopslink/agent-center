package service

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestListUnmergedIntegrations_RepairedByCycleMetaAdapter proves the v2.13.0
// I18/F3 fix to F4: once the role is persisted AND the concrete TaskRepoCycleMeta
// adapter is wired, ListUnmergedIntegrations lists the scaffolded plan's un-done
// Integrate nodes — it was ALWAYS empty before (no adapter, nothing keyed role).
// Without the adapter the board is empty (the nil-safe default); with it, the
// Integrate node appears.
func TestListUnmergedIntegrations_RepairedByCycleMetaAdapter(t *testing.T) {
	svc, _, tasks, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID: pid,
		Version:   "v2.13.0",
		Features:  []CycleFeature{{Name: "F1 规格", Branch: "f1-spec"}},
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan: %v", err)
	}
	drain(t, relay, ctx)

	// Before wiring the adapter: the board is empty (nil-safe default).
	board, err := svc.ListUnmergedIntegrations(ctx, res.PlanID)
	if err != nil {
		t.Fatalf("ListUnmergedIntegrations (no adapter): %v", err)
	}
	if len(board.Unmerged) != 0 {
		t.Fatalf("board without adapter = %d rows, want 0 (always-empty pre-fix)", len(board.Unmerged))
	}

	// Wire the concrete CycleNodeMetaPort (the F3 repair) and re-query.
	svc.SetCycleNodeMetaProvider(NewTaskRepoCycleMeta(tasks))
	board, err = svc.ListUnmergedIntegrations(ctx, res.PlanID)
	if err != nil {
		t.Fatalf("ListUnmergedIntegrations (with adapter): %v", err)
	}
	if len(board.Unmerged) != 1 {
		t.Fatalf("board with adapter = %d rows, want 1 (the un-done F1 Integrate node)", len(board.Unmerged))
	}
	got := board.Unmerged[0]
	if got.Branch != "f1-spec" || got.Base != "dev/v2.13.0" {
		t.Errorf("unmerged row = branch:%q base:%q, want f1-spec / dev/v2.13.0", got.Branch, got.Base)
	}
	if board.AllMerged() {
		t.Errorf("AllMerged() = true, want false (an Integrate node is still un-done)")
	}

	// The listed node must be the Integrate node (role==integrate), not Dev/Review.
	tk, err := tasks.FindByID(ctx, got.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Role() != pm.CycleRoleIntegrate {
		t.Errorf("listed node role = %q, want integrate", tk.Role())
	}
}

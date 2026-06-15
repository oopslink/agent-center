package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// v2.10.1 [T99]: the plan DTO carries org_ref ("P<n>") when a number is
// allocated, and OMITS it when 0 (the builtin pool / rows predating the
// allocator) so the SPA gracefully falls back to the hash handle.
func TestPMPlanMap_T99_OrgRef(t *testing.T) {
	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

	plan, err := pm.RehydratePlan(pm.RehydratePlanInput{
		ID: "plan-abc", ProjectID: "proj-1", Name: "P", Status: pm.PlanDraft,
		CreatorRef: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1, OrgNumber: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pmPlanMap(plan)["org_ref"]; got != "P42" {
		t.Fatalf("plan org_ref = %v, want P42", got)
	}

	// org_number 0 → org_ref omitted (graceful fallback to hash handle in the UI).
	unNumbered, err := pm.RehydratePlan(pm.RehydratePlanInput{
		ID: "plan-x", ProjectID: "proj-1", Name: "P", Status: pm.PlanDraft,
		CreatorRef: "user:a", CreatedAt: now, UpdatedAt: now, Version: 1, OrgNumber: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := pmPlanMap(unNumbered)["org_ref"]; present {
		t.Fatalf("plan org_ref must be omitted when org_number is 0")
	}
}

package api

import (
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestPmPlanNodeMap_StarvedField pins the BE-2↔FE contract: every pool-node DTO
// carries a `starved` bool — true when the node is in the lookup's starved set,
// false (always present, never omitted) otherwise.
func TestPmPlanNodeMap_StarvedField(t *testing.T) {
	n := pm.PlanNodeView{TaskID: "t1", TaskStatus: pm.TaskOpen, NodeStatus: pm.NodeDispatched}
	base := planNodeLookup{
		planID:       "pool",
		titleOf:      map[pm.TaskID]string{"t1": "x"},
		assigneeOf:   map[pm.TaskID]pm.IdentityRef{},
		archivedOf:   map[pm.TaskID]bool{},
		archivedAtOf: map[pm.TaskID]string{},
		orgRefOf:     map[pm.TaskID]string{},
	}

	// starved set marks t1 → the DTO reports starved=true.
	withStarved := base
	withStarved.starvedOf = map[pm.TaskID]bool{"t1": true}
	if got := pmPlanNodeMap(n, withStarved)["starved"]; got != true {
		t.Fatalf("starved=%v, want true", got)
	}

	// nil starved set (the common non-pool case) → field PRESENT and false.
	m := pmPlanNodeMap(n, base)
	v, ok := m["starved"]
	if !ok {
		t.Fatal("starved field must always be present in the node DTO")
	}
	if v != false {
		t.Fatalf("starved=%v, want false (absent from set)", v)
	}
}

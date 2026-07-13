package projectmanager

import (
	"reflect"
	"testing"
)

// TestNodeStatus_IsTerminal pins the terminal vs non-terminal partition the frontier
// read face relies on (only non-terminal nodes carry a blocked_on snapshot).
func TestNodeStatus_IsTerminal(t *testing.T) {
	terminal := []NodeStatus{NodeDone, NodeFailed, NodeSkipped}
	nonTerminal := []NodeStatus{NodeBlocked, NodeReady, NodeDispatched, NodeRunning, NodePaused}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s: IsTerminal()=false, want true", s)
		}
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%s: IsTerminal()=true, want false", s)
		}
	}
}

// bo is a terse BlockedOn factory for the frontier tests (only the fields the derivations
// read: task id + wait type).
func bo(task string, wt WaitType) BlockedOn {
	return BlockedOn{TaskID: TaskID(task), WaitType: wt}
}

// taskIDs extracts the ordered task ids of a group's nodes for comparison.
func taskIDs(nodes []BlockedOn) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, string(n.TaskID))
	}
	return out
}

// TestDeriveFrontier_GroupsByWaitTypeCanonicalOrder asserts the frontier buckets by
// wait_type, orders the GROUPS by the canonical priority (not input order), preserves
// input order WITHIN a group, and reports the right total.
func TestDeriveFrontier_GroupsByWaitTypeCanonicalOrder(t *testing.T) {
	// Deliberately NOT in canonical order, and two nodes share human_decision.
	in := []BlockedOn{
		bo("t-exec", WaitExecutorLiveness),
		bo("t-dec1", WaitHumanDecision),
		bo("t-up1", WaitUpstreamCompletion),
		bo("t-dec2", WaitHumanDecision),
		bo("t-up2", WaitUpstreamCompletion),
	}
	f := DeriveFrontier(in)

	if f.Total != 5 {
		t.Fatalf("Total = %d, want 5", f.Total)
	}
	// Canonical order: upstream_completion before human_decision before executor_liveness.
	wantTypes := []WaitType{WaitUpstreamCompletion, WaitHumanDecision, WaitExecutorLiveness}
	gotTypes := make([]WaitType, 0, len(f.Groups))
	for _, g := range f.Groups {
		gotTypes = append(gotTypes, g.WaitType)
	}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("group order = %v, want %v", gotTypes, wantTypes)
	}
	// Within-group input order preserved.
	if got := taskIDs(f.Groups[0].Nodes); !reflect.DeepEqual(got, []string{"t-up1", "t-up2"}) {
		t.Errorf("upstream group nodes = %v, want [t-up1 t-up2]", got)
	}
	if got := taskIDs(f.Groups[1].Nodes); !reflect.DeepEqual(got, []string{"t-dec1", "t-dec2"}) {
		t.Errorf("human_decision group nodes = %v, want [t-dec1 t-dec2]", got)
	}
	if got := taskIDs(f.Groups[2].Nodes); !reflect.DeepEqual(got, []string{"t-exec"}) {
		t.Errorf("executor group nodes = %v, want [t-exec]", got)
	}
}

// TestDeriveFrontier_Empty covers the boundary: an empty/nil snapshot list yields no
// groups and Total 0 (a fully-advancing or terminal plan).
func TestDeriveFrontier_Empty(t *testing.T) {
	for name, in := range map[string][]BlockedOn{"nil": nil, "empty": {}} {
		f := DeriveFrontier(in)
		if f.Total != 0 || len(f.Groups) != 0 {
			t.Errorf("%s: got Total=%d groups=%d, want 0/0", name, f.Total, len(f.Groups))
		}
	}
}

// TestDeriveFrontier_UnknownWaitTypeAppended asserts a wait_type outside the enum
// (defensive) is not dropped — it appears after the canonical groups in first-seen order.
func TestDeriveFrontier_UnknownWaitTypeAppended(t *testing.T) {
	in := []BlockedOn{
		bo("t-x", WaitType("mystery")),
		bo("t-up", WaitUpstreamCompletion),
	}
	f := DeriveFrontier(in)
	if len(f.Groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(f.Groups))
	}
	if f.Groups[0].WaitType != WaitUpstreamCompletion {
		t.Errorf("group[0] = %s, want upstream_completion (canonical first)", f.Groups[0].WaitType)
	}
	if f.Groups[1].WaitType != WaitType("mystery") {
		t.Errorf("group[1] = %s, want the unknown type appended last", f.Groups[1].WaitType)
	}
}

// TestDerivePendingDecisions_FiltersHumanDecision asserts ONLY human_decision waits pass
// through, in input order, and non-decision waits are excluded.
func TestDerivePendingDecisions_FiltersHumanDecision(t *testing.T) {
	in := []BlockedOn{
		bo("t-up", WaitUpstreamCompletion),
		bo("t-dec1", WaitHumanDecision),
		bo("t-exec", WaitExecutorLiveness),
		bo("t-dec2", WaitHumanDecision),
		bo("t-acc", WaitAcceptanceVerdict),
	}
	got := taskIDs(DerivePendingDecisions(in))
	if want := []string{"t-dec1", "t-dec2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pending = %v, want %v", got, want)
	}
}

// TestDerivePendingDecisions_None covers the boundary: no human_decision waits (and the
// empty list) → nil queue.
func TestDerivePendingDecisions_None(t *testing.T) {
	if got := DerivePendingDecisions([]BlockedOn{bo("t-up", WaitUpstreamCompletion)}); got != nil {
		t.Errorf("with no human_decision, got %v, want nil", got)
	}
	if got := DerivePendingDecisions(nil); got != nil {
		t.Errorf("nil input, got %v, want nil", got)
	}
}

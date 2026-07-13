package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// I103 §2 — planDetailMap (get_plan DTO) blocked_on透出: per-node blocked_on on each
// non-terminal node + the frontier aggregation (grouped by wait_type) + the pending-
// decision queue. These test the wire SHAPE the get_plan tool emits.
// =============================================================================

// dtoPlan builds a minimal graphed *pm.Plan for the DTO tests (GraphID set so it reads
// as a structured plan, though planDetailMap does not depend on that).
func dtoPlan(t *testing.T) *pm.Plan {
	t.Helper()
	p, err := pm.NewPlan(pm.NewPlanInput{
		ID: "plan-1", ProjectID: "proj-1", Name: "P", CreatorRef: "user:a",
		GraphID: "g-1", CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}
	return p
}

// nodeView is a terse PlanNodeView factory (only the fields planDetailMap reads).
func nodeView(task string, ns pm.NodeStatus) pm.PlanNodeView {
	return pm.PlanNodeView{TaskID: pm.TaskID(task), TaskStatus: pm.TaskOpen, NodeStatus: ns}
}

// nodeByTaskID finds the rendered node map with the given task_id.
func nodeByTaskID(t *testing.T, nodes any, taskID string) map[string]any {
	t.Helper()
	list, ok := nodes.([]map[string]any)
	if !ok {
		t.Fatalf("nodes not []map[string]any: %T", nodes)
	}
	for _, n := range list {
		if n["task_id"] == taskID {
			return n
		}
	}
	t.Fatalf("node %s not found in %+v", taskID, list)
	return nil
}

// TestPlanDetailMap_BlockedOnPerNode_Frontier_PendingQueue asserts the full透出: each
// non-terminal blocked node carries its blocked_on descriptor, the frontier groups by
// wait_type, and the pending-decision queue lists only the human_decision waits.
func TestPlanDetailMap_BlockedOnPerNode_Frontier_PendingQueue(t *testing.T) {
	waited := time.Unix(1_700_000_100, 0).UTC()
	detail := &pmservice.PlanDetail{
		Plan: dtoPlan(t),
		View: pm.PlanView{Nodes: []pm.PlanNodeView{
			nodeView("t-a", pm.NodeRunning),  // executor_liveness
			nodeView("t-b", pm.NodeBlocked),  // upstream_completion
			nodeView("t-dec", pm.NodeReady),  // human_decision
			nodeView("t-done", pm.NodeDone),  // terminal — no blocked_on even if a stray row exists
		}},
		BlockedOn: []pm.BlockedOn{
			{TaskID: "t-b", NodeID: "n-b", WaitType: pm.WaitUpstreamCompletion, WaitKeys: []string{"t-a"}, TriggerCondition: "A completes", WaitedSince: waited},
			{TaskID: "t-dec", NodeID: "n-dec", WaitType: pm.WaitHumanDecision, WaitKeys: []string{"t-dec"}, TriggerCondition: "a human records the decision outcome", WaitedSince: waited},
			{TaskID: "t-a", NodeID: "n-a", WaitType: pm.WaitExecutorLiveness, WaitKeys: []string{"user:x"}, TriggerCondition: "lease stays alive", WaitedSince: waited},
			// A stray snapshot for a terminal node (defensive) — must NOT attach per-node.
			{TaskID: "t-done", NodeID: "n-done", WaitType: pm.WaitTimeoutOnly},
		},
	}

	m := planDetailMap(detail)

	// (1) per-node blocked_on on the non-terminal nodes.
	bNode := nodeByTaskID(t, m["nodes"], "t-b")
	bBO, ok := bNode["blocked_on"].(map[string]any)
	if !ok {
		t.Fatalf("t-b missing blocked_on: %+v", bNode)
	}
	if bBO["wait_type"] != string(pm.WaitUpstreamCompletion) {
		t.Errorf("t-b wait_type = %v, want upstream_completion", bBO["wait_type"])
	}
	if keys, _ := bBO["wait_keys"].([]string); len(keys) != 1 || keys[0] != "t-a" {
		t.Errorf("t-b wait_keys = %v, want [t-a]", bBO["wait_keys"])
	}
	if bBO["trigger_condition"] != "A completes" {
		t.Errorf("t-b trigger_condition = %v", bBO["trigger_condition"])
	}
	if bBO["waited_since"] != waited.Format(time.RFC3339Nano) {
		t.Errorf("t-b waited_since = %v, want %v", bBO["waited_since"], waited.Format(time.RFC3339Nano))
	}
	// t-b has no deadline/on_timeout set → those keys are omitted.
	if _, present := bBO["deadline"]; present {
		t.Error("t-b blocked_on should omit deadline when unset")
	}
	if _, present := bBO["on_timeout"]; present {
		t.Error("t-b blocked_on should omit on_timeout when unset")
	}
	// (2) a TERMINAL node must NOT carry blocked_on even with a stray snapshot row.
	if _, present := nodeByTaskID(t, m["nodes"], "t-done")["blocked_on"]; present {
		t.Error("terminal node t-done must not carry blocked_on")
	}

	// (3) frontier grouped by wait_type, canonical order (upstream → human → executor).
	fr, ok := m["frontier"].(map[string]any)
	if !ok {
		t.Fatalf("no frontier in DTO: %+v", m)
	}
	if fr["total"] != 4 {
		t.Errorf("frontier total = %v, want 4", fr["total"])
	}
	groups, _ := fr["groups"].([]map[string]any)
	if len(groups) != 4 {
		t.Fatalf("frontier groups = %d, want 4 (one per distinct wait_type)", len(groups))
	}
	wantOrder := []string{
		string(pm.WaitUpstreamCompletion), string(pm.WaitHumanDecision),
		string(pm.WaitExecutorLiveness), string(pm.WaitTimeoutOnly),
	}
	for i, wt := range wantOrder {
		if groups[i]["wait_type"] != wt {
			t.Errorf("frontier group[%d] = %v, want %v", i, groups[i]["wait_type"], wt)
		}
	}

	// (4) pending-decision queue = only the human_decision wait.
	pend, ok := m["pending_decisions"].([]map[string]any)
	if !ok {
		t.Fatalf("no pending_decisions in DTO: %+v", m)
	}
	if len(pend) != 1 || pend[0]["task_id"] != "t-dec" {
		t.Fatalf("pending_decisions = %+v, want [t-dec]", pend)
	}
}

// TestBlockedOnMap_DeadlineOnTimeoutRenderedWhenSet asserts the downstream-owned
// deadline / on_timeout fields ride the DTO read-only when set (omitted otherwise,
// covered above).
func TestBlockedOnMap_DeadlineOnTimeoutRenderedWhenSet(t *testing.T) {
	deadline := time.Unix(1_700_000_500, 0).UTC()
	m := blockedOnMap(pm.BlockedOn{
		TaskID: "t-x", WaitType: pm.WaitTimeoutOnly,
		Deadline: deadline, OnTimeout: "escalate",
	})
	if m["deadline"] != deadline.Format(time.RFC3339Nano) {
		t.Errorf("deadline = %v, want %v", m["deadline"], deadline.Format(time.RFC3339Nano))
	}
	if m["on_timeout"] != "escalate" {
		t.Errorf("on_timeout = %v, want escalate", m["on_timeout"])
	}
	// wait_keys is always a non-nil slice (empty here).
	if keys, ok := m["wait_keys"].([]string); !ok || len(keys) != 0 {
		t.Errorf("wait_keys = %v, want empty non-nil slice", m["wait_keys"])
	}
}

// TestPlanDetailMap_NoBlockedOn_OmitsFrontierKeys asserts zero-regression: a plan with
// no blocked_on snapshots emits NEITHER frontier NOR pending_decisions (the keys are
// absent, exactly as before I103), and no node carries a blocked_on field.
func TestPlanDetailMap_NoBlockedOn_OmitsFrontierKeys(t *testing.T) {
	detail := &pmservice.PlanDetail{
		Plan: dtoPlan(t),
		View: pm.PlanView{Nodes: []pm.PlanNodeView{nodeView("t-a", pm.NodeReady)}},
		// BlockedOn nil.
	}
	m := planDetailMap(detail)
	if _, present := m["frontier"]; present {
		t.Error("frontier key present with no blocked_on snapshots (regression)")
	}
	if _, present := m["pending_decisions"]; present {
		t.Error("pending_decisions key present with no blocked_on snapshots (regression)")
	}
	if _, present := nodeByTaskID(t, m["nodes"], "t-a")["blocked_on"]; present {
		t.Error("node carries blocked_on with no snapshots (regression)")
	}
}

// TestPlanDetailMap_NoPendingDecisions_OmitsQueueKeepsFrontier asserts the queue key is
// omitted when there are blocked nodes but NONE is a human_decision — the frontier still
// renders (the un-advanced nodes) but pending_decisions is absent.
func TestPlanDetailMap_NoPendingDecisions_OmitsQueueKeepsFrontier(t *testing.T) {
	detail := &pmservice.PlanDetail{
		Plan: dtoPlan(t),
		View: pm.PlanView{Nodes: []pm.PlanNodeView{nodeView("t-b", pm.NodeBlocked)}},
		BlockedOn: []pm.BlockedOn{
			{TaskID: "t-b", WaitType: pm.WaitUpstreamCompletion, WaitKeys: []string{"t-a"}},
		},
	}
	m := planDetailMap(detail)
	if _, present := m["frontier"]; !present {
		t.Error("frontier should render for a blocked (non-decision) node")
	}
	if _, present := m["pending_decisions"]; present {
		t.Error("pending_decisions must be absent when no human_decision wait exists")
	}
}

package orchestration

import (
	"testing"
	"time"
)

func TestParseConditionConfig(t *testing.T) {
	meta := map[string]any{
		"evaluator":       "upstream_outcome",
		"logic":           "and",
		"on_success":      []any{"node-3", "node-4"},
		"on_failure":      []any{"node-1"},
		"max_rounds":      float64(3),
		"on_max_exceeded": "discard",
	}
	cfg, err := ParseConditionConfig(meta)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Evaluator != EvaluatorUpstream {
		t.Fatalf("evaluator = %s, want upstream_outcome", cfg.Evaluator)
	}
	if cfg.Logic != LogicAnd {
		t.Fatalf("logic = %s, want and", cfg.Logic)
	}
	if len(cfg.OnSuccess) != 2 || cfg.OnSuccess[0] != "node-3" {
		t.Fatalf("on_success = %v", cfg.OnSuccess)
	}
	if len(cfg.OnFailure) != 1 || cfg.OnFailure[0] != "node-1" {
		t.Fatalf("on_failure = %v", cfg.OnFailure)
	}
	if cfg.MaxRounds != 3 {
		t.Fatalf("max_rounds = %d, want 3", cfg.MaxRounds)
	}
	if cfg.OnMaxExceeded != MaxExceededDiscard {
		t.Fatalf("on_max_exceeded = %s, want discard", cfg.OnMaxExceeded)
	}
}

func TestEvaluateUpstream_And_AllSuccess(t *testing.T) {
	g := buildConditionTestGraph(t)
	// Complete both upstream nodes with success
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("success", t0)

	cfg := ConditionConfig{Evaluator: EvaluatorUpstream, Logic: LogicAnd}
	ok, err := EvaluateUpstream(g, "cond", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected success (all upstream success with AND)")
	}
}

func TestEvaluateUpstream_And_OneFailed(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	cfg := ConditionConfig{Evaluator: EvaluatorUpstream, Logic: LogicAnd}
	ok, err := EvaluateUpstream(g, "cond", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected failure (one upstream failure with AND)")
	}
}

func TestEvaluateUpstream_Or_OneSuccess(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("success", t0)

	cfg := ConditionConfig{Evaluator: EvaluatorUpstream, Logic: LogicOr}
	ok, err := EvaluateUpstream(g, "cond", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected success (one upstream success with OR)")
	}
}

func TestApplyConditionResult_Success(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("success", t0)

	cfg := ConditionConfig{
		OnSuccess: []NodeID{"downstream"},
	}
	// Add downstream node
	g.AddNode(NewNodeInput{ID: "downstream", Category: NodeCategoryBusiness, Title: "DS"})
	g.AddEdge("cond", "downstream")

	err := ApplyConditionResult(g, "cond", cfg, true, t0)
	if err != nil {
		t.Fatal(err)
	}
	// condition node itself should be completed
	if g.FindNode("cond").Status() != NodeCompleted {
		t.Fatalf("condition status = %s, want completed", g.FindNode("cond").Status())
	}
}

func TestApplyConditionResult_Failure_ReopensChain(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("success", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	// OnFailure targets both a and b explicitly so both are reopened.
	cfg := ConditionConfig{
		OnFailure: []NodeID{"a", "b"},
	}
	err := ApplyConditionResult(g, "cond", cfg, false, t0)
	if err != nil {
		t.Fatal(err)
	}

	// a and b should be reopened (both are explicit on_failure targets)
	if g.FindNode("a").Status() != NodeReopen {
		t.Fatalf("a status = %s, want reopen", g.FindNode("a").Status())
	}
	if g.FindNode("b").Status() != NodeReopen {
		t.Fatalf("b status = %s, want reopen", g.FindNode("b").Status())
	}
	// condition should be reopened too (so it can re-evaluate)
	if g.FindNode("cond").Status() != NodeReopen {
		t.Fatalf("cond status = %s, want reopen", g.FindNode("cond").Status())
	}
}

func TestApplyConditionResult_MaxRoundsExceeded_Discard(t *testing.T) {
	g := buildConditionTestGraph(t)

	cfg := ConditionConfig{
		OnFailure:     []NodeID{"a", "b"},
		MaxRounds:     1,
		OnMaxExceeded: MaxExceededDiscard,
	}

	// Round 1: complete upstreams, apply failure → reopen chain
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	err := ApplyConditionResult(g, "cond", cfg, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	// After round 1: cond should be reopened
	if g.FindNode("cond").Status() != NodeReopen {
		t.Fatalf("after round 1: cond status = %s, want reopen", g.FindNode("cond").Status())
	}

	// Round 2: re-complete upstreams, apply failure again → max exceeded → discard
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	err = ApplyConditionResult(g, "cond", cfg, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	if g.FindNode("cond").Status() != NodeDiscarded {
		t.Fatalf("after round 2: cond status = %s, want discarded", g.FindNode("cond").Status())
	}
}

func TestApplyConditionResult_MaxRoundsExceeded_ForceSuccess(t *testing.T) {
	g := buildConditionTestGraph(t)
	cfg := ConditionConfig{
		OnFailure:     []NodeID{"a", "b"},
		MaxRounds:     1,
		OnMaxExceeded: MaxExceededForceSuccess,
	}

	// Round 1
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)
	if err := ApplyConditionResult(g, "cond", cfg, false, t0); err != nil {
		t.Fatal(err)
	}

	// Round 2: max exceeded → force success
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)
	err := ApplyConditionResult(g, "cond", cfg, false, t0)
	if err != nil {
		t.Fatal(err)
	}
	if g.FindNode("cond").Status() != NodeCompleted {
		t.Fatalf("cond status = %s, want completed (force_success)", g.FindNode("cond").Status())
	}
	if g.FindNode("cond").Outcome() != "force_success" {
		t.Fatalf("cond outcome = %s, want force_success", g.FindNode("cond").Outcome())
	}
}

func TestEvaluateUpstream_Or_AllFail(t *testing.T) {
	g := buildConditionTestGraph(t)
	g.FindNode("a").Start(t0)
	g.FindNode("a").Complete("failure", t0)
	g.FindNode("b").Start(t0)
	g.FindNode("b").Complete("failure", t0)

	cfg := ConditionConfig{Evaluator: EvaluatorUpstream, Logic: LogicOr}
	ok, err := EvaluateUpstream(g, "cond", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected failure (all upstream failure with OR)")
	}
}

// buildConditionTestGraph creates: a -> cond, b -> cond (two upstream business nodes)
func buildConditionTestGraph(t *testing.T) *Graph {
	t.Helper()
	g, err := NewGraph(NewGraphInput{ID: "g1", PlanID: "p1", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	g.AddNode(NewNodeInput{ID: "a", Category: NodeCategoryBusiness, Title: "A"})
	g.AddNode(NewNodeInput{ID: "b", Category: NodeCategoryBusiness, Title: "B"})
	g.AddNode(NewNodeInput{
		ID: "cond", Category: NodeCategoryControl,
		ControlKind: ControlKindCondition, Title: "Check",
	})
	g.AddEdge("a", "cond")
	g.AddEdge("b", "cond")
	return g
}

// Ensure t0 is used (it's declared in node_test.go in the same package)
var _ = time.UTC

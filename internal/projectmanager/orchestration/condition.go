package orchestration

import (
	"errors"
	"fmt"
	"time"
)

// EvaluatorKind determines how a condition node computes its result.
type EvaluatorKind string

const (
	EvaluatorUpstream EvaluatorKind = "upstream_outcome"
	EvaluatorHook     EvaluatorKind = "external_hook"
	EvaluatorManual   EvaluatorKind = "manual"
)

// LogicKind for upstream_outcome aggregation.
type LogicKind string

const (
	LogicAnd LogicKind = "and"
	LogicOr  LogicKind = "or"
)

// MaxExceededAction determines what happens when max_rounds is exceeded.
type MaxExceededAction string

const (
	MaxExceededForceSuccess MaxExceededAction = "force_success"
	MaxExceededDiscard      MaxExceededAction = "discard"
)

// ConditionConfig holds the evaluation rules for a condition node.
type ConditionConfig struct {
	Evaluator        EvaluatorKind
	Logic            LogicKind // for upstream_outcome
	HookURL          string    // for external_hook
	HookMethod       string    // for external_hook
	SuccessCondition string    // for external_hook
	OnSuccess        []NodeID
	OnFailure        []NodeID
	MaxRounds        int // 0 = unlimited
	OnMaxExceeded    MaxExceededAction
}

// ParseConditionConfig extracts a ConditionConfig from a node's metadata map.
func ParseConditionConfig(metadata map[string]any) (ConditionConfig, error) {
	var cfg ConditionConfig

	if v, ok := metadata["evaluator"].(string); ok {
		cfg.Evaluator = EvaluatorKind(v)
	}
	if v, ok := metadata["logic"].(string); ok {
		cfg.Logic = LogicKind(v)
	}
	if v, ok := metadata["hook_url"].(string); ok {
		cfg.HookURL = v
	}
	if v, ok := metadata["hook_method"].(string); ok {
		cfg.HookMethod = v
	}
	if v, ok := metadata["success_condition"].(string); ok {
		cfg.SuccessCondition = v
	}
	if v, ok := metadata["on_success"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				cfg.OnSuccess = append(cfg.OnSuccess, NodeID(s))
			}
		}
	}
	if v, ok := metadata["on_failure"].([]any); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				cfg.OnFailure = append(cfg.OnFailure, NodeID(s))
			}
		}
	}
	if v, ok := metadata["max_rounds"].(float64); ok {
		cfg.MaxRounds = int(v)
	}
	if v, ok := metadata["on_max_exceeded"].(string); ok {
		cfg.OnMaxExceeded = MaxExceededAction(v)
	}
	// Validate evaluator if provided.
	if cfg.Evaluator != "" {
		switch cfg.Evaluator {
		case EvaluatorUpstream, EvaluatorHook, EvaluatorManual:
			// valid
		default:
			return ConditionConfig{}, fmt.Errorf("orchestration: unknown evaluator %q", cfg.Evaluator)
		}
	}
	return cfg, nil
}

// EvaluateUpstream checks upstream business nodes' outcomes using AND/OR logic.
// Returns true if the condition passes.
func EvaluateUpstream(g *Graph, conditionNodeID NodeID, cfg ConditionConfig) (bool, error) {
	edges := g.Edges()
	// Find all direct upstream nodes of the condition.
	var upstreamIDs []NodeID
	for _, e := range edges {
		if e.ToNodeID == conditionNodeID {
			upstreamIDs = append(upstreamIDs, e.FromNodeID)
		}
	}
	if len(upstreamIDs) == 0 {
		return false, errors.New("orchestration: condition node has no upstream nodes")
	}

	logic := cfg.Logic
	if logic == "" {
		logic = LogicAnd
	}

	businessCount := 0
	for _, id := range upstreamIDs {
		n := g.FindNode(id)
		if n == nil {
			return false, fmt.Errorf("orchestration: upstream node %s not found", id)
		}
		if n.Category() == NodeCategoryControl {
			continue // skip control nodes
		}
		businessCount++
		isSuccess := n.Status() == NodeCompleted && n.Outcome() == "success"

		if logic == LogicAnd && !isSuccess {
			return false, nil
		}
		if logic == LogicOr && isSuccess {
			return true, nil
		}
	}
	if businessCount == 0 {
		return false, errors.New("orchestration: condition node has no upstream business nodes")
	}

	if logic == LogicAnd {
		return true, nil // all passed
	}
	return false, nil // none passed (OR)
}

// ApplyConditionResult routes the condition's success/failure. On failure, it
// reopens the entire chain from the condition back to each on_failure target,
// plus all other completed upstream business nodes so they can re-run.
func ApplyConditionResult(g *Graph, conditionNodeID NodeID, cfg ConditionConfig, success bool, at time.Time) error {
	condNode := g.FindNode(conditionNodeID)
	if condNode == nil {
		return ErrNodeNotFound
	}

	if success {
		// Mark condition as completed with success outcome.
		if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
			if err := condNode.Start(at); err != nil {
				return err
			}
		}
		if condNode.Status() == NodeRunning {
			return condNode.Complete("success", at)
		}
		return nil
	}

	// Failure path: check max rounds.
	round := countReopens(condNode) + 1
	if cfg.MaxRounds > 0 && round > cfg.MaxRounds {
		switch cfg.OnMaxExceeded {
		case MaxExceededForceSuccess:
			if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
				if err := condNode.Start(at); err != nil {
					return err
				}
			}
			return condNode.Complete("force_success", at)
		case MaxExceededDiscard:
			if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
				if err := condNode.Start(at); err != nil {
					return err
				}
			}
			return condNode.Discard(at)
		default:
			return condNode.Discard(at)
		}
	}

	edges := g.Edges()

	// Collect the set of nodes to reopen from the on_failure chains.
	toReopen := map[NodeID]bool{}
	for _, targetID := range cfg.OnFailure {
		chain := ReopenChain(edges, conditionNodeID, targetID)
		for _, nid := range chain {
			toReopen[nid] = true
		}
	}

	// Reopen all collected nodes.
	for nid := range toReopen {
		n := g.FindNode(nid)
		if n == nil {
			continue
		}
		if n.Status() == NodeCompleted {
			if err := n.Reopen(fmt.Sprintf("reactivated_by:%s,round:%d", conditionNodeID, round), at); err != nil {
				return err
			}
		}
	}

	// Reopen the condition node itself so it can re-evaluate.
	// Must cycle through running->completed first if not already completed.
	if condNode.Status() == NodeOpen || condNode.Status() == NodeReopen {
		if err := condNode.Start(at); err != nil {
			return err
		}
	}
	if condNode.Status() == NodeRunning {
		if err := condNode.Complete(fmt.Sprintf("failure,round:%d", round), at); err != nil {
			return err
		}
	}
	if condNode.Status() == NodeCompleted {
		return condNode.Reopen(fmt.Sprintf("failure,round:%d", round), at)
	}
	return nil
}

// countReopens counts how many times a node has been reopened (from action logs).
func countReopens(n *Node) int {
	count := 0
	for _, log := range n.ActionLogs() {
		if log.Action == "reopened" {
			count++
		}
	}
	return count
}

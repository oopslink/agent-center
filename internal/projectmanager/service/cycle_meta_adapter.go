package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TaskRepoCycleMeta is the concrete CycleNodeMetaPort that repairs F4 (v2.13.0
// I18/F3 — docs/design/v2.13.0/cycle-node-graph-spec.md §5). F4 reads per-node
// cycle metadata (role/branch/base/skip_merge_check) through the nil-safe
// CycleNodeMetaPort, but no concrete adapter was wired (s.cycleMeta == nil), so
// the unmerged-branch board was ALWAYS empty. Now that F3 (0067) persists the
// node role on pm_tasks, this adapter rebuilds the per-plan metadata map straight
// from the task rows — no separate storage, no second source of truth.
//
// It wraps a pm.TaskRepository and projects ListByPlan into
// map[pm.TaskID]pm.CycleNodeMeta, including ONLY tasks whose Role() is non-empty
// (a node actually built by scaffold_cycle_plan). An ordinary backlog task (role
// "") carries no cycle metadata, so it is omitted — a missing key reads as "no
// metadata / not an Integrate node", exactly the contract F4's projection expects.
type TaskRepoCycleMeta struct {
	tasks pm.TaskRepository
}

// NewTaskRepoCycleMeta wraps a TaskRepository as a CycleNodeMetaPort. The same
// repo F2 writes through (scaffold_cycle_plan) is read here — single source of
// truth for the cycle metadata.
func NewTaskRepoCycleMeta(tasks pm.TaskRepository) *TaskRepoCycleMeta {
	return &TaskRepoCycleMeta{tasks: tasks}
}

// CycleNodeMeta lists a plan's tasks and projects each role-bearing node into its
// CycleNodeMeta. Tasks with an empty role (ordinary backlog work, not scaffolded)
// are skipped so the map keys ONLY genuine cycle nodes. An empty/unscaffolded plan
// returns an empty (non-nil) map without error.
func (a *TaskRepoCycleMeta) CycleNodeMeta(ctx context.Context, planID pm.PlanID) (map[pm.TaskID]pm.CycleNodeMeta, error) {
	tasks, err := a.tasks.ListByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	out := make(map[pm.TaskID]pm.CycleNodeMeta, len(tasks))
	for _, t := range tasks {
		if t.Role() == "" {
			continue // no cycle role → not a scaffolded node; omit (missing == "no meta")
		}
		out[t.ID()] = pm.CycleNodeMeta{
			Role:           t.Role(),
			Branch:         t.Branch(),
			Base:           t.Base(),
			SkipMergeCheck: t.SkipMergeCheck(),
		}
	}
	return out, nil
}

// compile-time interface check.
var _ CycleNodeMetaPort = (*TaskRepoCycleMeta)(nil)

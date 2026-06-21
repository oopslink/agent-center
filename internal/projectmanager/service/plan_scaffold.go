package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// scaffold_cycle_plan (v2.13.0 I18/F2) — server-side generation of a whole cycle
// node-graph from the F1 spec (docs/design/v2.13.0/cycle-node-graph-spec.md).
//
// DAG shape:  S0 → (Dev → Review → Integrate) × N → 集成完成 Gate → Accept → Ship
//   - every feature's Dev is blocked_by S0;
//   - per feature the chain is Dev → Review → Integrate (Review per-feature);
//   - the Gate is a barrier blocked_by EVERY feature's terminal node;
//   - Accept is blocked_by the Gate; Ship is blocked_by Accept.
//
// It is STRUCTURE-ONLY: every node is created UNASSIGNED — the PD assigns owners
// per node after scaffolding (oopslink 2026-06-21: "工具生成结构，指派给谁 PD 自己决定").
// Each node carries the cycle git metadata: branch (default the node's T<n>) + base
// (dev/<version>; the S0 node + Ship use main as the cross-trunk reference), which
// F3's Integrate-complete guard reads. A pure-doc feature (DocOnly) collapses to a
// single Dev node with skip_merge_check=true (structural merge-check exemption) and
// still counts toward the Gate.
//
// Implementation: it COMPOSES the existing, already-tested AppService primitives
// (CreatePlan / CreateTask / SelectTaskIntoPlan / AddPlanDependency) rather than
// re-implementing the plan/task/conversation projection logic. Each primitive runs
// in its own tx; the produced plan is a DRAFT (freely editable), so a partial
// failure leaves an editable draft the caller can delete + retry — no orphaned
// running state. NO new domain logic.
// =============================================================================

// ErrScaffoldVersionRequired / ErrScaffoldNoFeatures / ErrScaffoldFeatureNameRequired
// are the scaffold input-validation sentinels (surfaced as 422 by the tool layer).
var (
	ErrScaffoldVersionRequired     = errors.New("projectmanager: scaffold_cycle_plan requires a non-empty version")
	ErrScaffoldNoFeatures          = errors.New("projectmanager: scaffold_cycle_plan requires at least one feature")
	ErrScaffoldFeatureNameRequired = errors.New("projectmanager: scaffold_cycle_plan feature name required")
)

// CycleFeature is one feature in the cycle plan. Name is the human label used in
// node titles; Branch is the shared feature branch for its Dev/Review/Integrate
// chain (default = the Dev node's T<n>); DocOnly collapses the chain to a single
// Dev node (no Review/Integrate) exempt from the F3 merge-check guard.
type CycleFeature struct {
	Name    string
	Branch  string
	DocOnly bool
}

// ScaffoldCyclePlanCommand is the input to ScaffoldCyclePlan.
type ScaffoldCyclePlanCommand struct {
	ProjectID pm.ProjectID
	Version   string // e.g. "v2.13.0" — the integration trunk is dev/<version>
	Features  []CycleFeature
	CreatedBy pm.IdentityRef
}

// ScaffoldCycleNode describes one created node in the returned summary.
type ScaffoldCycleNode struct {
	TaskID  pm.TaskID `json:"task_id"`
	Title   string    `json:"title"`
	Branch  string    `json:"branch"`
	Base    string    `json:"base"`
	Kind    string    `json:"kind"`    // s0|dev|review|integrate|gate|accept|ship
	Feature string    `json:"feature"` // owning feature name (empty for s0/gate/accept/ship)
}

// ScaffoldCyclePlanResult is what ScaffoldCyclePlan returns: the new draft plan id
// + the created nodes (in creation order). Owners are intentionally absent (the PD
// assigns them next).
type ScaffoldCyclePlanResult struct {
	PlanID pm.PlanID           `json:"plan_id"`
	Nodes  []ScaffoldCycleNode `json:"nodes"`
}

// ScaffoldCyclePlan builds the whole cycle node-graph (see file header) and returns
// the draft plan id + the created nodes. The CreatedBy actor must be a member of the
// project (enforced by the composed CreatePlan/CreateTask AppServices).
func (s *Service) ScaffoldCyclePlan(ctx context.Context, cmd ScaffoldCyclePlanCommand) (ScaffoldCyclePlanResult, error) {
	var zero ScaffoldCyclePlanResult
	if s.plans == nil {
		return zero, ErrPlansUnavailable
	}
	if err := cmd.CreatedBy.Validate(); err != nil {
		return zero, err
	}
	version := strings.TrimSpace(cmd.Version)
	if version == "" {
		return zero, ErrScaffoldVersionRequired
	}
	if len(cmd.Features) == 0 {
		return zero, ErrScaffoldNoFeatures
	}
	for _, f := range cmd.Features {
		if strings.TrimSpace(f.Name) == "" {
			return zero, ErrScaffoldFeatureNameRequired
		}
	}

	trunk := "dev/" + version // the integration main branch for this cycle

	// 1) draft plan.
	planID, err := s.CreatePlan(ctx, CreatePlanCommand{
		ProjectID: cmd.ProjectID,
		Name:      version + " — cycle 节点图",
		Description: "由 scaffold_cycle_plan 生成的 cycle 节点图（" + version + "）。形状 " +
			"S0→(Dev→Review→Integrate)×N→集成完成 Gate→Accept→Ship。节点 owner 留空待 PD 指派。" +
			"规格见 docs/design/v2.13.0/cycle-node-graph-spec.md。",
		CreatedBy: cmd.CreatedBy,
	})
	if err != nil {
		return zero, err
	}
	result := ScaffoldCyclePlanResult{PlanID: planID}

	// addNode creates an UNASSIGNED task with cycle metadata + selects it into the
	// plan, appends to the summary, and returns its id.
	addNode := func(title, branch, base, kind, feature string, skip bool) (pm.TaskID, error) {
		tid, cerr := s.CreateTask(ctx, CreateTaskCommand{
			ProjectID:      cmd.ProjectID,
			Title:          title,
			CreatedBy:      cmd.CreatedBy,
			Branch:         branch,
			Base:           base,
			SkipMergeCheck: skip,
			// Persist the computed node kind as the cycle-node ROLE (v2.13.0 I18/F3):
			// it is what F3's Integrate-complete merge guard + F4's board key on
			// (Dev/Review/Integrate share branch/base — §4.2 — so role discriminates).
			Role: pm.CycleNodeRole(kind),
		})
		if cerr != nil {
			return "", cerr
		}
		if serr := s.SelectTaskIntoPlan(ctx, planID, tid, cmd.CreatedBy); serr != nil {
			return "", serr
		}
		result.Nodes = append(result.Nodes, ScaffoldCycleNode{
			TaskID: tid, Title: title, Branch: branch, Base: base, Kind: kind, Feature: feature,
		})
		return tid, nil
	}
	// dependsOn wires `from` blocked_by `to` (from dispatches only after to is done).
	dependsOn := func(from, to pm.TaskID) error {
		return s.AddPlanDependency(ctx, planID, from, to, cmd.CreatedBy)
	}

	// 2) S0 — cut dev/<version> from main. branch=trunk, base=main.
	s0, err := addNode("S0 开发主分支 — 切 "+trunk, trunk, "main", "s0", "", false)
	if err != nil {
		return result, err
	}

	// 3) per-feature chains; collect each chain's terminal node for the Gate.
	var terminals []pm.TaskID
	for _, f := range cmd.Features {
		name := strings.TrimSpace(f.Name)
		branch := strings.TrimSpace(f.Branch)

		// Dev node. If no explicit branch, default it to the Dev node's own T<n>
		// (F1 §4.1) — resolved after create, then applied to the whole chain.
		devID, derr := addNode(name+" · Dev", branch, trunk, "dev", name, f.DocOnly)
		if derr != nil {
			return result, derr
		}
		if branch == "" {
			resolved, rerr := s.resolveDefaultBranch(ctx, devID, trunk, f.DocOnly)
			if rerr != nil {
				return result, rerr
			}
			branch = resolved
			result.Nodes[len(result.Nodes)-1].Branch = branch // reflect in summary
		}
		if err := dependsOn(devID, s0); err != nil {
			return result, err
		}

		terminal := devID
		if !f.DocOnly {
			reviewID, rerr := addNode(name+" · Review", branch, trunk, "review", name, false)
			if rerr != nil {
				return result, rerr
			}
			if err := dependsOn(reviewID, devID); err != nil {
				return result, err
			}
			integrateID, ierr := addNode(name+" · Integrate", branch, trunk, "integrate", name, false)
			if ierr != nil {
				return result, ierr
			}
			if err := dependsOn(integrateID, reviewID); err != nil {
				return result, err
			}
			terminal = integrateID
		}
		terminals = append(terminals, terminal)
	}

	// 4) 集成完成 Gate — barrier blocked_by every feature's terminal node.
	gate, err := addNode("集成完成 Gate — PD 关门核对", "", trunk, "gate", "", false)
	if err != nil {
		return result, err
	}
	for _, t := range terminals {
		if err := dependsOn(gate, t); err != nil {
			return result, err
		}
	}

	// 5) Accept — blocked_by the Gate (verify on the integrated trunk).
	accept, err := addNode("Accept 验收（集成后主干整体）", "", trunk, "accept", "", false)
	if err != nil {
		return result, err
	}
	if err := dependsOn(accept, gate); err != nil {
		return result, err
	}

	// 6) Ship — blocked_by Accept. branch=trunk, base=main (trunk → main + tag).
	ship, err := addNode("Ship "+version, trunk, "main", "ship", "", false)
	if err != nil {
		return result, err
	}
	if err := dependsOn(ship, accept); err != nil {
		return result, err
	}

	return result, nil
}

// resolveDefaultBranch stamps a Dev node whose branch was left to default with its
// own T<n> (F1 §4.1) and returns that branch so the rest of the feature chain shares
// it. Falls back to the task id when the org number is unallocated (T<n> would be
// "T0"). A plain repo read/update (no tx) — the produced plan is a draft.
func (s *Service) resolveDefaultBranch(ctx context.Context, devID pm.TaskID, base string, skip bool) (string, error) {
	t, err := s.tasks.FindByID(ctx, devID)
	if err != nil {
		return "", err
	}
	branch := string(devID)
	if t.OrgNumber() > 0 {
		branch = "T" + strconv.Itoa(t.OrgNumber())
	}
	// Re-stamp branch/base/skip but PRESERVE the node's role (v2.13.0 I18/F3 —
	// SetCycleMeta now carries role; pass t.Role() back so the Dev role survives the
	// default-branch resolution, otherwise F3/F4 would lose the discriminator).
	if err := t.SetCycleMeta(t.Role(), branch, base, skip, s.clock.Now()); err != nil {
		return "", err
	}
	if err := s.tasks.Update(ctx, t); err != nil {
		return "", err
	}
	return branch, nil
}

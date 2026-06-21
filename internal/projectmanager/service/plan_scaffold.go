package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// scaffold_cycle_plan (v2.13.0 I18/F2 + B2) — server-side generation of a whole
// cycle CONTROL-FLOW graph from the B0 engine spec §9
// (docs/design/v2.13.0/control-flow-engine-spec.md), which redefines the
// per-feature chain from a linear DAG into
// Dev→Review→Decision{pass→Integrate, reject→Dev(bounded)}.
//
// Graph shape:  S0 → (per feature ↓) → 集成完成 Gate → Accept → Ship
//
//	        ┌──────────── loopback(when=reject, max=N) ────────────┐
//	        ▼                                                       │
//	S0 → Dev ─seq→ Review ─seq→ Decision ─conditional(pass)→ Integrate → (Gate)
//	                              │
//	                              └─conditional(reject_exhausted)→ Escape(人工兜底)
//
//   - every feature's Dev is blocked_by S0 (seq);
//   - Review depends_on Dev, Decision depends_on Review (seq);
//   - Integrate is a CONDITIONAL successor of Decision (active only on outcome=pass)
//     — it is the feature's terminal that feeds the Gate;
//   - a bounded LOOPBACK edge Decision→Dev (when=reject, MaxRounds=N) re-runs the
//     Dev→Review→Decision subgraph on a review reject, capped at N rounds (B0 §4);
//   - an Escape node is the CONDITIONAL successor of Decision on outcome
//     `reject_exhausted` — the §4.1 escape branch the engine routes to when the
//     loopback exhausts. Scaffold ALWAYS wires it (B0 §10 "双保险": a stuck loop
//     surfaces to a human node, not only a creator notification);
//   - the Gate is a barrier blocked_by EVERY feature's terminal (Integrate, or the
//     lone Dev for a doc-only feature);
//   - Accept is blocked_by the Gate; Ship is blocked_by Accept.
//
// A pure-doc feature (DocOnly) keeps the F2 collapse: a single Dev node with
// skip_merge_check=true, no Review/Decision/Integrate/loopback/escape.
//
// It is STRUCTURE-ONLY: every node is created UNASSIGNED — the PD assigns owners
// per node after scaffolding (oopslink 2026-06-21: "工具生成结构，指派给谁 PD 自己决定").
// Each node carries the cycle git metadata: branch (default the node's T<n>) + base
// (dev/<version>; the S0 node + Ship use main as the cross-trunk reference), plus
// its ROLE; F3's Integrate-complete guard + F4's board read them.
//
// Implementation: it COMPOSES the existing, already-tested AppService primitives
// (CreatePlan / CreateTask / SelectTaskIntoPlan / addPlanEdge) rather than
// re-implementing the plan/task/conversation projection logic. Each primitive runs
// in its own tx; the produced plan is a DRAFT (freely editable), so a partial
// failure leaves an editable draft the caller can delete + retry — no orphaned
// running state. NO new domain logic — the control-flow edges are the B1 engine's
// own conditional/loopback primitives, just wired into the cycle shape here.
// =============================================================================

// cycle control-flow outcome labels (B0 §9): a Decision node routes its conditional
// out-edges by these. reject is the bounded loopback's label; reject_exhausted is
// the escape label the engine records when the loop exhausts.
const (
	cycleOutcomePass            = "pass"
	cycleOutcomeReject          = "reject"
	cycleOutcomeRejectExhausted = "reject_exhausted"
	// defaultReviewRounds is the loopback bound when the caller leaves MaxReviewRounds
	// unset (B0 §4.1 suggests 3).
	defaultReviewRounds = 3
)

// ErrScaffoldVersionRequired / ErrScaffoldNoFeatures / ErrScaffoldFeatureNameRequired
// are the scaffold input-validation sentinels (surfaced as 422 by the tool layer).
var (
	ErrScaffoldVersionRequired     = errors.New("projectmanager: scaffold_cycle_plan requires a non-empty version")
	ErrScaffoldNoFeatures          = errors.New("projectmanager: scaffold_cycle_plan requires at least one feature")
	ErrScaffoldFeatureNameRequired = errors.New("projectmanager: scaffold_cycle_plan feature name required")
)

// CycleFeature is one feature in the cycle plan. Name is the human label used in
// node titles; Branch is the shared feature branch for its Dev/Review/Decision/
// Integrate chain (default = the Dev node's T<n>); DocOnly collapses the chain to a
// single Dev node (no Review/Decision/Integrate) exempt from the F3 merge-check guard.
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
	// MaxReviewRounds bounds each feature's review loopback (Decision→Dev on reject),
	// B0 §4.1. <=0 falls back to defaultReviewRounds (3). Ignored for doc-only features.
	MaxReviewRounds int
	CreatedBy       pm.IdentityRef
}

// ScaffoldCycleNode describes one created node in the returned summary.
type ScaffoldCycleNode struct {
	TaskID  pm.TaskID `json:"task_id"`
	Title   string    `json:"title"`
	Branch  string    `json:"branch"`
	Base    string    `json:"base"`
	Kind    string    `json:"kind"`    // s0|dev|review|decision|integrate|escape|gate|accept|ship
	Feature string    `json:"feature"` // owning feature name (empty for s0/gate/accept/ship)
}

// ScaffoldCycleEdge describes one created control-flow edge in the returned summary
// (B2): kind ∈ seq|conditional|loopback; when carries the routing outcome label for
// conditional/loopback (empty for seq); max_rounds is the loopback bound (0 otherwise).
type ScaffoldCycleEdge struct {
	From      pm.TaskID `json:"from"` // the dependent node (runs AFTER To, conditionally for conditional edges)
	To        pm.TaskID `json:"to"`   // the prerequisite/decision node
	Kind      string    `json:"kind"`
	When      string    `json:"when,omitempty"`
	MaxRounds int       `json:"max_rounds,omitempty"`
}

// ScaffoldCyclePlanResult is what ScaffoldCyclePlan returns: the new draft plan id
// + the created nodes and control-flow edges (in creation order). Owners are
// intentionally absent (the PD assigns them next).
type ScaffoldCyclePlanResult struct {
	PlanID pm.PlanID           `json:"plan_id"`
	Nodes  []ScaffoldCycleNode `json:"nodes"`
	Edges  []ScaffoldCycleEdge `json:"edges"`
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
	maxRounds := cmd.MaxReviewRounds
	if maxRounds <= 0 {
		maxRounds = defaultReviewRounds
	}

	// 1) draft plan.
	planID, err := s.CreatePlan(ctx, CreatePlanCommand{
		ProjectID: cmd.ProjectID,
		Name:      version + " — cycle 控制流图",
		Description: "由 scaffold_cycle_plan 生成的 cycle 控制流图（" + version + "）。形状 " +
			"S0→(Dev→Review→Decision{过→Integrate, 打回→回 Dev 有界})×N→集成完成 Gate→Accept→Ship。" +
			"Decision 产出 pass/reject，reject 走有界 loopback 重做、超限走 reject_exhausted 逃生节点。" +
			"节点 owner 留空待 PD 指派。规格见 docs/design/v2.13.0/control-flow-engine-spec.md。",
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
	// addEdge wires one control-flow edge (any Kind) and records it in the summary.
	// Forward (seq/conditional) edges must be added before the loopback back-edge so
	// the loopback's ancestry validation (ValidateLoopback) sees them (B0 §6.6).
	addEdge := func(dep pm.Dependency) error {
		dep.PlanID = planID
		if err := s.addPlanEdge(ctx, planID, dep, cmd.CreatedBy); err != nil {
			return err
		}
		result.Edges = append(result.Edges, ScaffoldCycleEdge{
			From: dep.FromTaskID, To: dep.ToTaskID,
			Kind: string(pm.NormalizeEdgeKind(dep.Kind)), When: dep.When, MaxRounds: dep.MaxRounds,
		})
		return nil
	}
	// dependsOn wires `from` blocked_by `to` (a plain seq edge: from dispatches only
	// after to is done).
	dependsOn := func(from, to pm.TaskID) error {
		return addEdge(pm.Dependency{FromTaskID: from, ToTaskID: to, Kind: pm.EdgeSeq})
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
			// Dev ─seq→ Review ─seq→ Decision (the forward review chain).
			reviewID, rerr := addNode(name+" · Review", branch, trunk, "review", name, false)
			if rerr != nil {
				return result, rerr
			}
			if err := dependsOn(reviewID, devID); err != nil {
				return result, err
			}
			decisionID, derr := addNode(name+" · Decision（评审结论 pass/reject）", branch, trunk, "decision", name, false)
			if derr != nil {
				return result, derr
			}
			if err := dependsOn(decisionID, reviewID); err != nil {
				return result, err
			}
			// Integrate is the CONDITIONAL successor of Decision on outcome=pass — the
			// feature terminal that feeds the Gate.
			integrateID, ierr := addNode(name+" · Integrate", branch, trunk, "integrate", name, false)
			if ierr != nil {
				return result, ierr
			}
			if err := addEdge(pm.Dependency{
				FromTaskID: integrateID, ToTaskID: decisionID,
				Kind: pm.EdgeConditional, When: cycleOutcomePass,
			}); err != nil {
				return result, err
			}
			// Escape node is the CONDITIONAL successor of Decision on outcome
			// reject_exhausted — where the engine routes when the loopback exhausts (B0
			// §4.1). A leaf (human takes over); pruned→skipped on the pass path.
			escapeID, eerr := addNode(name+" · 逃生/人工兜底（评审打回超限）", "", trunk, "escape", name, false)
			if eerr != nil {
				return result, eerr
			}
			if err := addEdge(pm.Dependency{
				FromTaskID: escapeID, ToTaskID: decisionID,
				Kind: pm.EdgeConditional, When: cycleOutcomeRejectExhausted,
			}); err != nil {
				return result, err
			}
			// Bounded LOOPBACK Decision→Dev on outcome=reject — re-runs Dev→Review→
			// Decision up to maxRounds (B0 §4). Added LAST so ValidateLoopback sees the
			// forward chain that makes Dev a forward ancestor of Decision.
			if err := addEdge(pm.Dependency{
				FromTaskID: decisionID, ToTaskID: devID,
				Kind: pm.EdgeLoopback, When: cycleOutcomeReject, MaxRounds: maxRounds,
			}); err != nil {
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

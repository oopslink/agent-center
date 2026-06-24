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
	// Issue, when set, links this feature's chain nodes (Dev/Review/Decision/
	// Integrate/Escape) to that issue as their derived_from_issue AT CREATE — it
	// OVERRIDES the plan-level SourceIssue for this feature. Empty → the feature
	// inherits the plan-level SourceIssue (T462). Must belong to the same project.
	Issue pm.IssueID
}

// ScaffoldCyclePlanCommand is the input to ScaffoldCyclePlan.
type ScaffoldCyclePlanCommand struct {
	ProjectID pm.ProjectID
	Version   string // e.g. "v2.13.0" — the integration trunk is dev/<version>
	Features  []CycleFeature
	// MaxReviewRounds bounds each feature's review loopback (Decision→Dev on reject),
	// B0 §4.1. <=0 falls back to defaultReviewRounds (3). Ignored for doc-only features.
	MaxReviewRounds int
	// SkipMergeCheck, when true, marks every Integrate node skip_merge_check at build
	// time so F3's Integrate-complete merge guard stands down for this cycle (T330).
	// Default false preserves the existing behavior (merge-check enforced). Use it for
	// cycles whose project has no CodeRepoRef, or that integrate outside this server's
	// reach. Doc-only features are already exempt regardless of this flag.
	SkipMergeCheck bool
	// SourceIssue, when set, is the cycle's source issue: EVERY generated node
	// (S0/Dev/Review/Decision/Integrate/Escape/Gate/Accept/Ship) is linked to it as
	// derived_from_issue AT CREATE (T462), so each node's owner can get_issue the
	// spec straight away (the get_issue derive-gate is satisfied). A feature may
	// override it for its own chain via CycleFeature.Issue. Empty → no node carries
	// a link (the pre-T462 behavior, unchanged). derived_from_issue is immutable
	// after create, so it MUST be set here, not back-filled later
	// (see [[derived-from-issue-set-at-creation]]).
	SourceIssue pm.IssueID
	CreatedBy   pm.IdentityRef
}

// ScaffoldCycleNode describes one created node in the returned summary.
type ScaffoldCycleNode struct {
	TaskID  pm.TaskID `json:"task_id"`
	Title   string    `json:"title"`
	Branch  string    `json:"branch"`
	Base    string    `json:"base"`
	Kind    string    `json:"kind"`    // s0|dev|review|decision|integrate|escape|gate|accept|ship
	Feature string    `json:"feature"` // owning feature name (empty for s0/gate/accept/ship)
	// DerivedFromIssue is the source issue this node was linked to at create (T462),
	// empty when the scaffold was called without a SourceIssue/feature Issue.
	DerivedFromIssue pm.IssueID `json:"derived_from_issue,omitempty"`
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
	// Validate every referenced source issue ONCE up-front (T462): it must exist and
	// belong to this project. derived_from_issue is set at node-create (CreateTask does
	// NOT itself validate the link), so a bad ref here would otherwise mint N nodes
	// with a dangling/cross-project link. Failing before any node is created keeps the
	// "partial failure leaves a deletable draft" contract clean. Mirrors the
	// applyDerivedFromIssue (T192) invariant used by the post-create edit path.
	for _, issueID := range cmd.distinctSourceIssues() {
		if err := s.validateDerivedIssue(ctx, issueID, cmd.ProjectID); err != nil {
			return zero, err
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
	// plan, appends to the summary, and returns its id. derived is the source issue
	// linked at create (T462; "" = no link).
	addNode := func(title, branch, base, kind, feature string, skip bool, derived pm.IssueID) (pm.TaskID, error) {
		tid, cerr := s.CreateTask(ctx, CreateTaskCommand{
			ProjectID:      cmd.ProjectID,
			Title:          title,
			CreatedBy:      cmd.CreatedBy,
			Branch:         branch,
			Base:           base,
			SkipMergeCheck: skip,
			// Link the source issue AT CREATE — derived_from_issue is immutable
			// afterward, so it cannot be back-filled (T462). Pre-validated above.
			DerivedFromIssue: derived,
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
			DerivedFromIssue: derived,
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

	// 2) S0 — cut dev/<version> from main. branch=trunk, base=main. Plan-level
	// SourceIssue (T462; a feature override does not apply to the shared S0 node).
	s0, err := addNode("S0 开发主分支 — 切 "+trunk, trunk, "main", "s0", "", false, cmd.SourceIssue)
	if err != nil {
		return result, err
	}

	// 3) per-feature chains; collect each chain's terminal node for the Gate.
	var terminals []pm.TaskID
	for _, f := range cmd.Features {
		name := strings.TrimSpace(f.Name)
		branch := strings.TrimSpace(f.Branch)
		// The feature's chain nodes derive from its own Issue when set, else the
		// plan-level SourceIssue (T462).
		featIssue := cmd.SourceIssue
		if strings.TrimSpace(string(f.Issue)) != "" {
			featIssue = f.Issue
		}

		// Dev node. If no explicit branch, default it to the Dev node's own T<n>
		// (F1 §4.1) — resolved after create, then applied to the whole chain.
		devID, derr := addNode(name+" · Dev", branch, trunk, "dev", name, f.DocOnly, featIssue)
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
			reviewID, rerr := addNode(name+" · Review", branch, trunk, "review", name, false, featIssue)
			if rerr != nil {
				return result, rerr
			}
			if err := dependsOn(reviewID, devID); err != nil {
				return result, err
			}
			decisionID, derr := addNode(name+" · Decision（评审结论 pass/reject）", branch, trunk, "decision", name, false, featIssue)
			if derr != nil {
				return result, derr
			}
			if err := dependsOn(decisionID, reviewID); err != nil {
				return result, err
			}
			// Integrate is the CONDITIONAL successor of Decision on outcome=pass — the
			// feature terminal that feeds the Gate.
			integrateID, ierr := addNode(name+" · Integrate", branch, trunk, "integrate", name, cmd.SkipMergeCheck, featIssue)
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
			escapeID, eerr := addNode(name+" · 逃生/人工兜底（评审打回超限）", "", trunk, "escape", name, false, featIssue)
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
	gate, err := addNode("集成完成 Gate — PD 关门核对", "", trunk, "gate", "", false, cmd.SourceIssue)
	if err != nil {
		return result, err
	}
	for _, t := range terminals {
		if err := dependsOn(gate, t); err != nil {
			return result, err
		}
	}

	// 5) Accept — blocked_by the Gate (verify on the integrated trunk).
	accept, err := addNode("Accept 验收（集成后主干整体）", "", trunk, "accept", "", false, cmd.SourceIssue)
	if err != nil {
		return result, err
	}
	if err := dependsOn(accept, gate); err != nil {
		return result, err
	}

	// 6) Ship — blocked_by Accept. branch=trunk, base=main (trunk → main + tag).
	ship, err := addNode("Ship "+version, trunk, "main", "ship", "", false, cmd.SourceIssue)
	if err != nil {
		return result, err
	}
	if err := dependsOn(ship, accept); err != nil {
		return result, err
	}

	return result, nil
}

// distinctSourceIssues returns the unique non-empty source issues referenced by the
// command (the plan-level SourceIssue + any per-feature Issue overrides) — the set the
// scaffold must validate before creating nodes (T462).
func (cmd ScaffoldCyclePlanCommand) distinctSourceIssues() []pm.IssueID {
	seen := map[pm.IssueID]bool{}
	var out []pm.IssueID
	add := func(id pm.IssueID) {
		if strings.TrimSpace(string(id)) == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(cmd.SourceIssue)
	for _, f := range cmd.Features {
		add(f.Issue)
	}
	return out
}

// validateDerivedIssue checks a source issue exists and belongs to projectID — the
// same invariant applyDerivedFromIssue (T192) enforces on the post-create edit path,
// applied up-front here because CreateTask sets derived_from_issue without validating
// it (T462). Empty issueID is a no-op (no link requested).
func (s *Service) validateDerivedIssue(ctx context.Context, issueID pm.IssueID, projectID pm.ProjectID) error {
	if strings.TrimSpace(string(issueID)) == "" {
		return nil
	}
	iss, err := s.issues.FindByID(ctx, issueID)
	if err != nil {
		return err // pm.ErrIssueNotFound when missing
	}
	if iss.ProjectID() != projectID {
		return pm.ErrDerivedIssueProjectMismatch
	}
	return nil
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

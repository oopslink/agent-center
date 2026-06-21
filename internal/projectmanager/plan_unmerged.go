package projectmanager

// Cycle node-graph metadata + the F4 "unmerged-branch board" read-model
// (v2.13.0 / I18, see docs/design/v2.13.0/cycle-node-graph-spec.md). PURE domain
// — no I/O.
//
// The cycle node-graph (F1 spec §1) is `S0 → (Dev→Review→Integrate)×N → 集成完成
// Gate → Accept → Ship`. Two downstream features read per-node metadata (F1 spec
// §4): F3 (the runtime merge-check guardrail, validated when an `Integrate(T)`
// node completes) and F4 (this file — the board of NOT-YET-merged feature
// branches the PD reconciles before Ship). Both must agree on which node is an
// `Integrate` node, so the metadata carries an explicit ROLE discriminator: the
// F1 §4 table lists branch/base/skip_merge_check, but Dev/Review/Integrate(T)
// SHARE branch/base (§4.2), so the role is the only thing that distinguishes the
// merge-check node from the rest of its chain.
//
// Storage of the metadata is OWNED by F2 (scaffold_cycle_plan persists it, F1
// spec §4.3); this file defines its LOGICAL schema (CycleNodeMeta) and the pure
// F4 projection over it (UnmergedIntegrations). The service layer reads the
// per-plan metadata through a narrow, nil-safe port (consumer-owned interface, so
// the F4 read composes with F2's storage without the two features sharing a
// concrete type).

// CycleNodeRole classifies a node in the cycle node-graph (F1 spec §2 node types
// + the §4 role discriminator). It is the single bit F3/F4 use to target the
// `Integrate(T)` node within a feature's Dev→Review→Integrate chain.
type CycleNodeRole string

const (
	CycleRoleS0        CycleNodeRole = "s0"        // 开发主分支 — cuts dev/vX.Y.0 from main
	CycleRoleDev       CycleNodeRole = "dev"       // 开发 — implements a feature on its branch
	CycleRoleReview    CycleNodeRole = "review"    // 评审 — code review + §-1 gate
	CycleRoleDecision  CycleNodeRole = "decision"  // 决策/网关 — produces pass/reject outcome routing its out-edges (B2/B0 §2.1)
	CycleRoleIntegrate CycleNodeRole = "integrate" // 集成 — merge-check landing point (F3/F4 target)
	CycleRoleEscape    CycleNodeRole = "escape"    // 逃生/人工兜底 — reached when a Decision's bounded loopback exhausts (B0 §4.1/§9)
	CycleRoleGate      CycleNodeRole = "gate"       // 集成完成 Gate — PD barrier
	CycleRoleAccept    CycleNodeRole = "accept"     // 验收
	CycleRoleShip      CycleNodeRole = "ship"       // Ship — dev/vX.Y.0 → main + tag
)

// IsValid reports enum membership (an empty/unknown role is invalid — it simply
// never matches the Integrate filter, which is the safe default). Decision/Escape
// (B2 control-flow nodes) are valid roles but, like every non-Integrate role, are
// ignored by the F4 board.
func (r CycleNodeRole) IsValid() bool {
	switch r {
	case CycleRoleS0, CycleRoleDev, CycleRoleReview, CycleRoleDecision,
		CycleRoleIntegrate, CycleRoleEscape, CycleRoleGate, CycleRoleAccept,
		CycleRoleShip:
		return true
	}
	return false
}

// CycleNodeMeta is one node's cycle-graph metadata (F1 spec §4). It is OWNED and
// persisted by F2; F4 (here) consumes Role to find Integrate nodes and Branch/
// Base for display, and F3 consumes the same Role + Branch/Base for the merge
// check. Zero value = no role (matches nothing).
type CycleNodeMeta struct {
	// Role is the node's classification in the cycle graph. F4 lists nodes whose
	// Role == CycleRoleIntegrate; everything else is ignored.
	Role CycleNodeRole
	// Branch is the feature branch this node works on (F1 spec §4.1; defaults to
	// the node's T-number). For an Integrate node it is the branch that must be
	// merged back into Base.
	Branch string
	// Base is the integration trunk the branch merges into (default dev/vX.Y.0).
	Base string
	// SkipMergeCheck structurally exempts a node from F3's merge validation
	// (pure-doc / no-code feature, F1 spec §4.1). It does NOT exempt the node from
	// the board: an un-done Integrate node is still "unmerged work" until it
	// reaches done — the flag rides on the row so the PD sees why F3 won't gate it.
	SkipMergeCheck bool
}

// UnmergedIntegration is one row of the F4 board: an `Integrate(T)` node that has
// NOT yet reached `done` — i.e. a feature branch not yet merged back into the
// integration trunk (F1 spec §2.5/§8). NodeStatus is the DERIVED node status
// (§9.2) so the PD sees WHY it is still open (blocked / running / dispatched / …).
type UnmergedIntegration struct {
	TaskID         TaskID
	NodeStatus     NodeStatus
	Branch         string
	Base           string
	SkipMergeCheck bool
}

// UnmergedIntegrations is the PURE F4 projection: over a plan's DERIVED view and
// its per-node cycle metadata, it returns every `Integrate(T)` node that is not
// yet `done`, in the view's (stable) node order. This is the PD's ship-gate
// reconciliation list — "which feature branches are still not merged back?" — and
// the structural counterpart of the §2.5 集成完成 Gate (the barrier is open iff
// this list is empty).
//
// Identification is by Role == CycleRoleIntegrate ONLY (never by title): a node
// with no metadata (meta missing, e.g. a non-scaffolded plan or F2 not yet wired)
// is not an Integrate node and is skipped, so the list is empty rather than wrong.
// A done Integrate node has merged and drops off the board.
func UnmergedIntegrations(view PlanView, meta map[TaskID]CycleNodeMeta) []UnmergedIntegration {
	out := make([]UnmergedIntegration, 0)
	for _, n := range view.Nodes {
		m, ok := meta[n.TaskID]
		if !ok || m.Role != CycleRoleIntegrate {
			continue
		}
		if n.NodeStatus == NodeDone {
			continue
		}
		out = append(out, UnmergedIntegration{
			TaskID:         n.TaskID,
			NodeStatus:     n.NodeStatus,
			Branch:         m.Branch,
			Base:           m.Base,
			SkipMergeCheck: m.SkipMergeCheck,
		})
	}
	return out
}

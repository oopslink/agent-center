// Package projectmanager is the ProjectManager bounded context (v2.7,
// ADR-0046): the single work-management truth for Projects, ProjectMembers,
// Issues, Tasks, their subscriber truth, and their state transitions.
//
// Boundaries (plan §1, ADR-0046):
//   - A Task/Issue belongs to exactly one Project; there are no global or
//     cross-Project work items.
//   - State NEVER changes by inference from Conversation messages — only
//     through this BC's explicit AppServices.
//   - Conversation participants mirror effective subscribers (ADR-0052), but
//     the subscriber truth lives HERE, not in Conversation.
//
// B1 (task #96) ships the aggregates + repositories + state machines. The
// AppServices + outbox-driven participant projection land in B2 (#97).
package projectmanager

import (
	"errors"
	"strings"
)

// Typed identifiers (conventions § 0.3).
type (
	ProjectID string
	IssueID   string
	TaskID    string
	MemberID  string
	PlanID    string
	// IdentityRef mirrors the kind-prefixed identity vocabulary (ADR-0033):
	// `user:<id>` / `agent:<id>` / `system`.
	IdentityRef string
)

func (id ProjectID) String() string  { return string(id) }
func (id IssueID) String() string    { return string(id) }
func (id TaskID) String() string     { return string(id) }
func (id MemberID) String() string   { return string(id) }
func (id PlanID) String() string     { return string(id) }
func (r IdentityRef) String() string { return string(r) }

// Validate enforces the kind-prefixed identity vocabulary (ADR-0033).
func (r IdentityRef) Validate() error {
	s := string(r)
	if s == "" {
		return errors.New("projectmanager: identity ref required")
	}
	if s == "system" {
		return nil
	}
	for _, p := range []string{"user:", "agent:"} {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return errors.New("projectmanager: identity ref must be 'system' or 'user:<id>' / 'agent:<id>' (ADR-0033)")
}

// ProjectMemberRole — v1 has domain isolation, NOT role permissions
// (plan §10 OQ6): membership is the minimum write-gate; all members have equal
// capability. The role field exists for the roadmap permission model but is
// not enforced in v2.7.
type ProjectMemberRole string

const (
	RoleMember ProjectMemberRole = "member"
	RoleOwner  ProjectMemberRole = "owner"
)

// IsValid reports enum membership.
func (r ProjectMemberRole) IsValid() bool {
	return r == RoleMember || r == RoleOwner
}

// BacklogNotActionableHint is the agent-facing remediation for the backlog-INERT
// invariant (T190 — see IsBacklogInert), surfaced VERBATIM (no "projectmanager:"
// prefix) wherever an action is refused on a backlog task. It is the SINGLE source
// of the message for both ErrTaskBacklogNotActionable and the agent-tools
// `task_backlog_not_actionable` envelope.
const BacklogNotActionableHint = "task is in backlog — add it to a plan (add_task_to_plan) or dispatch it into the assignment pool"

// Sentinel errors.
var (
	ErrProjectNotFound     = errors.New("projectmanager: project not found")
	ErrProjectExists       = errors.New("projectmanager: project already exists")
	ErrMemberNotFound      = errors.New("projectmanager: project member not found")
	ErrMemberExists        = errors.New("projectmanager: project member already exists")
	ErrIssueNotFound       = errors.New("projectmanager: issue not found")
	ErrIssueExists         = errors.New("projectmanager: issue already exists")
	ErrTaskNotFound        = errors.New("projectmanager: task not found")
	ErrTaskExists          = errors.New("projectmanager: task already exists")
	ErrSubscriberNotFound  = errors.New("projectmanager: subscriber not found")
	ErrCodeRepoRefNotFound = errors.New("projectmanager: code repo ref not found")
	ErrCrossProject        = errors.New("projectmanager: cross-project operation rejected (scope invariant)")
	ErrInvalidStatus       = errors.New("projectmanager: invalid status")
	ErrIllegalTransition   = errors.New("projectmanager: illegal status transition")
	// ErrTaskArchived guards an archived Task (v2.9 P3): archival is an ORTHOGONAL
	// terminal state (does not change task.status) that makes the Task read-only —
	// every mutator (Rename/SetDescription/status transitions/Assign/…) rejects with
	// this once the task is archived. Re-archiving an already-archived task also
	// returns it (mirrors Conversation.Archive → ErrConversationArchived).
	ErrTaskArchived = errors.New("projectmanager: task is archived")
	// ErrTaskNotArchived guards the FinalizeArchived escape hatch (T339): it concludes
	// an ALREADY-archived dead task and rejects a live (non-archived) task — a live task
	// must use the normal Discard path, not this read-only-lock bypass.
	ErrTaskNotArchived     = errors.New("projectmanager: task is not archived")
	ErrBlockReasonRequired = errors.New("projectmanager: blocked requires a reason (plan §2.2)")
	// ErrInvalidBlockReasonType (v2.14.0 I14/F3, finding 01KVNFR…/§13.A): block_task
	// must carry a reasonType ∈ {input_required, obstacle}. F1's Task.Block validates
	// only that the reason text is non-empty (not the type), so F3's BlockTask entry
	// enforces BlockReasonType.IsValid() and rejects any other value (incl. "").
	ErrInvalidBlockReasonType = errors.New("projectmanager: invalid block reason type (must be input_required or obstacle)")
	// ErrNotTaskAssignee / ErrTaskBlocked guard the v2.14.0 I14 block+lease model:
	// only the assignee agent may Block its own running task, and a legally blocked
	// task cannot renew its execution lease (a block is a lease-free pause).
	ErrNotTaskAssignee = errors.New("projectmanager: actor is not the task assignee")
	ErrTaskBlocked     = errors.New("projectmanager: task is blocked (no execution lease)")
	// ErrAgentHasActiveTask (v2.14.0 I14/F3 §13.B/§13.F-①; generalized v2.18.0 W4c)
	// — the run-slot cap: an agent may have at most EffectiveConcurrencyCap running,
	// non-blocked Tasks at a time (1 for a default agent — single-active, no
	// regression; EffectiveMaxConcurrentTasks for a concurrency-enabled agent).
	// Surfaced when a task→running transition (start_task / unblock→running /
	// reassign-of-running) would push the agent OVER its cap. Pre-v2.18 this was a DB
	// guarantee (the idx_pm_tasks_one_active_per_agent UNIQUE partial index, migration
	// 0072); 0084 dropped that index (UNIQUE can only express ≤1, never per-agent ≤N)
	// and the check moved to the application layer (Service.enforceConcurrencyCap),
	// kept race-safe by the start tx's whole-tx replay. The agent must finish, block,
	// or yield a running task first. A blocked task does NOT occupy a run slot.
	ErrAgentHasActiveTask = errors.New("projectmanager: agent is at its running-task cap (no free run slot; finish, block, or yield a running task first)")
	ErrVersionConflict    = errors.New("projectmanager: version conflict (optimistic lock)")
	ErrEmptyProjectScope  = errors.New("projectmanager: project_id required (no global work items)")
	ErrCrossOrgAssignee   = errors.New("projectmanager: assignee agent is not in the project's organization (OQ6: org membership is the prerequisite for project membership)")
	// ErrAgentDirectoryUnavailable is returned (fail-closed) when an agent is
	// assigned but no AgentDirectory is wired to verify the agent's org — a
	// missing dependency must not silently bypass the cross-org guard.
	ErrAgentDirectoryUnavailable = errors.New("projectmanager: agent directory unavailable — cannot verify assignee agent's organization")
	// Plan orchestration (v2.9 #283).
	ErrEmptyPlanName  = errors.New("projectmanager: plan name required")
	ErrPlanCycle      = errors.New("projectmanager: dependency would create a cycle")
	ErrSelfDependency = errors.New("projectmanager: a task cannot depend on itself")
	// ErrInvalidLoopback (v2.13.0 I18/B1): a loopback edge must carry a When label,
	// MaxRounds≥1, and point To a forward ancestor of From (a real bounded loop).
	ErrInvalidLoopback       = errors.New("projectmanager: invalid loopback edge (needs When + MaxRounds>=1 + To must be a forward ancestor of From)")
	ErrIllegalPlanTransition = errors.New("projectmanager: illegal plan status transition")
	ErrInvalidPlanStatus     = errors.New("projectmanager: invalid plan status")
	ErrPlanNotDraft          = errors.New("projectmanager: plan dependencies/tasks editable only in draft")
	ErrPlanNotFound          = errors.New("projectmanager: plan not found")
	ErrPlanExists            = errors.New("projectmanager: plan already exists")
	// ErrTaskInOtherPlan rejects selecting a task into a Plan when it already
	// belongs to a DIFFERENT Plan (Task ↔ Plan = 0..1, design §2). Re-selecting
	// into the SAME plan is a no-op (not an error).
	ErrTaskInOtherPlan = errors.New("projectmanager: task already belongs to another plan")
	// T83 claimability (open-claim of built-in pool tasks):
	// ErrTaskNotClaimable — the task is not an open, dispatched built-in-pool task
	// (backlog / structured-plan node / wrong status / not dispatched). Claim is
	// rejected; for a structured-plan node the assigned agent uses normal dispatch.
	ErrTaskNotClaimable = errors.New("projectmanager: task is not claimable from the assignment pool")
	// ErrTaskAlreadyClaimed — a concurrent claim won the race (the task already has
	// an assignee). Idempotent-readable, not a hard failure (T83 §3.3).
	ErrTaskAlreadyClaimed = errors.New("projectmanager: task already claimed by another agent")
	// ErrPoolClaimLimitReached — the agent already holds the max concurrent claimed
	// pool tasks (T83 §3.6, default N=3). Does not affect structured-plan nodes.
	ErrPoolClaimLimitReached = errors.New("projectmanager: pool claim limit reached")
	// ErrTaskNotRunnable (T130, rewritten v2.14.0 I14/F3 §13.A) — the task may not
	// enter running because its blockedBy DEPENDENCIES are not yet satisfied. It is the
	// 抢跑 (run-ahead) guard: a DAG node may start ONLY once every upstream it
	// depends_on is completed/discarded (the engine derives it to `ready`/`dispatched`);
	// a node still `blocked` on an unfinished upstream, a `skipped` dead conditional
	// branch, or a pure-backlog task (no plan) is rejected. The builtin pool keeps its
	// own rule (a member must be DISPATCHED). Enforced at the open→running gate
	// (start_task / start_work via the agent TaskRunGate) AND at direct (re)assignment.
	// Remedy: wait for the upstream dependencies to finish, add the task to a plan, or
	// dispatch it into the Assignment Pool.
	ErrTaskNotRunnable = errors.New("projectmanager: task is not runnable — its dependencies are not yet satisfied (or it is backlog / a not-dispatched pool member)")
	// ErrTaskBacklogNotActionable (T190) — the UNIFIED sentinel for "this action is
	// not allowed because the task is BACKLOG (inert)". A backlog task (planID=="",
	// see IsBacklogInert) is rejected by claim_task / start_work / complete_task /
	// block_task with this ONE error (surfaced to agents as the
	// `task_backlog_not_actionable` code), replacing the prior scattered
	// not_claimable / not_runnable / not_agents_task. The remedy is always the same:
	// add_task_to_plan (real plan) or dispatch into the Assignment Pool; discard /
	// delete are exempt. Message = BacklogNotActionableHint (no "projectmanager:"
	// prefix — it is surfaced VERBATIM to agents). Rule: docs/rules/backlog-task-inert.md.
	ErrTaskBacklogNotActionable = errors.New(BacklogNotActionableHint)
	// ErrPlanProjectMismatch rejects selecting a task whose project differs from
	// the Plan's project (a Plan selects only its own project's backlog, §2/§9.6d).
	ErrPlanProjectMismatch = errors.New("projectmanager: task and plan belong to different projects")
	// ErrDerivedIssueProjectMismatch (T192) rejects linking a task to a
	// derived_from_issue that belongs to a DIFFERENT project — a task may only be
	// derived from an Issue in its OWN project (mirrors the Task↔Project scope
	// invariant). Clearing the link ("") and same-project links are allowed; a
	// missing issue surfaces ErrIssueNotFound. Enforced by UpdateTask /
	// BatchUpdateTask when derived_from_issue is (re)set after creation.
	ErrDerivedIssueProjectMismatch = errors.New("projectmanager: derived_from_issue belongs to a different project")
	// Start validation (v2.9 #285, §9.6).
	ErrPlanNoTasks              = errors.New("projectmanager: plan must have at least one task to start")
	ErrPlanUnassignedTask       = errors.New("projectmanager: every plan task must have an assignee to start")
	ErrPlanUnresolvableAssignee = errors.New("projectmanager: a plan task's assignee is unresolvable (identity missing or agent archived/deleted)")
	// ErrPlanNotRunning rejects advance on a Plan that is not running (§9.6/§3).
	ErrPlanNotRunning = errors.New("projectmanager: plan is not running")
	// v2.9 P3 (delete + archive).
	// ErrPlanRunning rejects DeletePlan/ArchivePlan on a RUNNING Plan: a running
	// plan must be stopped (or finished) before it can be deleted or archived
	// (maps to 409). Distinct from ErrPlanNotRunning (advance's not-running guard).
	ErrPlanRunning = errors.New("projectmanager: plan is running — stop it before deleting or archiving")
	// ErrPlanArchived rejects re-archiving an already-archived (terminal,
	// irreversible) Plan, mirroring Conversation.ErrConversationArchived.
	ErrPlanArchived = errors.New("projectmanager: plan is already archived")
	// ADR-0047 built-in assignment pool.
	// ErrBuiltinPlanImmutable rejects stop/done/archive/delete on the per-project
	// built-in pool (it is always-started and archived only with its project).
	ErrBuiltinPlanImmutable = errors.New("projectmanager: the built-in plan cannot be stopped, archived, or deleted on its own")
	// ErrBuiltinPlanNoEdges rejects adding a dependency edge inside the built-in
	// pool (it is a FLAT pool, not a DAG — every task is immediately dispatchable).
	ErrBuiltinPlanNoEdges = errors.New("projectmanager: the built-in plan is a flat pool — dependency edges are not allowed")
	// ErrBuiltinPlanExists rejects creating a second built-in plan for a project.
	ErrBuiltinPlanExists = errors.New("projectmanager: project already has a built-in plan")
	// ErrProjectArchived guards an archived Project (v2.9 #297). @oopslink ruled
	// project archive is IRREVERSIBLE (no restore), so an archived project is PURE
	// READ-ONLY: every project-CHILD mutation (member add/remove, issue/task
	// create/edit/transition, plan create/edit/lifecycle, …) is rejected with this
	// once the project is archived, while reads (GetX/ListX) are unaffected. Maps to
	// 409 cross-surface (webconsole + MCP), mirroring ErrPlanArchived's state-conflict
	// class. The Archive operation itself is NOT guarded (it is the transition into
	// this terminal state).
	ErrProjectArchived = errors.New("projectmanager: project is archived")
	// ErrPlanHasRunningTasks rejects archiving a plan that still has a member task
	// in the running state (v2.9 #299, @oopslink): after stop, a draft plan may
	// still have an in-flight running task, and archiving would orphan it. Archive
	// requires no running member task (maps to 409). Distinct from ErrPlanRunning
	// (which guards the PLAN's own running status).
	ErrPlanHasRunningTasks = errors.New("projectmanager: plan has running tasks — complete or stop them before archiving")
	// Plan Shared Findings (v2.10, ADR-0053 — DeLM shared verified context).
	ErrPlanFindingNotFound = errors.New("projectmanager: plan finding not found")
	ErrPlanFindingNoPlan   = errors.New("projectmanager: plan finding requires a plan_id")
	ErrPlanFindingNoTask   = errors.New("projectmanager: plan finding requires a source task_id")
	ErrInvalidFindingKind  = errors.New("projectmanager: invalid finding kind (want fact|failure|constraint|patch_summary)")
	ErrEmptyFindingContent = errors.New("projectmanager: finding content required")
	// ErrFindingContentTooLong rejects a finding whose gist exceeds MaxFindingContentLen
	// (findings stay COMPACT; large content belongs in the task trace, not a gist).
	ErrFindingContentTooLong = errors.New("projectmanager: finding content too long (keep the gist compact)")
	// ErrFindingTaskNotInPlan rejects recording a finding whose source task does not
	// belong to the named plan (the finding must be grounded in a task IN this plan).
	ErrFindingTaskNotInPlan = errors.New("projectmanager: source task does not belong to this plan")
	// ErrFindingNotTaskAssignee is the v1 ADMISSION gate (ADR-0053 decision 2 —
	// evidence attribution): only the source task's assignee may record a finding for
	// it (you can only gist what you actually executed). Full LLM-verifier deferred.
	ErrFindingNotTaskAssignee = errors.New("projectmanager: only the source task's assignee may record a finding for it")
	// ErrFindingForbidden rejects retracting a finding by an actor who is neither its
	// author nor a project owner.
	ErrFindingForbidden = errors.New("projectmanager: only the finding author or a project owner may retract it")
	// ErrPlanFindingExists rejects inserting a finding whose id already exists (a
	// write CONFLICT, not a not-found). IDs are server-generated ULIDs so this is
	// effectively unreachable, but the repo must not mislabel a unique violation as
	// a 404 (review finding #5).
	ErrPlanFindingExists = errors.New("projectmanager: plan finding already exists")
)

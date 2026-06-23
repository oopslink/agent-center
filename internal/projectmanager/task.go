package projectmanager

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskStatus enum + state machine. v2.9.1 ADR-0046 simplified 7→5 states:
//
//	open → running → completed
//	open/running → discarded (terminal)
//	completed → reopened → open
//
// "blocked" is NO LONGER a state (ADR-0046): being stuck-with-a-reason is now a
// `blocked_reason` ANNOTATION on a `running` task (block_task writes it; resume /
// unblock_task / complete / discard clear it). This removes the "enters
// automatically but has no legal exit" deadlock class (T16) and the name clash with
// Plan's derived `node_status: blocked`. "verified" is also removed (unused; the
// "nobody self-accepts" discipline lives in process — PD §-1 + Tester/Tester2 — not
// in a task state). The former "assigned" STATE was removed in v2.8.1 (assignee is
// metadata); "canceled" was renamed "discarded".
type TaskStatus string

const (
	TaskOpen      TaskStatus = "open"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskDiscarded TaskStatus = "discarded" // was "canceled" (v2.8.1 rename)
	TaskReopened  TaskStatus = "reopened"
)

// IsValid reports enum membership.
func (s TaskStatus) IsValid() bool {
	switch s {
	case TaskOpen, TaskRunning, TaskCompleted, TaskDiscarded, TaskReopened:
		return true
	}
	return false
}

// taskTransitions is the allowed-transition adjacency. Start moves open→running
// directly (assignment is metadata, not a precondition state). ADR-0046: there is
// NO `blocked` node (stuck = a running-task annotation) and NO `verified` node, so
// every non-terminal state always has a forward path — no deadlock is reachable.
var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskOpen:      {TaskRunning, TaskDiscarded},
	TaskRunning:   {TaskCompleted, TaskDiscarded},
	TaskCompleted: {TaskReopened},
	TaskDiscarded: {}, // terminal
	TaskReopened:  {TaskOpen},
}

// CanTransitionTo reports whether from→to is a legal Task transition.
func (s TaskStatus) CanTransitionTo(to TaskStatus) bool {
	for _, n := range taskTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

// IsTerminal reports whether the task has reached a concluded state: work is
// done (completed/verified) or abandoned (discarded). A Reopen can re-activate a
// completed task, but in any concluded state the task is not "active work in
// flight". The complement (the active / non-terminal set) is exactly
// {open, running, reopened}. v2.7 #107 Phase-2 (proj-B): the observability default
// task-query set is the non-terminal set. ADR-0046: "blocked" is no longer a state
// (a running annotation), so a stuck task is non-terminal (running) as expected.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskCompleted, TaskDiscarded:
		return true
	}
	return false
}

// BlockReasonType classifies WHY a running Task is blocked (issue I14 §2.4). It
// drives both the UI rendering and the Unblock handling path:
//   - input_required: the agent needs a user answer; the user replies in the
//     bound Conversation, which triggers Unblock with the reply as the comment.
//   - obstacle: an external blocker the agent cannot clear on its own; owner/PM
//     intervenes and records what they did via the Unblock comment.
//
// The empty value ("") is the "not blocked" sentinel (no blocked_reason).
type BlockReasonType string

const (
	BlockReasonInputRequired BlockReasonType = "input_required" // needs a user reply
	BlockReasonObstacle      BlockReasonType = "obstacle"       // needs owner/PM intervention
)

// IsValid reports whether the type is one of the known block kinds. The empty
// value is intentionally NOT valid here (it is the unblocked sentinel, not a kind).
func (b BlockReasonType) IsValid() bool {
	switch b {
	case BlockReasonInputRequired, BlockReasonObstacle:
		return true
	}
	return false
}

// TaskAction names a lifecycle event recorded on a Task's append-only action log
// (issue I14 §2.4). The TaskActionLog replaces the deleted AgentWorkItem
// transition history — reassignment, block/unblock, lease expiry, etc. all become
// single-table entries keyed by this action.
type TaskAction string

const (
	TaskActionAssigned     TaskAction = "assigned"
	TaskActionReassigned   TaskAction = "reassigned"
	TaskActionAgentStarted TaskAction = "agent_started"
	TaskActionBlocked      TaskAction = "blocked"
	TaskActionUnblocked    TaskAction = "unblocked"
	TaskActionLeaseExpired TaskAction = "lease_expired"
	TaskActionCompleted    TaskAction = "completed"
)

// TaskActionLog is an immutable, append-only record of a key Task lifecycle event
// (issue I14 §2.4). It supersedes the AgentWorkItem transition log: reassignment
// history (replacing WorkItem Supersede+New), block/unblock, and lease expiry
// (replacing FailFromAgentDeath) are all entries in this one log.
//
// ID is assigned by the PERSISTENCE layer (the F2 repo, via idgen) on insert and
// repopulated on rehydrate — a freshly appended in-memory entry carries an empty
// ID until persisted. This keeps the domain package free of an id-generation
// (infra) dependency: the aggregate never mints ULIDs itself.
type TaskActionLog struct {
	ID         string
	OccurredAt time.Time
	Action     TaskAction
	ActorRef   IdentityRef // who triggered it (user / owner / PM / agent / "system")
	AgentRef   IdentityRef // the agent the action concerns (may be empty)
	Note       string
}

// Task is a project-scoped unit of work and its assignment state. It binds a
// stable Conversation via owner_ref pm://tasks/{id} (held by Conversation,
// ADR-0047) across reassignments. A Task may be independent or derived from an
// Issue (DerivedFromIssue).
type Task struct {
	id               TaskID
	projectID        ProjectID
	title            string
	description      string
	status           TaskStatus
	assignee         IdentityRef // empty when unassigned
	derivedFromIssue IssueID     // empty when independent
	completedBy      IdentityRef // who set completed (enforces no self-verify)
	blockedReason    string
	createdBy        IdentityRef
	createdAt        time.Time
	updatedAt        time.Time
	version          int
	// orgNumber is the per-org, per-type monotonic display/reference number
	// (v2.7.1 #245, rendered "T<n>"). Allocated at create by the org sequence; 0
	// for rows predating the allocator / not yet backfilled (DTO omits org_ref then).
	orgNumber int
	// tags is the free-form label set (v2.8.1 edit-task #278). nil/empty when no
	// tags; cleaned + deduped + bounded (1..16 chars each, <=10 entries) by SetTags.
	tags []string
	// statusChangedAt records when status last changed (v2.8.1 #278). Set to
	// createdAt at construction; updated to `at` on every status mutation (NOT on
	// metadata edits like rename/assign/tags).
	statusChangedAt time.Time
	// planID is the Plan this task is selected into (v2.9 plan orchestration
	// #283). "" when in no plan; a task is in 0..1 Plan (design §2). Tasks are
	// created in the backlog first (no PlanID in NewTaskInput) and selected into a
	// Plan later via SetPlan. NOT a node_status — node status is derived, never
	// stored (§9.2).
	planID PlanID
	// archivedAt/archivedBy hold the ORTHOGONAL archived state (v2.9 P3). Archival
	// does NOT change task.status — a task can be archived in ANY status, and its
	// status is preserved through archive (so a verified/discarded/running task
	// stays verified/discarded/running). Both nil/empty when not archived. An
	// archived Task is read-only: every mutator rejects with ErrTaskArchived. This
	// mirrors Conversation's archivedAt/archivedBy (ADR-0032 §5). Cascade-set by
	// ArchivePlan when its Plan is archived.
	archivedAt *time.Time
	archivedBy IdentityRef
	// branch/base/skipMergeCheck are the cycle-node git metadata (v2.13.0 I18/F2 —
	// see docs/design/v2.13.0/cycle-node-graph-spec.md). branch = the feature
	// branch a node works on (default the feature's T<n>); base = the integration
	// trunk (dev/vX.Y.0); skipMergeCheck structurally exempts a node from the F3
	// merge-check guard (pure-doc / no-code features whose chain stops at Dev). All
	// zero-valued ("" / "" / false) for ordinary backlog tasks not built by
	// scaffold_cycle_plan. They are the INPUT to F3's `origin/<base> --contains
	// <branch>` Integrate-complete check; F2 only writes them.
	branch         string
	base           string
	skipMergeCheck bool
	// role is the cycle-node ROLE discriminator (v2.13.0 I18/F3 —
	// docs/design/v2.13.0/cycle-node-graph-spec.md §5). Dev/Review/Integrate(T)
	// SHARE branch/base (§4.2), so role is the ONLY thing that distinguishes the
	// Integrate node (F3's merge-check landing point + F4's board target) from its
	// chain siblings. "" for ordinary backlog tasks not built by scaffold_cycle_plan
	// (= no role; matches neither the F3 guard nor the F4 board). F2 (0066) stored
	// branch/base/skip_merge_check but NOT role — F3 (0067) persists it.
	role CycleNodeRole
	// --- v2.14.0 I14 (remove AgentWorkItem →收敛到 Task): block annotation + lease + log ---
	// blockedReasonType classifies blockedReason (input_required vs obstacle); "" when
	// not blocked. Set by Block, cleared by Unblock / ExpireLease / RecordReassignment.
	blockedReasonType BlockReasonType
	// blockedComment is filled by Unblock: the user's reply (input_required) or the
	// owner/PM resolution note (obstacle). The agent reads it on resume. Survives
	// Unblock (unlike blockedReason) so the resumed agent can see the answer.
	blockedComment string
	// executionLeaseExpiresAt is the running agent's heartbeat-lease deadline (nil =
	// no live lease: not running, or a legal block). RenewLease extends it, Block
	// clears it (a blocked task needs no heartbeat), ExpireLease reclaims a lapsed one.
	executionLeaseExpiresAt *time.Time
	// actionLogs is the append-only lifecycle history (assign/reassign/block/unblock/
	// lease_expired/…) that replaces the AgentWorkItem transition log (§2.4).
	actionLogs []TaskActionLog
}

// NewTaskInput captures constructor args.
type NewTaskInput struct {
	ID               TaskID
	ProjectID        ProjectID
	Title            string
	Description      string
	DerivedFromIssue IssueID
	CreatedBy        IdentityRef
	CreatedAt        time.Time
	// OrgNumber is the allocated per-org task number (v2.7.1 #245), supplied by
	// the service from the org sequence within the create tx.
	OrgNumber int
	// Branch/Base/SkipMergeCheck are the cycle-node git metadata (v2.13.0 I18/F2),
	// set at create only by scaffold_cycle_plan; empty/false for ordinary tasks.
	Branch         string
	Base           string
	SkipMergeCheck bool
	// Role is the cycle-node role discriminator (v2.13.0 I18/F3); set at create by
	// scaffold_cycle_plan, "" for ordinary tasks.
	Role CycleNodeRole
}

// NewTask constructs a fresh open Task. A Task must belong to a Project (no
// global/cross-project tasks — ADR-0046 §3).
func NewTask(in NewTaskInput) (*Task, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: task id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("projectmanager: task title required")
	}
	if err := in.CreatedBy.Validate(); err != nil {
		return nil, err
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Task{
		id:               in.ID,
		projectID:        in.ProjectID,
		title:            in.Title,
		description:      in.Description,
		status:           TaskOpen,
		derivedFromIssue: in.DerivedFromIssue,
		createdBy:        in.CreatedBy,
		createdAt:        at,
		updatedAt:        at,
		version:          1,
		orgNumber:        in.OrgNumber,
		statusChangedAt:  at,
		branch:           in.Branch,
		base:             in.Base,
		skipMergeCheck:   in.SkipMergeCheck,
		role:             in.Role,
	}, nil
}

// RehydrateTaskInput is for repository round-trip.
type RehydrateTaskInput struct {
	ID               TaskID
	ProjectID        ProjectID
	Title            string
	Description      string
	Status           TaskStatus
	Assignee         IdentityRef
	DerivedFromIssue IssueID
	CompletedBy      IdentityRef
	BlockedReason    string
	CreatedBy        IdentityRef
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int
	OrgNumber        int
	Tags             []string
	StatusChangedAt  time.Time
	PlanID           PlanID
	ArchivedAt       *time.Time
	ArchivedBy       IdentityRef
	Branch           string
	Base             string
	SkipMergeCheck   bool
	Role             CycleNodeRole
	// v2.14.0 I14 — block annotation + lease + action log (F2 round-trip).
	BlockedReasonType       BlockReasonType
	BlockedComment          string
	ExecutionLeaseExpiresAt *time.Time
	ActionLogs              []TaskActionLog
}

// RehydrateTask reconstructs without invariant checks.
func RehydrateTask(in RehydrateTaskInput) (*Task, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidStatus
	}
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	// statusChangedAt fallback: old rows predating the column store '' (zero) →
	// fall back to updated_at so the field is never zero for a valid row.
	statusChangedAt := in.StatusChangedAt.UTC()
	if in.StatusChangedAt.IsZero() {
		statusChangedAt = in.UpdatedAt.UTC()
	}
	return &Task{
		id:                      in.ID,
		projectID:               in.ProjectID,
		title:                   in.Title,
		description:             in.Description,
		status:                  in.Status,
		assignee:                in.Assignee,
		derivedFromIssue:        in.DerivedFromIssue,
		completedBy:             in.CompletedBy,
		blockedReason:           in.BlockedReason,
		createdBy:               in.CreatedBy,
		createdAt:               in.CreatedAt.UTC(),
		updatedAt:               in.UpdatedAt.UTC(),
		version:                 in.Version,
		orgNumber:               in.OrgNumber,
		tags:                    in.Tags,
		statusChangedAt:         statusChangedAt,
		planID:                  in.PlanID,
		archivedAt:              copyTaskTimePtr(in.ArchivedAt),
		archivedBy:              in.ArchivedBy,
		branch:                  in.Branch,
		base:                    in.Base,
		skipMergeCheck:          in.SkipMergeCheck,
		role:                    in.Role,
		blockedReasonType:       in.BlockedReasonType,
		blockedComment:          in.BlockedComment,
		executionLeaseExpiresAt: copyTaskTimePtr(in.ExecutionLeaseExpiresAt),
		actionLogs:              in.ActionLogs,
	}, nil
}

// copyTaskTimePtr UTC-normalizes a non-nil, non-zero timestamp pointer (nil/zero
// → nil), so archivedAt round-trips through rehydrate without aliasing the input.
func copyTaskTimePtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// Getters.
func (t *Task) ID() TaskID                { return t.id }
func (t *Task) ProjectID() ProjectID      { return t.projectID }
func (t *Task) Title() string             { return t.title }
func (t *Task) Description() string       { return t.description }
func (t *Task) Status() TaskStatus        { return t.status }
func (t *Task) Assignee() IdentityRef     { return t.assignee }
func (t *Task) DerivedFromIssue() IssueID { return t.derivedFromIssue }
func (t *Task) CompletedBy() IdentityRef  { return t.completedBy }
func (t *Task) BlockedReason() string     { return t.blockedReason }
func (t *Task) CreatedBy() IdentityRef    { return t.createdBy }

// BlockedReasonType / BlockedComment expose the v2.14.0 I14 block annotation
// (§2.4). Type is "" when not blocked; Comment carries the user reply / resolution
// note set by Unblock (it survives Unblock so the resumed agent can read it).
func (t *Task) BlockedReasonType() BlockReasonType { return t.blockedReasonType }
func (t *Task) BlockedComment() string             { return t.blockedComment }
func (t *Task) OrgNumber() int                     { return t.orgNumber }
func (t *Task) CreatedAt() time.Time               { return t.createdAt }
func (t *Task) UpdatedAt() time.Time               { return t.updatedAt }
func (t *Task) Version() int                       { return t.version }
func (t *Task) Tags() []string                     { return t.tags }
func (t *Task) StatusChangedAt() time.Time         { return t.statusChangedAt }
func (t *Task) PlanID() PlanID                     { return t.planID }
func (t *Task) ArchivedAt() *time.Time             { return t.archivedAt }
func (t *Task) ArchivedBy() IdentityRef            { return t.archivedBy }

// ExecutionLeaseExpiresAt returns a COPY of the running agent's lease deadline
// (nil = no live lease), so callers cannot mutate the aggregate's pointer (v2.14.0
// I14).
func (t *Task) ExecutionLeaseExpiresAt() *time.Time {
	return copyTaskTimePtr(t.executionLeaseExpiresAt)
}

// ActionLogs returns a COPY of the append-only lifecycle history (v2.14.0 I14) so
// callers cannot mutate the aggregate's internal slice. nil when empty.
func (t *Task) ActionLogs() []TaskActionLog {
	if len(t.actionLogs) == 0 {
		return nil
	}
	out := make([]TaskActionLog, len(t.actionLogs))
	copy(out, t.actionLogs)
	return out
}

// Branch/Base/SkipMergeCheck expose the cycle-node git metadata (v2.13.0 I18/F2).
// Empty/false for tasks not built by scaffold_cycle_plan. See task struct doc.
func (t *Task) Branch() string       { return t.branch }
func (t *Task) Base() string         { return t.base }
func (t *Task) SkipMergeCheck() bool { return t.skipMergeCheck }

// Role exposes the cycle-node role discriminator (v2.13.0 I18/F3). "" for tasks
// not built by scaffold_cycle_plan. The F3 merge guard targets the role ==
// CycleRoleIntegrate node; F4's board keys on the same field. See task struct doc.
func (t *Task) Role() CycleNodeRole { return t.role }

// IsArchived reports the ORTHOGONAL archived state (v2.9 P3). Independent of
// status: a task may be archived in any status.
func (t *Task) IsArchived() bool { return t.archivedAt != nil }

// Archive sets the ORTHOGONAL archived state (v2.9 P3, mirroring
// Conversation.Archive): it records archivedAt/archivedBy and makes the Task
// read-only, but does NOT change task.status — the status is preserved through
// archive. Re-archiving an already-archived task returns ErrTaskArchived
// (idempotency is the caller's concern, consistent with Conversation). by must
// validate.
func (t *Task) Archive(at time.Time, by IdentityRef) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if err := by.Validate(); err != nil {
		return err
	}
	at = at.UTC()
	t.archivedAt = &at
	t.archivedBy = by
	// NOTE: status is intentionally NOT changed (orthogonal archive).
	t.touch(at)
	return nil
}

// FinalizeForArchive moves a NON-terminal task to discarded as it is being archived
// (T339). Archiving is orthogonal and preserves status, so a plan's escape/skipped
// node — which never ran and stays `open` — would otherwise become open+archived: a
// dead task that leaks into the task board / list_tasks(open) yet is locked
// (ErrTaskArchived) so no normal transition can ever finalize it. Calling this in the
// archive cascade BEFORE Archive() closes that hole.
//
// Unlike Discard() it accepts ANY non-terminal status (open/running/reopened) without
// the adjacency check, because an archive abandons whatever was in flight — there is
// no "illegal" non-terminal→discarded here. It MUST run before Archive() (the
// IsArchived guard rejects it afterward — that lock is exactly why this exists). A
// task already terminal (completed/discarded) is left untouched (no-op, nil), so the
// cascade preserves a finished node's real outcome.
func (t *Task) FinalizeForArchive(at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if t.status.IsTerminal() {
		return nil // already concluded — preserve its real outcome
	}
	t.forceDiscard(at)
	return nil
}

// FinalizeArchived is the T339 escape hatch: it concludes an ALREADY-archived but
// non-terminal task to discarded — the ONE permitted write on an archived task. An
// archive is orthogonal, so a legacy/edge path could leave a task archived yet `open`:
// a dead state that leaks into the board / list_tasks(open) but is locked
// (ErrTaskArchived) so no normal mutator — including Discard() — can ever conclude it.
// This concludes (does not edit) such a leaked task. It REQUIRES archived (a live task
// must use Discard → ErrTaskNotArchived) and is a no-op on an already-terminal archived
// task. Wired behind discard_task, which becomes the operator tool to close the leak.
func (t *Task) FinalizeArchived(at time.Time) error {
	if !t.IsArchived() {
		return ErrTaskNotArchived
	}
	if t.status.IsTerminal() {
		return nil
	}
	t.forceDiscard(at)
	return nil
}

// forceDiscard concludes a non-terminal task to discarded, bypassing BOTH the
// adjacency check and the archived read-only lock. It is the shared core of the two
// terminal-cleanup paths (FinalizeForArchive before archiving, FinalizeArchived after)
// — the only places a discard legitimately ignores those guards. Callers MUST gate on
// non-terminal first (this would otherwise overwrite a real completed/discarded
// outcome).
func (t *Task) forceDiscard(at time.Time) {
	t.status = TaskDiscarded
	t.statusChangedAt = at.UTC()
	t.blockedReason = "" // a discarded task is not stuck (mirrors Discard)
	t.touch(at)
}

// SetPlan selects this task into a Plan (v2.9 #283). A task is in 0..1 Plan
// (design §2), so this overwrites any prior plan membership. Metadata edit (NOT
// a status change): does not touch statusChangedAt. The 1:1 DAG-scope invariant
// (§9.8) is enforced at the edge level by the Plan repository.
func (t *Task) SetPlan(planID PlanID, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	t.planID = planID
	t.touch(at)
	return nil
}

// ClearPlan removes this task from its Plan (back to the backlog).
func (t *Task) ClearPlan(at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	t.planID = ""
	t.touch(at)
	return nil
}

// SetTags replaces the task's label set (metadata edit, NOT a status change).
// Each tag is trimmed; blank tags and tags longer than 16 chars are rejected;
// exact duplicates are dropped; more than 10 distinct tags is rejected. The
// cleaned/deduped slice is stored. Does NOT touch statusChangedAt.
func (t *Task) SetTags(tags []string, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	cleaned, err := cleanTags(tags)
	if err != nil {
		return err
	}
	t.tags = cleaned
	t.touch(at)
	return nil
}

// Rename updates the display title (metadata edit, not a state transition).
func (t *Task) Rename(title string, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if strings.TrimSpace(title) == "" {
		return errors.New("projectmanager: task title required")
	}
	t.title = title
	t.touch(at)
	return nil
}

// SetDescription updates the description (metadata edit).
func (t *Task) SetDescription(desc string, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	t.description = desc
	t.touch(at)
	return nil
}

// SetCycleMeta sets the cycle-node git metadata (v2.13.0 I18/F2+F3) — role,
// branch, base, and the skip-merge-check exemption. Pure metadata edit (NOT a
// status change), so statusChangedAt is untouched; rejected on an archived task.
// scaffold_cycle_plan normally stamps these at create via NewTaskInput; this
// setter is the editable path (and resolveDefaultBranch's re-stamp). v2.13.0
// I18/F3 added the role parameter so a re-stamp PRESERVES the node's role (callers
// must pass t.Role() back when only adjusting branch/base).
func (t *Task) SetCycleMeta(role CycleNodeRole, branch, base string, skipMergeCheck bool, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	t.role = role
	t.branch = branch
	t.base = base
	t.skipMergeCheck = skipMergeCheck
	t.touch(at)
	return nil
}

// SetDerivedFromIssue links (or, with issueID=="", UNLINKS) this task to the Issue
// it was derived from (T192 — editable after creation; previously create-only via
// NewTaskInput). Pure metadata edit — NOT a status change, so statusChangedAt is
// untouched. The EXISTENCE + SAME-PROJECT invariant (the linked issue must exist and
// belong to this task's project) is enforced by the AppService, which holds the issue
// repository — the aggregate cannot see other issues. Empty clears the link.
func (t *Task) SetDerivedFromIssue(issueID IssueID, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	t.derivedFromIssue = issueID
	t.touch(at)
	return nil
}

// Assign sets the assignee as METADATA — it does NOT change the task's workflow
// state (v2.8.1 model fix: there is no "assigned" state; an assigned task is
// still "open" until started). Allowed in any non-terminal state; re-targets an
// already-assigned task. The AppService still emits pm.task.assigned so the
// WorkItemProjector dispatches the agent WorkItem.
func (t *Task) Assign(assignee IdentityRef, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if err := assignee.Validate(); err != nil {
		return err
	}
	if t.status.IsTerminal() {
		return ErrIllegalTransition
	}
	t.assignee = assignee
	t.touch(at)
	return nil
}

// Unassign clears the assignee (metadata edit; no state change). Allowed in any
// non-terminal state.
func (t *Task) Unassign(at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if t.status.IsTerminal() {
		return ErrIllegalTransition
	}
	t.assignee = ""
	t.touch(at)
	return nil
}

// Start moves open→running (the agent picked up the work; assignment is metadata,
// not a precondition state). ADR-0046: starting/re-activating a task clears any
// stale blocked_reason — the agent is back, so it is no longer stuck.
func (t *Task) Start(at time.Time) error {
	if err := t.simpleTransition(TaskRunning, at); err != nil {
		return err
	}
	t.blockedReason = ""
	return nil
}

// Block records a stuck-reason ANNOTATION on a RUNNING task (issue I14 §2.5,
// ADR-0046: "blocked" is not a state, so a blocked task can never deadlock). It is
// the SINGLE pause entrypoint — reasonType=input_required means the agent needs a
// user reply, obstacle means an external blocker needs owner/PM intervention. Block
// does NOT change status, KEEPS the assignee, and clears the execution lease (a
// blocked task is a legal pause and needs no heartbeat). Only the assignee agent
// may block its own running task. A reason is required.
func (t *Task) Block(reason string, reasonType BlockReasonType, agentRef IdentityRef, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if t.status != TaskRunning {
		return ErrIllegalTransition
	}
	if t.assignee != agentRef {
		return ErrNotTaskAssignee
	}
	if strings.TrimSpace(reason) == "" {
		return ErrBlockReasonRequired
	}
	t.blockedReason = reason
	t.blockedReasonType = reasonType
	t.blockedComment = ""
	t.executionLeaseExpiresAt = nil
	t.appendLog(TaskActionBlocked, agentRef, agentRef, fmt.Sprintf("[%s] %s", reasonType, reason), at)
	t.touch(at)
	return nil
}

// Unblock is the SINGLE recovery entrypoint (issue I14 §2.5). It clears the block
// annotation and stores comment (the user's reply for input_required, or the
// owner/PM resolution note for obstacle) for the agent to read on resume —
// blockedComment SURVIVES the unblock. Status and assignee are unchanged (the task
// was running the whole time), so it is immediately resumable. Idempotent: a no-op
// (no log, no version bump) when the task is not blocked. Handing the work to a
// DIFFERENT agent goes through RecordReassignment, not Unblock.
func (t *Task) Unblock(comment string, actorRef IdentityRef, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if t.blockedReason == "" {
		return nil // not blocked → idempotent no-op
	}
	t.blockedComment = comment
	t.blockedReason = ""
	t.blockedReasonType = ""
	t.appendLog(TaskActionUnblocked, actorRef, t.assignee, comment, at)
	t.touch(at)
	return nil
}

// RenewLease extends the running agent's execution lease by ttl (issue I14 §2.5,
// driven by the MCP heartbeat tool). Only a RUNNING, non-blocked task can renew: a
// blocked task is a legal pause with no lease (ErrTaskBlocked), and a non-running
// task has no live execution to keep alive (ErrIllegalTransition).
func (t *Task) RenewLease(ttl time.Duration, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if t.status != TaskRunning {
		return ErrIllegalTransition
	}
	if t.blockedReason != "" {
		return ErrTaskBlocked
	}
	exp := at.Add(ttl).UTC()
	t.executionLeaseExpiresAt = &exp
	t.touch(at)
	return nil
}

// ExpireLease reclaims a running task whose agent lease has lapsed (issue I14
// §2.5 — the replacement for the deleted AgentWorkItem.FailFromAgentDeath, driven
// by the background lease-checker). The task returns to open with its assignee
// cleared so the PM can re-dispatch it. It is a no-op (returns nil) when there is
// nothing to reclaim: archived, not running, legally blocked (ANY reasonType — a
// blocked task is never reclaimed by lease), no lease set, or the lease has not yet
// lapsed.
func (t *Task) ExpireLease(at time.Time) error {
	if t.IsArchived() {
		return nil
	}
	if t.status != TaskRunning {
		return nil
	}
	if t.blockedReason != "" {
		return nil // legal pause — not reclaimed by lease
	}
	if t.executionLeaseExpiresAt == nil || at.Before(*t.executionLeaseExpiresAt) {
		return nil
	}
	prev := t.assignee
	t.status = TaskOpen
	t.statusChangedAt = at.UTC()
	t.blockedReason = ""
	t.blockedReasonType = ""
	t.blockedComment = ""
	t.executionLeaseExpiresAt = nil
	t.assignee = ""
	t.appendLog(TaskActionLeaseExpired, IdentityRef("system"), prev, "agent lease expired", at)
	t.touch(at)
	return nil
}

// RecordReassignment reassigns the task to newAssignee and logs it (issue I14
// §2.5 — the replacement for AgentWorkItem Supersede+New). It reuses Assign (which
// validates newAssignee and rejects an archived/terminal task), then clears any
// block annotation + lease so the new agent starts clean, and appends a reassigned
// log attributed to `by`. Assign's touch covers the version bump.
func (t *Task) RecordReassignment(newAssignee, by IdentityRef, at time.Time) error {
	if err := t.Assign(newAssignee, at); err != nil {
		return err
	}
	t.blockedReason = ""
	t.blockedReasonType = ""
	t.blockedComment = ""
	t.executionLeaseExpiresAt = nil
	t.appendLog(TaskActionReassigned, by, newAssignee, "", at)
	return nil
}

// appendLog appends an immutable lifecycle entry (issue I14 §2.4). The entry's ID
// is left empty on purpose: the persistence layer (F2 repo) mints a ULID on insert
// and rehydrate repopulates it, so this domain package needs no id-generation
// dependency.
func (t *Task) appendLog(action TaskAction, actor, agent IdentityRef, note string, at time.Time) {
	t.actionLogs = append(t.actionLogs, TaskActionLog{
		OccurredAt: at.UTC(),
		Action:     action,
		ActorRef:   actor,
		AgentRef:   agent,
		Note:       note,
	})
}

// Complete moves running→completed and records who completed it. ADR-0046: clears
// any blocked_reason (a completed task is not stuck).
func (t *Task) Complete(by IdentityRef, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if err := by.Validate(); err != nil {
		return err
	}
	if !t.status.CanTransitionTo(TaskCompleted) {
		return ErrIllegalTransition
	}
	t.status = TaskCompleted
	t.statusChangedAt = at.UTC()
	t.completedBy = by
	t.blockedReason = ""
	t.touch(at)
	return nil
}

// Discard moves open/running→discarded (terminal; was "Cancel" pre-v2.8.1).
// ADR-0046: clears any blocked_reason (a discarded task is not stuck).
func (t *Task) Discard(at time.Time) error {
	if err := t.simpleTransition(TaskDiscarded, at); err != nil {
		return err
	}
	t.blockedReason = ""
	return nil
}

// SetStatus sets the status to any VALID target with NO adjacency enforcement
// (v2.8.1 @oopslink: "task state = agent's self-reported progress, the center does
// not enforce workflow rules"). The only check is enum validity; any valid state
// is reachable from any state (the Change-status menu offers the full enum). The
// typed transitions (Start/Block/Complete/Discard/Reopen) remain for the agent's
// structured self-reports + the system projector, which carry their own
// side-effects (blocked reason, completedBy); SetStatus is the free user override.
func (t *Task) SetStatus(target TaskStatus, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if !target.IsValid() {
		return ErrInvalidStatus
	}
	if target == t.status {
		return nil // no-op (idempotent); avoids a spurious version bump
	}
	t.status = target
	t.statusChangedAt = at.UTC()
	t.touch(at)
	return nil
}

// Reopen moves completed→reopened.
func (t *Task) Reopen(at time.Time) error { return t.simpleTransition(TaskReopened, at) }

// ToOpenFromReopened moves reopened→open (completing the reopen chain).
func (t *Task) ToOpenFromReopened(at time.Time) error {
	if err := t.simpleTransition(TaskOpen, at); err != nil {
		return err
	}
	// A reopened task starts fresh: clear assignment + completion truth.
	t.assignee = ""
	t.completedBy = ""
	t.blockedReason = ""
	return nil
}

// simpleTransition applies a status-only move guarded by the state machine.
func (t *Task) simpleTransition(to TaskStatus, at time.Time) error {
	if t.IsArchived() {
		return ErrTaskArchived
	}
	if !to.IsValid() {
		return ErrInvalidStatus
	}
	if !t.status.CanTransitionTo(to) {
		return ErrIllegalTransition
	}
	t.status = to
	t.statusChangedAt = at.UTC()
	t.touch(at)
	return nil
}

func (t *Task) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	t.updatedAt = at.UTC()
	t.version++
}

// cleanTags trims, validates, and dedups a tag set shared by Task.SetTags and
// Issue.SetTags (v2.8.1 edit #278): each tag must be 1..16 chars after trimming;
// exact duplicates are dropped (first occurrence kept); at most 10 distinct tags.
// Returns nil for an empty input (no tags).
func cleanTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			return nil, errors.New("projectmanager: tag must be 1..16 chars")
		}
		if len([]rune(tag)) > 16 {
			return nil, errors.New("projectmanager: tag must be 1..16 chars")
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > 10 {
		return nil, errors.New("projectmanager: at most 10 tags allowed")
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

package projectmanager

import (
	"context"
	"time"
)

// Repository interfaces for the ProjectManager ARs (B1, task #96). All
// implementations live in the sqlite subpackage and honor
// persistence.ExecutorFromCtx so the B2 AppServices can compose a write +
// outbox event in one transaction (plan §10 OQ1).

// OrgListQuery is the cross-project, filtered, sorted, SQL-paginated list query
// the org list handlers push down to the repository (real LIMIT/OFFSET + COUNT,
// no handler-side aggregate-then-slice). All fields are optional; the caller
// (handler) resolves the project-id set + status default BEFORE calling.
//
//   - ProjectIDs: restrict to these projects (the handler already applied the
//     project filter + archived-project exclusion). Empty ⇒ empty result.
//   - Statuses: explicit include set; when empty, ExcludeStatuses applies (the
//     "all open" default that hides terminal states). Both empty ⇒ no status
//     predicate (the ?status=all escape hatch).
//   - Assignee: tasks only — matches the full ref OR the bare member-id; "" = any.
//   - Q: case-insensitive substring of title/name (issues/tasks/plans); "" = any.
//   - Created/Updated bounds: inclusive RFC3339 instants (UTC); nil = unbounded.
//   - SortColumn: a row key (created_at|updated_at|status|title|name|org_ref);
//     "" ⇒ updated_at. SortDesc selects direction. The repo maps the key to a
//     vetted DB column (never interpolates raw input).
//   - Limit (<=0 ⇒ no limit) / Offset: the page window.
type OrgListQuery struct {
	ProjectIDs      []ProjectID
	Statuses        []string
	ExcludeStatuses []string
	Assignee        string
	CreatedBy       string // issues: exact created_by (author) filter; ignored elsewhere
	Q               string
	CreatedAfter    *time.Time
	CreatedBefore   *time.Time
	UpdatedAfter    *time.Time
	UpdatedBefore   *time.Time
	SortColumn      string
	SortDesc        bool
	Limit           int
	Offset          int
	// IncludeArchived, when false (the default), excludes ORTHOGONALLY-archived tasks
	// (archived_at != '') from a tasks list (T339): an archived task is read-only and
	// must not surface in the board / list_tasks as if it were live work. Tasks-only —
	// issues/plans have no archived_at column and ignore it. Set true for an explicit
	// "show archived" view.
	IncludeArchived bool
}

// ProjectRepository persists Project ARs.
type ProjectRepository interface {
	Save(ctx context.Context, p *Project) error
	Update(ctx context.Context, p *Project) error
	FindByID(ctx context.Context, id ProjectID) (*Project, error)
	// ListByOrg returns active+archived projects in an Organization.
	ListByOrg(ctx context.Context, orgID string) ([]*Project, error)
	// ListAll returns ALL projects across ALL organizations
	// (operator-global, no org filter), stable-ordered (created_at, id).
	// It is the operator-scoped successor to the retired workforce
	// ProjectRepository.FindAll full scan, used ONLY by operator-scoped
	// readers (CLI `project list`, admin project find-all). It MUST NOT be
	// called from org-scoped / webconsole paths — those use ListByOrg.
	// v2.7 #131 PR-3 (A9-consistent operator scope).
	ListAll(ctx context.Context) ([]*Project, error)
}

// ProjectMemberRepository persists ProjectMember ARs.
type ProjectMemberRepository interface {
	Save(ctx context.Context, m *ProjectMember) error
	FindByID(ctx context.Context, id MemberID) (*ProjectMember, error)
	// FindByProjectAndIdentity is the write-gate lookup (is X a member of P?).
	FindByProjectAndIdentity(ctx context.Context, projectID ProjectID, identityID IdentityRef) (*ProjectMember, error)
	ListByProject(ctx context.Context, projectID ProjectID) ([]*ProjectMember, error)
	Delete(ctx context.Context, id MemberID) error
}

// IssueRepository persists Issue ARs.
type IssueRepository interface {
	Save(ctx context.Context, i *Issue) error
	Update(ctx context.Context, i *Issue) error
	FindByID(ctx context.Context, id IssueID) (*Issue, error)
	ListByProject(ctx context.Context, projectID ProjectID) ([]*Issue, error)
	// FindByStatuses returns issues in any of the given statuses across ALL
	// projects (global), oldest-first, capped at limit (<=0 = uncapped). It is
	// the pm successor to the retired discussion FindByStatus full scan, used by
	// the fleet pending-issues segment's global-admin path (v2.7 #107 #119).
	FindByStatuses(ctx context.Context, statuses []IssueStatus, limit int) ([]*Issue, error)
	// ListOrgPage returns one page of issues across the query's projects, filtered
	// + sorted + LIMIT/OFFSET in SQL, plus the TOTAL (pre-page) count. The org
	// Issues list (cross-project) uses it for real server-side pagination.
	ListOrgPage(ctx context.Context, q OrgListQuery) ([]*Issue, int, error)
}

// TaskRepository persists Task ARs.
// AgentTaskLoad is the per-assignee active-task split behind the agent-load
// metric (T342): Running ("doing") + Pending ("open") non-terminal tasks. The
// load value is Running / (Running+Pending), 0 when the assignee has none.
type AgentTaskLoad struct {
	Running int
	Pending int
}

type TaskRepository interface {
	Save(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
	// ClaimIfUnassigned persists a claimed task (assignee set + status running)
	// ONLY if the stored row is still `open` AND unassigned — the atomic
	// open-claim CAS (T83 §3.3). Returns true when this call won the claim, false
	// when a concurrent claim already took it (no row updated). The passed Task
	// must already carry the post-claim state.
	ClaimIfUnassigned(ctx context.Context, t *Task) (bool, error)
	FindByID(ctx context.Context, id TaskID) (*Task, error)
	ListByProject(ctx context.Context, projectID ProjectID) ([]*Task, error)
	ListByAssignee(ctx context.Context, assignee IdentityRef) ([]*Task, error)
	// CountByStatus returns a grouped count of tasks per status across ALL
	// projects/orgs (global), mirroring the old taskruntime FindByStatus full
	// scan that stats used. since, if non-nil, restricts to tasks created
	// at/after it. v2.7 #107 Phase-2 stats repoint.
	CountByStatus(ctx context.Context, since *time.Time) (map[TaskStatus]int, error)
	// CountActiveByAssignee returns, per assignee, the active-task split (Running
	// "doing" + Pending "open") across ALL projects/orgs in ONE grouped scan — the
	// agent-load metric source (no per-agent N+1). Terminal tasks are excluded;
	// unassigned rows are omitted. v2.14.0 T342.
	CountActiveByAssignee(ctx context.Context) (map[IdentityRef]AgentTaskLoad, error)
	// CountRunningUnblockedByAssignee counts the assignee's tasks that currently
	// occupy a RUN SLOT — status='running' AND blocked_reason IS NULL/'' — which is
	// the EXACT predicate of the dropped single-active partial UNIQUE index
	// (migration 0072, removed by 0084). excludeTaskID, when non-empty, omits that
	// one task id (the task being transitioned, so the caller counts the OTHER live
	// run-slots). It backs the application-layer ≤max_concurrent cap
	// (Service.enforceConcurrencyCap, v2.18.0 W4c) and the running-count
	// observability read. Called inside the start/transition tx; race-safety comes
	// from RunInTx's whole-tx BUSY_SNAPSHOT replay (the read snapshot + a conflicting
	// concurrent commit forces a replay that re-reads a fresh count), mirroring the
	// claim holding-cap. (On a Postgres backend the same guard would take a
	// `SELECT count(*) ... FOR UPDATE` row lock in-tx; see enforceConcurrencyCap.)
	CountRunningUnblockedByAssignee(ctx context.Context, assignee IdentityRef, excludeTaskID TaskID) (int, error)
	// ListRunningUnblockedByAssignee returns the assignee's RUN-SLOT-occupying tasks
	// (the same status='running' AND blocked_reason IS NULL/'' predicate as the count
	// twin) as rows. It backs the report_usage task-attribution fallback (I54): the
	// center fills an empty usage task_id from the agent's running task ONLY when
	// there is exactly one (the unambiguous case), so callers check len()==1.
	ListRunningUnblockedByAssignee(ctx context.Context, assignee IdentityRef) ([]*Task, error)
	// ListActiveByAssignee returns the actual task rows CountActiveByAssignee
	// counts for one assignee (non-terminal tasks not in a terminal plan),
	// stable-ordered (created_at, id) — the list-shaped twin of the backlog
	// metric so the Agent Tasks panel matches the "backlog: N" badge.
	ListActiveByAssignee(ctx context.Context, assignee IdentityRef) ([]*Task, error)
	// ListByStatuses returns tasks whose status is in any of the given statuses,
	// across ALL projects/orgs (global), stable-ordered (created_at, id). Empty
	// input → empty result. v2.7 #107 Phase-2 (proj-B): observability task query
	// reads pm_tasks by status (by-status filter = one status; default = the
	// non-terminal active set).
	ListByStatuses(ctx context.Context, statuses []TaskStatus) ([]*Task, error)
	// ListByPlan returns the tasks selected into a Plan (v2.9 #283), stable-ordered
	// (created_at, id). A task is in 0..1 Plan (design §2).
	ListByPlan(ctx context.Context, planID PlanID) ([]*Task, error)
	// ListUnplannedByProject returns the project's backlog (v2.9): tasks with an
	// empty plan_id — i.e. not yet selected into any Plan. It is the complement of
	// ListByPlan, stable-ordered (created_at, id).
	ListUnplannedByProject(ctx context.Context, projectID ProjectID) ([]*Task, error)
	// ListOrgPage returns one page of tasks across the query's projects, filtered
	// (incl. assignee) + sorted + LIMIT/OFFSET in SQL, plus the TOTAL count.
	ListOrgPage(ctx context.Context, q OrgListQuery) ([]*Task, int, error)
}

// PlanRepository persists Plan ARs and their execution-DAG edges (v2.9 #283).
// The DAG is 1:1-scoped to one Plan (§9.8): every Dependency carries a plan_id
// and AddDependency rejects any edge that would create a cycle or self-edge
// before persisting. Node status is DERIVED, never stored (§9.2) — there is no
// node_status read/write here.
type PlanRepository interface {
	Save(ctx context.Context, p *Plan) error
	Update(ctx context.Context, p *Plan) error
	FindByID(ctx context.Context, id PlanID) (*Plan, error)
	ListByProject(ctx context.Context, projectID ProjectID) ([]*Plan, error)
	// ListOrgPage returns one page of NON-builtin plans across the query's
	// projects, filtered + sorted + LIMIT/OFFSET in SQL, plus the TOTAL count.
	// Rows are base plan ARs; the service enriches only the page (progress).
	ListOrgPage(ctx context.Context, q OrgListQuery) ([]*Plan, int, error)
	// ListRunningPlans returns every Plan in status `running` across ALL projects
	// (global, no project filter), stable-ordered (created_at, id). It backs the
	// v2.9 P2-3 reconciliation sweep — the background safety net that re-dispatches
	// ready-but-undispatched nodes for missed events / crash recovery.
	ListRunningPlans(ctx context.Context) ([]*Plan, error)
	Delete(ctx context.Context, id PlanID) error
	// DeletePlan hard-deletes a Plan and its DAG state in one call (v2.9 P3): it
	// CASCADE-removes the plan's depends_on edges (pm_task_dependencies) and dispatch
	// records (pm_plan_dispatch_records) before deleting the pm_plans row, so no
	// orphan edge/record survives. The caller (DeletePlan AppService) UNLOADs the
	// plan's tasks back to the backlog FIRST (tasks are NOT deleted). Returns
	// ErrPlanNotFound if the plan row does not exist.
	DeletePlan(ctx context.Context, id PlanID) error
	// AddDependency loads the plan's existing edges, calls WouldCreateCycle, and
	// rejects (ErrPlanCycle / ErrSelfDependency) before inserting.
	AddDependency(ctx context.Context, dep Dependency) error
	RemoveDependency(ctx context.Context, dep Dependency) error
	// ListDependencies returns all depends_on edges scoped to one Plan (§9.8).
	ListDependencies(ctx context.Context, planID PlanID) ([]Dependency, error)
	// ListDependenciesByPlans is the BATCH form of ListDependencies: it returns the
	// depends_on edges for ALL of the given plans in ONE query (WHERE plan_id IN
	// (...)), so a per-project read (ListPlanSummaries) loads every plan's DAG
	// without an N+1 loop. Each Dependency carries its PlanID so callers group
	// in-memory. Empty planIDs → empty slice (no malformed `IN ()`).
	ListDependenciesByPlans(ctx context.Context, planIDs []PlanID) ([]Dependency, error)

	// Dispatch records (v2.9 #285, §9.3) — the ONLY orchestrator-owned stored
	// state. RecordDispatch writes the once-only {plan_id, task_id} record when a
	// ready node's @mention is posted (idempotent on the PK: a duplicate write for
	// an already-dispatched node is a no-op, never an error). ListDispatchRecords
	// returns one Plan's records (§9.8 per-plan scoping). ClearDispatch deletes one
	// node's record so a creator re-run re-dispatches it on the next advance.
	RecordDispatch(ctx context.Context, planID PlanID, taskID TaskID, at time.Time, messageID string) error
	ListDispatchRecords(ctx context.Context, planID PlanID) ([]DispatchRecord, error)
	// ListDispatchRecordsByPlans is the BATCH form of ListDispatchRecords: it
	// returns the dispatch records for ALL of the given plans in ONE query (WHERE
	// plan_id IN (...)), so a per-project read (ListPlanSummaries) loads every
	// plan's dispatch state without an N+1 loop. Each DispatchRecord carries its
	// PlanID so callers group in-memory. Empty planIDs → empty slice.
	ListDispatchRecordsByPlans(ctx context.Context, planIDs []PlanID) ([]DispatchRecord, error)
	ClearDispatch(ctx context.Context, planID PlanID, taskID TaskID) error

	// Decision outcomes (v2.13.0 I18/B1, control-flow §2.3) — a decision node's
	// recorded outcome (latest-wins per plan_id,task_id), routing its conditional/
	// loopback out-edges. RecordDecisionOutcome upserts (overwrite on re-decision);
	// ListDecisionOutcomes returns one plan's outcomes (fed to DerivePlanView);
	// ClearDecisionOutcome removes one (loopback reopen → re-decide).
	RecordDecisionOutcome(ctx context.Context, planID PlanID, taskID TaskID, outcome string, at time.Time) error
	ListDecisionOutcomes(ctx context.Context, planID PlanID) ([]DecisionOutcome, error)
	ListDecisionOutcomesByPlans(ctx context.Context, planIDs []PlanID) ([]DecisionOutcome, error)
	ClearDecisionOutcome(ctx context.Context, planID PlanID, taskID TaskID) error

	// Loop rounds (v2.13.0 I18/B1, control-flow §4) — the completed-round count per
	// loopback edge, for the max-rounds exit guard. GetLoopRound returns the current
	// count (0 if none); IncrementLoopRound bumps it and returns the new round.
	GetLoopRound(ctx context.Context, planID PlanID, from, to TaskID) (int, error)
	IncrementLoopRound(ctx context.Context, planID PlanID, from, to TaskID) (int, error)

	// Review verdicts (v2.13.0 I18/B3, T468 / issue-f7ad5a54) — a Review node's
	// structured, SINGLE-SLOT, round-tagged verdict feeding B3's auto-decision.
	// RecordReviewVerdict upserts (latest-wins per plan_id,task_id — each round
	// overwrites); GetReviewVerdict returns one node's verdict (ok=false when none);
	// ListReviewVerdicts returns a plan's verdicts (PD read path — see the verdict
	// without entering the Review conversation).
	RecordReviewVerdict(ctx context.Context, planID PlanID, v ReviewVerdict, at time.Time) error
	GetReviewVerdict(ctx context.Context, planID PlanID, taskID TaskID) (ReviewVerdict, bool, error)
	ListReviewVerdicts(ctx context.Context, planID PlanID) ([]ReviewVerdict, error)
}

// PlanFindingRepository persists PlanFinding ARs (v2.10, ADR-0053 — the DeLM
// plan-scoped shared-findings store). Findings are IMMUTABLE: there is Save (once)
// + reads + Delete (retract / cascade), but no Update. ListByPlan backs both the
// dispatch injection and the list_findings tool; DeleteByPlan is the Plan-delete
// cascade (a plan's findings die with the plan).
type PlanFindingRepository interface {
	Save(ctx context.Context, f *PlanFinding) error
	FindByID(ctx context.Context, id PlanFindingID) (*PlanFinding, error)
	// ListByPlan returns one Plan's findings, stable-ordered (created_at, id).
	ListByPlan(ctx context.Context, planID PlanID) ([]*PlanFinding, error)
	// CountByPlan returns the number of findings in a Plan (for the bounded
	// dispatch read's "latest N of M" notice).
	CountByPlan(ctx context.Context, planID PlanID) (int, error)
	// ListLatestByPlan returns at most `limit` of a Plan's most-recent findings,
	// re-ordered oldest-first for display. Backs the bounded dispatch injection so a
	// plan with a large shared context does not load every row into the dispatch tx.
	ListLatestByPlan(ctx context.Context, planID PlanID, limit int) ([]*PlanFinding, error)
	// Delete removes one finding (retract). ErrPlanFindingNotFound if absent.
	Delete(ctx context.Context, id PlanFindingID) error
	// DeleteByPlan removes every finding of a Plan (the Plan-delete cascade).
	// Deleting zero rows is NOT an error (a plan may have no findings).
	DeleteByPlan(ctx context.Context, planID PlanID) error
}

// TaskSubscriberRepository persists manual Task subscriber records.
type TaskSubscriberRepository interface {
	Add(ctx context.Context, s *TaskSubscriber) error
	Remove(ctx context.Context, taskID TaskID, identityID IdentityRef) error
	ListByTask(ctx context.Context, taskID TaskID) ([]*TaskSubscriber, error)
}

// IssueSubscriberRepository persists manual Issue subscriber records.
type IssueSubscriberRepository interface {
	Add(ctx context.Context, s *IssueSubscriber) error
	Remove(ctx context.Context, issueID IssueID, identityID IdentityRef) error
	ListByIssue(ctx context.Context, issueID IssueID) ([]*IssueSubscriber, error)
}

// TaskActionLogRepository persists the append-only Task lifecycle log (v2.14.0
// I14 §7.3) that replaces the deleted agent_work_item_transitions. The aggregate
// never mints infra IDs (see Task.TaskActionLog doc): Append assigns a ULID to any
// entry whose ID is empty, then inserts it — so the service layer (F3) appends the
// new entries Task.ActionLogs() produced after a domain op, exactly once, in the
// same tx as the Task.Update. ListByTask returns a task's log stable-ordered
// (occurred_at, id) — the source for RehydrateTaskInput.ActionLogs and the
// `agent_started` interactions count (issue §九).
type TaskActionLogRepository interface {
	// Append inserts the given entries for taskID. An entry with an empty ID gets
	// a fresh ULID (time-ordered); a non-empty ID is inserted as-is (idempotent
	// re-inserts are the caller's responsibility — entries are immutable). It runs
	// in the caller's ambient tx when one is set.
	Append(ctx context.Context, taskID TaskID, logs []TaskActionLog) error
	// ListByTask returns taskID's action log stable-ordered (occurred_at, id);
	// empty (not an error) when the task has no entries.
	ListByTask(ctx context.Context, taskID TaskID) ([]TaskActionLog, error)
}

// AuditLogRepository persists the append-only object-level change ledger
// (pm_audit_log, migration 0099 — design §4.3). Like TaskActionLogRepository the
// aggregate never mints infra IDs: Append assigns a ULID to any entry whose ID is
// empty, then inserts it in the caller's ambient tx (so the audit row commits
// atomically with the change it records — no eventual-consistency gap). ListByObject
// returns one object's ledger time-DESCENDING (newest first) with cursor pagination
// for the read API (design §6).
type AuditLogRepository interface {
	// Append inserts entry, minting a fresh ULID when entry.ID is empty. Runs in the
	// caller's ambient tx when ctx carries one.
	Append(ctx context.Context, entry AuditEntry) error
	// ListByObject returns (objType, objID)'s ledger newest-first. cursor is the
	// opaque id of the last row from the previous page ("" = first page); limit caps
	// the page size (<=0 ⇒ a default). The returned nextCursor is "" when the page is
	// the last one.
	ListByObject(ctx context.Context, objType AuditObjectType, objID string, cursor string, limit int) (entries []AuditEntry, nextCursor string, err error)
}

// CodeRepoRefRepository persists CodeRepoRef records attached to a Project.
type CodeRepoRefRepository interface {
	Save(ctx context.Context, c *CodeRepoRef) error
	// Update persists mutable ref fields (label, repo_id, is_primary); url/project
	// are immutable (v2.18.4 BE-1).
	Update(ctx context.Context, c *CodeRepoRef) error
	FindByID(ctx context.Context, id string) (*CodeRepoRef, error)
	ListByProject(ctx context.Context, projectID ProjectID) ([]*CodeRepoRef, error)
	Delete(ctx context.Context, id string) error
	// ClearPrimaryForProject unsets is_primary on all of a project's refs except
	// exceptID — the at-most-one-primary invariant when setting a new primary
	// (v2.18.4 BE-1).
	ClearPrimaryForProject(ctx context.Context, projectID ProjectID, exceptID string) error
}

// TemplateRepository persists Template aggregates.
type TemplateRepository interface {
	Save(ctx context.Context, t *Template) error
	Update(ctx context.Context, t *Template) error
	FindByID(ctx context.Context, id TemplateID) (*Template, error)
	ListByOrg(ctx context.Context, orgID string) ([]*Template, error)
	Delete(ctx context.Context, id TemplateID) error
}

// OrgSequenceRepository allocates per-organization, per-entity-type monotonic
// numbers (v2.7.1 #245 — the T<n>/I<n> display/reference tokens). Allocate is
// atomic + race-safe (one SQL UPSERT...RETURNING; SQLite serializes per-row
// writes), so concurrent CreateTask/CreateIssue never collide or skip.
type OrgSequenceRepository interface {
	// Allocate returns the next number for (orgID, entityType) and advances the
	// counter, in the caller's tx. entityType is "issue" or "task".
	Allocate(ctx context.Context, orgID, entityType string) (int, error)
}

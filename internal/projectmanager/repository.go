package projectmanager

import (
	"context"
	"time"
)

// Repository interfaces for the ProjectManager ARs (B1, task #96). All
// implementations live in the sqlite subpackage and honor
// persistence.ExecutorFromCtx so the B2 AppServices can compose a write +
// outbox event in one transaction (plan §10 OQ1).

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
}

// TaskRepository persists Task ARs.
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

// CodeRepoRefRepository persists CodeRepoRef records attached to a Project.
type CodeRepoRefRepository interface {
	Save(ctx context.Context, c *CodeRepoRef) error
	FindByID(ctx context.Context, id string) (*CodeRepoRef, error)
	ListByProject(ctx context.Context, projectID ProjectID) ([]*CodeRepoRef, error)
	Delete(ctx context.Context, id string) error
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

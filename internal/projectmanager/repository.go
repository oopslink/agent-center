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

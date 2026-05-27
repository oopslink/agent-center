package discussion

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// IssueFilter narrows IssueRepository queries.
type IssueFilter struct {
	Status *Status
	Cursor *IssueID
	Limit  int
}

// DefaultIssueLimit is the cap when Filter.Limit <= 0.
const DefaultIssueLimit = 100

// IssueRepository per discussion/00 § 5.1.
//
// Per conventions § 9.w the underlying schema does not declare any FOREIGN
// KEY constraint; referential integrity (project_id / conversation_id /
// opener identity) is enforced at the application layer (IssueLifecycleService
// + IssueBindConversationService etc.).
type IssueRepository interface {
	FindByID(ctx context.Context, id IssueID) (*Issue, error)
	FindByProject(ctx context.Context, projectID string, filter IssueFilter) ([]*Issue, error)
	FindByStatus(ctx context.Context, status Status, filter IssueFilter) ([]*Issue, error)
	FindByOpener(ctx context.Context, openerIdentityID string) ([]*Issue, error)
	// FindAll returns every issue, with optional status / cursor / limit
	// from IssueFilter applied. Added in v2.5.15 to back the Web Console
	// "All projects" filter — previously the only cross-project read
	// was FindByStatus, which required a concrete status.
	FindAll(ctx context.Context, filter IssueFilter) ([]*Issue, error)
	Save(ctx context.Context, i *Issue) error
	UpdateStatus(ctx context.Context, id IssueID, from, to Status, version int, at time.Time) error
	UpdateConversationID(ctx context.Context, id IssueID, conversationID conversation.ConversationID, version int, at time.Time) error
	UpdateConclusion(ctx context.Context, id IssueID, summary, concludedBy string, concludedAt time.Time, version int) error
	UpdateRelatedConversationIDs(ctx context.Context, id IssueID, ids []conversation.ConversationID, version int, at time.Time) error
	UpdateWithdraw(ctx context.Context, id IssueID, reason, message, withdrawnBy string, withdrawnAt time.Time, version int) error
	// v2.5.x #64
	UpdateMetadata(ctx context.Context, id IssueID, title, description string, version int, at time.Time) error
	UpdateReopen(ctx context.Context, id IssueID, version int, at time.Time) error
}

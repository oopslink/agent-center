// Package discussion hosts the Discussion BC tactical types (single AR:
// Issue + VOs + Repository interface + sentinel errors).
//
// Per discussion/00-overview § 1: single AR (Issue), 6-state state machine,
// no IssueComment entity (议事 messages live in Conversation BC per
// ADR-0021). Per conventions § 9.w: schema does NOT declare FOREIGN KEY;
// referential integrity is enforced at the application layer.
package discussion

import "errors"

// IssueID is the ULID string identifying an Issue AR.
type IssueID string

// String returns the underlying ULID.
func (id IssueID) String() string { return string(id) }

// Sentinel errors per discussion/00 § 5.1.
//
// Callers MUST use errors.Is to discriminate. The Repository implementation
// is responsible for returning these on the documented conditions.
var (
	// ErrIssueNotFound is returned when an Issue id has no row.
	ErrIssueNotFound = errors.New("discussion: issue not found")
	// ErrIssueAlreadyExists is returned on duplicate Save (uniq PK clash).
	ErrIssueAlreadyExists = errors.New("discussion: issue already exists")
	// ErrIssueInvalidTransition is returned when a status transition is not
	// in the allowed set (status.go transition table).
	ErrIssueInvalidTransition = errors.New("discussion: invalid issue status transition")
	// ErrIssueVersionConflict is returned by CAS UPDATE when version no
	// longer matches (optimistic lock conflict).
	ErrIssueVersionConflict = errors.New("discussion: issue version conflict (optimistic lock)")
	// ErrIssueAlreadyConcluded is returned when conclude is called on an
	// already-concluded or terminal-conclude issue.
	ErrIssueAlreadyConcluded = errors.New("discussion: issue already concluded")
	// ErrIssueWithdrawn is returned when an action requires a non-withdrawn
	// issue (e.g. comment / re-conclude).
	ErrIssueWithdrawn = errors.New("discussion: issue is withdrawn, cannot mutate")
	// ErrIssueNoConversationBound is returned by IssueCommentService when
	// the Issue has no conversation_id bound; caller must run
	// `issue bind-conversation` first.
	ErrIssueNoConversationBound = errors.New("discussion: issue has no conversation bound (use issue bind-conversation)")
)

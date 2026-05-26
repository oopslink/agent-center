package discussion

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// Issue is the single Discussion BC AR (discussion/00-overview § 1.1).
//
// Invariants per § 2:
//  1. status is monotonic; terminal states (closed_*, withdrawn) are sticky
//  2. opener_identity_id is set at construction and never mutates
//  3. closed_with_tasks requires conclude flow to have already validated /
//     committed the IssueConcludeSpawn batch
//  4. conversation_id is null→non-null only; rebinding to a different
//     conversation is not allowed in v1
//  5. terminal status (closed_* / withdrawn) blocks further mutation
type Issue struct {
	id                       IssueID
	projectID                string
	title                    string
	description              string
	descriptionBlobRef       string
	openedByIdentityID       string
	origin                   Origin
	openedAt                 time.Time
	status                   Status
	concludedAt              *time.Time
	conclusionSummary        string
	concludedByIdentityID    string
	withdrawReason           string
	withdrawMessage          string
	conversationID           conversation.ConversationID
	relatedConversationIDs   []conversation.ConversationID
	createdAt                time.Time
	updatedAt                time.Time
	version                  int
}

// NewIssueInput captures the constructor arguments.
type NewIssueInput struct {
	ID                  IssueID
	ProjectID           string
	Title               string
	Description         string
	DescriptionBlobRef  string
	OpenedByIdentityID  string
	Origin              Origin
	ConversationID      conversation.ConversationID // optional; sync-build path fills this
	OpenedAt            time.Time
}

// NewIssue constructs a fresh open Issue. Returns the validation error if
// any invariant is violated.
func NewIssue(in NewIssueInput) (*Issue, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("issue: id required")
	}
	if strings.TrimSpace(in.ProjectID) == "" {
		return nil, errors.New("issue: project_id required")
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("issue: title required")
	}
	if strings.TrimSpace(in.OpenedByIdentityID) == "" {
		return nil, errors.New("issue: opened_by_identity_id required")
	}
	if !in.Origin.IsValid() {
		return nil, fmt.Errorf("issue: invalid origin %q", in.Origin)
	}
	if in.OpenedAt.IsZero() {
		return nil, errors.New("issue: opened_at required")
	}
	at := in.OpenedAt.UTC()
	return &Issue{
		id:                 in.ID,
		projectID:          in.ProjectID,
		title:              in.Title,
		description:        in.Description,
		descriptionBlobRef: in.DescriptionBlobRef,
		openedByIdentityID: in.OpenedByIdentityID,
		origin:             in.Origin,
		conversationID:     in.ConversationID,
		openedAt:           at,
		status:             StatusOpen,
		createdAt:          at,
		updatedAt:          at,
		version:            1,
	}, nil
}

// RehydrateIssueInput is the repository round-trip input (no invariants
// checked beyond shape / enum validity).
type RehydrateIssueInput struct {
	ID                       IssueID
	ProjectID                string
	Title                    string
	Description              string
	DescriptionBlobRef       string
	OpenedByIdentityID       string
	Origin                   Origin
	OpenedAt                 time.Time
	Status                   Status
	ConcludedAt              *time.Time
	ConclusionSummary        string
	ConcludedByIdentityID    string
	WithdrawReason           string
	WithdrawMessage          string
	ConversationID           conversation.ConversationID
	RelatedConversationIDs   []conversation.ConversationID
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Version                  int
}

// RehydrateIssue reconstructs from persisted fields.
func RehydrateIssue(in RehydrateIssueInput) (*Issue, error) {
	if !in.Status.IsValid() {
		return nil, fmt.Errorf("issue: invalid status %q", in.Status)
	}
	if !in.Origin.IsValid() {
		return nil, fmt.Errorf("issue: invalid origin %q", in.Origin)
	}
	if in.Version < 1 {
		return nil, errors.New("issue: version must be >= 1")
	}
	var concAt *time.Time
	if in.ConcludedAt != nil {
		v := in.ConcludedAt.UTC()
		concAt = &v
	}
	related := make([]conversation.ConversationID, len(in.RelatedConversationIDs))
	copy(related, in.RelatedConversationIDs)
	return &Issue{
		id:                     in.ID,
		projectID:              in.ProjectID,
		title:                  in.Title,
		description:            in.Description,
		descriptionBlobRef:     in.DescriptionBlobRef,
		openedByIdentityID:     in.OpenedByIdentityID,
		origin:                 in.Origin,
		openedAt:               in.OpenedAt.UTC(),
		status:                 in.Status,
		concludedAt:            concAt,
		conclusionSummary:      in.ConclusionSummary,
		concludedByIdentityID:  in.ConcludedByIdentityID,
		withdrawReason:         in.WithdrawReason,
		withdrawMessage:        in.WithdrawMessage,
		conversationID:         in.ConversationID,
		relatedConversationIDs: related,
		createdAt:              in.CreatedAt.UTC(),
		updatedAt:              in.UpdatedAt.UTC(),
		version:                in.Version,
	}, nil
}

// ----- Getters -----

func (i *Issue) ID() IssueID                    { return i.id }
func (i *Issue) ProjectID() string              { return i.projectID }
func (i *Issue) Title() string                  { return i.title }
func (i *Issue) Description() string            { return i.description }
func (i *Issue) DescriptionBlobRef() string     { return i.descriptionBlobRef }
func (i *Issue) OpenedByIdentityID() string     { return i.openedByIdentityID }
func (i *Issue) Origin() Origin                 { return i.origin }
func (i *Issue) OpenedAt() time.Time            { return i.openedAt }
func (i *Issue) Status() Status                 { return i.status }
func (i *Issue) ConclusionSummary() string      { return i.conclusionSummary }
func (i *Issue) ConcludedByIdentityID() string  { return i.concludedByIdentityID }
func (i *Issue) WithdrawReason() string         { return i.withdrawReason }
func (i *Issue) WithdrawMessage() string        { return i.withdrawMessage }
func (i *Issue) ConversationID() conversation.ConversationID { return i.conversationID }
func (i *Issue) CreatedAt() time.Time           { return i.createdAt }
func (i *Issue) UpdatedAt() time.Time           { return i.updatedAt }
func (i *Issue) Version() int                   { return i.version }

// ConcludedAt returns a defensive copy.
func (i *Issue) ConcludedAt() *time.Time {
	if i.concludedAt == nil {
		return nil
	}
	v := i.concludedAt.UTC()
	return &v
}

// RelatedConversationIDs returns a defensive copy.
func (i *Issue) RelatedConversationIDs() []conversation.ConversationID {
	return append([]conversation.ConversationID(nil), i.relatedConversationIDs...)
}

// IsTerminal reports whether the issue is in a terminal status.
func (i *Issue) IsTerminal() bool { return i.status.IsTerminal() }

// HasConversation reports whether issue.conversation_id is bound.
func (i *Issue) HasConversation() bool {
	return strings.TrimSpace(string(i.conversationID)) != ""
}

// ----- Lifecycle ops -----

// MarkUnderDiscussion transitions open → under_discussion. Idempotent
// when already under_discussion. Rejects from any terminal / concluded
// state with ErrIssueInvalidTransition.
func (i *Issue) MarkUnderDiscussion(now time.Time) error {
	if i.status == StatusUnderDiscussion {
		return nil // idempotent
	}
	if !CanTransitionTo(i.status, StatusUnderDiscussion) {
		return fmt.Errorf("%w: %s → under_discussion", ErrIssueInvalidTransition, i.status)
	}
	i.status = StatusUnderDiscussion
	i.updatedAt = now.UTC()
	i.version++
	return nil
}

// Withdraw transitions any non-terminal state to withdrawn. reason +
// message are both required (conventions § 16).
func (i *Issue) Withdraw(reason, message string, actor string, now time.Time) error {
	if i.status == StatusWithdrawn {
		return ErrIssueWithdrawn
	}
	if i.IsTerminal() {
		return fmt.Errorf("%w: %s → withdrawn", ErrIssueInvalidTransition, i.status)
	}
	if !CanTransitionTo(i.status, StatusWithdrawn) {
		return fmt.Errorf("%w: %s → withdrawn", ErrIssueInvalidTransition, i.status)
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("issue: withdraw reason required (conventions § 16)")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("issue: withdraw message required (conventions § 16)")
	}
	if strings.TrimSpace(actor) == "" {
		return errors.New("issue: withdraw actor required")
	}
	i.status = StatusWithdrawn
	i.withdrawReason = reason
	i.withdrawMessage = message
	i.concludedByIdentityID = actor
	t := now.UTC()
	i.concludedAt = &t
	i.updatedAt = t
	i.version++
	return nil
}

// Conclude transitions to a closed_* terminal per the resolution. tasks
// have already been spawned by the caller (IssueLifecycleService.Conclude
// orchestrates the cross-BC tx and feeds the spawned ids through summary
// text). This method only flips the AR state.
func (i *Issue) Conclude(resolution Resolution, concludedBy string, now time.Time) error {
	if err := resolution.Validate(); err != nil {
		return err
	}
	if i.status == StatusWithdrawn {
		return ErrIssueWithdrawn
	}
	if i.IsTerminal() {
		return ErrIssueAlreadyConcluded
	}
	target := resolution.Kind.TargetStatus()
	if !CanTransitionTo(i.status, target) {
		return fmt.Errorf("%w: %s → %s", ErrIssueInvalidTransition, i.status, target)
	}
	if strings.TrimSpace(concludedBy) == "" {
		return errors.New("issue: concluded_by required")
	}
	t := now.UTC()
	i.status = target
	i.conclusionSummary = resolution.Summary
	i.concludedByIdentityID = concludedBy
	i.concludedAt = &t
	i.updatedAt = t
	i.version++
	return nil
}

// UpdateMetadata edits title + description on a non-terminal issue
// (v2.5.x #64). Terminal issues must be reopened first if the operator
// wants to change them. Bumps version on success.
func (i *Issue) UpdateMetadata(title, description string, now time.Time) error {
	if i.IsTerminal() {
		return fmt.Errorf("%w: terminal issue is immutable (reopen first)", ErrIssueInvalidTransition)
	}
	if strings.TrimSpace(title) == "" {
		return errors.New("issue: title required")
	}
	i.title = title
	i.description = description
	i.updatedAt = now.UTC()
	i.version++
	return nil
}

// Reopen transitions any concluded/withdrawn terminal back to open
// (v2.5.x #64, semantics (c) per @oopslink #agent-center:93118955).
// Spawned tasks (closed_with_tasks) are NOT cascaded — the mental model
// is "the discussion is reopened" rather than "we abandon the work that
// was already spawned". Clears conclusion / withdraw fields since they
// no longer reflect current state; the event log captures the
// historical conclude / withdraw.
func (i *Issue) Reopen(reopenedBy string, now time.Time) error {
	if !i.IsTerminal() {
		return fmt.Errorf("%w: %s is not reopen-able (not terminal)", ErrIssueInvalidTransition, i.status)
	}
	if !CanTransitionTo(i.status, StatusOpen) {
		return fmt.Errorf("%w: %s → open not allowed", ErrIssueInvalidTransition, i.status)
	}
	if strings.TrimSpace(reopenedBy) == "" {
		return errors.New("issue: reopen actor required")
	}
	i.status = StatusOpen
	i.conclusionSummary = ""
	i.concludedByIdentityID = ""
	i.concludedAt = nil
	i.withdrawReason = ""
	i.withdrawMessage = ""
	i.updatedAt = now.UTC()
	i.version++
	return nil
}

// BindConversation sets conversation_id (null → non-null only). Rebinding
// to a different id is rejected (invariant 5).
func (i *Issue) BindConversation(convID conversation.ConversationID, now time.Time) error {
	if i.IsTerminal() {
		return fmt.Errorf("%w: terminal issue rejects bind", ErrIssueInvalidTransition)
	}
	if strings.TrimSpace(string(convID)) == "" {
		return errors.New("issue: conversation_id required for BindConversation")
	}
	if i.HasConversation() {
		return fmt.Errorf("%w: issue already bound to conversation %s", ErrIssueInvalidTransition, i.conversationID)
	}
	i.conversationID = convID
	i.updatedAt = now.UTC()
	i.version++
	return nil
}

// AddRelatedConversation appends a weakly-linked conversation_id, deduping
// against existing entries. No-op when already present.
func (i *Issue) AddRelatedConversation(convID conversation.ConversationID, now time.Time) error {
	if strings.TrimSpace(string(convID)) == "" {
		return errors.New("issue: related conversation_id required")
	}
	if i.IsTerminal() {
		// Terminal issues can still accept link-conversation (read-only
		// metadata about the lineage doesn't violate the "封锁 IO"
		// invariant — § 2 talks about議事 IO writes to issue.conversation_id).
		// But to stay conservative we reject on terminal.
		return fmt.Errorf("%w: terminal issue rejects link", ErrIssueInvalidTransition)
	}
	for _, existing := range i.relatedConversationIDs {
		if existing == convID {
			return nil
		}
	}
	i.relatedConversationIDs = append(i.relatedConversationIDs, convID)
	i.updatedAt = now.UTC()
	i.version++
	return nil
}

// MarshalRelatedConversationIDsJSON serialises the slice as JSON array for
// repository storage. Empty slice → "[]".
func (i *Issue) MarshalRelatedConversationIDsJSON() (string, error) {
	if len(i.relatedConversationIDs) == 0 {
		return "[]", nil
	}
	asStr := make([]string, len(i.relatedConversationIDs))
	for k, id := range i.relatedConversationIDs {
		asStr[k] = string(id)
	}
	b, err := json.Marshal(asStr)
	if err != nil {
		return "", fmt.Errorf("marshal related_conversation_ids: %w", err)
	}
	return string(b), nil
}

// UnmarshalRelatedConversationIDsJSON parses the JSON storage shape.
func UnmarshalRelatedConversationIDsJSON(s string) ([]conversation.ConversationID, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var raw []string
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("unmarshal related_conversation_ids: %w", err)
	}
	out := make([]conversation.ConversationID, len(raw))
	for i, s := range raw {
		out[i] = conversation.ConversationID(s)
	}
	return out, nil
}

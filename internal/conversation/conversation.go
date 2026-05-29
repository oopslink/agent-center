package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Conversation is the Conversation BC AR (v2 per ADR-0032 + ADR-0034).
//
// Schema fields (per migration 0020):
//   - identifier: id, kind (immutable)
//   - universal naming: name, description (channel kind requires name)
//   - hierarchy: parentConversationID
//   - lifecycle: status (active → closed → archived), opened/closed/archived
//     timestamps + closed reason/message
//   - audit: createdBy / archivedBy / participants (JSON VO)
//   - optimistic locking: version
type Conversation struct {
	id                   ConversationID
	kind                 ConversationKind
	ownerRef             OwnerRef
	name                 string
	description          string
	parentConversationID ConversationID
	participants         []ParticipantElement
	createdBy            IdentityRef
	status               ConversationStatus
	openedAt             time.Time
	closedAt             *time.Time
	closedReason         string
	closedMessage        string
	archivedAt           *time.Time
	archivedBy           IdentityRef
	createdAt            time.Time
	updatedAt            time.Time
	version              int
	organizationID       string
}

// NewConversationInput captures the constructor args.
type NewConversationInput struct {
	ID                   ConversationID
	Kind                 ConversationKind
	OwnerRef             OwnerRef
	Name                 string
	Description          string
	ParentConversationID ConversationID
	CreatedBy            IdentityRef
	OpenedAt             time.Time
	Participants         []ParticipantElement
	OrganizationID       string
}

// NewConversation constructs a fresh active Conversation. Per ADR-0032:
// channel kind requires non-empty name; other kinds may have name=NULL.
// task / issue kinds are accepted at AR level (for cross-BC sync-create
// paths); direct CLI `conversation open` validation happens at service.
func NewConversation(in NewConversationInput) (*Conversation, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("conversation: id required")
	}
	if !in.Kind.IsValid() {
		return nil, ErrConversationInvalidKind
	}
	if in.Kind == ConversationKindProjectChannel && strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("conversation: name required for kind=project_channel (ADR-0047 §1)")
	}
	if err := in.CreatedBy.Validate(); err != nil {
		return nil, fmt.Errorf("conversation: created_by: %w", err)
	}
	if in.OpenedAt.IsZero() {
		return nil, errors.New("conversation: opened_at required")
	}
	at := in.OpenedAt.UTC()
	parts := append([]ParticipantElement(nil), in.Participants...)
	return &Conversation{
		id:                   in.ID,
		kind:                 in.Kind,
		ownerRef:             in.OwnerRef,
		name:                 in.Name,
		description:          in.Description,
		parentConversationID: in.ParentConversationID,
		participants:         parts,
		createdBy:            in.CreatedBy,
		status:               ConversationActive,
		openedAt:             at,
		createdAt:            at,
		updatedAt:            at,
		version:              1,
		organizationID:       in.OrganizationID,
	}, nil
}

// RehydrateConversationInput is for repository round-trip.
type RehydrateConversationInput struct {
	ID                   ConversationID
	Kind                 ConversationKind
	OwnerRef             OwnerRef
	Name                 string
	Description          string
	ParentConversationID ConversationID
	Participants         []ParticipantElement
	CreatedBy            IdentityRef
	Status               ConversationStatus
	OpenedAt             time.Time
	ClosedAt             *time.Time
	ClosedReason         string
	ClosedMessage        string
	ArchivedAt           *time.Time
	ArchivedBy           IdentityRef
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
	OrganizationID       string
}

// RehydrateConversation reconstructs without invariant checks.
func RehydrateConversation(in RehydrateConversationInput) (*Conversation, error) {
	if !in.Kind.IsValid() {
		return nil, ErrConversationInvalidKind
	}
	if !in.Status.IsValid() {
		return nil, ErrConversationInvalidStatus
	}
	if in.Version < 1 {
		return nil, errors.New("conversation: version must be >= 1")
	}
	parts := append([]ParticipantElement(nil), in.Participants...)
	return &Conversation{
		id:                   in.ID,
		kind:                 in.Kind,
		ownerRef:             in.OwnerRef,
		name:                 in.Name,
		description:          in.Description,
		parentConversationID: in.ParentConversationID,
		participants:         parts,
		createdBy:            in.CreatedBy,
		status:               in.Status,
		openedAt:             in.OpenedAt.UTC(),
		closedAt:             copyTimePtr(in.ClosedAt),
		closedReason:         in.ClosedReason,
		closedMessage:        in.ClosedMessage,
		archivedAt:           copyTimePtr(in.ArchivedAt),
		archivedBy:           in.ArchivedBy,
		createdAt:            in.CreatedAt.UTC(),
		updatedAt:            in.UpdatedAt.UTC(),
		version:              in.Version,
		organizationID:       in.OrganizationID,
	}, nil
}

// Getters.

func (c *Conversation) ID() ConversationID                   { return c.id }
func (c *Conversation) Kind() ConversationKind               { return c.kind }
func (c *Conversation) OwnerRef() OwnerRef                   { return c.ownerRef }
func (c *Conversation) Name() string                         { return c.name }
func (c *Conversation) Description() string                  { return c.description }
func (c *Conversation) ParentConversationID() ConversationID { return c.parentConversationID }
func (c *Conversation) CreatedBy() IdentityRef               { return c.createdBy }
func (c *Conversation) Status() ConversationStatus           { return c.status }
func (c *Conversation) OpenedAt() time.Time                  { return c.openedAt }
func (c *Conversation) ClosedAt() *time.Time                 { return copyTimePtr(c.closedAt) }
func (c *Conversation) ClosedReason() string                 { return c.closedReason }
func (c *Conversation) ClosedMessage() string                { return c.closedMessage }
func (c *Conversation) ArchivedAt() *time.Time               { return copyTimePtr(c.archivedAt) }
func (c *Conversation) ArchivedBy() IdentityRef              { return c.archivedBy }
func (c *Conversation) CreatedAt() time.Time                 { return c.createdAt }
func (c *Conversation) UpdatedAt() time.Time                 { return c.updatedAt }
func (c *Conversation) Version() int                         { return c.version }
func (c *Conversation) OrganizationID() string               { return c.organizationID }

// Participants returns a defensive copy of the participants slice.
func (c *Conversation) Participants() []ParticipantElement {
	if len(c.participants) == 0 {
		return nil
	}
	out := make([]ParticipantElement, len(c.participants))
	copy(out, c.participants)
	return out
}

// SetParticipants replaces the participant list (caller-supplied JSON
// r-m-w; ParticipantManagementService owns the read-modify-write loop).
func (c *Conversation) SetParticipants(p []ParticipantElement, at time.Time) {
	c.participants = append([]ParticipantElement(nil), p...)
	c.updatedAt = at.UTC()
	c.version++
}

// HasActiveParticipant returns true when the identity is in the
// participant list and has not left. Used by ChannelManagementService /
// MessageWriter to enforce join invariants.
func (c *Conversation) HasActiveParticipant(id IdentityRef) bool {
	for _, p := range c.participants {
		if p.IdentityID == id && p.IsActive() {
			return true
		}
	}
	return false
}

// IsActive reports whether the conversation accepts new messages.
func (c *Conversation) IsActive() bool { return c.status.AcceptsMessages() }

// IsTerminal reports whether the conversation refuses further mutation.
func (c *Conversation) IsTerminal() bool { return c.status.IsTerminal() }

// Close transitions active→closed with reason+message (conventions § 16).
func (c *Conversation) Close(at time.Time, reason, message string) error {
	if c.status == ConversationClosed {
		return ErrConversationClosed
	}
	if c.status == ConversationArchived {
		return ErrConversationArchived
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("conversation: close reason required (conventions § 16)")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("conversation: close message required (conventions § 16)")
	}
	at = at.UTC()
	c.status = ConversationClosed
	c.closedAt = &at
	c.closedReason = reason
	c.closedMessage = message
	c.updatedAt = at
	c.version++
	return nil
}

// Archive transitions any non-archived status to archived (terminal,
// read-only per ADR-0032 § 5).
func (c *Conversation) Archive(at time.Time, by IdentityRef) error {
	if c.status == ConversationArchived {
		return ErrConversationArchived
	}
	if err := by.Validate(); err != nil {
		return fmt.Errorf("conversation: archived_by: %w", err)
	}
	at = at.UTC()
	c.status = ConversationArchived
	c.archivedAt = &at
	c.archivedBy = by
	c.updatedAt = at
	c.version++
	return nil
}

// MarshalParticipantsJSON returns the JSON encoding for SQLite storage.
// Always returns "[]" for an empty slice (column has NOT NULL DEFAULT '[]').
func MarshalParticipantsJSON(p []ParticipantElement) (string, error) {
	if len(p) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalParticipantsJSON parses the SQLite-stored JSON.
func UnmarshalParticipantsJSON(s string) ([]ParticipantElement, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var out []ParticipantElement
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("conversation: parse participants JSON: %w", err)
	}
	return out, nil
}

func copyTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// Package conversation hosts the Conversation BC tactical types:
//   - Aggregate Root: Conversation (+ Message sub-entity)
//   - Value Objects: ConversationID / MessageID / kinds / directions / etc.
//   - Repository interfaces + sentinel errors
//
// Per conversation/00-overview § 1 + § 5. The BC zero-touches vendor SDKs
// (conventions § 9.y); Bridge subscribes to its events.
//
// Phase 1 scope: Identity AR is deferred to Phase 5 (plan § 6 R4).
// sender_identity_id is treated as a typed formal string here.
package conversation

import (
	"errors"
	"strings"
)

// Typed identifiers (conventions § 0.3).
type (
	ConversationID string
	MessageID      string
	// IdentityRef is the formal prefix-or-`system` string used until Phase
	// 5 (plan § 6 R4: Identity AR is deferred). Format examples:
	// `user:hayang`, `supervisor:inv-1`, `agent:a-1`, `system`.
	IdentityRef string
)

func (id ConversationID) String() string { return string(id) }
func (id MessageID) String() string      { return string(id) }
func (id IdentityRef) String() string    { return string(id) }

// Validate enforces the same prefix vocabulary as observability.Actor —
// shared formal-string contract.
func (r IdentityRef) Validate() error {
	s := string(r)
	if s == "" {
		return errors.New("identity ref: required")
	}
	if s == "system" {
		return nil
	}
	allowed := []string{"user:", "supervisor:", "worker:", "agent:", "bot"}
	if s == "bot" {
		return nil
	}
	for _, p := range allowed {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return errors.New("identity ref: must be 'system', 'bot', or one of user:/supervisor:/worker:/agent: with non-empty suffix")
}

// ConversationKind is the 6-value enum (conversation/01 § 2).
type ConversationKind string

const (
	ConversationKindDM           ConversationKind = "dm"
	ConversationKindGroupThread  ConversationKind = "group_thread"
	ConversationKindAdhoc        ConversationKind = "adhoc"
	ConversationKindNotification ConversationKind = "notification"
	ConversationKindTask         ConversationKind = "task"
	ConversationKindIssue        ConversationKind = "issue"
)

// IsValid checks enum membership.
func (k ConversationKind) IsValid() bool {
	switch k {
	case ConversationKindDM, ConversationKindGroupThread, ConversationKindAdhoc,
		ConversationKindNotification, ConversationKindTask, ConversationKindIssue:
		return true
	}
	return false
}

// IsPhase1OpenAllowed reports whether kind is allowed for direct
// `conversation open` in Phase 1 (workforce/01 § 6 + plan-1 § 3.7 hidden
// admin command). task/issue go through their respective BC paths in
// Phase 2/3 (conversation/01 § 6.5 invariant).
func (k ConversationKind) IsPhase1OpenAllowed() bool {
	switch k {
	case ConversationKindDM, ConversationKindGroupThread,
		ConversationKindAdhoc, ConversationKindNotification:
		return true
	}
	return false
}

// String returns the enum value.
func (k ConversationKind) String() string { return string(k) }

// ConversationStatus is the 2-state enum (conversation/01 § 1).
type ConversationStatus string

const (
	ConversationOpen   ConversationStatus = "open"
	ConversationClosed ConversationStatus = "closed"
)

// IsValid checks enum membership.
func (s ConversationStatus) IsValid() bool {
	switch s {
	case ConversationOpen, ConversationClosed:
		return true
	}
	return false
}

// String returns the enum.
func (s ConversationStatus) String() string { return string(s) }

// MessageContentKind is the 6-value enum (conversation/01 § 4.2).
type MessageContentKind string

const (
	MessageContentText              MessageContentKind = "text"
	MessageContentSystem            MessageContentKind = "system"
	MessageContentAgentFinding      MessageContentKind = "agent_finding"
	MessageContentSupervisorSummary MessageContentKind = "supervisor_summary"
	MessageContentConclusionDraft   MessageContentKind = "conclusion_draft"
	MessageContentTaskProposal      MessageContentKind = "task_proposal"
)

// IsValid checks enum membership.
func (k MessageContentKind) IsValid() bool {
	switch k {
	case MessageContentText, MessageContentSystem, MessageContentAgentFinding,
		MessageContentSupervisorSummary, MessageContentConclusionDraft, MessageContentTaskProposal:
		return true
	}
	return false
}

// String returns the enum.
func (k MessageContentKind) String() string { return string(k) }

// MessageDirection is the 3-value enum (conversation/01 § 4.3).
type MessageDirection string

const (
	DirectionInbound  MessageDirection = "inbound"
	DirectionOutbound MessageDirection = "outbound"
	DirectionInternal MessageDirection = "internal"
)

// IsValid checks enum membership.
func (d MessageDirection) IsValid() bool {
	switch d {
	case DirectionInbound, DirectionOutbound, DirectionInternal:
		return true
	}
	return false
}

// String returns the enum.
func (d MessageDirection) String() string { return string(d) }

// Sentinel errors.
var (
	// Conversation
	ErrConversationNotFound      = errors.New("conversation: conversation not found")
	ErrConversationAlreadyExists = errors.New("conversation: id or (channel,thread_key) already exists")
	ErrConversationClosed        = errors.New("conversation: conversation is closed, cannot accept new message")
	ErrConversationInvalidKind   = errors.New("conversation: invalid kind for operation")
	ErrConversationInvalidStatus = errors.New("conversation: invalid status")
	ErrConversationVersionConflict = errors.New("conversation: conversation version conflict (optimistic lock)")

	// Message
	ErrMessageNotFound      = errors.New("conversation: message not found")
	ErrMessageDuplicate     = errors.New("conversation: vendor_msg_ref duplicate")
	ErrMessageImmutable     = errors.New("conversation: message is append-only, cannot modify (only vendor_msg_ref backfill allowed)")
	ErrMessageInvalidSender = errors.New("conversation: message sender_identity_id invalid")
)

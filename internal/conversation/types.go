// Package conversation hosts the Conversation BC tactical types:
//   - Aggregate Root: Conversation (+ Message sub-entity)
//   - Value Objects: ConversationID / MessageID / kinds / directions /
//     ParticipantElement
//   - Repository interfaces + sentinel errors
//
// v2 (post P10): vendor SDK coupling fully removed per ADR-0031;
// ConversationKind 'group_thread' renamed to 'channel' per ADR-0032;
// participants JSON field per ADR-0034; carry-over per ADR-0035.
package conversation

import (
	"errors"
	"strings"
)

// Typed identifiers (conventions § 0.3).
type (
	ConversationID string
	MessageID      string
	// IdentityRef is the formal kind-prefixed string per ADR-0033 (v2):
	// `user:<id>` / `agent:<id>` / `system`. Other prefixes rejected.
	IdentityRef string
)

func (id ConversationID) String() string { return string(id) }
func (id MessageID) String() string      { return string(id) }
func (id IdentityRef) String() string    { return string(id) }

// Validate enforces the v2 kind-prefixed vocabulary (ADR-0033): one of
// `user:<id>`, `agent:<id>`, or the literal `system`.
func (r IdentityRef) Validate() error {
	s := string(r)
	if s == "" {
		return errors.New("identity ref: required")
	}
	if s == "system" {
		return nil
	}
	for _, p := range []string{"user:", "agent:"} {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return errors.New("identity ref: must be 'system' or 'user:<id>' / 'agent:<id>' (ADR-0033)")
}

// IsHuman reports whether the ref denotes a human identity (`user:<id>`).
// Agents (`agent:<id>`) and `system` are not human. Used by the v2.8 #268
// unread/follow model to enforce Q-T1 human-only: agents never accumulate
// read- or follow-state and are zeroed in the badge DTO (directed-wake D3).
func (r IdentityRef) IsHuman() bool {
	return strings.HasPrefix(string(r), "user:") && len(string(r)) > len("user:")
}

// ConversationKind is the v2.7 four-value enum (ADR-0047 §1, finalized in
// plan §10 OQ10): channel / issue / task / dm. `channel` is retained as a
// generic Org-level group chat (owner_ref id://organizations/{org_id}, NOT
// project-bound; may carry an optional project_ref soft label). Vestigial
// 'adhoc'/'notification' are removed.
type ConversationKind string

const (
	ConversationKindDM ConversationKind = "dm"
	// ConversationKindChannel is a generic Org-level group chat. It belongs to
	// exactly one Org (owner_ref id://organizations/{org_id}); it is NOT bound
	// to a Project, but MAY carry an optional project_ref soft label for
	// grouping/navigation only (no constraint) — plan §10 OQ10.
	ConversationKindChannel ConversationKind = "channel"
	ConversationKindTask    ConversationKind = "task"
	ConversationKindIssue   ConversationKind = "issue"
	// ConversationKindPlan is a Plan's dedicated 1:1 conversation (v2.9 plan
	// orchestration, design §2). Auto-created by the ProjectManager sync-create
	// path (owner_ref pm://plans/{id}); the orchestrator @mentions a node's
	// assignee here to dispatch. Like task/issue it is NOT directly openable.
	ConversationKindPlan ConversationKind = "plan"
)

// IsValid checks enum membership.
func (k ConversationKind) IsValid() bool {
	switch k {
	case ConversationKindDM, ConversationKindChannel,
		ConversationKindTask, ConversationKindIssue, ConversationKindPlan:
		return true
	}
	return false
}

// IsDirectOpenAllowed reports whether kind is allowed for direct
// `conversation open` (dm / channel). task / issue must come via the
// ProjectManager sync-create paths (ADR-0047 §2, plan §4.1).
func (k ConversationKind) IsDirectOpenAllowed() bool {
	switch k {
	case ConversationKindDM, ConversationKindChannel:
		return true
	}
	return false
}

// String returns the enum value.
func (k ConversationKind) String() string { return string(k) }

// ConversationStatus is the 3-state enum (ADR-0032 § 5):
// active → closed (task done / issue concluded) → archived (terminal, read-only).
type ConversationStatus string

const (
	ConversationActive   ConversationStatus = "active"
	ConversationClosed   ConversationStatus = "closed"
	ConversationArchived ConversationStatus = "archived"
)

// IsValid checks enum membership.
func (s ConversationStatus) IsValid() bool {
	switch s {
	case ConversationActive, ConversationClosed, ConversationArchived:
		return true
	}
	return false
}

// IsTerminal returns true when no further messages may be added.
func (s ConversationStatus) IsTerminal() bool { return s == ConversationArchived }

// AcceptsMessages returns true only when status == active.
func (s ConversationStatus) AcceptsMessages() bool { return s == ConversationActive }

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

// MessageDirection is the 3-value enum. v2 keeps it for back-compat
// with the audit columns but the value is always derivable from sender
// kind; v3 may drop entirely (ADR-0039 § 6).
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

// ParticipantElement is the JSON-encoded VO stored in
// conversations.participants (ADR-0034 § 2).
type ParticipantElement struct {
	IdentityID IdentityRef `json:"identity_id"`
	Role       string      `json:"role"`      // owner | member | observer
	JoinedAt   string      `json:"joined_at"` // RFC3339Nano
	JoinedBy   IdentityRef `json:"joined_by"`
	LeftAt     string      `json:"left_at,omitempty"`
	LeftReason string      `json:"left_reason,omitempty"`
}

// IsActive returns true when the participant has not left.
func (p ParticipantElement) IsActive() bool { return p.LeftAt == "" }

// Sentinel errors.
var (
	// Conversation
	ErrConversationNotFound        = errors.New("conversation: conversation not found")
	ErrConversationAlreadyExists   = errors.New("conversation: id or channel name already exists")
	ErrConversationClosed          = errors.New("conversation: conversation is closed, cannot accept new message")
	ErrConversationArchived        = errors.New("conversation: conversation is archived, read-only (ADR-0032 § 5)")
	ErrConversationInvalidKind     = errors.New("conversation: invalid kind for operation")
	ErrConversationInvalidStatus   = errors.New("conversation: invalid status")
	ErrConversationVersionConflict = errors.New("conversation: conversation version conflict (optimistic lock)")

	// Message
	ErrMessageNotFound      = errors.New("conversation: message not found")
	ErrMessageImmutable     = errors.New("conversation: message is append-only, cannot modify")
	ErrMessageInvalidSender = errors.New("conversation: message sender_identity_id invalid")
	// Thread (v2.9.1 P1): a reply must hang off a ROOT message — parent and root
	// are either both empty (a root message) or both set and EQUAL (depth-1,
	// Slack-style). Any other combination is inconsistent.
	ErrMessageInvalidThread = errors.New("conversation: message thread refs invalid (parent/root must be both empty or equal; depth-1)")
	// ErrMessageSelfReply guards against a message being its own parent/root.
	ErrMessageSelfReply = errors.New("conversation: message cannot reply to itself")
	// ErrMessageParentMismatch is returned when a reply targets a parent message
	// that belongs to a DIFFERENT conversation (conversations are org-scoped, so
	// this also blocks cross-org thread stitching). The HTTP edge maps it to 404
	// (existence-non-disclosure, §5.7).
	ErrMessageParentMismatch = errors.New("conversation: parent message is in a different conversation")
)

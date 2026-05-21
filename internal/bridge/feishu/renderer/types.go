// Package renderer translates Conversation Message + Conversation kind +
// optional InputRequest into vendor-shaped payloads (RenderedCard).
//
// Per plan-5 § 3.6 / bridge/01 § 6:
//   - Pure function, no IO, no DB access.
//   - Does NOT import any feishu vendor SDK type (raw JSON strings only).
//     The import-graph test verifies this leaf.
//   - Returns ErrUnknownContentKind for any input outside the closed enum
//     (conventions § 17 — never silently fall back).
package renderer

import "errors"

// MessageKind is the vendor message type discriminator.
type MessageKind string

// MessageKind values.
const (
	MessageKindText        MessageKind = "text"
	MessageKindInteractive MessageKind = "interactive"
)

// RenderedCard is the output of the renderer.
//
// The card_json string is the raw JSON payload the FeishuClient sends as
// `content` to `im.v1.messages.create`. For text messages it is the
// `{"text":"..."}` envelope; for interactive cards it is the full card
// JSON (i18n-less).
type RenderedCard struct {
	MessageKind    MessageKind
	CardJSON       string
	IdempotencyKey string
}

// MessageInput is the abstract message description the renderer accepts.
// We model only the fields the renderer needs so it does NOT depend on
// the Conversation BC types — keeps the package free of import cycles +
// reusable from tests.
type MessageInput struct {
	MessageID       string
	ContentKind     string // matches conversation.MessageContentKind values
	Content         string
	Sender          string // "agent:..." / "supervisor:..." / "user:..."
	InputRequestRef string
}

// ConversationInput captures the conversation kind + identifiers for root
// card rendering.
type ConversationInput struct {
	ConversationID string
	Kind           string // "task" | "issue" — only used by root-card renderers
	Title          string
}

// RootCardInput is the input for conversation.opened root card rendering.
type RootCardInput struct {
	Conversation ConversationInput
	// SubjectRef is the human-visible reference label (e.g. "Task #42" /
	// "Issue #7"). The dispatcher derives it from the bound entity.
	SubjectRef string
}

// InputRequestInput is the optional InputRequest snapshot passed when the
// message content_kind is "agent_finding" with non-empty input_request_ref.
type InputRequestInput struct {
	ID       string
	Question string
	Options  []string
}

// Sentinel errors.
var (
	ErrUnknownContentKind  = errors.New("renderer: unknown content_kind")
	ErrMissingInputRequest = errors.New("renderer: agent_finding with input_request_ref requires InputRequestInput")
	ErrEmptyContent        = errors.New("renderer: empty content")
)

// Closed set of content_kind values recognised by the renderer.
const (
	ContentKindText              = "text"
	ContentKindSystem            = "system"
	ContentKindAgentFinding      = "agent_finding"
	ContentKindSupervisorSummary = "supervisor_summary"
	ContentKindConclusionDraft   = "conclusion_draft"
	ContentKindTaskProposal      = "task_proposal"
)

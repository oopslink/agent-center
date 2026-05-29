package agent

import (
	"errors"
	"strings"
	"time"
)

// AgentActivityEvent is one entry in the append-only observation stream
// (ADR-0049 §2). Because there is no AgentRun, "what the agent did" is
// reconstructed from this stream. The verbose ClaudeCode stdout (assistant
// text, tool-use) is parsed into these events and does NOT auto-post to the
// Conversation (plan §2.6) — only explicit agent messages reach humans.
//
// WorkItemRef/InteractionRef are optional: they let the UI group activity by
// WorkItem segment and by interaction (logical turn) within it (plan §2.4).
type AgentActivityEvent struct {
	id             string
	agentID        AgentID
	workItemRef    string // optional
	interactionRef string // optional
	eventType      string // e.g. "assistant_text" | "tool_use" | "status" | "lifecycle"
	payload        string // JSON
	occurredAt     time.Time
}

// NewActivityEventInput captures constructor args.
type NewActivityEventInput struct {
	ID             string
	AgentID        AgentID
	WorkItemRef    string
	InteractionRef string
	EventType      string
	Payload        string
	OccurredAt     time.Time
}

// NewActivityEvent constructs an append-only activity event.
func NewActivityEvent(in NewActivityEventInput) (*AgentActivityEvent, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("agent: activity event id required")
	}
	if strings.TrimSpace(string(in.AgentID)) == "" {
		return nil, errors.New("agent: activity event agent id required")
	}
	if strings.TrimSpace(in.EventType) == "" {
		return nil, errors.New("agent: activity event type required")
	}
	if in.OccurredAt.IsZero() {
		return nil, errors.New("agent: occurred_at required")
	}
	payload := in.Payload
	if strings.TrimSpace(payload) == "" {
		payload = "{}"
	}
	return &AgentActivityEvent{
		id: in.ID, agentID: in.AgentID, workItemRef: in.WorkItemRef,
		interactionRef: in.InteractionRef, eventType: in.EventType,
		payload: payload, occurredAt: in.OccurredAt.UTC(),
	}, nil
}

// Getters.
func (e *AgentActivityEvent) ID() string             { return e.id }
func (e *AgentActivityEvent) AgentID() AgentID       { return e.agentID }
func (e *AgentActivityEvent) WorkItemRef() string    { return e.workItemRef }
func (e *AgentActivityEvent) InteractionRef() string { return e.interactionRef }
func (e *AgentActivityEvent) EventType() string      { return e.eventType }
func (e *AgentActivityEvent) Payload() string        { return e.payload }
func (e *AgentActivityEvent) OccurredAt() time.Time  { return e.occurredAt }

package service

import (
	"context"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

// PlanDispatchAdapter adapts MessageWriter to the pm Service's PlanDispatcher
// (v2.9 #285): it posts the node-ready @mention into a Plan conversation so the
// existing wake+mention path (#220) wakes an agent assignee. The message is
// posted as a `system` actor with sender `system` (the orchestrator speaks for
// the Plan); the @mention of the assignee inside the content reaches the agent
// via the WakeProjector because the assignee is a Plan-conversation participant
// (§9.5). Returns the new message id for the dispatch record (§9.3).
//
// It runs inside the caller's tx (AddMessage's RunInTx is reentrant), so the
// @mention message + the dispatch record commit atomically with AdvancePlan.
type PlanDispatchAdapter struct {
	writer *MessageWriter
}

// NewPlanDispatchAdapter wraps a MessageWriter as a PlanDispatcher.
func NewPlanDispatchAdapter(w *MessageWriter) *PlanDispatchAdapter {
	return &PlanDispatchAdapter{writer: w}
}

// PostMention posts `content` (which contains the @assignee mention) into the
// Plan conversation and returns the new message id. assigneeRef is accepted for
// interface symmetry / future routing but the mention is carried in content.
func (a *PlanDispatchAdapter) PostMention(ctx context.Context, conversationID, assigneeRef, content string) (string, error) {
	_ = assigneeRef // mention is embedded in content (the wake path scans message text)
	res, err := a.writer.AddMessage(ctx, AddMessageCommand{
		ConversationID:   conversation.ConversationID(conversationID),
		SenderIdentityID: conversation.IdentityRef("system"),
		ContentKind:      conversation.MessageContentText,
		Content:          content,
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("system"),
	})
	if err != nil {
		return "", err
	}
	return string(res.MessageID), nil
}

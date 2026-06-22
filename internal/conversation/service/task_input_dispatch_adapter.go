package service

import (
	"context"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

// TaskInputDispatchAdapter adapts MessageWriter to the pm Service's
// TaskInputConversationPort (v2.14.0 I14/F6): it posts the input_request /
// input_reply messages into a Task's bound Conversation when a task agent blocks
// needing a user reply (input_request, sender = the agent) and when the user
// replies via unblock (input_reply, sender = the user, threaded under the
// request). It runs inside the caller's tx (AddMessage's RunInTx is reentrant),
// so the message commits atomically with the projector's MarkApplied.
//
// The sender identity is the agent/user ref carried on the event (NOT "system"):
// the input_request must read as the AGENT asking, and the input_reply as the
// USER answering. The Actor (observability audit author) is "system" — the
// projector, not a human request, drove the write.
type TaskInputDispatchAdapter struct {
	writer *MessageWriter
}

// NewTaskInputDispatchAdapter wraps a MessageWriter as a TaskInputConversationPort.
func NewTaskInputDispatchAdapter(w *MessageWriter) *TaskInputDispatchAdapter {
	return &TaskInputDispatchAdapter{writer: w}
}

// PostInputRequest posts the agent's input_request into the task conversation
// (kind=input_request, sender=agentRef). Returns the new message id (the reply's
// thread anchor).
func (a *TaskInputDispatchAdapter) PostInputRequest(ctx context.Context, conversationID, agentRef, reason string) (string, error) {
	res, err := a.writer.AddMessage(ctx, AddMessageCommand{
		ConversationID:   conversation.ConversationID(conversationID),
		SenderIdentityID: conversation.IdentityRef(agentRef),
		ContentKind:      conversation.MessageContentInputRequest,
		Content:          reason,
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("system"),
	})
	if err != nil {
		return "", err
	}
	return string(res.MessageID), nil
}

// PostInputReply posts the user's input_reply into the task conversation
// (kind=input_reply, sender=actorRef), threaded under the original input_request
// when inputRequestMessageID is non-empty (depth-1 thread). Returns the new
// message id.
func (a *TaskInputDispatchAdapter) PostInputReply(ctx context.Context, conversationID, actorRef, comment, inputRequestMessageID string) (string, error) {
	cmd := AddMessageCommand{
		ConversationID:   conversation.ConversationID(conversationID),
		SenderIdentityID: conversation.IdentityRef(actorRef),
		ContentKind:      conversation.MessageContentInputReply,
		Content:          comment,
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("system"),
	}
	if inputRequestMessageID != "" {
		cmd.ParentMessageID = conversation.MessageID(inputRequestMessageID)
	}
	res, err := a.writer.AddMessage(ctx, cmd)
	if err != nil {
		return "", err
	}
	return string(res.MessageID), nil
}

package service

import (
	"context"
	"log/slog"
	"strings"

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
	// resolveDisplayName turns an assignee identity ref (e.g. "agent:agent-xxx"
	// or "user:uuu") into the participant's display_name — the SAME resolution the
	// WakeProjector uses (lookupDisplayName), so the @mention text this adapter
	// builds matches exactly what the wake detector (mention.Present) scans for.
	// nil → no resolution wired: PostMention falls back to posting content verbatim
	// (the wake won't fire — see the fallback in PostMention).
	resolveDisplayName func(ctx context.Context, assigneeRef string) (string, bool)
}

// NewPlanDispatchAdapter wraps a MessageWriter as a PlanDispatcher.
//
// resolveDisplayName MUST mirror the WakeProjector's display-name resolution
// (ref → display_name) so the @mention this adapter prepends matches the name
// the wake detector compares against (mention.Present). Pass nil only in tests /
// builds where wake is irrelevant; the @mention then won't be prepended and an
// idle agent will NOT be woken.
func NewPlanDispatchAdapter(w *MessageWriter, resolveDisplayName func(ctx context.Context, assigneeRef string) (string, bool)) *PlanDispatchAdapter {
	return &PlanDispatchAdapter{writer: w, resolveDisplayName: resolveDisplayName}
}

// PostMention resolves assigneeRef → display_name and posts
// `"@" + displayName + " " + content` into the Plan conversation so the wake
// path (#220) — which only fires when mention.Present(text, displayName) is true
// — actually wakes an idle agent assignee. It returns the new message id.
//
// FALLBACK: when the display name can't be resolved (no resolver wired, or the
// ref doesn't resolve to a name), it posts `content` verbatim — nothing breaks,
// but the wake will NOT fire (there is no @display_name token to match), so a
// warning is logged.
func (a *PlanDispatchAdapter) PostMention(ctx context.Context, conversationID, assigneeRef, content string) (string, error) {
	text := content
	if name, ok := a.lookupDisplayName(ctx, assigneeRef); ok {
		text = "@" + name + " " + content
	} else {
		// No resolvable display_name → post the body as-is. The wake detector has
		// no @display_name token to match, so an idle agent will not be woken here.
		slog.Warn("plan dispatch: assignee display_name unresolved — posting without @mention (idle wake will not fire)",
			"assignee_ref", assigneeRef, "conversation_id", conversationID)
	}
	res, err := a.writer.AddMessage(ctx, AddMessageCommand{
		ConversationID:   conversation.ConversationID(conversationID),
		SenderIdentityID: conversation.IdentityRef("system"),
		ContentKind:      conversation.MessageContentText,
		Content:          text,
		Direction:        conversation.DirectionInbound,
		Actor:            observability.Actor("system"),
	})
	if err != nil {
		return "", err
	}
	return string(res.MessageID), nil
}

// lookupDisplayName safely calls the optional resolver, returning the trimmed
// name and ok=false when the resolver is unwired or yields nothing (mirrors the
// WakeProjector's lookupDisplayName so resolution stays symmetric).
func (a *PlanDispatchAdapter) lookupDisplayName(ctx context.Context, assigneeRef string) (string, bool) {
	if a.resolveDisplayName == nil {
		return "", false
	}
	n, ok := a.resolveDisplayName(ctx, assigneeRef)
	if !ok || strings.TrimSpace(n) == "" {
		return "", false
	}
	return strings.TrimSpace(n), true
}

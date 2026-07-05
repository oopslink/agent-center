package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/outbox"
)

// 引用 (quote): a woken agent's converse brief must inline WHAT the sender quoted,
// so a quoted @mention is not perceived as a bare reply. The quote rides the PUSH
// wake like attachments — the wake advances the read cursor, so a later
// get_my_unread comes back empty; the quote MUST be inlined at wake time.

// convMessageEventPlain builds a conversation.message_added event (no attachments).
func convMessageEventPlain(id, convID, msgID, sender, text string) outbox.Event {
	pl, err := json.Marshal(map[string]any{
		"conversation_id": convID,
		"owner_ref":       "",
		"message_id":      msgID,
		"sender":          sender,
		"text":            text,
	})
	if err != nil {
		panic(err)
	}
	return outbox.Event{
		ID:        id,
		EventType: convservice.EvtConversationMessageAdded,
		Payload:   string(pl),
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}

// appendMsgQuote appends a message with an explicit id + quoted_message_id so the
// projector can re-read it and resolve the quote preview.
func (f *wakeFixture) appendMsgQuote(t *testing.T, id, convID, sender, content, quotedID string) {
	t.Helper()
	f.clk.Advance(time.Second)
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID(id),
		ConversationID:   conversation.ConversationID(convID),
		SenderIdentityID: conversation.IdentityRef(sender),
		ContentKind:      conversation.MessageContentText,
		Content:          content,
		Direction:        conversation.DirectionInbound,
		PostedAt:         f.clk.Now(),
		QuotedMessageID:  conversation.MessageID(quotedID),
	})
	if err != nil {
		t.Fatalf("new msg %s: %v", id, err)
	}
	if err := f.msgs.Append(f.ctx, m); err != nil {
		t.Fatalf("append msg %s: %v", id, err)
	}
}

func TestWakeProjector_DM_InlinesQuotedContext(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(nil, nil)

	// Seed the ORIGINAL, then a trigger message that quotes it (both in the repo —
	// the projector re-reads the trigger by id to resolve its quoted_message_id).
	f.appendMsgQuote(t, "orig1", "dm-1", "user:bob", "the ORIGINAL decision text", "")
	f.appendMsgQuote(t, "m1", "dm-1", "user:bob", "see above @A AG1", "orig1")

	if err := p.Project(f.ctx, convMessageEventPlain("EV1", "dm-1", "m1", "user:bob", "see above @A AG1")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse, got %d (%v)", len(cmds), cmds)
	}
	pl := cmds[0].Payload()
	// The quoted original's content is inlined into message_text so the agent knows
	// WHAT was quoted — not just the bare reply.
	if !strings.Contains(pl, "引用") {
		t.Errorf("converse payload missing the 引用 (quote) block: %s", pl)
	}
	if !strings.Contains(pl, "the ORIGINAL decision text") {
		t.Errorf("converse payload missing the quoted original content: %s", pl)
	}
	// The reply text itself is preserved.
	if !strings.Contains(pl, "see above") {
		t.Errorf("converse payload dropped the reply text: %s", pl)
	}
}

func TestWakeProjector_DM_NoQuote_NoBlock(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(nil, nil)

	f.appendMsgQuote(t, "m1", "dm-1", "user:bob", "plain message @A AG1", "")
	if err := p.Project(f.ctx, convMessageEventPlain("EV1", "dm-1", "m1", "user:bob", "plain message @A AG1")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d", len(cmds))
	}
	if strings.Contains(cmds[0].Payload(), "引用") {
		t.Errorf("non-quoting message should carry no 引用 block: %s", cmds[0].Payload())
	}
}

func TestWakeProjector_DM_DeletedQuoteTarget_Stub(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(nil, nil)

	// Trigger quotes an id that was never seeded (deleted/absent) → an "unavailable"
	// stub so the reference is surfaced, never silently dropped.
	f.appendMsgQuote(t, "m1", "dm-1", "user:bob", "see gone @A AG1", "missing-id")
	if err := p.Project(f.ctx, convMessageEventPlain("EV1", "dm-1", "m1", "user:bob", "see gone @A AG1")); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 command, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0].Payload(), "unavailable") {
		t.Errorf("deleted quote target should render an unavailable stub: %s", cmds[0].Payload())
	}
}

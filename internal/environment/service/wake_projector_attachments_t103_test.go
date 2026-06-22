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

// v2.10.1 [T103]: inbound attachments must ride the PUSH wake. The wake/converse
// delivery advances the read cursor, so a later get_my_unread comes back empty —
// the file_uri therefore has to be inlined into the woken agent's brief at wake
// time, or the agent can never download_file it. T74 carried only the count.

// convMessageEventWithAtts builds a conversation.message_added event for a
// NON-task (DM/channel) conversation carrying attachments.
func convMessageEventWithAtts(id, convID, msgID, sender, text string, atts []conversation.MessageAttachment) outbox.Event {
	pl, err := json.Marshal(map[string]any{
		"conversation_id": convID,
		"owner_ref":       "",
		"message_id":      msgID,
		"sender":          sender,
		"text":            text,
		"attachments":     atts,
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

func TestWakeProjector_DM_WithAttachments_InlinesFileURI_T103(t *testing.T) {
	f := newWakeFixture(t)
	f.saveRunningAgent(t, "AG1", "W1")
	f.saveConv(t, "dm-1", conversation.ConversationKindDM, "", agentPart("AG1"), userPart("bob"))
	p := f.projWith(nil, nil)

	atts := []conversation.MessageAttachment{
		{URI: "ac://files/abc", Filename: "screenshot.png", MimeType: "image/png", Size: 2048},
	}
	ev := convMessageEventWithAtts("EV1", "dm-1", "m1", "user:bob", "see this", atts)
	if err := p.Project(f.ctx, ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	cmds := f.commandsFor(t, "W1")
	if len(cmds) != 1 || cmds[0].CommandType() != "agent.converse" {
		t.Fatalf("want 1 agent.converse, got %d (%v)", len(cmds), cmds)
	}
	pl := cmds[0].Payload()
	// The file_uri + metadata are inlined into message_text so the agent can
	// download_file directly (NOT just an attachment_count hint).
	if !strings.Contains(pl, "ac://files/abc") {
		t.Errorf("converse payload missing inlined file_uri: %s", pl)
	}
	if !strings.Contains(pl, "screenshot.png") || !strings.Contains(pl, "image/png") {
		t.Errorf("converse payload missing attachment metadata: %s", pl)
	}
	// the original text is preserved.
	if !strings.Contains(pl, "see this") {
		t.Errorf("converse payload dropped the message text: %s", pl)
	}
}

func TestRenderInboundAttachments_T103(t *testing.T) {
	// empty → "".
	if got := renderInboundAttachments(nil); got != "" {
		t.Fatalf("empty = %q, want \"\"", got)
	}
	// one attachment → one bracketed line with uri + name + mime + size.
	got := renderInboundAttachments([]wakeAttachment{
		{URI: "ac://files/x", Filename: "a.pdf", MimeType: "application/pdf", Size: 100},
	})
	for _, want := range []string{"[attachment: ", "ac://files/x", "a.pdf", "application/pdf", "100 bytes)"} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q: %q", want, got)
		}
	}
	// multiple attachments → multiple lines.
	multi := renderInboundAttachments([]wakeAttachment{
		{URI: "ac://files/1", Filename: "1.png", MimeType: "image/png", Size: 1},
		{URI: "ac://files/2", Filename: "2.png", MimeType: "image/png", Size: 2},
	})
	if strings.Count(multi, "[attachment: ") != 2 {
		t.Errorf("want 2 attachment lines, got: %q", multi)
	}
}

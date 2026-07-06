package workerdaemon

import (
	"strings"
	"testing"
)

// F4: when the triggering mention is INSIDE a thread (RootMessageID set), the
// converse brief must tell the agent to reply in-thread by passing
// parent_message_id=<root>, so its reply lands in the thread, not at top-level.
func TestBuildConverseBrief_ThreadReply_InstructsParent(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-1", ConvKind: "channel", ConvName: "general",
		SenderDisplay: "hayang", MessageID: "m-reply", MessageText: "@bot ping",
		RootMessageID: "m-root",
	})
	if !strings.Contains(brief, "parent_message_id=\"m-root\"") {
		t.Fatalf("thread brief must instruct parent_message_id=<root>, got:\n%s", brief)
	}
	if !strings.Contains(brief, "conversation_id=\"conv-1\"") {
		t.Fatalf("brief must still carry conversation_id, got:\n%s", brief)
	}
	if !strings.Contains(brief, "thread") {
		t.Fatalf("thread brief should mention thread, got:\n%s", brief)
	}
}

// A top-level mention (no RootMessageID) keeps the ordinary reply hint — no
// parent_message_id (the reply stays top-level).
func TestBuildConverseBrief_TopLevel_NoParent(t *testing.T) {
	brief := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-1", ConvKind: "channel", ConvName: "general",
		SenderDisplay: "hayang", MessageID: "m-1", MessageText: "@bot ping",
	})
	if strings.Contains(brief, "parent_message_id") {
		t.Fatalf("top-level brief must NOT mention parent_message_id, got:\n%s", brief)
	}
	if !strings.Contains(brief, "post_message tool with conversation_id=\"conv-1\"") {
		t.Fatalf("top-level brief should keep the ordinary reply hint, got:\n%s", brief)
	}
}

// v2.10.0 [T74]: when the triggering message carries file attachment(s) (e.g. a
// human sent a screenshot), the brief must tell the agent so it doesn't read the
// message as text-only — and how to fetch the file(s) (get_my_unread →
// download_file). Text-only messages get no such hint.
func TestBuildConverseBrief_AttachmentHint_T74(t *testing.T) {
	withAtt := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-1", ConvKind: "dm",
		SenderDisplay: "hayang", MessageID: "m-1", MessageText: "what's wrong with this screenshot?",
		AttachmentCount: 1,
	})
	if !strings.Contains(withAtt, "1 file attachment") {
		t.Fatalf("brief must note the attachment count, got:\n%s", withAtt)
	}
	if !strings.Contains(withAtt, "get_my_unread") || !strings.Contains(withAtt, "download_file") {
		t.Fatalf("brief must tell the agent how to fetch the file(s), got:\n%s", withAtt)
	}
	// plural
	multi := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-1", ConvKind: "dm",
		SenderDisplay: "hayang", MessageID: "m-2", MessageText: "see these", AttachmentCount: 3,
	})
	if !strings.Contains(multi, "3 file attachments") {
		t.Fatalf("brief must pluralize multiple attachments, got:\n%s", multi)
	}
	// text-only → no attachment hint.
	textOnly := buildConverseBrief(conversePayload{
		AgentID: "a1", ConversationID: "conv-1", ConvKind: "dm",
		SenderDisplay: "hayang", MessageID: "m-3", MessageText: "just text",
	})
	if strings.Contains(textOnly, "file attachment") {
		t.Fatalf("text-only brief must NOT mention attachments, got:\n%s", textOnly)
	}
}

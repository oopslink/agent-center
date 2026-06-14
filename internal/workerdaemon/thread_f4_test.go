package workerdaemon

import "strings"

import "testing"

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

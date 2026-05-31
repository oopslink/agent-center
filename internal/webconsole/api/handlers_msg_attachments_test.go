package api

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

func mkMsg(t *testing.T, atts []conversation.MessageAttachment) *conversation.Message {
	t.Helper()
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:               conversation.MessageID("m-1"),
		ConversationID:   conversation.ConversationID("c-1"),
		SenderIdentityID: conversation.IdentityRef("user:hayang"),
		ContentKind:      conversation.MessageContentText,
		Content:          "hi",
		Direction:        conversation.DirectionInbound,
		Attachments:      atts,
		PostedAt:         time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestMsgPublicMap_Attachments: #133 expose — attachments appear (metadata) only
// when present; a plain message carries no "attachments" key (same as context_refs).
func TestMsgPublicMap_Attachments(t *testing.T) {
	// With attachments → key present + metadata exposed.
	m := mkMsg(t, []conversation.MessageAttachment{
		{URI: "ac://files/abc", Filename: "design.png", MimeType: "image/png", Size: 2048},
	})
	out := msgPublicMap(m)
	raw, ok := out["attachments"]
	if !ok {
		t.Fatalf("expected attachments key when present: %+v", out)
	}
	arr, ok := raw.([]map[string]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("attachments shape wrong: %+v", raw)
	}
	a := arr[0]
	if a["uri"] != "ac://files/abc" || a["filename"] != "design.png" || a["mime_type"] != "image/png" || a["size"] != int64(2048) {
		t.Fatalf("attachment metadata wrong: %+v", a)
	}

	// Plain message → no attachments key (not an empty array).
	plain := mkMsg(t, nil)
	if _, ok := msgPublicMap(plain)["attachments"]; ok {
		t.Fatalf("plain message should not carry an attachments key")
	}
}

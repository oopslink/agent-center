package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
)

// TestV27A0_OwnerRefRoundTrip verifies a task Conversation can be expressed by
// owner_ref URI and survives persistence (A0 acceptance: task/issue conv via
// owner_ref).
func TestV27A0_OwnerRefRoundTrip(t *testing.T) {
	r := setupDB(t)
	ctx := context.Background()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:        "conv-task-1",
		Kind:      conversation.ConversationKindTask,
		OwnerRef:  conversation.NewTaskOwnerRef("task-123"),
		CreatedBy: conversation.IdentityRef("user:hayang"),
		OpenedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(ctx, "conv-task-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerRef() != conversation.OwnerRef("pm://tasks/task-123") {
		t.Fatalf("owner_ref = %q, want pm://tasks/task-123", got.OwnerRef())
	}
	if !got.OwnerRef().WellFormed() {
		t.Fatalf("owner_ref %q should be well-formed", got.OwnerRef())
	}
}

// TestV27A0_ProjectChannelOwnerRefNull verifies a project_channel carries a
// NULL owner_ref in A0 (placeholder until phase B), and that NULL round-trips
// as empty (not the literal "null").
func TestV27A0_ProjectChannelOwnerRefNull(t *testing.T) {
	r := setupDB(t)
	ctx := context.Background()
	c := mkConv(t, "conv-ch-1", conversation.ConversationKindProjectChannel, "general")
	if err := r.Save(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(ctx, "conv-ch-1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.OwnerRef().IsEmpty() {
		t.Fatalf("project_channel owner_ref should be empty in A0, got %q", got.OwnerRef())
	}
}

// TestV27A0_MessageContextRefsAndAttachments verifies a Message carries
// context_refs (work_item_ref) and a unified attachment (one structure for
// image/file) through persistence without UI breakage (A0 acceptance).
func TestV27A0_MessageContextRefsAndAttachments(t *testing.T) {
	convRepo, msgRepo := setupMsgDB(t)
	ctx := context.Background()
	c := mkConv(t, "conv-task-2", conversation.ConversationKindTask, "")
	if err := convRepo.Save(ctx, c); err != nil {
		t.Fatal(err)
	}
	m, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:               "msg-1",
		ConversationID:   "conv-task-2",
		SenderIdentityID: "agent:coder",
		ContentKind:      conversation.MessageContentText,
		Content:          "see attached",
		Direction:        conversation.DirectionOutbound,
		ContextRefs:      conversation.ContextRefs{WorkItemRef: "wi-1", TaskRef: "task-123", AgentRef: "agent:coder"},
		Attachments: []conversation.MessageAttachment{
			{URI: "ac://files/01ARZ3NDEKTSV4RRFFQ69G5FAV", Filename: "design.png", MimeType: "image/png", Size: 2048},
		},
		PostedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := msgRepo.Append(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := msgRepo.FindByID(ctx, "msg-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextRefs().WorkItemRef != "wi-1" || got.ContextRefs().TaskRef != "task-123" || got.ContextRefs().AgentRef != "agent:coder" {
		t.Fatalf("context_refs round-trip failed: %+v", got.ContextRefs())
	}
	atts := got.Attachments()
	if len(atts) != 1 || atts[0].URI != "ac://files/01ARZ3NDEKTSV4RRFFQ69G5FAV" || atts[0].MimeType != "image/png" || atts[0].Size != 2048 {
		t.Fatalf("attachment round-trip failed: %+v", atts)
	}
}

// TestV27A0_EmptyContextRefsAndAttachmentsDefault verifies a plain message
// (no refs/attachments) round-trips with empty defaults, so existing messages
// remain valid after the schema rebuild.
func TestV27A0_EmptyContextRefsAndAttachmentsDefault(t *testing.T) {
	convRepo, msgRepo := setupMsgDB(t)
	ctx := context.Background()
	c := mkConv(t, "conv-ch-2", conversation.ConversationKindProjectChannel, "plain")
	_ = convRepo.Save(ctx, c)
	m := mkMsg(t, "msg-plain", "conv-ch-2")
	if err := msgRepo.Append(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := msgRepo.FindByID(ctx, "msg-plain")
	if err != nil {
		t.Fatal(err)
	}
	if !got.ContextRefs().IsEmpty() {
		t.Fatalf("expected empty context_refs, got %+v", got.ContextRefs())
	}
	if len(got.Attachments()) != 0 {
		t.Fatalf("expected no attachments, got %d", len(got.Attachments()))
	}
}

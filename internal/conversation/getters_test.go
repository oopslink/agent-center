package conversation

import (
	"testing"
	"time"
)

// TestConversationGetters_AllFields exercises every accessor on a fully
// rehydrated AR (mirrors a SELECT round-trip + each Getter the CLI / Repo
// touches).
func TestConversationGetters_AllFields(t *testing.T) {
	now := time.Now().UTC()
	closed := now.Add(time.Hour)
	archived := closed.Add(time.Hour)
	c, err := RehydrateConversation(RehydrateConversationInput{
		ID: "C-1", Kind: ConversationKindChannel,
		Name: "general", Description: "shared",
		ParentConversationID: "P-1",
		Participants:         []ParticipantElement{{IdentityID: "user:a", Role: "owner", JoinedAt: "t", JoinedBy: "system"}},
		CreatedBy:            "user:hayang",
		Status:               ConversationArchived,
		OpenedAt:             now,
		ClosedAt:             &closed, ClosedReason: "done", ClosedMessage: "wrapped",
		ArchivedAt: &archived, ArchivedBy: "user:hayang",
		CreatedAt:  now, UpdatedAt: archived,
		Version: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID() != "C-1" || c.Kind() != ConversationKindChannel {
		t.Fatal()
	}
	if c.Name() != "general" || c.Description() != "shared" {
		t.Fatal()
	}
	if c.ParentConversationID() != "P-1" || c.CreatedBy() != "user:hayang" {
		t.Fatal()
	}
	if c.Status() != ConversationArchived || c.IsActive() || !c.IsTerminal() {
		t.Fatal()
	}
	if !c.OpenedAt().Equal(now) {
		t.Fatal()
	}
	if c.ClosedAt() == nil || !c.ClosedAt().Equal(closed) {
		t.Fatal()
	}
	if c.ClosedReason() != "done" || c.ClosedMessage() != "wrapped" {
		t.Fatal()
	}
	if c.ArchivedAt() == nil || !c.ArchivedAt().Equal(archived) || c.ArchivedBy() != "user:hayang" {
		t.Fatal()
	}
	if !c.CreatedAt().Equal(now) || !c.UpdatedAt().Equal(archived) {
		t.Fatal()
	}
	if c.Version() != 5 {
		t.Fatal()
	}
	parts := c.Participants()
	if len(parts) != 1 || parts[0].IdentityID != "user:a" {
		t.Fatal()
	}
	// Mutating the returned slice must not leak back.
	parts[0].IdentityID = "user:b"
	if c.Participants()[0].IdentityID != "user:a" {
		t.Fatal("defensive copy broken")
	}
	if c.HasActiveParticipant("user:nope") {
		t.Fatal()
	}
}

func TestConversationGetters_OptionalNilSafe(t *testing.T) {
	c, _ := NewConversation(NewConversationInput{
		ID: "X", Kind: ConversationKindDM,
		CreatedBy: IdentityRef("system"), OpenedAt: time.Now(),
	})
	if c.ClosedAt() != nil || c.ArchivedAt() != nil {
		t.Fatal()
	}
	if c.Participants() != nil {
		t.Fatal()
	}
}

func TestMessageGetters_AllFields(t *testing.T) {
	now := time.Now().UTC()
	m, err := RehydrateMessage(RehydrateMessageInput{
		ID: "M-1", ConversationID: "C-1", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Content: "hi",
		Direction: DirectionInbound, InputRequestRef: "IR-1",
		PostedAt: now, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != "M-1" || m.ConversationID() != "C-1" {
		t.Fatal()
	}
	if m.SenderIdentityID() != "user:a" || m.ContentKind() != MessageContentText {
		t.Fatal()
	}
	if m.Content() != "hi" || m.Direction() != DirectionInbound {
		t.Fatal()
	}
	if m.InputRequestRef() != "IR-1" {
		t.Fatal()
	}
	if !m.PostedAt().Equal(now) || !m.CreatedAt().Equal(now) {
		t.Fatal()
	}
}

func TestIDStringers(t *testing.T) {
	if ConversationID("c").String() != "c" {
		t.Fatal()
	}
	if MessageID("m").String() != "m" {
		t.Fatal()
	}
	if IdentityRef("user:x").String() != "user:x" {
		t.Fatal()
	}
	if ConversationKindChannel.String() != "channel" {
		t.Fatal()
	}
	if ConversationActive.String() != "active" {
		t.Fatal()
	}
	if MessageContentText.String() != "text" {
		t.Fatal()
	}
	if DirectionInbound.String() != "inbound" {
		t.Fatal()
	}
}

func TestMarshalParticipants_ErrorPath_NotExercisableInGo(t *testing.T) {
	// Encoding []ParticipantElement always succeeds (all-string struct);
	// document that the error-path is theoretical for completeness.
	if _, err := MarshalParticipantsJSON([]ParticipantElement{{IdentityID: "user:x", Role: "owner", JoinedAt: "t", JoinedBy: "system"}}); err != nil {
		t.Fatal(err)
	}
}

func TestCopyTimePtr_Nil(t *testing.T) {
	if copyTimePtr(nil) != nil {
		t.Fatal()
	}
	now := time.Now()
	got := copyTimePtr(&now)
	if got == nil || !got.Equal(now) {
		t.Fatal()
	}
}

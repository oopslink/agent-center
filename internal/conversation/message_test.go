package conversation

import (
	"testing"
	"time"
)

func TestNewMessage_Happy(t *testing.T) {
	m, err := NewMessage(NewMessageInput{
		ID: "m-1", ConversationID: "c-1",
		SenderIdentityID: "user:hayang", ContentKind: MessageContentText,
		Content: "hi", Direction: DirectionInbound, PostedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != "m-1" || m.ContentKind() != MessageContentText {
		t.Fatalf("got %+v", m)
	}
}

func TestNewMessage_BadSender(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c",
		SenderIdentityID: "", ContentKind: MessageContentText,
		Direction: DirectionInbound, PostedAt: time.Now(),
	})
	if err != ErrMessageInvalidSender {
		t.Fatalf("got %v", err)
	}
}

func TestNewMessage_BadKind(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "system",
		ContentKind: "x", Direction: DirectionInbound, PostedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewMessage_BadDirection(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "system",
		ContentKind: MessageContentText, Direction: "x", PostedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewMessage_MissingIDs(t *testing.T) {
	if _, err := NewMessage(NewMessageInput{
		ContentKind: MessageContentText, Direction: DirectionInbound,
		SenderIdentityID: "system", PostedAt: time.Now(),
	}); err == nil {
		t.Fatal("id required")
	}
	if _, err := NewMessage(NewMessageInput{
		ID: "m", ContentKind: MessageContentText, Direction: DirectionInbound,
		SenderIdentityID: "system", PostedAt: time.Now(),
	}); err == nil {
		t.Fatal("conv id required")
	}
}

func TestNewMessage_ZeroPostedAt(t *testing.T) {
	_, err := NewMessage(NewMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "system",
		ContentKind: MessageContentText, Direction: DirectionInbound,
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRehydrateMessage_Happy(t *testing.T) {
	m, err := RehydrateMessage(RehydrateMessageInput{
		ID: "m", ConversationID: "c", SenderIdentityID: "user:a",
		ContentKind: MessageContentText, Direction: DirectionInbound,
		PostedAt: time.Now(), CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.SenderIdentityID() != "user:a" {
		t.Fatal()
	}
}

func TestRehydrateMessage_BadKind(t *testing.T) {
	if _, err := RehydrateMessage(RehydrateMessageInput{
		ContentKind: "x", Direction: DirectionInbound,
	}); err == nil {
		t.Fatal()
	}
}

func TestRehydrateMessage_BadDirection(t *testing.T) {
	if _, err := RehydrateMessage(RehydrateMessageInput{
		ContentKind: MessageContentText, Direction: "x",
	}); err == nil {
		t.Fatal()
	}
}

func TestIdentityRefValidate(t *testing.T) {
	cases := []struct {
		in IdentityRef
		ok bool
	}{
		{"", false},
		{"system", true},
		{"user:hayang", true},
		{"agent:s-1", true},
		{"supervisor:x", false},
		{"bot", false},
		{"user:", false},
	}
	for _, c := range cases {
		err := c.in.Validate()
		if (err == nil) != c.ok {
			t.Errorf("ref %q ok=%v err=%v", c.in, c.ok, err)
		}
	}
}

func TestMessageContentKindEnum(t *testing.T) {
	for _, k := range []MessageContentKind{MessageContentText, MessageContentSystem,
		MessageContentAgentFinding, MessageContentSupervisorSummary,
		MessageContentConclusionDraft, MessageContentTaskProposal} {
		if !k.IsValid() {
			t.Fatalf("%s should be valid", k)
		}
	}
	if MessageContentKind("nope").IsValid() {
		t.Fatal()
	}
}

func TestMessageDirectionEnum(t *testing.T) {
	for _, d := range []MessageDirection{DirectionInbound, DirectionOutbound, DirectionInternal} {
		if !d.IsValid() {
			t.Fatalf("%s should be valid", d)
		}
	}
	if MessageDirection("nope").IsValid() {
		t.Fatal()
	}
}

func TestParticipantElement_IsActive(t *testing.T) {
	p := ParticipantElement{IdentityID: "user:a", Role: "owner"}
	if !p.IsActive() {
		t.Fatal()
	}
	p.LeftAt = "t"
	if p.IsActive() {
		t.Fatal()
	}
}

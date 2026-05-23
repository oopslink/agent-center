package conversation

import (
	"testing"
	"time"
)

func TestNewConversation_ChannelHappy(t *testing.T) {
	now := time.Now().UTC()
	c, err := NewConversation(NewConversationInput{
		ID:        "conv-1",
		Kind:      ConversationKindChannel,
		Name:      "general",
		CreatedBy: IdentityRef("user:hayang"),
		OpenedAt:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind() != ConversationKindChannel || c.Name() != "general" {
		t.Fatalf("got kind=%s name=%s", c.Kind(), c.Name())
	}
	if c.Status() != ConversationActive || !c.IsActive() {
		t.Fatalf("expected active, got %s", c.Status())
	}
	if c.Version() != 1 {
		t.Fatalf("version: %d", c.Version())
	}
}

func TestNewConversation_ChannelRequiresName(t *testing.T) {
	_, err := NewConversation(NewConversationInput{
		ID:        "conv-1",
		Kind:      ConversationKindChannel,
		CreatedBy: IdentityRef("user:hayang"),
		OpenedAt:  time.Now(),
	})
	if err == nil {
		t.Fatal("expected error for channel without name")
	}
}

func TestNewConversation_DMNameOptional(t *testing.T) {
	c, err := NewConversation(NewConversationInput{
		ID:        "conv-2",
		Kind:      ConversationKindDM,
		CreatedBy: IdentityRef("user:hayang"),
		OpenedAt:  time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name() != "" {
		t.Fatalf("dm name should be empty: %s", c.Name())
	}
}

func TestNewConversation_RejectsBadID(t *testing.T) {
	_, err := NewConversation(NewConversationInput{
		Kind:      ConversationKindDM,
		CreatedBy: IdentityRef("user:hayang"),
		OpenedAt:  time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewConversation_RejectsBadKind(t *testing.T) {
	_, err := NewConversation(NewConversationInput{
		ID:        "x",
		Kind:      "invalid",
		CreatedBy: IdentityRef("user:hayang"),
		OpenedAt:  time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewConversation_RejectsBadCreatedBy(t *testing.T) {
	_, err := NewConversation(NewConversationInput{
		ID:       "x",
		Kind:     ConversationKindChannel,
		Name:     "n",
		OpenedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewConversation_RejectsZeroOpenedAt(t *testing.T) {
	_, err := NewConversation(NewConversationInput{
		ID:        "x",
		Kind:      ConversationKindDM,
		CreatedBy: IdentityRef("system"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestConversation_Close_Happy(t *testing.T) {
	now := time.Now().UTC()
	c, _ := NewConversation(NewConversationInput{
		ID: "x", Kind: ConversationKindDM, CreatedBy: IdentityRef("system"), OpenedAt: now,
	})
	if err := c.Close(now, "user_request", "wrapped up"); err != nil {
		t.Fatal(err)
	}
	if c.Status() != ConversationClosed {
		t.Fatalf("status: %s", c.Status())
	}
	if c.Version() != 2 {
		t.Fatalf("version: %d", c.Version())
	}
	// Re-close: returns ErrConversationClosed.
	if err := c.Close(now, "r", "m"); err != ErrConversationClosed {
		t.Fatalf("expected ErrConversationClosed, got %v", err)
	}
}

func TestConversation_Close_RequiresReasonMessage(t *testing.T) {
	now := time.Now()
	c, _ := NewConversation(NewConversationInput{
		ID: "x", Kind: ConversationKindDM, CreatedBy: IdentityRef("system"), OpenedAt: now,
	})
	if err := c.Close(now, "", "m"); err == nil {
		t.Fatal()
	}
	if err := c.Close(now, "r", ""); err == nil {
		t.Fatal()
	}
}

func TestConversation_Archive_Happy(t *testing.T) {
	now := time.Now()
	c, _ := NewConversation(NewConversationInput{
		ID: "x", Kind: ConversationKindDM, CreatedBy: IdentityRef("system"), OpenedAt: now,
	})
	if err := c.Archive(now, IdentityRef("user:hayang")); err != nil {
		t.Fatal(err)
	}
	if c.Status() != ConversationArchived || !c.IsTerminal() {
		t.Fatalf("status: %s", c.Status())
	}
	if c.ArchivedBy() != "user:hayang" {
		t.Fatalf("archived_by: %s", c.ArchivedBy())
	}
	if c.ArchivedAt() == nil {
		t.Fatal("archived_at nil")
	}
	// Re-archive returns error.
	if err := c.Archive(now, IdentityRef("user:hayang")); err != ErrConversationArchived {
		t.Fatalf("expected ErrConversationArchived, got %v", err)
	}
}

func TestConversation_Archive_RejectsBadActor(t *testing.T) {
	now := time.Now()
	c, _ := NewConversation(NewConversationInput{
		ID: "x", Kind: ConversationKindDM, CreatedBy: IdentityRef("system"), OpenedAt: now,
	})
	if err := c.Archive(now, IdentityRef("")); err == nil {
		t.Fatal()
	}
}

func TestConversation_Close_FromArchivedRejected(t *testing.T) {
	now := time.Now()
	c, _ := NewConversation(NewConversationInput{
		ID: "x", Kind: ConversationKindDM, CreatedBy: IdentityRef("system"), OpenedAt: now,
	})
	_ = c.Archive(now, IdentityRef("system"))
	if err := c.Close(now, "r", "m"); err != ErrConversationArchived {
		t.Fatalf("expected ErrConversationArchived: %v", err)
	}
}

func TestConversation_Participants_RoundTrip(t *testing.T) {
	c, _ := NewConversation(NewConversationInput{
		ID: "x", Kind: ConversationKindDM, CreatedBy: IdentityRef("system"), OpenedAt: time.Now(),
		Participants: []ParticipantElement{{IdentityID: "user:a", Role: "owner", JoinedAt: "t", JoinedBy: "system"}},
	})
	if len(c.Participants()) != 1 {
		t.Fatalf("participants: %v", c.Participants())
	}
	if !c.HasActiveParticipant("user:a") {
		t.Fatal()
	}
	c.SetParticipants([]ParticipantElement{
		{IdentityID: "user:a", Role: "owner", JoinedAt: "t", JoinedBy: "system", LeftAt: "u", LeftReason: "kick"},
		{IdentityID: "user:b", Role: "member", JoinedAt: "t", JoinedBy: "system"},
	}, time.Now())
	if c.HasActiveParticipant("user:a") {
		t.Fatal("user:a should have left")
	}
	if !c.HasActiveParticipant("user:b") {
		t.Fatal()
	}
	if c.Version() != 2 {
		t.Fatalf("version: %d", c.Version())
	}
}

func TestMarshalParticipantsJSON_RoundTrip(t *testing.T) {
	parts := []ParticipantElement{{IdentityID: "user:a", Role: "owner", JoinedAt: "t", JoinedBy: "system"}}
	s, err := MarshalParticipantsJSON(parts)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalParticipantsJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].IdentityID != "user:a" {
		t.Fatalf("got: %v", got)
	}
}

func TestMarshalParticipantsJSON_Empty(t *testing.T) {
	s, err := MarshalParticipantsJSON(nil)
	if err != nil || s != "[]" {
		t.Fatalf("got (%q, %v)", s, err)
	}
}

func TestUnmarshalParticipantsJSON_Empty(t *testing.T) {
	got, err := UnmarshalParticipantsJSON("")
	if err != nil || got != nil {
		t.Fatalf("got (%v, %v)", got, err)
	}
	got, err = UnmarshalParticipantsJSON("[]")
	if err != nil || got != nil {
		t.Fatalf("got (%v, %v)", got, err)
	}
}

func TestUnmarshalParticipantsJSON_Bad(t *testing.T) {
	if _, err := UnmarshalParticipantsJSON(`{bad json`); err == nil {
		t.Fatal()
	}
}

func TestRehydrate_RejectsInvalidStatus(t *testing.T) {
	_, err := RehydrateConversation(RehydrateConversationInput{
		ID: "x", Kind: ConversationKindDM, Status: "weird", Version: 1, OpenedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRehydrate_RejectsZeroVersion(t *testing.T) {
	_, err := RehydrateConversation(RehydrateConversationInput{
		ID: "x", Kind: ConversationKindDM, Status: ConversationActive, Version: 0, OpenedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRehydrate_RejectsInvalidKind(t *testing.T) {
	_, err := RehydrateConversation(RehydrateConversationInput{
		ID: "x", Kind: "weird", Status: ConversationActive, Version: 1, OpenedAt: time.Now(),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestKindEnumValid(t *testing.T) {
	for _, k := range []ConversationKind{ConversationKindDM, ConversationKindChannel, ConversationKindAdhoc,
		ConversationKindNotification, ConversationKindTask, ConversationKindIssue} {
		if !k.IsValid() {
			t.Fatalf("%s should be valid", k)
		}
	}
	if ConversationKind("nope").IsValid() {
		t.Fatal()
	}
}

func TestKindDirectOpenAllowed(t *testing.T) {
	yes := []ConversationKind{ConversationKindDM, ConversationKindChannel, ConversationKindAdhoc, ConversationKindNotification}
	for _, k := range yes {
		if !k.IsDirectOpenAllowed() {
			t.Fatalf("%s should be direct-open-allowed", k)
		}
	}
	for _, k := range []ConversationKind{ConversationKindTask, ConversationKindIssue} {
		if k.IsDirectOpenAllowed() {
			t.Fatalf("%s should not be direct-open-allowed", k)
		}
	}
}

func TestStatusBehaviour(t *testing.T) {
	if !ConversationActive.AcceptsMessages() {
		t.Fatal()
	}
	if ConversationClosed.AcceptsMessages() {
		t.Fatal()
	}
	if !ConversationArchived.IsTerminal() {
		t.Fatal()
	}
}

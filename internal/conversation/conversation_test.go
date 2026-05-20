package conversation

import (
	"errors"
	"testing"
	"time"
)

func newTestConv(t *testing.T) *Conversation {
	t.Helper()
	c, err := NewConversation(NewConversationInput{
		ID:       "C-1",
		Kind:     ConversationKindDM,
		OpenedAt: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewConversation: %v", err)
	}
	return c
}

func TestConversation_New_Happy(t *testing.T) {
	c := newTestConv(t)
	if c.Status() != ConversationOpen {
		t.Fatal()
	}
	if c.Version() != 1 {
		t.Fatal()
	}
	if c.Kind() != ConversationKindDM {
		t.Fatal()
	}
	if !c.IsOpen() {
		t.Fatal()
	}
}

func TestConversation_New_RejectsEmptyID(t *testing.T) {
	_, err := NewConversation(NewConversationInput{Kind: ConversationKindDM, OpenedAt: time.Now()})
	if err == nil {
		t.Fatal()
	}
}

func TestConversation_New_BadKind(t *testing.T) {
	_, err := NewConversation(NewConversationInput{ID: "C-1", Kind: "bogus", OpenedAt: time.Now()})
	if !errors.Is(err, ErrConversationInvalidKind) {
		t.Fatalf("got %v", err)
	}
}

func TestConversation_New_RequiresOpenedAt(t *testing.T) {
	_, err := NewConversation(NewConversationInput{ID: "C-1", Kind: ConversationKindDM})
	if err == nil {
		t.Fatal()
	}
}

func TestConversation_Close_Happy(t *testing.T) {
	c := newTestConv(t)
	err := c.Close(time.Now(), "done", "task finished")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status() != ConversationClosed {
		t.Fatal()
	}
	if c.ClosedAt() == nil {
		t.Fatal()
	}
	if c.ClosedReason() != "done" || c.ClosedMessage() != "task finished" {
		t.Fatal()
	}
	if c.Version() != 2 {
		t.Fatal()
	}
}

func TestConversation_Close_AlreadyClosed(t *testing.T) {
	c := newTestConv(t)
	_ = c.Close(time.Now(), "done", "x")
	err := c.Close(time.Now(), "done", "x")
	if !errors.Is(err, ErrConversationClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestConversation_Close_RequiresReason(t *testing.T) {
	c := newTestConv(t)
	if err := c.Close(time.Now(), "", "x"); err == nil {
		t.Fatal()
	}
}

func TestConversation_Close_RequiresMessage(t *testing.T) {
	c := newTestConv(t)
	if err := c.Close(time.Now(), "done", ""); err == nil {
		t.Fatal()
	}
}

func TestConversation_SetPrimaryChannel(t *testing.T) {
	c := newTestConv(t)
	if err := c.SetPrimaryChannel("feishu", "thread-x", time.Now()); err != nil {
		t.Fatal(err)
	}
	if c.PrimaryChannelHint() != "feishu" || c.PrimaryChannelThreadKey() != "thread-x" {
		t.Fatal()
	}
	if c.Version() != 2 {
		t.Fatal()
	}
}

func TestConversation_SetPrimaryChannel_AllowsClosed(t *testing.T) {
	c := newTestConv(t)
	_ = c.Close(time.Now(), "x", "y")
	err := c.SetPrimaryChannel("feishu", "t", time.Now())
	if err != nil {
		t.Fatal()
	}
}

func TestConversation_SetPrimaryChannel_RequiresValues(t *testing.T) {
	c := newTestConv(t)
	if err := c.SetPrimaryChannel("", "t", time.Now()); err == nil {
		t.Fatal()
	}
	if err := c.SetPrimaryChannel("h", "", time.Now()); err == nil {
		t.Fatal()
	}
}

func TestConversation_RehydrateBadKind(t *testing.T) {
	_, err := RehydrateConversation(RehydrateConversationInput{Kind: "bogus", Status: ConversationOpen, Version: 1})
	if err == nil {
		t.Fatal()
	}
}

func TestConversation_RehydrateBadStatus(t *testing.T) {
	_, err := RehydrateConversation(RehydrateConversationInput{Kind: ConversationKindDM, Status: "bogus", Version: 1})
	if err == nil {
		t.Fatal()
	}
}

func TestConversation_RehydrateBadVersion(t *testing.T) {
	_, err := RehydrateConversation(RehydrateConversationInput{Kind: ConversationKindDM, Status: ConversationOpen, Version: 0})
	if err == nil {
		t.Fatal()
	}
}

func TestConversationKind_Validation(t *testing.T) {
	for _, k := range []ConversationKind{
		ConversationKindDM, ConversationKindGroupThread, ConversationKindAdhoc,
		ConversationKindNotification, ConversationKindTask, ConversationKindIssue,
	} {
		if !k.IsValid() {
			t.Fatalf("%v should be valid", k)
		}
	}
	if ConversationKind("x").IsValid() {
		t.Fatal()
	}
	for _, k := range []ConversationKind{ConversationKindDM, ConversationKindGroupThread, ConversationKindAdhoc, ConversationKindNotification} {
		if !k.IsPhase1OpenAllowed() {
			t.Fatalf("%v should be open-allowed", k)
		}
	}
	for _, k := range []ConversationKind{ConversationKindTask, ConversationKindIssue} {
		if k.IsPhase1OpenAllowed() {
			t.Fatalf("%v should NOT be open-allowed", k)
		}
	}
	if ConversationKindDM.String() != "dm" {
		t.Fatal()
	}
}

func TestConversationStatus_Validation(t *testing.T) {
	for _, s := range []ConversationStatus{ConversationOpen, ConversationClosed} {
		if !s.IsValid() {
			t.Fatal()
		}
	}
	if ConversationStatus("x").IsValid() {
		t.Fatal()
	}
	if ConversationOpen.String() != "open" {
		t.Fatal()
	}
}

func TestIDs_String(t *testing.T) {
	if ConversationID("C-1").String() != "C-1" {
		t.Fatal()
	}
	if MessageID("M-1").String() != "M-1" {
		t.Fatal()
	}
	if IdentityRef("user:x").String() != "user:x" {
		t.Fatal()
	}
}

func TestIdentityRef_Validate(t *testing.T) {
	cases := []struct {
		in IdentityRef
		ok bool
	}{
		{"", false},
		{"system", true},
		{"bot", true},
		{"user:hayang", true},
		{"supervisor:inv-1", true},
		{"worker:W-1", true},
		{"agent:a-1", true},
		{"foo:bar", false},
		{"user:", false},
	}
	for _, c := range cases {
		err := c.in.Validate()
		if (err == nil) != c.ok {
			t.Fatalf("IdentityRef(%q).Validate() ok=%v err=%v", c.in, c.ok, err)
		}
	}
}

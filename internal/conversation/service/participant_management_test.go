package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

func setupParticipant(t *testing.T) (*ChannelManagementService, *ParticipantManagementService) {
	t.Helper()
	db, w := setupRaw(t)
	chSvc := NewChannelManagementService(db, w.convRepo, w.sink, w.idgen, w.clock)
	pSvc := NewParticipantManagementService(db, w.convRepo, w.sink, w.clock)
	return chSvc, pSvc
}

func seedChannel(t *testing.T, ch *ChannelManagementService, name string) conversation.ConversationID {
	t.Helper()
	res, err := ch.CreateChannel(context.Background(), CreateChannelCommand{
		Name: name, CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.ConversationID
}

func TestInvite_Happy(t *testing.T) {
	ch, p := setupParticipant(t)
	cid := seedChannel(t, ch, "general")
	evID, err := p.Invite(context.Background(), InviteCommand{
		ConversationName: "general",
		IdentityID:       conversation.IdentityRef("user:bob"),
		InvitedBy:        conversation.IdentityRef("user:hayang"),
		Actor:            observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if evID == "" {
		t.Fatal()
	}
	conv, _ := ch.convRepo.FindByID(context.Background(), cid)
	if !conv.HasActiveParticipant(conversation.IdentityRef("user:bob")) {
		t.Fatal("bob should be active participant")
	}
	if len(conv.Participants()) != 2 { // owner + bob
		t.Fatalf("got %d participants", len(conv.Participants()))
	}
}

func TestInvite_AlreadyActive(t *testing.T) {
	ch, p := setupParticipant(t)
	_ = seedChannel(t, ch, "alpha")
	_, _ = p.Invite(context.Background(), InviteCommand{
		ConversationName: "alpha", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	_, err := p.Invite(context.Background(), InviteCommand{
		ConversationName: "alpha", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, ErrParticipantAlreadyActive) {
		t.Fatalf("got %v", err)
	}
}

func TestInvite_BadRole(t *testing.T) {
	ch, p := setupParticipant(t)
	_ = seedChannel(t, ch, "x")
	_, err := p.Invite(context.Background(), InviteCommand{
		ConversationName: "x", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Role: "weird",
		Actor: observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestInvite_ToArchived(t *testing.T) {
	ch, p := setupParticipant(t)
	_ = seedChannel(t, ch, "ar")
	_, _ = ch.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "ar", ArchivedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	_, err := p.Invite(context.Background(), InviteCommand{
		ConversationName: "ar", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, conversation.ErrConversationArchived) {
		t.Fatalf("got %v", err)
	}
}

func TestInvite_BadArgs(t *testing.T) {
	_, p := setupParticipant(t)
	cases := []InviteCommand{
		{Actor: "", IdentityID: "user:bob", InvitedBy: "user:h", ConversationName: "x"},
		{Actor: "user:h", IdentityID: "", InvitedBy: "user:h", ConversationName: "x"},
		{Actor: "user:h", IdentityID: "user:bob", InvitedBy: "", ConversationName: "x"},
		{Actor: "user:h", IdentityID: "user:bob", InvitedBy: "user:h", ConversationName: ""},
	}
	for i, c := range cases {
		if _, err := p.Invite(context.Background(), c); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
}

func TestLeave_Happy(t *testing.T) {
	ch, p := setupParticipant(t)
	_ = seedChannel(t, ch, "lv")
	_, _ = p.Invite(context.Background(), InviteCommand{
		ConversationName: "lv", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	evID, err := p.Leave(context.Background(), LeaveCommand{
		ConversationName: "lv", IdentityID: "user:bob",
		Actor: observability.Actor("user:bob"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if evID == "" {
		t.Fatal()
	}
	conv, _ := ch.convRepo.FindByName(context.Background(), "lv")
	if conv.HasActiveParticipant(conversation.IdentityRef("user:bob")) {
		t.Fatal("bob should have left")
	}
}

func TestLeave_NotActive(t *testing.T) {
	ch, p := setupParticipant(t)
	_ = seedChannel(t, ch, "nl")
	_, err := p.Leave(context.Background(), LeaveCommand{
		ConversationName: "nl", IdentityID: "user:bob",
		Actor: observability.Actor("user:bob"),
	})
	if !errors.Is(err, ErrParticipantNotActive) {
		t.Fatalf("got %v", err)
	}
}

func TestKick_Happy(t *testing.T) {
	ch, p := setupParticipant(t)
	_ = seedChannel(t, ch, "k")
	_, _ = p.Invite(context.Background(), InviteCommand{
		ConversationName: "k", IdentityID: "user:bob",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	evID, err := p.Kick(context.Background(), KickCommand{
		ConversationName: "k", IdentityID: "user:bob",
		KickedBy: "user:hayang", Reason: "bye",
		Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if evID == "" {
		t.Fatal()
	}
}

func TestKick_RequiresOwner(t *testing.T) {
	ch, p := setupParticipant(t)
	_ = seedChannel(t, ch, "r")
	_, _ = p.Invite(context.Background(), InviteCommand{
		ConversationName: "r", IdentityID: "user:bob", Role: "member",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	_, _ = p.Invite(context.Background(), InviteCommand{
		ConversationName: "r", IdentityID: "user:carol", Role: "member",
		InvitedBy: "user:hayang", Actor: observability.Actor("user:hayang"),
	})
	// carol (non-owner) tries to kick bob
	_, err := p.Kick(context.Background(), KickCommand{
		ConversationName: "r", IdentityID: "user:bob",
		KickedBy: "user:carol", Actor: observability.Actor("user:carol"),
	})
	if !errors.Is(err, ErrParticipantNotOwner) {
		t.Fatalf("got %v", err)
	}
}

func TestKick_BadKickedBy(t *testing.T) {
	_, p := setupParticipant(t)
	_, err := p.Kick(context.Background(), KickCommand{
		ConversationName: "x", IdentityID: "user:b", KickedBy: "",
		Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestLeave_NameRequired(t *testing.T) {
	_, p := setupParticipant(t)
	_, err := p.Leave(context.Background(), LeaveCommand{
		ConversationName: "", IdentityID: "user:bob",
		Actor: observability.Actor("user:bob"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestLeave_BadIdentityID(t *testing.T) {
	_, p := setupParticipant(t)
	_, err := p.Leave(context.Background(), LeaveCommand{
		ConversationName: "x", IdentityID: "",
		Actor: observability.Actor("user:bob"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestLeave_BadActor(t *testing.T) {
	_, p := setupParticipant(t)
	_, err := p.Leave(context.Background(), LeaveCommand{
		ConversationName: "x", IdentityID: "user:b", Actor: "",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestNewParticipantManagementService_NilClock(t *testing.T) {
	db, w := setupRaw(t)
	svc := NewParticipantManagementService(db, w.convRepo, w.sink, nil)
	if svc == nil {
		t.Fatal()
	}
}

func TestValidRole(t *testing.T) {
	for _, r := range []string{"owner", "member", "observer"} {
		if !validRole(r) {
			t.Fatalf("%s should be valid", r)
		}
	}
	if validRole("bogus") {
		t.Fatal()
	}
}

func TestOrDefault(t *testing.T) {
	if orDefault("", "x") != "x" {
		t.Fatal()
	}
	if orDefault("y", "x") != "y" {
		t.Fatal()
	}
	if orDefault("  ", "x") != "x" {
		t.Fatal()
	}
}

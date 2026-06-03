package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
)

func setupChan(t *testing.T) *ChannelManagementService {
	t.Helper()
	db, w := setupRaw(t)
	return NewChannelManagementService(db, w.convRepo, w.sink, w.idgen, w.clock)
}

func TestCreateChannel_Happy(t *testing.T) {
	svc := setupChan(t)
	res, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name:        "general",
		Description: "shared",
		CreatedBy:   conversation.IdentityRef("user:hayang"),
		Actor:       observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ConversationID == "" || res.EventID == "" {
		t.Fatalf("got %+v", res)
	}
}

func TestCreateChannel_CreatorBecomesOwnerParticipant(t *testing.T) {
	svc := setupChan(t)
	res, _ := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name:      "alpha",
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	conv, err := svc.convRepo.FindByID(context.Background(), res.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	parts := conv.Participants()
	if len(parts) != 1 {
		t.Fatalf("expected 1 participant, got %d", len(parts))
	}
	if parts[0].IdentityID != "user:hayang" || parts[0].Role != "owner" {
		t.Fatalf("got %+v", parts[0])
	}
	if !conv.HasActiveParticipant(conversation.IdentityRef("user:hayang")) {
		t.Fatal()
	}
}

func TestCreateChannel_NameRequired(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name:      "",
		CreatedBy: conversation.IdentityRef("user:hayang"),
		Actor:     observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestCreateChannel_NameUnique(t *testing.T) {
	svc := setupChan(t)
	_, _ = svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "shared", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "shared", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

// v2.7 #195: channel name uniqueness is ORG-SCOPED. Two orgs may each have a
// "general"; a duplicate within ONE org is rejected.
func TestCreateChannel_NameUniquePerOrg(t *testing.T) {
	svc := setupChan(t)
	mk := func(org string) error {
		_, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
			Name: "general", OrganizationID: org,
			CreatedBy: "user:hayang", Actor: "user:hayang",
		})
		return err
	}
	if err := mk("org-A"); err != nil {
		t.Fatalf("org-A general: %v", err)
	}
	if err := mk("org-B"); err != nil {
		t.Fatalf("org-B general (different org must succeed): %v", err)
	}
	if err := mk("org-A"); !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("dup general in org-A want ErrConversationAlreadyExists, got %v", err)
	}
}

// v2.7 #201: channel create seeds members[] as participants (like DM) — an agent
// member must become an ACTIVE participant so a channel @mention can wake it.
// The creator-owner is not duplicated; invalid refs are rejected.
func TestCreateChannel_SeedsMemberParticipants(t *testing.T) {
	svc := setupChan(t)
	res, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "general", OrganizationID: "org-1",
		CreatedBy: "user:alice", Actor: "user:alice",
		Members: []conversation.IdentityRef{"agent:agent-x", "user:bob", "user:alice"}, // alice = creator dup
	})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := svc.convRepo.FindByID(context.Background(), res.ConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if !conv.HasActiveParticipant("agent:agent-x") {
		t.Fatal("agent member must be an active participant (#201 — else channel @mention can't wake it)")
	}
	if !conv.HasActiveParticipant("user:bob") {
		t.Fatal("human member must be an active participant")
	}
	if !conv.HasActiveParticipant("user:alice") {
		t.Fatal("creator must be the owner participant")
	}
	if n := len(conv.Participants()); n != 3 {
		t.Fatalf("want 3 participants (owner + agent + bob; creator not duplicated), got %d", n)
	}
}

func TestCreateChannel_RejectsInvalidMember(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "x", OrganizationID: "org-1", CreatedBy: "user:alice", Actor: "user:alice",
		Members: []conversation.IdentityRef{"no-prefix-bareid"},
	})
	if err == nil {
		t.Fatal("invalid member ref must be rejected")
	}
}

func TestCreateChannel_BadActor(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "x", CreatedBy: "user:hayang", Actor: "",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestCreateChannel_BadCreatedBy(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "x", CreatedBy: "", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestArchiveChannel_Happy(t *testing.T) {
	svc := setupChan(t)
	_, _ = svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "to-archive", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	evID, err := svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "to-archive", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if evID == "" {
		t.Fatal()
	}
}

func TestArchiveChannel_NotFound(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "nope", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestArchiveChannel_TwiceIsError(t *testing.T) {
	svc := setupChan(t)
	_, _ = svc.CreateChannel(context.Background(), CreateChannelCommand{
		Name: "twice", CreatedBy: "user:hayang", Actor: "user:hayang",
	})
	_, _ = svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "twice", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	_, err := svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "twice", ArchivedBy: "user:hayang", Actor: "user:hayang",
	})
	if err == nil {
		t.Fatal("expected second archive to error")
	}
}

func TestArchiveChannel_BadActor(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "x", ArchivedBy: "user:h", Actor: "",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestArchiveChannel_BadArchivedBy(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "x", ArchivedBy: "", Actor: "user:h",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestArchiveChannel_NameRequired(t *testing.T) {
	svc := setupChan(t)
	_, err := svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "", ArchivedBy: "user:h", Actor: "user:h",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestArchiveChannel_WrongKind(t *testing.T) {
	svc := setupChan(t)
	// Save a non-channel conversation with name "dm-named".
	conv, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "C-DM", Kind: conversation.ConversationKindDM,
		Name:      "dm-named",
		CreatedBy: "user:h", OpenedAt: svc.clock.Now(),
	})
	_ = svc.convRepo.Save(context.Background(), conv)
	// FindByName only matches kind='channel' so this will be NotFound;
	// the kind-mismatch branch is reachable only via id-based lookup
	// which Archive doesn't expose. Document the test as a NotFound case.
	_, err := svc.ArchiveChannel(context.Background(), ArchiveChannelCommand{
		Name: "dm-named", ArchivedBy: "user:h", Actor: "user:h",
	})
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestNewChannelManagementService_NilClock(t *testing.T) {
	db, w := setupRaw(t)
	svc := NewChannelManagementService(db, w.convRepo, w.sink, w.idgen, nil)
	if svc == nil {
		t.Fatal()
	}
}

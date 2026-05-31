package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupDB(t *testing.T) *ConversationRepo {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewConversationRepo(db)
}

func mkConv(t *testing.T, id conversation.ConversationID, kind conversation.ConversationKind, name string) *conversation.Conversation {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:        id,
		Kind:      kind,
		Name:      name,
		CreatedBy: conversation.IdentityRef("user:hayang"),
		OpenedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConversationRepo_SaveAndFindByID(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "conv-1", conversation.ConversationKindChannel, "general")
	if err := r.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(context.Background(), "conv-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != "general" || got.Kind() != conversation.ConversationKindChannel {
		t.Fatalf("got %+v", got)
	}
	if got.Status() != conversation.ConversationActive {
		t.Fatalf("status: %s", got.Status())
	}
}

func TestConversationRepo_FindByName(t *testing.T) {
	r := setupDB(t)
	if err := r.Save(context.Background(), mkConv(t, "c-1", conversation.ConversationKindChannel, "general")); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByName(context.Background(), "general")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "c-1" {
		t.Fatalf("got id=%s", got.ID())
	}
}

func TestConversationRepo_FindByName_NotFound(t *testing.T) {
	r := setupDB(t)
	_, err := r.FindByName(context.Background(), "nope")
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_FindByID_NotFound(t *testing.T) {
	r := setupDB(t)
	_, err := r.FindByID(context.Background(), "nope")
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_Save_DuplicateID(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "conv-1", conversation.ConversationKindDM, "")
	if err := r.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(context.Background(), c); !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("expected ErrConversationAlreadyExists, got %v", err)
	}
}

func TestConversationRepo_Save_DuplicateChannelName(t *testing.T) {
	r := setupDB(t)
	if err := r.Save(context.Background(), mkConv(t, "c-a", conversation.ConversationKindChannel, "shared")); err != nil {
		t.Fatal(err)
	}
	c2 := mkConv(t, "c-b", conversation.ConversationKindChannel, "shared")
	if err := r.Save(context.Background(), c2); !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("expected dup channel name to error, got %v", err)
	}
}

func TestConversationRepo_UpdateStatus_ActiveToClosed(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	now := time.Now().UTC()
	if err := r.UpdateStatus(context.Background(), "c-1",
		conversation.ConversationActive, conversation.ConversationClosed, 1,
		"user_request", "done", now); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), "c-1")
	if got.Status() != conversation.ConversationClosed || got.Version() != 2 {
		t.Fatalf("got status=%s ver=%d", got.Status(), got.Version())
	}
	if got.ClosedAt() == nil || got.ClosedReason() != "user_request" {
		t.Fatalf("closed_*: %v / %s", got.ClosedAt(), got.ClosedReason())
	}
}

func TestConversationRepo_UpdateStatus_VersionConflict(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	err := r.UpdateStatus(context.Background(), "c-1",
		conversation.ConversationActive, conversation.ConversationClosed, 99,
		"r", "m", time.Now())
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdateStatus_NotFound(t *testing.T) {
	r := setupDB(t)
	err := r.UpdateStatus(context.Background(), "nope",
		conversation.ConversationActive, conversation.ConversationClosed, 1,
		"r", "m", time.Now())
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdateArchive_Happy(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	now := time.Now().UTC()
	if err := r.UpdateArchive(context.Background(), "c-1", 1,
		conversation.IdentityRef("user:hayang"), now); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), "c-1")
	if got.Status() != conversation.ConversationArchived {
		t.Fatalf("got %s", got.Status())
	}
	if got.ArchivedBy() != "user:hayang" || got.ArchivedAt() == nil {
		t.Fatalf("archived fields: %s / %v", got.ArchivedBy(), got.ArchivedAt())
	}
}

func TestConversationRepo_UpdateArchive_Conflict(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindDM, "")
	_ = r.Save(context.Background(), c)
	err := r.UpdateArchive(context.Background(), "c-1", 999, "user:h", time.Now())
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdateArchive_NotFound(t *testing.T) {
	r := setupDB(t)
	err := r.UpdateArchive(context.Background(), "nope", 1, "user:h", time.Now())
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdateParticipants(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindChannel, "ch")
	_ = r.Save(context.Background(), c)
	parts := []conversation.ParticipantElement{
		{IdentityID: "user:a", Role: "owner", JoinedAt: "t", JoinedBy: "system"},
		{IdentityID: "user:b", Role: "member", JoinedAt: "t", JoinedBy: "user:a"},
	}
	if err := r.UpdateParticipants(context.Background(), "c-1", parts, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), "c-1")
	if len(got.Participants()) != 2 || got.Version() != 2 {
		t.Fatalf("got %d participants ver=%d", len(got.Participants()), got.Version())
	}
}

func TestConversationRepo_UpdateParticipants_Conflict(t *testing.T) {
	r := setupDB(t)
	c := mkConv(t, "c-1", conversation.ConversationKindChannel, "ch")
	_ = r.Save(context.Background(), c)
	err := r.UpdateParticipants(context.Background(), "c-1", nil, 99, time.Now())
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_FindByParent(t *testing.T) {
	r := setupDB(t)
	parent := mkConv(t, "p-1", conversation.ConversationKindChannel, "p")
	_ = r.Save(context.Background(), parent)
	child, _ := conversation.NewConversation(conversation.NewConversationInput{
		ID: "c-1", Kind: conversation.ConversationKindIssue,
		CreatedBy: "user:h", ParentConversationID: "p-1",
		OpenedAt: time.Now(),
	})
	_ = r.Save(context.Background(), child)
	got, err := r.FindByParent(context.Background(), "p-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "c-1" {
		t.Fatalf("got %d children", len(got))
	}
}

func TestConversationRepo_Find_KindAndStatus(t *testing.T) {
	r := setupDB(t)
	_ = r.Save(context.Background(), mkConv(t, "c-a", conversation.ConversationKindChannel, "a"))
	_ = r.Save(context.Background(), mkConv(t, "c-b", conversation.ConversationKindDM, ""))
	k := conversation.ConversationKindChannel
	got, err := r.Find(context.Background(), conversation.ConversationFilter{Kind: &k})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "c-a" {
		t.Fatalf("got %v", got)
	}
}

// TestConversationRepo_Find_ByOwnerRef_OrgScoped is the v2.7 #137 §-1 gate: the
// owner_ref conversation filter is org-scoped by construction — a cross-org
// owner_ref returns no rows (fail-closed, no leak), so the UI can't fetch
// another org's task/issue conversation by guessing its owner_ref.
func TestConversationRepo_Find_ByOwnerRef_OrgScoped(t *testing.T) {
	r := setupDB(t)
	taskRef := conversation.NewTaskOwnerRef("T-1")
	cA, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: "conv-a", Kind: conversation.ConversationKindTask, OwnerRef: taskRef,
		Name: "ta", CreatedBy: conversation.IdentityRef("user:a"), OpenedAt: time.Now().UTC(),
		OrganizationID: "org-A",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save(context.Background(), cA); err != nil {
		t.Fatal(err)
	}
	cB, err := conversation.NewConversation(conversation.NewConversationInput{
		ID: "conv-b", Kind: conversation.ConversationKindTask, OwnerRef: conversation.NewTaskOwnerRef("T-2"),
		Name: "tb", CreatedBy: conversation.IdentityRef("user:b"), OpenedAt: time.Now().UTC(),
		OrganizationID: "org-B",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save(context.Background(), cB); err != nil {
		t.Fatal(err)
	}

	// org-A filtered by T-1's owner_ref → its conversation.
	got, err := r.Find(context.Background(), conversation.ConversationFilter{OrganizationID: "org-A", OwnerRef: &taskRef})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "conv-a" {
		t.Fatalf("owner_ref filter: want [conv-a], got %+v", got)
	}
	// §-1: org-B querying org-A's owner_ref → empty (org-scoped fail-closed, no leak).
	got, err = r.Find(context.Background(), conversation.ConversationFilter{OrganizationID: "org-B", OwnerRef: &taskRef})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-org owner_ref must be empty (no leak), got %+v", got)
	}
}

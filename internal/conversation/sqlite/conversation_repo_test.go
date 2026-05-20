package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func newConv(t *testing.T, kind conversation.ConversationKind) *conversation.Conversation {
	t.Helper()
	c, err := conversation.NewConversation(conversation.NewConversationInput{
		ID:       conversation.ConversationID(idgen.MustNewULID()),
		Kind:     kind,
		Title:    "Title",
		OpenedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestConversationRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	if err := repo.Save(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(context.Background(), c.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != conversation.ConversationKindDM {
		t.Fatal()
	}
	if got.Status() != conversation.ConversationOpen {
		t.Fatal()
	}
}

func TestConversationRepo_Save_Duplicate(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c)
	err := repo.Save(context.Background(), c)
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_FindByID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	_, err := repo.FindByID(context.Background(), "C-NEVER")
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdateStatus_OpenToClosed(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c)
	err := repo.UpdateStatus(context.Background(), c.ID(),
		conversation.ConversationOpen, conversation.ConversationClosed,
		1, "done", "task finished", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), c.ID())
	if got.Status() != conversation.ConversationClosed {
		t.Fatal()
	}
	if got.ClosedReason() != "done" {
		t.Fatal()
	}
}

func TestConversationRepo_UpdateStatus_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c)
	err := repo.UpdateStatus(context.Background(), c.ID(),
		conversation.ConversationOpen, conversation.ConversationClosed,
		99, "x", "y", time.Now())
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdateStatus_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	err := repo.UpdateStatus(context.Background(), "C-NEVER",
		conversation.ConversationOpen, conversation.ConversationClosed,
		1, "x", "y", time.Now())
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdateStatus_InvalidStatus(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	err := repo.UpdateStatus(context.Background(), "C-1",
		"bogus", conversation.ConversationClosed, 1, "x", "y", time.Now())
	if !errors.Is(err, conversation.ErrConversationInvalidStatus) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdatePrimaryChannel(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c)
	err := repo.UpdatePrimaryChannel(context.Background(), c.ID(),
		"feishu", "thread-x", 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), c.ID())
	if got.PrimaryChannelHint() != "feishu" {
		t.Fatal()
	}
}

func TestConversationRepo_UpdatePrimaryChannel_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	err := repo.UpdatePrimaryChannel(context.Background(), "C-NEVER", "x", "y", 1, time.Now())
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdatePrimaryChannel_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c)
	err := repo.UpdatePrimaryChannel(context.Background(), c.ID(), "x", "y", 99, time.Now())
	if !errors.Is(err, conversation.ErrConversationVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_UpdatePrimaryChannel_DuplicateChannelThread(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c1 := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c1)
	_ = repo.UpdatePrimaryChannel(context.Background(), c1.ID(), "feishu", "t-1", 1, time.Now())

	c2 := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c2)
	err := repo.UpdatePrimaryChannel(context.Background(), c2.ID(), "feishu", "t-1", 1, time.Now())
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_FindByChannelAndThreadKey(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c := newConv(t, conversation.ConversationKindDM)
	_ = repo.Save(context.Background(), c)
	_ = repo.UpdatePrimaryChannel(context.Background(), c.ID(), "feishu", "t-1", 1, time.Now())
	got, err := repo.FindByChannelAndThreadKey(context.Background(), "feishu", "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != c.ID() {
		t.Fatal()
	}
}

func TestConversationRepo_FindByChannelAndThreadKey_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	_, err := repo.FindByChannelAndThreadKey(context.Background(), "feishu", "missing")
	if !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestConversationRepo_Find_AllFilters(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	c1 := newConv(t, conversation.ConversationKindDM)
	c2 := newConv(t, conversation.ConversationKindGroupThread)
	_ = repo.Save(context.Background(), c1)
	_ = repo.Save(context.Background(), c2)

	kind := conversation.ConversationKindDM
	got, _ := repo.Find(context.Background(), conversation.ConversationFilter{Kind: &kind})
	if len(got) != 1 {
		t.Fatalf("kind filter: %d", len(got))
	}

	status := conversation.ConversationOpen
	got, _ = repo.Find(context.Background(), conversation.ConversationFilter{Status: &status})
	if len(got) != 2 {
		t.Fatalf("status filter: %d", len(got))
	}
}

func TestConversationRepo_Find_Pagination(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	for i := 0; i < 5; i++ {
		_ = repo.Save(context.Background(), newConv(t, conversation.ConversationKindDM))
	}
	page1, _ := repo.Find(context.Background(), conversation.ConversationFilter{Limit: 2})
	if len(page1) != 2 {
		t.Fatalf("page1: %d", len(page1))
	}
	cursor := page1[1].ID()
	page2, _ := repo.Find(context.Background(), conversation.ConversationFilter{Cursor: &cursor, Limit: 2})
	if len(page2) != 2 {
		t.Fatalf("page2: %d", len(page2))
	}
	for _, e := range page2 {
		for _, p := range page1 {
			if e.ID() == p.ID() {
				t.Fatalf("cursor leak")
			}
		}
	}
}

func TestConversationRepo_Save_NilConversation(t *testing.T) {
	db := openTestDB(t)
	repo := NewConversationRepo(db)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

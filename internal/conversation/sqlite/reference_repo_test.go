package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupRefRepo(t *testing.T) *ReferenceRepo {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewReferenceRepo(db)
}

func mkRef(id string, child, source conversation.ConversationID, msgID conversation.MessageID) *conversation.ConversationMessageReference {
	return &conversation.ConversationMessageReference{
		ID:                   id,
		ChildConversationID:  child,
		SourceConversationID: source,
		SourceMessageID:      msgID,
		CreatedBy:            conversation.IdentityRef("user:hayang"),
		CreatedAt:            time.Now().UTC(),
	}
}

func TestReferenceRepo_SaveAndFindByChild(t *testing.T) {
	r := setupRefRepo(t)
	refs := []*conversation.ConversationMessageReference{
		mkRef("r-1", "child-1", "src-1", "msg-1"),
		mkRef("r-2", "child-1", "src-1", "msg-2"),
	}
	if err := r.Save(context.Background(), refs); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByChildConvID(context.Background(), "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}

func TestReferenceRepo_FindBySource(t *testing.T) {
	r := setupRefRepo(t)
	_ = r.Save(context.Background(), []*conversation.ConversationMessageReference{
		mkRef("r-1", "child-A", "src-1", "msg-X"),
		mkRef("r-2", "child-B", "src-1", "msg-X"),
	})
	got, err := r.FindBySourceMsgID(context.Background(), "msg-X")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
}

func TestReferenceRepo_Save_Duplicate(t *testing.T) {
	r := setupRefRepo(t)
	ref := mkRef("r-1", "child-1", "src-1", "msg-1")
	_ = r.Save(context.Background(), []*conversation.ConversationMessageReference{ref})
	err := r.Save(context.Background(), []*conversation.ConversationMessageReference{
		mkRef("r-2", "child-1", "src-1", "msg-1"),
	})
	if !errors.Is(err, conversation.ErrConversationAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestReferenceRepo_Save_Empty(t *testing.T) {
	r := setupRefRepo(t)
	if err := r.Save(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceRepo_Save_NilInBatch(t *testing.T) {
	r := setupRefRepo(t)
	if err := r.Save(context.Background(), []*conversation.ConversationMessageReference{nil}); err == nil {
		t.Fatal()
	}
}

func TestReferenceRepo_DeleteByChild(t *testing.T) {
	r := setupRefRepo(t)
	_ = r.Save(context.Background(), []*conversation.ConversationMessageReference{
		mkRef("r-1", "child-1", "src-1", "msg-1"),
		mkRef("r-2", "child-2", "src-1", "msg-2"),
	})
	if err := r.DeleteByChildConvID(context.Background(), "child-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByChildConvID(context.Background(), "child-1")
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	got, _ = r.FindByChildConvID(context.Background(), "child-2")
	if len(got) != 1 {
		t.Fatal()
	}
}

func TestReferenceRepo_FindByChild_Empty(t *testing.T) {
	r := setupRefRepo(t)
	got, err := r.FindByChildConvID(context.Background(), "no-such-child")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal()
	}
}

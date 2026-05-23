package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

func setupIDRepo(t *testing.T) *SQLiteIdentityRepo {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLiteIdentityRepo(db)
}

func mkID(t *testing.T, id IdentityID, kind Kind, name string) *Identity {
	t.Helper()
	i, err := NewIdentity(NewIdentityInput{ID: id, Kind: kind, DisplayName: name, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestIdentityRepo_SaveAndFindByID(t *testing.T) {
	r := setupIDRepo(t)
	if err := r.Save(context.Background(), mkID(t, "user:hayang", KindUser, "Hayang")); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(context.Background(), "user:hayang")
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName() != "Hayang" {
		t.Fatalf("got %v", got)
	}
}

func TestIdentityRepo_FindByID_NotFound(t *testing.T) {
	r := setupIDRepo(t)
	_, err := r.FindByID(context.Background(), "nope")
	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestIdentityRepo_Save_Duplicate(t *testing.T) {
	r := setupIDRepo(t)
	i := mkID(t, "user:a", KindUser, "x")
	_ = r.Save(context.Background(), i)
	if err := r.Save(context.Background(), i); !errors.Is(err, ErrIdentityAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestIdentityRepo_Find_Filter(t *testing.T) {
	r := setupIDRepo(t)
	_ = r.Save(context.Background(), mkID(t, "user:a", KindUser, "A"))
	_ = r.Save(context.Background(), mkID(t, "agent:b", KindAgent, "B"))
	k := KindUser
	got, err := r.Find(context.Background(), IdentityFilter{Kind: &k})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID() != "user:a" {
		t.Fatalf("got %v", got)
	}
}

func TestIdentityRepo_Update_CAS(t *testing.T) {
	r := setupIDRepo(t)
	i := mkID(t, "user:a", KindUser, "x")
	_ = r.Save(context.Background(), i)
	_ = i.Rename("y", time.Now())
	if err := r.Update(context.Background(), i, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(context.Background(), "user:a")
	if got.DisplayName() != "y" || got.Version() != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestIdentityRepo_Update_VersionConflict(t *testing.T) {
	r := setupIDRepo(t)
	i := mkID(t, "user:a", KindUser, "x")
	_ = r.Save(context.Background(), i)
	err := r.Update(context.Background(), i, 99)
	if !errors.Is(err, ErrIdentityVersionConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestIdentityRepo_Update_NotFound(t *testing.T) {
	r := setupIDRepo(t)
	i := mkID(t, "user:never", KindUser, "x")
	err := r.Update(context.Background(), i, 1)
	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestIdentityRepo_NilGuards(t *testing.T) {
	r := setupIDRepo(t)
	if err := r.Save(context.Background(), nil); err == nil {
		t.Fatal()
	}
	if err := r.Update(context.Background(), nil, 1); err == nil {
		t.Fatal()
	}
}

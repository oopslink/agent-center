package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupRepo(t *testing.T) (*Repo, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := persistence.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(db), db
}

// mintAR is a test-only AR builder. Each call uses a unique value_hash
// so the UNIQUE index on (value_hash) doesn't trip.
func mintAR(t *testing.T, id admintoken.TokenID, owner admintoken.Owner, scopes []admintoken.Scope) *admintoken.AdminToken {
	t.Helper()
	tok, err := admintoken.New(admintoken.NewAdminTokenInput{
		ID:        id,
		Owner:     owner,
		Scopes:    scopes,
		ValueHash: admintoken.HashPlaintext(string("acat_" + id)),
		CreatedAt: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC),
		CreatedBy: "system",
	})
	if err != nil {
		t.Fatalf("mint AR: %v", err)
	}
	return tok
}

func TestRepo_SaveAndFindByID(t *testing.T) {
	repo, _ := setupRepo(t)
	tok := mintAR(t, "T-1", "cli:hayang", []admintoken.Scope{"*"})
	if err := repo.Save(context.Background(), tok); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "T-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Owner() != "cli:hayang" {
		t.Fatalf("owner mismatch: %s", got.Owner())
	}
}

func TestRepo_Save_NilFails(t *testing.T) {
	repo, _ := setupRepo(t)
	if err := repo.Save(context.Background(), nil); err == nil {
		t.Fatal("Save(nil) should error")
	}
}

func TestRepo_Save_DuplicateHashRejected(t *testing.T) {
	repo, _ := setupRepo(t)
	// Both ARs share the same plaintext → same value_hash. UNIQUE index
	// must surface ErrTokenAlreadyExists.
	hash := admintoken.HashPlaintext("acat_dup")
	build := func(id admintoken.TokenID) *admintoken.AdminToken {
		t.Helper()
		ar, err := admintoken.New(admintoken.NewAdminTokenInput{
			ID: id, Owner: "cli:x", Scopes: []admintoken.Scope{"*"},
			ValueHash: hash, CreatedAt: time.Now(), CreatedBy: "system",
		})
		if err != nil {
			t.Fatal(err)
		}
		return ar
	}
	if err := repo.Save(context.Background(), build("A")); err != nil {
		t.Fatalf("first save: %v", err)
	}
	err := repo.Save(context.Background(), build("B"))
	if !errors.Is(err, admintoken.ErrTokenAlreadyExists) {
		t.Fatalf("want ErrTokenAlreadyExists, got %v", err)
	}
}

func TestRepo_FindByID_NotFound(t *testing.T) {
	repo, _ := setupRepo(t)
	_, err := repo.FindByID(context.Background(), "ghost")
	if !errors.Is(err, admintoken.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v", err)
	}
}

func TestRepo_FindByHash(t *testing.T) {
	repo, _ := setupRepo(t)
	tok := mintAR(t, "T-1", "cli:x", []admintoken.Scope{"*"})
	_ = repo.Save(context.Background(), tok)
	got, err := repo.FindByHash(context.Background(), tok.ValueHash())
	if err != nil {
		t.Fatalf("FindByHash: %v", err)
	}
	if got.ID() != "T-1" {
		t.Fatalf("id mismatch: %s", got.ID())
	}
}

func TestRepo_FindByHash_EmptyHash(t *testing.T) {
	repo, _ := setupRepo(t)
	_, err := repo.FindByHash(context.Background(), nil)
	if !errors.Is(err, admintoken.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v", err)
	}
}

func TestRepo_FindByHash_UnknownHash(t *testing.T) {
	repo, _ := setupRepo(t)
	bogus := admintoken.HashPlaintext("acat_no_such")
	_, err := repo.FindByHash(context.Background(), bogus)
	if !errors.Is(err, admintoken.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v", err)
	}
}

func TestRepo_FindAllAndFindByOwner(t *testing.T) {
	repo, _ := setupRepo(t)
	_ = repo.Save(context.Background(), mintAR(t, "T-A", "cli:a", []admintoken.Scope{"*"}))
	_ = repo.Save(context.Background(), mintAR(t, "T-B", "cli:b", []admintoken.Scope{"*"}))
	_ = repo.Save(context.Background(), mintAR(t, "T-C", "cli:a", []admintoken.Scope{"task:*"}))

	all, err := repo.FindAll(context.Background())
	if err != nil || len(all) != 3 {
		t.Fatalf("FindAll: got=%d err=%v", len(all), err)
	}
	byOwner, err := repo.FindByOwner(context.Background(), "cli:a")
	if err != nil {
		t.Fatalf("FindByOwner: %v", err)
	}
	if len(byOwner) != 2 {
		t.Fatalf("expected 2 for cli:a, got %d", len(byOwner))
	}
}

func TestRepo_Revoke_HappyPath(t *testing.T) {
	repo, _ := setupRepo(t)
	tok := mintAR(t, "T-R", "cli:x", []admintoken.Scope{"*"})
	_ = repo.Save(context.Background(), tok)
	if err := repo.Revoke(context.Background(), "T-R", "system", "rotated", tok.Version()); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "T-R")
	if !got.IsRevoked() {
		t.Fatal("not revoked after Revoke")
	}
}

func TestRepo_Revoke_NotFound(t *testing.T) {
	repo, _ := setupRepo(t)
	err := repo.Revoke(context.Background(), "ghost", "system", "x", 1)
	if !errors.Is(err, admintoken.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v", err)
	}
}

func TestRepo_Revoke_TwiceTerminal(t *testing.T) {
	repo, _ := setupRepo(t)
	tok := mintAR(t, "T-R2", "cli:x", []admintoken.Scope{"*"})
	_ = repo.Save(context.Background(), tok)
	if err := repo.Revoke(context.Background(), "T-R2", "system", "first", tok.Version()); err != nil {
		t.Fatal(err)
	}
	// Second revoke fetches version (now 2) but row is already revoked.
	err := repo.Revoke(context.Background(), "T-R2", "system", "second", 2)
	if !errors.Is(err, admintoken.ErrTokenRevoked) {
		t.Fatalf("want ErrTokenRevoked, got %v", err)
	}
}

func TestRepo_Revoke_VersionConflict(t *testing.T) {
	repo, _ := setupRepo(t)
	tok := mintAR(t, "T-V", "cli:x", []admintoken.Scope{"*"})
	_ = repo.Save(context.Background(), tok)
	// Pass a stale version number.
	err := repo.Revoke(context.Background(), "T-V", "system", "x", 99)
	if !errors.Is(err, admintoken.ErrTokenVersionConflict) {
		t.Fatalf("want ErrTokenVersionConflict, got %v", err)
	}
}

func TestRepo_UpdateLastUsedAt(t *testing.T) {
	repo, _ := setupRepo(t)
	tok := mintAR(t, "T-U", "cli:x", []admintoken.Scope{"*"})
	_ = repo.Save(context.Background(), tok)
	now := time.Now().UTC()
	if err := repo.UpdateLastUsedAt(context.Background(), "T-U", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("UpdateLastUsedAt: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "T-U")
	if got.LastUsedAt() == nil {
		t.Fatal("LastUsedAt still nil after update")
	}
}

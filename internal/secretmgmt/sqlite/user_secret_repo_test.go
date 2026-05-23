package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
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

func freshUserSecret(t *testing.T, id secretmgmt.UserSecretID, name string) *secretmgmt.UserSecret {
	t.Helper()
	s, err := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: id, Name: name, Kind: secretmgmt.UserSecretKindMCP,
		Ciphertext: []byte("ciphered-binary"),
		Nonce:      []byte("123456789012"),
		CreatedAt:  time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		CreatedBy:  "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUserSecretRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	s := freshUserSecret(t, "01HUS1", "github-pat")
	if err := repo.Save(context.Background(), s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "01HUS1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name() != "github-pat" {
		t.Fatalf("name: %s", got.Name())
	}
	if string(got.Ciphertext()) != "ciphered-binary" {
		t.Fatalf("ciphertext roundtrip mismatch")
	}
}

func TestUserSecretRepo_Save_DuplicateName(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "github-pat"))
	err := repo.Save(context.Background(), freshUserSecret(t, "01HUS2", "github-pat"))
	if !errors.Is(err, secretmgmt.ErrUserSecretNameTaken) {
		t.Fatalf("expected name taken, got %v", err)
	}
}

func TestUserSecretRepo_FindByName(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "github-pat"))
	got, err := repo.FindByName(context.Background(), "github-pat")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "01HUS1" {
		t.Fatalf("id: %s", got.ID())
	}
}

func TestUserSecretRepo_FindByName_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_, err := repo.FindByName(context.Background(), "nope")
	if !errors.Is(err, secretmgmt.ErrUserSecretNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestUserSecretRepo_FindAll_FilterByKind(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "github-pat"))
	cloud, _ := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "01HUS2", Name: "aws-key", Kind: secretmgmt.UserSecretKindCloudCredential,
		Ciphertext: []byte("c"), Nonce: []byte("n"),
		CreatedAt: time.Now(), CreatedBy: "u",
	})
	_ = repo.Save(context.Background(), cloud)
	mcp := secretmgmt.UserSecretKindMCP
	got, _ := repo.FindAll(context.Background(), secretmgmt.UserSecretFilter{Kind: &mcp})
	if len(got) != 1 || got[0].Name() != "github-pat" {
		t.Fatalf("filter MCP: %v", got)
	}
}

func TestUserSecretRepo_UpdateValue(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "github-pat"))
	at := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if err := repo.UpdateValue(context.Background(), "01HUS1", []byte("new-c"), []byte("new-n"), at, 1); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "01HUS1")
	if string(got.Ciphertext()) != "new-c" || string(got.Nonce()) != "new-n" {
		t.Fatal("value not updated")
	}
	if got.Version() != 2 {
		t.Fatalf("version: %d", got.Version())
	}
}

func TestUserSecretRepo_UpdateValue_VersionConflict(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "n"))
	err := repo.UpdateValue(context.Background(), "01HUS1", []byte("c"), []byte("n"), time.Now(), 99)
	if !errors.Is(err, secretmgmt.ErrUserSecretVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

func TestUserSecretRepo_UpdateValue_RevokedRejected(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "n"))
	_ = repo.UpdateState(context.Background(), "01HUS1",
		secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
		time.Now(), "user:x", secretmgmt.UserSecretRevokedReasonManual, "test", 1)
	err := repo.UpdateValue(context.Background(), "01HUS1", []byte("c"), []byte("n"), time.Now(), 2)
	if !errors.Is(err, secretmgmt.ErrUserSecretRevoked) {
		t.Fatalf("expected revoked, got %v", err)
	}
}

func TestUserSecretRepo_UpdateState_RevokeHappy(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "n"))
	at := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if err := repo.UpdateState(context.Background(), "01HUS1",
		secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
		at, "user:x", secretmgmt.UserSecretRevokedReasonManual, "test", 1); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "01HUS1")
	if got.State() != secretmgmt.UserSecretRevoked {
		t.Fatalf("state: %s", got.State())
	}
	if got.RevokedBy() != "user:x" {
		t.Fatalf("revoked_by: %s", got.RevokedBy())
	}
}

func TestUserSecretRepo_UpdateLastUsedAt(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HUS1", "n"))
	at := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if err := repo.UpdateLastUsedAt(context.Background(), "01HUS1", at); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.FindByID(context.Background(), "01HUS1")
	if got.LastUsedAt() == nil || !got.LastUsedAt().Equal(at) {
		t.Fatalf("last_used_at: %v", got.LastUsedAt())
	}
	// version NOT bumped (per design).
	if got.Version() != 1 {
		t.Fatalf("version should not bump: %d", got.Version())
	}
}

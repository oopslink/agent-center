package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// =============================================================================
// Closed-DB error paths
// =============================================================================

func TestUserSecretRepo_ClosedDB(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = db.Close()
	if _, err := repo.FindByID(context.Background(), "x"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindByName(context.Background(), "n"); err == nil {
		t.Fatal()
	}
	if _, err := repo.FindAll(context.Background(), secretmgmt.UserSecretFilter{}); err == nil {
		t.Fatal()
	}
	if err := repo.Save(context.Background(), freshUserSecret(t, "01H", "n")); err == nil {
		t.Fatal()
	}
	if err := repo.UpdateValue(context.Background(), "x", []byte("c"), []byte("n"), time.Now(), 1); err == nil {
		t.Fatal()
	}
	if err := repo.UpdateState(context.Background(), "x",
		secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
		time.Now(), "u", secretmgmt.UserSecretRevokedReasonManual, "m", 1); err == nil {
		t.Fatal()
	}
	if err := repo.UpdateLastUsedAt(context.Background(), "x", time.Now()); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// Scan error paths
// =============================================================================

func TestUserSecretRepo_Scan_BadCreatedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO user_secrets
		(id, name, kind, value_ciphertext, value_nonce, state,
		 created_at, created_by, version)
		VALUES ('01H-BC', 'n', 'mcp', X'01', X'01', 'active', 'not-a-time', 'u', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewUserSecretRepo(db)
	if _, err := repo.FindByID(context.Background(), "01H-BC"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestUserSecretRepo_Scan_BadLastUsedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO user_secrets
		(id, name, kind, value_ciphertext, value_nonce, state,
		 created_at, created_by, last_used_at, version)
		VALUES ('01H-BL', 'n', 'mcp', X'01', X'01', 'active',
		        '2026-05-22T00:00:00Z', 'u', 'not-a-time', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewUserSecretRepo(db)
	if _, err := repo.FindByID(context.Background(), "01H-BL"); err == nil {
		t.Fatal()
	}
}

func TestUserSecretRepo_Scan_BadRotatedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO user_secrets
		(id, name, kind, value_ciphertext, value_nonce, state,
		 created_at, created_by, rotated_at, version)
		VALUES ('01H-BR', 'n', 'mcp', X'01', X'01', 'active',
		        '2026-05-22T00:00:00Z', 'u', 'not-a-time', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewUserSecretRepo(db)
	if _, err := repo.FindByID(context.Background(), "01H-BR"); err == nil {
		t.Fatal()
	}
}

func TestUserSecretRepo_Scan_BadRevokedAt(t *testing.T) {
	db := openTestDB(t)
	_, err := db.ExecContext(context.Background(), `INSERT INTO user_secrets
		(id, name, kind, value_ciphertext, value_nonce, state,
		 created_at, created_by, revoked_at, version)
		VALUES ('01H-BV', 'n', 'mcp', X'01', X'01', 'revoked',
		        '2026-05-22T00:00:00Z', 'u', 'not-a-time', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewUserSecretRepo(db)
	if _, err := repo.FindByID(context.Background(), "01H-BV"); err == nil {
		t.Fatal()
	}
}

// =============================================================================
// diagnoseUserSecretUpdate branches
// =============================================================================

// Not-found: UpdateValue on non-existent id triggers diagnose → ErrNotFound.
func TestUserSecretRepo_UpdateValue_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	err := repo.UpdateValue(context.Background(), "01H-MISSING",
		[]byte("c"), []byte("n"), time.Now(), 1)
	if !errors.Is(err, secretmgmt.ErrUserSecretNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// UpdateState not-found path
func TestUserSecretRepo_UpdateState_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	err := repo.UpdateState(context.Background(), "01H-MISSING",
		secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
		time.Now(), "u", secretmgmt.UserSecretRevokedReasonManual, "m", 1)
	if !errors.Is(err, secretmgmt.ErrUserSecretNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// UpdateState double-revoke triggers diagnose → ErrRevoked path.
func TestUserSecretRepo_UpdateState_DoubleRevoke(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01H", "n"))
	_ = repo.UpdateState(context.Background(), "01H",
		secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
		time.Now(), "u", secretmgmt.UserSecretRevokedReasonManual, "m", 1)
	// Second revoke on already-revoked row: from=active mismatch → diagnose
	err := repo.UpdateState(context.Background(), "01H",
		secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
		time.Now(), "u", secretmgmt.UserSecretRevokedReasonManual, "m", 2)
	if !errors.Is(err, secretmgmt.ErrUserSecretRevoked) {
		t.Fatalf("expected revoked, got %v", err)
	}
}

// =============================================================================
// UpdateLastUsedAt not-found
// =============================================================================

func TestUserSecretRepo_UpdateLastUsedAt_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	err := repo.UpdateLastUsedAt(context.Background(), "01H-MISSING", time.Now())
	if !errors.Is(err, secretmgmt.ErrUserSecretNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

// =============================================================================
// Save: duplicate PK (non-name) and duplicate ciphertext absence
// =============================================================================

func TestUserSecretRepo_Save_DuplicateID(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HX", "first"))
	// Same id, different name → PK conflict (not name conflict).
	other, _ := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "01HX", Name: "second", Kind: secretmgmt.UserSecretKindMCP,
		Ciphertext: []byte("c2"), Nonce: []byte("n2"),
		CreatedAt: time.Now(), CreatedBy: "u",
	})
	err := repo.Save(context.Background(), other)
	if !errors.Is(err, secretmgmt.ErrUserSecretAlreadyExists) {
		t.Fatalf("expected already exists, got %v", err)
	}
}

// =============================================================================
// nullString / nullTimePtr helper edges
// =============================================================================

func TestUserSecret_NullHelpers(t *testing.T) {
	if nullString("") != nil {
		t.Fatal()
	}
	if nullString("x") != "x" {
		t.Fatal()
	}
	if nullTimePtr(nil) != nil {
		t.Fatal()
	}
	now := time.Now()
	if nullTimePtr(&now) == nil {
		t.Fatal()
	}
}

func TestUserSecret_IsUnique(t *testing.T) {
	if isUnique(nil) {
		t.Fatal("nil err not unique")
	}
	if isUnique(errors.New("some other error")) {
		t.Fatal()
	}
	if !isUnique(errors.New("UNIQUE constraint failed: user_secrets.name")) {
		t.Fatal("real unique should match")
	}
}

// FindAll filter by both kind + state
func TestUserSecretRepo_FindAll_BothFilters(t *testing.T) {
	db := openTestDB(t)
	repo := NewUserSecretRepo(db)
	_ = repo.Save(context.Background(), freshUserSecret(t, "01HA", "a"))
	revoked, _ := secretmgmt.NewUserSecret(secretmgmt.NewUserSecretInput{
		ID: "01HB", Name: "b", Kind: secretmgmt.UserSecretKindMCP,
		Ciphertext: []byte("c"), Nonce: []byte("n"),
		CreatedAt: time.Now(), CreatedBy: "u",
	})
	_ = repo.Save(context.Background(), revoked)
	_ = repo.UpdateState(context.Background(), "01HB",
		secretmgmt.UserSecretActive, secretmgmt.UserSecretRevoked,
		time.Now(), "u", secretmgmt.UserSecretRevokedReasonManual, "m", 1)
	mcp := secretmgmt.UserSecretKindMCP
	active := secretmgmt.UserSecretActive
	got, err := repo.FindAll(context.Background(), secretmgmt.UserSecretFilter{
		Kind: &mcp, State: &active,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name() != "a" {
		t.Fatalf("filter: %v", got)
	}
}

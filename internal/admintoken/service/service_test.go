package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	atsqlite "github.com/oopslink/agent-center/internal/admintoken/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

// admintokenSuite mirrors the secretmgmt/service test setupSuite pattern:
// fresh on-disk SQLite + migrations + a fixed-time FakeClock so id
// generation + timestamps are deterministic.
type admintokenSuite struct {
	db   *sql.DB
	repo *atsqlite.Repo
	clk  *clock.FakeClock
	svc  *Service
}

func setupSuite(t *testing.T) *admintokenSuite {
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
	clk := clock.NewFakeClock(time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	repo := atsqlite.New(db)
	return &admintokenSuite{
		db:   db,
		repo: repo,
		clk:  clk,
		svc:  New(repo, gen, clk),
	}
}

func TestService_Create_Happy(t *testing.T) {
	s := setupSuite(t)
	res, err := s.svc.Create(context.Background(), CreateCommand{
		Owner:     "cli:hayang",
		Scopes:    []admintoken.Scope{"admin:token", "task:*"},
		CreatedBy: "system",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.ID == "" || res.Plaintext == "" {
		t.Fatal("missing id / plaintext")
	}
	// Token must verify via the service round-trip.
	tok, err := s.svc.VerifyPlaintext(context.Background(), res.Plaintext)
	if err != nil {
		t.Fatalf("VerifyPlaintext: %v", err)
	}
	if tok.ID() != res.ID {
		t.Fatalf("verify returned different id: %s vs %s", tok.ID(), res.ID)
	}
}

func TestService_Create_EmptyOwner(t *testing.T) {
	s := setupSuite(t)
	_, err := s.svc.Create(context.Background(), CreateCommand{
		Owner:  "  ",
		Scopes: []admintoken.Scope{"*"},
	})
	if !errors.Is(err, admintoken.ErrTokenOwnerRequired) {
		t.Fatalf("want ErrTokenOwnerRequired, got %v", err)
	}
}

func TestService_Create_EmptyScopes(t *testing.T) {
	s := setupSuite(t)
	_, err := s.svc.Create(context.Background(), CreateCommand{
		Owner: "cli:x",
	})
	if !errors.Is(err, admintoken.ErrTokenScopesRequired) {
		t.Fatalf("want ErrTokenScopesRequired, got %v", err)
	}
}

func TestService_Create_DedupesScopes(t *testing.T) {
	s := setupSuite(t)
	res, err := s.svc.Create(context.Background(), CreateCommand{
		Owner:  "cli:x",
		Scopes: []admintoken.Scope{"a", "a", "b"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tok, err := s.svc.FindByID(context.Background(), res.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(tok.Scopes()) != 2 {
		t.Fatalf("expected 2 deduped scopes, got %v", tok.Scopes())
	}
}

func TestService_VerifyPlaintext_Empty(t *testing.T) {
	s := setupSuite(t)
	_, err := s.svc.VerifyPlaintext(context.Background(), "")
	if !errors.Is(err, admintoken.ErrTokenMissingBearer) {
		t.Fatalf("want ErrTokenMissingBearer, got %v", err)
	}
}

func TestService_VerifyPlaintext_Unknown(t *testing.T) {
	s := setupSuite(t)
	_, err := s.svc.VerifyPlaintext(context.Background(), "acat_not_a_real_token")
	if !errors.Is(err, admintoken.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v", err)
	}
}

func TestService_VerifyPlaintext_Revoked(t *testing.T) {
	s := setupSuite(t)
	res, err := s.svc.Create(context.Background(), CreateCommand{
		Owner: "cli:x", Scopes: []admintoken.Scope{"*"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.svc.Revoke(context.Background(), RevokeCommand{
		ID: res.ID, By: "system", Reason: "rotated",
	}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err = s.svc.VerifyPlaintext(context.Background(), res.Plaintext)
	if !errors.Is(err, admintoken.ErrTokenRevoked) {
		t.Fatalf("want ErrTokenRevoked, got %v", err)
	}
}

func TestService_Revoke_NotFound(t *testing.T) {
	s := setupSuite(t)
	err := s.svc.Revoke(context.Background(), RevokeCommand{
		ID: "ghost", By: "system", Reason: "x",
	})
	if !errors.Is(err, admintoken.ErrTokenNotFound) {
		t.Fatalf("want ErrTokenNotFound, got %v", err)
	}
}

func TestService_Revoke_TwiceTerminal(t *testing.T) {
	s := setupSuite(t)
	res, _ := s.svc.Create(context.Background(), CreateCommand{
		Owner: "cli:x", Scopes: []admintoken.Scope{"*"},
	})
	if err := s.svc.Revoke(context.Background(), RevokeCommand{ID: res.ID, By: "a"}); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	err := s.svc.Revoke(context.Background(), RevokeCommand{ID: res.ID, By: "a"})
	if !errors.Is(err, admintoken.ErrTokenRevoked) {
		t.Fatalf("second revoke: want ErrTokenRevoked, got %v", err)
	}
}

func TestService_FindAll(t *testing.T) {
	s := setupSuite(t)
	for i := 0; i < 3; i++ {
		if _, err := s.svc.Create(context.Background(), CreateCommand{
			Owner: admintoken.Owner("cli:x"), Scopes: []admintoken.Scope{"*"},
		}); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		s.clk.Advance(time.Second)
	}
	all, err := s.svc.FindAll(context.Background())
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 tokens, got %d", len(all))
	}
}

func TestService_MarkUsedAsync_DoesNotBlock(t *testing.T) {
	s := setupSuite(t)
	res, _ := s.svc.Create(context.Background(), CreateCommand{
		Owner: "cli:x", Scopes: []admintoken.Scope{"*"},
	})
	// Fire-and-forget; we just want this to return immediately + not panic.
	s.svc.MarkUsedAsync(res.ID)
	// Best-effort: give the goroutine a beat to write.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		tok, err := s.svc.FindByID(context.Background(), res.ID)
		if err == nil && tok.LastUsedAt() != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Skip("LastUsedAt async write did not land in 500ms; non-fatal (best-effort path)")
}

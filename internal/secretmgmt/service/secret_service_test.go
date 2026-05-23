package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
	smsqlite "github.com/oopslink/agent-center/internal/secretmgmt/sqlite"
)

type secretSuite struct {
	db        *sql.DB
	repo      *smsqlite.UserSecretRepo
	eventRepo *obsqlite.EventRepo
	sink      *observability.EventSink
	clk       *clock.FakeClock
	mk        *secretmgmt.MasterKey
	svc       *UserSecretService
	resolver  *SecretResolutionService
}

func setupSecretSuite(t *testing.T) *secretSuite {
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
	clk := clock.NewFakeClock(time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, clk)
	repo := smsqlite.NewUserSecretRepo(db)
	mk, _ := secretmgmt.GenerateMasterKey()
	return &secretSuite{
		db:        db,
		repo:      repo,
		eventRepo: er,
		sink:      sink,
		clk:       clk,
		mk:        mk,
		svc:       NewUserSecretService(db, repo, gen, sink, clk, mk),
		resolver:  NewSecretResolutionService(db, repo, sink, clk, mk),
	}
}

// =============================================================================
// UserSecretService
// =============================================================================

func TestUserSecretService_Create_Happy(t *testing.T) {
	s := setupSecretSuite(t)
	plain := []byte("ghp_TestPlaintext")
	res, err := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "github-pat", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: plain, ActorIdentity: "user:hayang",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.EventID == "" {
		t.Fatal("event id missing")
	}
	// Plaintext NOT in stored ciphertext.
	got, _ := s.repo.FindByID(context.Background(), res.ID)
	if strings.Contains(string(got.Ciphertext()), "TestPlaintext") {
		t.Fatal("plaintext leaked into ciphertext")
	}
	// Event payload contains no plaintext.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	for _, e := range events {
		for _, v := range e.Payload() {
			if str, ok := v.(string); ok && strings.Contains(str, "TestPlaintext") {
				t.Fatalf("plaintext leaked into event payload: %s", str)
			}
		}
	}
}

func TestUserSecretService_Create_NoMasterKey(t *testing.T) {
	s := setupSecretSuite(t)
	s.svc.masterKey = nil
	_, err := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "n", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("p"), ActorIdentity: "user:x",
	})
	if !errors.Is(err, secretmgmt.ErrMasterKeyNotLoaded) {
		t.Fatalf("expected no-master-key, got %v", err)
	}
}

func TestUserSecretService_Create_DuplicateName(t *testing.T) {
	s := setupSecretSuite(t)
	if _, err := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "x", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("p"), ActorIdentity: "user:x",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "x", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("p2"), ActorIdentity: "user:x",
	})
	if !errors.Is(err, secretmgmt.ErrUserSecretNameTaken) {
		t.Fatalf("expected name taken, got %v", err)
	}
}

func TestUserSecretService_Create_EmptyPlaintext(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "x", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: nil, ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestUserSecretService_Rotate_Happy(t *testing.T) {
	s := setupSecretSuite(t)
	created, _ := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "x", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("v1"), ActorIdentity: "user:x",
	})
	evID, err := s.svc.Rotate(context.Background(), RotateSecretCommand{
		ID: created.ID, NewPlaintext: []byte("v2"), Version: 1, ActorIdentity: "user:x",
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if evID == "" {
		t.Fatal("event id missing")
	}
	// Resolve returns the new plaintext.
	res, _ := s.resolver.Resolve(context.Background(), ResolveRequest{
		SecretName: "x", CallerActor: "worker:W-1",
	})
	if string(res.Plaintext) != "v2" {
		t.Fatalf("plaintext after rotate: %s", res.Plaintext)
	}
}

func TestUserSecretService_Revoke_Happy(t *testing.T) {
	s := setupSecretSuite(t)
	created, _ := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "x", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("v"), ActorIdentity: "user:x",
	})
	evID, err := s.svc.Revoke(context.Background(), RevokeSecretCommand{
		ID: created.ID, Reason: secretmgmt.UserSecretRevokedReasonManual,
		Message: "test", Version: 1, ActorIdentity: "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if evID == "" {
		t.Fatal("event id missing")
	}
	got, _ := s.repo.FindByID(context.Background(), created.ID)
	if got.State() != secretmgmt.UserSecretRevoked {
		t.Fatalf("state: %s", got.State())
	}
}

// =============================================================================
// SecretResolutionService
// =============================================================================

func TestResolver_Happy(t *testing.T) {
	s := setupSecretSuite(t)
	if _, err := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "github-pat", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("ghp_real_value"), ActorIdentity: "user:hayang",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := s.resolver.Resolve(context.Background(), ResolveRequest{
		SecretName: "github-pat", CallerActor: "worker:W-1",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(res.Plaintext) != "ghp_real_value" {
		t.Fatalf("plaintext: %s", res.Plaintext)
	}
	// last_used_at updated.
	got, _ := s.repo.FindByID(context.Background(), res.ID)
	if got.LastUsedAt() == nil {
		t.Fatal("last_used_at should be set")
	}
	// accessed event emitted; payload has NO plaintext.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	found := false
	for _, e := range events {
		if e.Type() == observability.EventType("secretmgmt.user_secret.accessed") {
			found = true
			for _, v := range e.Payload() {
				if str, ok := v.(string); ok && strings.Contains(str, "ghp_real_value") {
					t.Fatalf("plaintext leaked in accessed event")
				}
			}
		}
	}
	if !found {
		t.Fatal("no accessed event")
	}
}

func TestResolver_RevokedRejected(t *testing.T) {
	s := setupSecretSuite(t)
	created, _ := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "x", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("v"), ActorIdentity: "user:x",
	})
	_, _ = s.svc.Revoke(context.Background(), RevokeSecretCommand{
		ID: created.ID, Reason: secretmgmt.UserSecretRevokedReasonManual,
		Message: "test", Version: 1, ActorIdentity: "user:x",
	})
	_, err := s.resolver.Resolve(context.Background(), ResolveRequest{
		SecretName: "x", CallerActor: "worker:W-1",
	})
	if !errors.Is(err, secretmgmt.ErrUserSecretRevoked) {
		t.Fatalf("expected revoked, got %v", err)
	}
	// access_denied event emitted.
	events, _ := s.eventRepo.Find(context.Background(), observability.EventQueryFilter{})
	found := false
	for _, e := range events {
		if e.Type() == observability.EventType("secretmgmt.user_secret.access_denied") {
			found = true
		}
	}
	if !found {
		t.Fatal("no access_denied event")
	}
}

func TestResolver_NotFound(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.resolver.Resolve(context.Background(), ResolveRequest{
		SecretName: "nope", CallerActor: "worker:W-1",
	})
	if !errors.Is(err, secretmgmt.ErrUserSecretNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestResolver_NoMasterKey(t *testing.T) {
	s := setupSecretSuite(t)
	s.resolver.masterKey = nil
	_, err := s.resolver.Resolve(context.Background(), ResolveRequest{
		SecretName: "x", CallerActor: "worker:W-1",
	})
	if !errors.Is(err, secretmgmt.ErrMasterKeyNotLoaded) {
		t.Fatalf("expected no-master-key, got %v", err)
	}
}

func TestResolver_BadActor(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.resolver.Resolve(context.Background(), ResolveRequest{
		SecretName: "x", CallerActor: "bogus:x",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

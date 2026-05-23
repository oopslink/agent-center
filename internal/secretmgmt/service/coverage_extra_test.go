package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// NewUserSecretService nil clock default path
func TestNewUserSecretService_NilClock(t *testing.T) {
	s := setupSecretSuite(t)
	svc := NewUserSecretService(s.db, s.repo, nil, s.sink, nil, s.mk)
	if svc == nil {
		t.Fatal()
	}
	// no panic — that's enough
	_ = clock.SystemClock{}
}

func TestNewSecretResolutionService_NilClock(t *testing.T) {
	s := setupSecretSuite(t)
	rs := NewSecretResolutionService(s.db, s.repo, s.sink, nil, s.mk)
	if rs == nil {
		t.Fatal()
	}
}

func TestRotate_NoMasterKey(t *testing.T) {
	s := setupSecretSuite(t)
	s.svc.masterKey = nil
	_, err := s.svc.Rotate(context.Background(), RotateSecretCommand{
		ID: "x", NewPlaintext: []byte("p"), Version: 1, ActorIdentity: "user:x",
	})
	if !errors.Is(err, secretmgmt.ErrMasterKeyNotLoaded) {
		t.Fatalf("expected no-key, got %v", err)
	}
}

func TestRotate_BadActor(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.svc.Rotate(context.Background(), RotateSecretCommand{
		ID: "x", NewPlaintext: []byte("p"), Version: 1, ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRotate_EmptyPlaintext(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.svc.Rotate(context.Background(), RotateSecretCommand{
		ID: "x", NewPlaintext: nil, Version: 1, ActorIdentity: "user:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRotate_NotFound(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.svc.Rotate(context.Background(), RotateSecretCommand{
		ID: "01H-NOPE", NewPlaintext: []byte("p"), Version: 1, ActorIdentity: "user:x",
	})
	if !errors.Is(err, secretmgmt.ErrUserSecretNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestRevoke_BadActor(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.svc.Revoke(context.Background(), RevokeSecretCommand{
		ID: "x", Reason: secretmgmt.UserSecretRevokedReasonManual,
		Message: "m", Version: 1, ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRevoke_NotFound(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.svc.Revoke(context.Background(), RevokeSecretCommand{
		ID: "01H-NOPE", Reason: secretmgmt.UserSecretRevokedReasonManual,
		Message: "m", Version: 1, ActorIdentity: "user:x",
	})
	if !errors.Is(err, secretmgmt.ErrUserSecretNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestResolve_EmptyName(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.resolver.Resolve(context.Background(), ResolveRequest{
		SecretName: "", CallerActor: "worker:W-1",
	})
	if err == nil {
		t.Fatal()
	}
}

func TestCreate_BadActor(t *testing.T) {
	s := setupSecretSuite(t)
	_, err := s.svc.Create(context.Background(), CreateSecretCommand{
		Name: "x", Kind: secretmgmt.UserSecretKindMCP,
		Plaintext: []byte("p"), ActorIdentity: "bogus:x",
	})
	if err == nil {
		t.Fatal()
	}
}

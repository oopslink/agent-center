package secretmgmt

import (
	"errors"
	"testing"
	"time"
)

func TestNewUserSecret_Valid(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	s, err := NewUserSecret(NewUserSecretInput{
		ID: "01HUS1", Name: "github-pat", Kind: UserSecretKindMCP,
		Ciphertext: []byte("ciphered"), Nonce: []byte("nonce-12byte"),
		CreatedAt: now, CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatalf("NewUserSecret: %v", err)
	}
	if s.State() != UserSecretActive {
		t.Fatalf("state: %s", s.State())
	}
	if s.Version() != 1 {
		t.Fatalf("version: %d", s.Version())
	}
}

func TestNewUserSecret_Validations(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	base := NewUserSecretInput{
		ID: "01H", Name: "n", Kind: UserSecretKindMCP,
		Ciphertext: []byte("c"), Nonce: []byte("n"),
		CreatedAt: now, CreatedBy: "u",
	}
	cases := []struct {
		name  string
		patch func(*NewUserSecretInput)
	}{
		{"empty_id", func(in *NewUserSecretInput) { in.ID = "" }},
		{"empty_name", func(in *NewUserSecretInput) { in.Name = "" }},
		{"bad_kind", func(in *NewUserSecretInput) { in.Kind = "bogus" }},
		{"empty_ciphertext", func(in *NewUserSecretInput) { in.Ciphertext = nil }},
		{"empty_nonce", func(in *NewUserSecretInput) { in.Nonce = nil }},
		{"zero_created_at", func(in *NewUserSecretInput) { in.CreatedAt = time.Time{} }},
		{"empty_created_by", func(in *NewUserSecretInput) { in.CreatedBy = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.patch(&in)
			if _, err := NewUserSecret(in); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestUserSecret_Rotate(t *testing.T) {
	s := freshSecret(t)
	prevVer := s.Version()
	if err := s.Rotate(time.Now(), []byte("new-c"), []byte("new-n")); err != nil {
		t.Fatal(err)
	}
	if string(s.Ciphertext()) != "new-c" || string(s.Nonce()) != "new-n" {
		t.Fatal("rotate did not replace value")
	}
	if s.Version() != prevVer+1 {
		t.Fatalf("version: %d", s.Version())
	}
	if s.RotatedAt() == nil {
		t.Fatal("rotated_at should be set")
	}
}

func TestUserSecret_Rotate_RejectsRevoked(t *testing.T) {
	s := freshSecret(t)
	_ = s.Revoke(time.Now(), "user:x", UserSecretRevokedReasonManual, "test")
	err := s.Rotate(time.Now(), []byte("c"), []byte("n"))
	if !errors.Is(err, ErrUserSecretRevoked) {
		t.Fatalf("expected revoked, got %v", err)
	}
}

func TestUserSecret_Revoke_Happy(t *testing.T) {
	s := freshSecret(t)
	if err := s.Revoke(time.Now(), "user:x", UserSecretRevokedReasonManual, "user requested"); err != nil {
		t.Fatal(err)
	}
	if s.State() != UserSecretRevoked {
		t.Fatalf("state: %s", s.State())
	}
	if s.RevokedBy() != "user:x" {
		t.Fatalf("revoked_by: %s", s.RevokedBy())
	}
}

func TestUserSecret_Revoke_DoubleRejects(t *testing.T) {
	s := freshSecret(t)
	_ = s.Revoke(time.Now(), "user:x", UserSecretRevokedReasonManual, "first")
	err := s.Revoke(time.Now(), "user:x", UserSecretRevokedReasonManual, "second")
	if !errors.Is(err, ErrUserSecretRevoked) {
		t.Fatalf("expected revoked, got %v", err)
	}
}

func TestUserSecret_Revoke_Validations(t *testing.T) {
	s := freshSecret(t)
	if err := s.Revoke(time.Now(), "user:x", "bogus", "msg"); err == nil {
		t.Fatal("bad reason should reject")
	}
	if err := s.Revoke(time.Now(), "user:x", UserSecretRevokedReasonManual, ""); err == nil {
		t.Fatal("empty message should reject")
	}
	if err := s.Revoke(time.Now(), "", UserSecretRevokedReasonManual, "test"); err == nil {
		t.Fatal("empty revoked_by should reject")
	}
}

func TestUserSecret_MarkUsed(t *testing.T) {
	s := freshSecret(t)
	at := time.Now()
	s.MarkUsed(at)
	if s.LastUsedAt() == nil {
		t.Fatal("last_used_at should be set")
	}
}

func TestUserSecretState_TerminalAndValidity(t *testing.T) {
	if !UserSecretActive.IsValid() || !UserSecretRevoked.IsValid() {
		t.Fatal()
	}
	if UserSecretState("bogus").IsValid() {
		t.Fatal()
	}
	if !UserSecretRevoked.IsTerminal() {
		t.Fatal("revoked is terminal")
	}
	if UserSecretActive.IsTerminal() {
		t.Fatal("active not terminal")
	}
}

func TestUserSecretKind_Validity(t *testing.T) {
	for _, k := range []UserSecretKind{UserSecretKindMCP, UserSecretKindCloudCredential, UserSecretKindRepoDeployKey, UserSecretKindOther} {
		if !k.IsValid() {
			t.Fatalf("%s should be valid", k)
		}
	}
	if UserSecretKind("bogus").IsValid() {
		t.Fatal()
	}
}

func freshSecret(t *testing.T) *UserSecret {
	t.Helper()
	s, err := NewUserSecret(NewUserSecretInput{
		ID: "01HUS", Name: "test", Kind: UserSecretKindMCP,
		Ciphertext: []byte("c"), Nonce: []byte("n"),
		CreatedAt: time.Now(), CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

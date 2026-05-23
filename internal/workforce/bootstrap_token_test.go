package workforce

import (
	"errors"
	"testing"
	"time"
)

func TestNewBootstrapToken_Valid(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tok, err := NewBootstrapToken(NewBootstrapTokenInput{
		ID:        "01HFAKE",
		WorkerID:  "W-1",
		ValueHash: HashTokenValue("plain"),
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
		CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatalf("NewBootstrapToken: %v", err)
	}
	if tok.Status() != BootstrapTokenActive {
		t.Fatalf("status: %s", tok.Status())
	}
	if tok.ValueHash() == "plain" {
		t.Fatal("ValueHash should be hashed, not plaintext")
	}
	if !tok.ExpiresAt().After(tok.CreatedAt()) {
		t.Fatal("expires_at must be after created_at")
	}
}

func TestNewBootstrapToken_Validations(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	base := NewBootstrapTokenInput{
		ID:        "01HFAKE",
		WorkerID:  "W-1",
		ValueHash: "hash",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Minute),
		CreatedBy: "user:hayang",
	}
	cases := []struct {
		name  string
		patch func(*NewBootstrapTokenInput)
	}{
		{"empty_id", func(in *NewBootstrapTokenInput) { in.ID = "" }},
		{"empty_value_hash", func(in *NewBootstrapTokenInput) { in.ValueHash = "" }},
		{"zero_created_at", func(in *NewBootstrapTokenInput) { in.CreatedAt = time.Time{} }},
		{"expires_before_created", func(in *NewBootstrapTokenInput) { in.ExpiresAt = now.Add(-time.Second) }},
		{"expires_equal_created", func(in *NewBootstrapTokenInput) { in.ExpiresAt = now }},
		{"empty_created_by", func(in *NewBootstrapTokenInput) { in.CreatedBy = "" }},
		{"invalid_worker_id", func(in *NewBootstrapTokenInput) { in.WorkerID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.patch(&in)
			if _, err := NewBootstrapToken(in); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestBootstrapToken_MarkUsed(t *testing.T) {
	tok := newActiveTokenForTest(t)
	at := time.Now()
	if err := tok.MarkUsed(at); err != nil {
		t.Fatal(err)
	}
	if tok.Status() != BootstrapTokenUsed {
		t.Fatalf("status: %s", tok.Status())
	}
	if tok.UsedAt() == nil {
		t.Fatal("UsedAt should be set")
	}
	// Re-mark must fail.
	if err := tok.MarkUsed(at); !errors.Is(err, ErrBootstrapTokenNotActive) {
		t.Fatalf("expected not-active, got %v", err)
	}
}

func TestBootstrapToken_MarkExpired(t *testing.T) {
	tok := newActiveTokenForTest(t)
	if err := tok.MarkExpired(); err != nil {
		t.Fatal(err)
	}
	if tok.Status() != BootstrapTokenExpired {
		t.Fatalf("status: %s", tok.Status())
	}
	if err := tok.MarkExpired(); !errors.Is(err, ErrBootstrapTokenNotActive) {
		t.Fatalf("expected not-active, got %v", err)
	}
}

func TestBootstrapToken_MarkRevoked(t *testing.T) {
	tok := newActiveTokenForTest(t)
	at := time.Now()
	if err := tok.MarkRevoked(at, BootstrapTokenRevokedReasonManual, "user revoked"); err != nil {
		t.Fatal(err)
	}
	if tok.Status() != BootstrapTokenRevoked {
		t.Fatalf("status: %s", tok.Status())
	}
	if tok.RevokedReason() != BootstrapTokenRevokedReasonManual {
		t.Fatalf("reason: %s", tok.RevokedReason())
	}
	if tok.RevokedMessage() != "user revoked" {
		t.Fatalf("message: %s", tok.RevokedMessage())
	}
}

func TestBootstrapToken_MarkRevoked_RequiresMessage(t *testing.T) {
	tok := newActiveTokenForTest(t)
	if err := tok.MarkRevoked(time.Now(), BootstrapTokenRevokedReasonManual, ""); err == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestBootstrapToken_MarkRevoked_InvalidReason(t *testing.T) {
	tok := newActiveTokenForTest(t)
	if err := tok.MarkRevoked(time.Now(), "bogus", "msg"); err == nil {
		t.Fatal("expected error for invalid reason")
	}
}

func TestBootstrapToken_IsExpiredAt(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tok, _ := NewBootstrapToken(NewBootstrapTokenInput{
		ID: "01HFAKE", WorkerID: "W-1", ValueHash: "h",
		CreatedAt: now, ExpiresAt: now.Add(time.Minute), CreatedBy: "u",
	})
	if tok.IsExpiredAt(now) {
		t.Fatal("not expired at created time")
	}
	if !tok.IsExpiredAt(now.Add(time.Hour)) {
		t.Fatal("expected expired one hour later")
	}
	if !tok.IsExpiredAt(now.Add(time.Minute)) {
		t.Fatal("expected expired exactly at TTL boundary (inclusive)")
	}
}

func TestHashTokenValue_Deterministic(t *testing.T) {
	a := HashTokenValue("hello")
	b := HashTokenValue("hello")
	if a != b {
		t.Fatal("hash must be deterministic")
	}
	if a == "hello" {
		t.Fatal("hash must differ from plaintext")
	}
	if len(a) != 64 { // SHA-256 hex
		t.Fatalf("expected 64-char hex, got %d", len(a))
	}
}

func TestBootstrapTokenStatus_Validity(t *testing.T) {
	for _, s := range []BootstrapTokenStatus{BootstrapTokenActive, BootstrapTokenUsed, BootstrapTokenExpired, BootstrapTokenRevoked} {
		if !s.IsValid() {
			t.Fatalf("%s should be valid", s)
		}
	}
	if BootstrapTokenStatus("bogus").IsValid() {
		t.Fatal("bogus should be invalid")
	}
}

func TestBootstrapTokenStatus_IsTerminal(t *testing.T) {
	if BootstrapTokenActive.IsTerminal() {
		t.Fatal("active not terminal")
	}
	for _, s := range []BootstrapTokenStatus{BootstrapTokenUsed, BootstrapTokenExpired, BootstrapTokenRevoked} {
		if !s.IsTerminal() {
			t.Fatalf("%s should be terminal", s)
		}
	}
}

func newActiveTokenForTest(t *testing.T) *BootstrapToken {
	t.Helper()
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tok, err := NewBootstrapToken(NewBootstrapTokenInput{
		ID: "01HTEST", WorkerID: "W-1", ValueHash: HashTokenValue("pw"),
		CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute), CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

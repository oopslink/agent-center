package admintoken

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Plaintext helpers
// =============================================================================

func TestGeneratePlaintext_Shape(t *testing.T) {
	tok, err := GeneratePlaintext()
	if err != nil {
		t.Fatalf("GeneratePlaintext: %v", err)
	}
	if !strings.HasPrefix(tok, PlaintextPrefix) {
		t.Fatalf("missing prefix: %s", tok)
	}
	body := strings.TrimPrefix(tok, PlaintextPrefix)
	// 32 bytes base64url no-padding → 43 chars.
	if len(body) < 40 || len(body) > 50 {
		t.Fatalf("unexpected body length %d for token %s", len(body), tok)
	}
	// Two generations must differ (cryptographic randomness).
	tok2, err := GeneratePlaintext()
	if err != nil {
		t.Fatal(err)
	}
	if tok == tok2 {
		t.Fatal("two generations produced identical plaintext")
	}
}

func TestHashPlaintext_StableAndUnique(t *testing.T) {
	h1 := HashPlaintext("acat_alpha")
	h2 := HashPlaintext("acat_alpha")
	if !bytes.Equal(h1, h2) {
		t.Fatal("hash is not stable across calls")
	}
	h3 := HashPlaintext("acat_beta")
	if bytes.Equal(h1, h3) {
		t.Fatal("different inputs produced equal hashes")
	}
	if len(h1) != 32 {
		t.Fatalf("hash should be 32 bytes; got %d", len(h1))
	}
}

// =============================================================================
// ParseBearer
// =============================================================================

func TestParseBearer(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		want      string
		wantErrIs error
	}{
		{"missing", "", "", ErrTokenMissingBearer},
		// "Bearer " with trailing space → TrimSpace strips it → looks like
		// the bare value "Bearer" which lacks the acat_ prefix.
		{"just bearer trailing space", "Bearer ", "", ErrTokenInvalidFormat},
		{"invalid format - no prefix", "Bearer xyz", "", ErrTokenInvalidFormat},
		{"invalid format - bare no prefix", "xyz", "", ErrTokenInvalidFormat},
		{"valid bearer form", "Bearer acat_xyz", "acat_xyz", nil},
		{"valid bare form", "acat_xyz", "acat_xyz", nil},
		{"lowercased bearer", "bearer acat_xyz", "acat_xyz", nil},
		{"whitespace tolerance", "  Bearer   acat_xyz  ", "acat_xyz", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseBearer(tc.header)
			if tc.wantErrIs != nil {
				if !errors.Is(err, tc.wantErrIs) {
					t.Fatalf("want err=%v got=%v", tc.wantErrIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %q got %q", tc.want, got)
			}
		})
	}
}

// =============================================================================
// AR construction + invariants
// =============================================================================

func validHash() []byte { return HashPlaintext("acat_xx") }

func TestNew_HappyPath(t *testing.T) {
	tok, err := New(NewAdminTokenInput{
		ID:        "T-1",
		Owner:     "cli:hayang",
		Scopes:    []Scope{"task:*", "dispatch:pull"},
		ValueHash: validHash(),
		CreatedAt: time.Now(),
		CreatedBy: "system",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if tok.ID() != "T-1" || tok.Owner() != "cli:hayang" {
		t.Fatalf("identity mismatch: %+v", tok)
	}
	if tok.Version() != 1 {
		t.Fatalf("version: %d", tok.Version())
	}
	if tok.IsRevoked() {
		t.Fatal("new token should not be revoked")
	}
}

func TestNew_RejectsEmptyOwner(t *testing.T) {
	_, err := New(NewAdminTokenInput{
		Owner:     "  ",
		Scopes:    []Scope{"*"},
		ValueHash: validHash(),
	})
	if !errors.Is(err, ErrTokenOwnerRequired) {
		t.Fatalf("want ErrTokenOwnerRequired, got %v", err)
	}
}

func TestNew_RejectsEmptyScopes(t *testing.T) {
	_, err := New(NewAdminTokenInput{
		Owner:     "cli:x",
		Scopes:    []Scope{},
		ValueHash: validHash(),
	})
	if !errors.Is(err, ErrTokenScopesRequired) {
		t.Fatalf("want ErrTokenScopesRequired, got %v", err)
	}
}

func TestNew_RejectsAllBlankScopes(t *testing.T) {
	_, err := New(NewAdminTokenInput{
		Owner:     "cli:x",
		Scopes:    []Scope{"  ", ""},
		ValueHash: validHash(),
	})
	if !errors.Is(err, ErrTokenScopesRequired) {
		t.Fatalf("want ErrTokenScopesRequired after trim, got %v", err)
	}
}

func TestNew_DedupesScopes(t *testing.T) {
	tok, err := New(NewAdminTokenInput{
		Owner:     "cli:x",
		Scopes:    []Scope{"a", "a", "b"},
		ValueHash: validHash(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tok.Scopes()) != 2 {
		t.Fatalf("expected 2 deduped scopes, got %v", tok.Scopes())
	}
}

func TestNew_RejectsBadHashSize(t *testing.T) {
	_, err := New(NewAdminTokenInput{
		Owner:     "cli:x",
		Scopes:    []Scope{"*"},
		ValueHash: []byte{1, 2, 3},
	})
	if err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("want 32-byte error, got %v", err)
	}
}

// =============================================================================
// HasScope
// =============================================================================

func TestHasScope(t *testing.T) {
	tok, _ := New(NewAdminTokenInput{
		Owner:     "cli:x",
		Scopes:    []Scope{"task:*", "dispatch:pull"},
		ValueHash: validHash(),
	})
	if !tok.HasScope("task:*") {
		t.Fatal("expected to have own scope")
	}
	if !tok.HasScope("dispatch:pull") {
		t.Fatal("expected to have own scope")
	}
	if tok.HasScope("admin:token") {
		t.Fatal("unexpected scope")
	}
}

func TestHasScope_SuperuserWildcard(t *testing.T) {
	tok, _ := New(NewAdminTokenInput{
		Owner:     "system:bootstrap",
		Scopes:    []Scope{"*"},
		ValueHash: validHash(),
	})
	if !tok.HasScope("admin:token") {
		t.Fatal("* should grant any scope")
	}
	if !tok.HasScope("anything") {
		t.Fatal("* should grant any scope")
	}
}

// =============================================================================
// Revoke
// =============================================================================

func TestRevoke_HappyAndDoubleRevoke(t *testing.T) {
	tok, _ := New(NewAdminTokenInput{
		Owner:     "cli:x",
		Scopes:    []Scope{"*"},
		ValueHash: validHash(),
	})
	now := time.Now()
	if err := tok.Revoke(now, "system", "rotated"); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if !tok.IsRevoked() {
		t.Fatal("should be revoked after Revoke")
	}
	if tok.Version() != 2 {
		t.Fatalf("version bump expected; got %d", tok.Version())
	}
	if err := tok.Revoke(now, "system", "again"); !errors.Is(err, ErrTokenRevoked) {
		t.Fatalf("double revoke want ErrTokenRevoked, got %v", err)
	}
}

func TestMarkUsed_UpdatesLastUsedAt(t *testing.T) {
	tok, _ := New(NewAdminTokenInput{
		Owner:     "cli:x",
		Scopes:    []Scope{"*"},
		ValueHash: validHash(),
	})
	if tok.LastUsedAt() != nil {
		t.Fatal("new token should have nil LastUsedAt")
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok.MarkUsed(now)
	got := tok.LastUsedAt()
	if got == nil || !got.Equal(now) {
		t.Fatalf("LastUsedAt mismatch: %v", got)
	}
}

// =============================================================================
// Rehydrate
// =============================================================================

func TestRehydrate_PreservesAllFields(t *testing.T) {
	rev := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	used := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)
	tok := Rehydrate(RehydrateInput{
		ID:            "T-9",
		Owner:         "worker:w1",
		Scopes:        []Scope{"dispatch:pull"},
		ValueHash:     validHash(),
		CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:     "system",
		RevokedAt:     &rev,
		RevokedBy:     "admin",
		RevokedReason: "rotated",
		LastUsedAt:    &used,
		Version:       7,
	})
	if !tok.IsRevoked() || tok.Version() != 7 || tok.RevokedReason() != "rotated" {
		t.Fatalf("rehydrate dropped fields: %+v", tok)
	}
}

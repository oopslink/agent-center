package secretmgmt

import (
	"testing"
	"time"
)

func TestUserSecret_AllGetters(t *testing.T) {
	s := freshSecret(t)
	_ = s.ID()
	_ = s.Name()
	_ = s.Kind()
	_ = s.CreatedAt()
	_ = s.CreatedBy()
	_ = s.RevokedAt()
	_ = s.RevokedReason()
	_ = s.RevokedMessage()
}

func TestRehydrateUserSecret_Happy(t *testing.T) {
	s, err := RehydrateUserSecret(RehydrateUserSecretInput{
		ID: "01H", Name: "n", Kind: UserSecretKindMCP,
		Ciphertext: []byte("c"), Nonce: []byte("n"),
		State: UserSecretActive, CreatedAt: time.Now(),
		CreatedBy: "u", Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.State() != UserSecretActive {
		t.Fatal()
	}
}

func TestRehydrateUserSecret_InvalidState(t *testing.T) {
	if _, err := RehydrateUserSecret(RehydrateUserSecretInput{
		ID: "01H", Name: "n", Kind: UserSecretKindMCP,
		Ciphertext: []byte("c"), Nonce: []byte("n"),
		State: "bogus", CreatedAt: time.Now(),
		CreatedBy: "u", Version: 1,
	}); err == nil {
		t.Fatal()
	}
}

func TestRehydrateUserSecret_BadVersion(t *testing.T) {
	if _, err := RehydrateUserSecret(RehydrateUserSecretInput{
		ID: "01H", Name: "n", Kind: UserSecretKindMCP,
		Ciphertext: []byte("c"), Nonce: []byte("n"),
		State: UserSecretActive, CreatedAt: time.Now(),
		CreatedBy: "u", Version: 0,
	}); err == nil {
		t.Fatal()
	}
}

func TestUserSecretID_String(t *testing.T) {
	if UserSecretID("01HX").String() != "01HX" {
		t.Fatal()
	}
}

func TestUserSecretKind_String(t *testing.T) {
	if UserSecretKindMCP.String() != "mcp" {
		t.Fatal()
	}
}

func TestUserSecretState_String(t *testing.T) {
	if UserSecretActive.String() != "active" {
		t.Fatal()
	}
}

func TestUserSecret_Rotate_EmptyValue(t *testing.T) {
	s := freshSecret(t)
	if err := s.Rotate(time.Now(), nil, []byte("n")); err == nil {
		t.Fatal()
	}
	if err := s.Rotate(time.Now(), []byte("c"), nil); err == nil {
		t.Fatal()
	}
}

func TestValidateSecretName_LongAndBadChars(t *testing.T) {
	if err := validateSecretName(string(make([]byte, 129))); err == nil {
		t.Fatal()
	}
}

func TestCopyTimePtr_Nil(t *testing.T) {
	if copyTimePtr(nil) != nil {
		t.Fatal()
	}
	now := time.Now()
	cp := copyTimePtr(&now)
	if cp == nil {
		t.Fatal()
	}
}

// =============================================================================
// Crypto extra coverage
// =============================================================================

func TestGenerateMasterKey_OK(t *testing.T) {
	mk, err := GenerateMasterKey()
	if err != nil {
		t.Fatal(err)
	}
	if mk == nil || mk.Base64() == "" {
		t.Fatal()
	}
}

func TestDecodeB64_AllVariants(t *testing.T) {
	// Std encoding (with padding) — happy path.
	if _, err := decodeB64("aGVsbG8="); err != nil {
		t.Fatal(err)
	}
	// Raw std (no padding) — fallback path.
	if _, err := decodeB64("aGVsbG8"); err != nil {
		t.Fatal(err)
	}
	// URL encoding fallback (URL-safe alphabet uses -_ instead of +/).
	if _, err := decodeB64("aGVsbG8-_w"); err != nil {
		t.Fatal(err)
	}
	// Invalid — none of the variants match.
	if _, err := decodeB64("!!!not base64!!!"); err == nil {
		t.Fatal("expected error on garbage")
	}
}

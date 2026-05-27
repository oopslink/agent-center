package identity

import (
	"testing"
)

func TestHashPasscode_VerifyPasscode(t *testing.T) {
	hash, err := HashPasscode("123456")
	if err != nil {
		t.Fatalf("HashPasscode: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}

	if !VerifyPasscode(hash, "123456") {
		t.Error("VerifyPasscode should return true for correct passcode")
	}
	if VerifyPasscode(hash, "000000") {
		t.Error("VerifyPasscode should return false for wrong passcode")
	}
}

func TestHashPasscode_DifferentSalts(t *testing.T) {
	h1, _ := HashPasscode("123456")
	h2, _ := HashPasscode("123456")
	if h1 == h2 {
		t.Error("expected different hashes for same input (different salts)")
	}
	if !VerifyPasscode(h1, "123456") || !VerifyPasscode(h2, "123456") {
		t.Error("both hashes should verify correctly")
	}
}

func TestValidatePasscodePlain(t *testing.T) {
	valid := []string{"000000", "123456", "999999"}
	for _, p := range valid {
		if err := ValidatePasscodePlain(p); err != nil {
			t.Errorf("expected valid passcode %q, got error: %v", p, err)
		}
	}

	invalid := []string{"", "12345", "1234567", "abcdef", "12345a"}
	for _, p := range invalid {
		if err := ValidatePasscodePlain(p); err == nil {
			t.Errorf("expected invalid passcode %q, got no error", p)
		}
	}
}

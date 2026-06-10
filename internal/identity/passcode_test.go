package identity

import (
	"strings"
	"testing"
)

func TestHashPasscode_VerifyPasscode(t *testing.T) {
	hash, err := HashPasscode("Passw0rd!")
	if err != nil {
		t.Fatalf("HashPasscode: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty hash")
	}

	if !VerifyPasscode(hash, "Passw0rd!") {
		t.Error("VerifyPasscode should return true for correct passcode")
	}
	if VerifyPasscode(hash, "Wrongpass1!") {
		t.Error("VerifyPasscode should return false for wrong passcode")
	}
}

func TestHashPasscode_DifferentSalts(t *testing.T) {
	h1, _ := HashPasscode("Passw0rd!")
	h2, _ := HashPasscode("Passw0rd!")
	if h1 == h2 {
		t.Error("expected different hashes for same input (different salts)")
	}
	if !VerifyPasscode(h1, "Passw0rd!") || !VerifyPasscode(h2, "Passw0rd!") {
		t.Error("both hashes should verify correctly")
	}
}

func TestValidatePasscodePlain(t *testing.T) {
	longOK := strings.Repeat("a", 126) + "1!" // 128 runes, all three classes
	valid := []string{
		"abc12!@",     // letter + digit + symbol
		"Passw0rd!",   // mixed
		"a1!bcd",      // 6-char minimal valid
		"密码1!ab",      // unicode letters count via unicode.IsLetter
		longOK,        // exactly 128 chars
	}
	for _, p := range valid {
		if err := ValidatePasscodePlain(p); err != nil {
			t.Errorf("expected valid passcode %q, got error: %v", p, err)
		}
	}

	tooLong := strings.Repeat("a", 127) + "1!" // 129 runes
	invalid := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"too short", "abc"},
		{"all digits, no letter or symbol", "123456"},
		{"letters only, no digit or symbol", "abcdef"},
		{"letters+digits, no symbol", "abc123"},
		{"letter+digit, no symbol (7 chars)", "abcdef1"},
		{"too long", tooLong},
	}
	for _, tc := range invalid {
		if err := ValidatePasscodePlain(tc.in); err == nil {
			t.Errorf("expected invalid passcode (%s) %q, got no error", tc.name, tc.in)
		}
	}
}

// TestGenerateTempPasscode_AlwaysValid verifies the generator can never emit a
// passcode that fails ValidatePasscodePlain (verify-not-trust the boundary).
func TestGenerateTempPasscode_AlwaysValid(t *testing.T) {
	const n = 50
	for i := 0; i < n; i++ {
		p, err := generateTempPasscode()
		if err != nil {
			t.Fatalf("generateTempPasscode: %v", err)
		}
		if err := ValidatePasscodePlain(p); err != nil {
			t.Errorf("generated temp passcode %q failed validation: %v", p, err)
		}
	}
}

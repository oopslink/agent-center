package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// passcode complexity bounds (length measured in runes).
const (
	passcodeMinLen = 6
	passcodeMaxLen = 128
)

// ValidatePasscodePlain enforces the passcode complexity rules: at least 6 and
// at most 128 characters, with at least one letter, one digit, and one symbol
// (a real punctuation/symbol rune — unicode.IsPunct||IsSymbol). v2.9 #290: symbol
// EXCLUDES whitespace and control chars (PD lock) so "Abc123 " with a trailing
// space does NOT satisfy the symbol requirement (trailing-space weak-passcode leak
// + easily lost on input).
func ValidatePasscodePlain(plain string) error {
	n := utf8.RuneCountInString(plain)
	if n < passcodeMinLen {
		return errors.New("passcode: must be at least 6 characters")
	}
	if n > passcodeMaxLen {
		return errors.New("passcode: must be at most 128 characters")
	}
	var hasLetter, hasDigit, hasSymbol bool
	for _, r := range plain {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}
	if !hasLetter {
		return errors.New("passcode: must contain a letter")
	}
	if !hasDigit {
		return errors.New("passcode: must contain a digit")
	}
	if !hasSymbol {
		return errors.New("passcode: must contain a symbol")
	}
	return nil
}

// argon2 parameters per OWASP cheat sheet (v2.6-design § 6.1).
const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // 64 MiB
	argon2Threads = 4
	argon2KeyLen  = 32
)

// HashPasscode hashes a plaintext passcode with argon2id.
// Format: "argon2id$t=3,m=65536,p=4$<base64salt>$<base64hash>"
func HashPasscode(plain string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("passcode: generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plain), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	encoded := fmt.Sprintf("argon2id$t=%d,m=%d,p=%d$%s$%s",
		argon2Time, argon2Memory, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
	return encoded, nil
}

// VerifyPasscode returns true if plain matches the stored hash.
func VerifyPasscode(storedHash, plain string) bool {
	t, m, p, salt, hash, err := decodeHash(storedHash)
	if err != nil {
		return false
	}
	candidate := argon2.IDKey([]byte(plain), salt, t, m, p, uint32(len(hash)))
	return subtle.ConstantTimeCompare(hash, candidate) == 1
}

func decodeHash(encoded string) (t, m uint32, p uint8, salt, hash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "argon2id" {
		err = errors.New("passcode: invalid hash format")
		return
	}
	// Parse params: "t=3,m=65536,p=4"
	for _, kv := range strings.Split(parts[1], ",") {
		kv = strings.TrimSpace(kv)
		eqIdx := strings.Index(kv, "=")
		if eqIdx < 0 {
			continue
		}
		key, val := kv[:eqIdx], kv[eqIdx+1:]
		n, parseErr := strconv.ParseUint(val, 10, 32)
		if parseErr != nil {
			err = fmt.Errorf("passcode: parse param %s: %w", key, parseErr)
			return
		}
		switch key {
		case "t":
			t = uint32(n)
		case "m":
			m = uint32(n)
		case "p":
			p = uint8(n)
		}
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return
	}
	hash, err = base64.RawStdEncoding.DecodeString(parts[3])
	return
}

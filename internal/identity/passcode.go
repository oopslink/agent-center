package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// passcode format: 6-digit numeric string.
var passcodeRegex = regexp.MustCompile(`^\d{6}$`)

// ValidatePasscodePlain checks the 6-digit format constraint.
func ValidatePasscodePlain(plain string) error {
	if !passcodeRegex.MatchString(plain) {
		return errors.New("passcode: must be exactly 6 digits")
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

package secretmgmt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// MasterKeySize is AES-256 key size in bytes.
const MasterKeySize = 32

// MasterKey is an in-memory AES-256 key. Construct via LoadMasterKey or
// NewMasterKey (testing only).
type MasterKey struct {
	bytes []byte // length == MasterKeySize
}

// NewMasterKey returns a MasterKey from supplied bytes. Bytes are copied.
// Returns ErrMasterKeyInvalidSize if not exactly 32 bytes.
func NewMasterKey(key []byte) (*MasterKey, error) {
	if len(key) != MasterKeySize {
		return nil, ErrMasterKeyInvalidSize
	}
	cp := make([]byte, len(key))
	copy(cp, key)
	return &MasterKey{bytes: cp}, nil
}

// GenerateMasterKey returns a freshly-randomized MasterKey. For testing /
// initial provisioning.
func GenerateMasterKey() (*MasterKey, error) {
	buf := make([]byte, MasterKeySize)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, fmt.Errorf("secretmgmt: generate master key: %w", err)
	}
	return &MasterKey{bytes: buf}, nil
}

// LoadMasterKey reads the file at path and parses its first non-empty line
// as base64. Enforces mode 0600 unless skipPermsCheck=true (for tests).
func LoadMasterKey(path string, skipPermsCheck bool) (*MasterKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrMasterKeyFileMissing, path)
		}
		return nil, err
	}
	if !skipPermsCheck {
		// Require mode 0600 — owner rw only.
		mode := info.Mode().Perm()
		if mode != 0o600 {
			return nil, fmt.Errorf("%w (got %o; chmod 0600)", ErrMasterKeyFileBadPerms, mode)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Take first non-empty line; trim whitespace.
	var b64 string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			b64 = line
			break
		}
	}
	if b64 == "" {
		return nil, errors.New("secretmgmt: master key file is empty")
	}
	decoded, err := decodeB64(b64)
	if err != nil {
		return nil, fmt.Errorf("secretmgmt: decode master key: %w", err)
	}
	return NewMasterKey(decoded)
}

func decodeB64(s string) ([]byte, error) {
	// Try std then URL encoding.
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(s)
}

// Base64 returns the master key as base64. For tests / initial provisioning
// CLI output ONLY.
func (k *MasterKey) Base64() string {
	return base64.StdEncoding.EncodeToString(k.bytes)
}

// Bytes returns a copy of the raw key material. Used by services that need
// the signing key directly (e.g. JWT HS256 in the Identity BC auth services).
func (k *MasterKey) Bytes() []byte {
	cp := make([]byte, len(k.bytes))
	copy(cp, k.bytes)
	return cp
}

// Encrypt encrypts plaintext with AES-GCM. Returns (ciphertext, nonce).
// Nonce is fresh per call (12-byte GCM standard).
func (k *MasterKey) Encrypt(plaintext []byte) (ciphertext, nonce []byte, err error) {
	if k == nil || len(k.bytes) != MasterKeySize {
		return nil, nil, ErrMasterKeyNotLoaded
	}
	block, err := aes.NewCipher(k.bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("secretmgmt: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("secretmgmt: gcm: %w", err)
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("secretmgmt: gcm nonce: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Decrypt reverses Encrypt. Returns plaintext or error (auth tag mismatch
// → error, never silent corruption).
func (k *MasterKey) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	if k == nil || len(k.bytes) != MasterKeySize {
		return nil, ErrMasterKeyNotLoaded
	}
	block, err := aes.NewCipher(k.bytes)
	if err != nil {
		return nil, fmt.Errorf("secretmgmt: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretmgmt: gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secretmgmt: gcm open: %w", err)
	}
	return plaintext, nil
}

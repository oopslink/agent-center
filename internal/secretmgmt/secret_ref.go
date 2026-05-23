package secretmgmt

import (
	"errors"
	"fmt"
	"strings"
)

// SecretRef is the reference syntax used inside mcp_config and other
// secret-bearing config fields. Format: "secret:<name>" (per ADR-0026 § 5).
//
// v2 supports only the centralised `secret:` scheme; future schemes
// (env: / file: / vault:) can extend by adding new constructors.
type SecretRef struct {
	scheme string
	name   string
}

// SecretRefScheme is the recognised scheme prefix.
const SecretRefScheme = "secret"

// ErrInvalidSecretRef is returned when ParseSecretRef cannot parse the input.
var ErrInvalidSecretRef = errors.New("secretmgmt: invalid SecretRef syntax (expected 'secret:<name>')")

// NewSecretRef constructs a SecretRef for the given name. Name is validated
// the same way as UserSecret.Name.
func NewSecretRef(name string) (SecretRef, error) {
	if err := validateSecretName(name); err != nil {
		return SecretRef{}, err
	}
	return SecretRef{scheme: SecretRefScheme, name: name}, nil
}

// ParseSecretRef parses a "secret:<name>" string. Returns ErrInvalidSecretRef
// on syntax mismatch.
func ParseSecretRef(s string) (SecretRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return SecretRef{}, ErrInvalidSecretRef
	}
	idx := strings.IndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return SecretRef{}, fmt.Errorf("%w: %q", ErrInvalidSecretRef, s)
	}
	scheme := s[:idx]
	name := s[idx+1:]
	if scheme != SecretRefScheme {
		return SecretRef{}, fmt.Errorf("%w: unsupported scheme %q (only %q in v2)", ErrInvalidSecretRef, scheme, SecretRefScheme)
	}
	if err := validateSecretName(name); err != nil {
		return SecretRef{}, fmt.Errorf("%w: %v", ErrInvalidSecretRef, err)
	}
	return SecretRef{scheme: scheme, name: name}, nil
}

// IsSecretRefValue reports whether the given string parses as a SecretRef.
// Used by config-walker code (mcp_config integration in P9) to identify which
// JSON values to substitute.
func IsSecretRefValue(s string) bool {
	_, err := ParseSecretRef(s)
	return err == nil
}

// Scheme returns the scheme part (always "secret" in v2).
func (r SecretRef) Scheme() string { return r.scheme }

// Name returns the secret name part.
func (r SecretRef) Name() string { return r.name }

// String returns the canonical "secret:<name>" form.
func (r SecretRef) String() string {
	if r.scheme == "" {
		return ""
	}
	return r.scheme + ":" + r.name
}

// IsZero reports whether the ref is the zero value.
func (r SecretRef) IsZero() bool { return r.scheme == "" }

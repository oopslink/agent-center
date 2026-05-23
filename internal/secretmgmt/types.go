// Package secretmgmt is BC8 SecretManagement (per ADR-0026):
// user-supplied secrets (MCP env vars, cloud creds, repo deploy keys) with
// AES-GCM encrypted-at-rest storage and just-in-time decryption by worker
// daemons.
//
// Master key never lives in the DB; it's loaded from a 0600 file at startup
// (configured via secret_management.master_key_file).
package secretmgmt

import "errors"

// UserSecretID is the typed ULID PK.
type UserSecretID string

// String returns the underlying value.
func (id UserSecretID) String() string { return string(id) }

// UserSecretKind is the open-set enum (per ADR-0026 § 2).
type UserSecretKind string

const (
	UserSecretKindMCP             UserSecretKind = "mcp"
	UserSecretKindCloudCredential UserSecretKind = "cloud_credential"
	UserSecretKindRepoDeployKey   UserSecretKind = "repo_deploy_key"
	UserSecretKindOther           UserSecretKind = "other"
)

// IsValid reports enum membership.
func (k UserSecretKind) IsValid() bool {
	switch k {
	case UserSecretKindMCP, UserSecretKindCloudCredential, UserSecretKindRepoDeployKey, UserSecretKindOther:
		return true
	}
	return false
}

// String returns the value.
func (k UserSecretKind) String() string { return string(k) }

// UserSecretState — 2-state lifecycle (active → revoked; revoked terminal).
type UserSecretState string

const (
	UserSecretActive  UserSecretState = "active"
	UserSecretRevoked UserSecretState = "revoked"
)

// IsValid reports enum membership.
func (s UserSecretState) IsValid() bool {
	switch s {
	case UserSecretActive, UserSecretRevoked:
		return true
	}
	return false
}

// IsTerminal returns true for revoked state.
func (s UserSecretState) IsTerminal() bool { return s == UserSecretRevoked }

// String returns the value.
func (s UserSecretState) String() string { return string(s) }

// UserSecretRevokedReason (closed enum per conv § 16 reason+message).
type UserSecretRevokedReason string

const (
	UserSecretRevokedReasonManual    UserSecretRevokedReason = "manual"
	UserSecretRevokedReasonRotated   UserSecretRevokedReason = "rotated"
	UserSecretRevokedReasonCompromise UserSecretRevokedReason = "compromise"
)

// IsValid reports enum membership.
func (r UserSecretRevokedReason) IsValid() bool {
	switch r {
	case UserSecretRevokedReasonManual, UserSecretRevokedReasonRotated, UserSecretRevokedReasonCompromise:
		return true
	}
	return false
}

// String returns the value.
func (r UserSecretRevokedReason) String() string { return string(r) }

// SecretManagement BC sentinel errors.
var (
	ErrUserSecretNotFound      = errors.New("secretmgmt: user secret not found")
	ErrUserSecretAlreadyExists = errors.New("secretmgmt: user secret id already exists")
	ErrUserSecretNameTaken     = errors.New("secretmgmt: user secret name already taken")
	ErrUserSecretVersionConflict = errors.New("secretmgmt: user secret version conflict (optimistic lock)")
	ErrUserSecretRevoked       = errors.New("secretmgmt: user secret is revoked (terminal)")
	ErrUserSecretInvalidKind   = errors.New("secretmgmt: invalid user secret kind")
	ErrUserSecretInvalidState  = errors.New("secretmgmt: invalid user secret state")

	ErrMasterKeyNotLoaded    = errors.New("secretmgmt: master key not loaded (call LoadMasterKey first)")
	ErrMasterKeyInvalidSize  = errors.New("secretmgmt: master key must be 32 bytes after base64 decode")
	ErrMasterKeyFileMissing  = errors.New("secretmgmt: master key file not found")
	ErrMasterKeyFileBadPerms = errors.New("secretmgmt: master key file must have 0600 perms")
)

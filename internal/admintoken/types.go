// Package admintoken is BC9 AdminToken — bearer tokens that gate every
// call to the admin endpoint (per v2.3-3a task #28).
//
// Tokens are owned by a principal (worker / cli / agent / system),
// carry a set of scopes, and can be revoked. Server stores only the
// sha256 hash of the value; plaintext is shown to the operator once
// at creation (same lifecycle as UserSecret, per ADR-0026).
//
// Bearer format: `acat_<32-char-base64url>` — 32 bytes of entropy
// (~43 chars body) preceded by an `acat_` prefix so logs / grep can
// recognise stray plaintext immediately.
package admintoken

import (
	"errors"
)

// TokenID is the typed ULID PK.
type TokenID string

// String returns the underlying value.
func (id TokenID) String() string { return string(id) }

// Owner is the typed principal string. Convention: `<kind>:<id>` where
// kind is one of `worker`, `cli`, `agent`, `system`. The middleware
// uses Owner verbatim as the audit actor.
type Owner string

// String returns the underlying value.
func (o Owner) String() string { return string(o) }

// Scope is a free-form permission tag. Stable initial set:
//   - `*` — superuser, matches any required scope
//   - `secret:resolve` — call /admin/secret/user-secret/resolve
//   - `blob:put` — call /admin/blob/put
//   - `dispatch:pull` — drain dispatch queue (worker daemons)
//   - `task:*` — full task CLI surface
//   - `admin:token` — create/list/revoke admin tokens (bootstrap)
//
// We use string instead of an enum to keep the persistence shape stable
// as new scopes land in later phases.
type Scope string

// String returns the underlying value.
func (s Scope) String() string { return string(s) }

// AdminToken BC sentinel errors.
var (
	ErrTokenNotFound        = errors.New("admintoken: not found")
	ErrTokenAlreadyExists   = errors.New("admintoken: id already exists")
	ErrTokenRevoked         = errors.New("admintoken: revoked (terminal)")
	ErrTokenOwnerRequired   = errors.New("admintoken: owner required")
	ErrTokenScopesRequired  = errors.New("admintoken: at least one scope required")
	ErrTokenInvalidFormat   = errors.New("admintoken: invalid plaintext format (expect acat_<value>)")
	ErrTokenMissingBearer   = errors.New("admintoken: request missing Authorization bearer")
	ErrTokenScopeForbidden  = errors.New("admintoken: token lacks required scope")
	ErrTokenVersionConflict = errors.New("admintoken: version conflict (optimistic lock)")
)

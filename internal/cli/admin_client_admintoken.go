// Package cli — admin_client_admintoken.go: Client methods for the
// AdminToken management surface (v2.3-3a task #28). Mirrors
// internal/admin/api/admintoken.go 1:1.
package cli

import "context"

// =============================================================================
// DTOs — JSON shape returned by admin/api/admintoken.go::admintokenMap.
// NEVER includes value_hash or plaintext (list/show).
// =============================================================================

// AdminTokenDTO mirrors the JSON envelope returned by list/show. It
// intentionally omits plaintext + value_hash per ADR-aligned policy.
type AdminTokenDTO struct {
	ID            string   `json:"id"`
	Owner         string   `json:"owner"`
	Scopes        []string `json:"scopes"`
	CreatedAt     string   `json:"created_at"`
	CreatedBy     string   `json:"created_by"`
	Version       int      `json:"version"`
	RevokedAt     string   `json:"revoked_at,omitempty"`
	RevokedBy     string   `json:"revoked_by,omitempty"`
	RevokedReason string   `json:"revoked_reason,omitempty"`
	LastUsedAt    string   `json:"last_used_at,omitempty"`
}

// =============================================================================
// Request payloads — match admin/api request structs.
// =============================================================================

// AdminTokenCreateRequest is the body for /admin/admintoken/create.
type AdminTokenCreateRequest struct {
	Owner     string   `json:"owner"`
	Scopes    []string `json:"scopes"`
	CreatedBy string   `json:"created_by"`
}

// AdminTokenCreateResponse is the success body — id + plaintext. The
// plaintext is the operator's only chance to see the bearer; the server
// never echoes it again.
type AdminTokenCreateResponse struct {
	ID        string `json:"id"`
	Plaintext string `json:"plaintext"`
}

// AdminTokenRevokeRequest is the body for /admin/admintoken/revoke.
type AdminTokenRevokeRequest struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// =============================================================================
// Client methods
// =============================================================================

// AdminTokenCreate POSTs /admin/admintoken/create. The caller must hold
// a bearer with `admin:token` scope.
func (c *Client) AdminTokenCreate(ctx context.Context, req AdminTokenCreateRequest) (AdminTokenCreateResponse, error) {
	var res AdminTokenCreateResponse
	err := c.postJSON(ctx, "/admin/admintoken/create", req, &res)
	return res, err
}

// AdminTokenList GETs /admin/admintoken/list.
func (c *Client) AdminTokenList(ctx context.Context) ([]AdminTokenDTO, error) {
	var out []AdminTokenDTO
	err := c.getJSON(ctx, "/admin/admintoken/list", &out)
	return out, err
}

// AdminTokenRevoke POSTs /admin/admintoken/revoke.
func (c *Client) AdminTokenRevoke(ctx context.Context, req AdminTokenRevokeRequest) error {
	return c.postJSON(ctx, "/admin/admintoken/revoke", req, nil)
}

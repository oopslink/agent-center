// Package cli — admin_client_identity.go: Client methods for the
// Identity BC admin surface (find / register). Mirrors
// internal/admin/api/identity.go 1:1.
package cli

import "context"

// =============================================================================
// DTOs — JSON shape returned by admin/api/identity.go::identityMap.
// =============================================================================

// IdentityDTO mirrors admin api identityMap.
type IdentityDTO struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at"`
}

// =============================================================================
// Request payloads.
// =============================================================================

// IdentityRegisterRequest mirrors api identityRegisterReq.
type IdentityRegisterRequest struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
}

// IdentityRegisterResponse mirrors api success body (identity + event id).
type IdentityRegisterResponse struct {
	Identity IdentityDTO `json:"identity"`
	EventID  string      `json:"event_id"`
}

// =============================================================================
// IdentityRepo — Find
// =============================================================================

// IdentityFind GETs /admin/identity/find?kind=…
func (c *Client) IdentityFind(ctx context.Context, kind string) ([]IdentityDTO, error) {
	var out []IdentityDTO
	err := c.getJSON(ctx, "/admin/identity/find"+buildQuery("kind", kind), &out)
	return out, err
}

// =============================================================================
// IdentityRegistration — RegisterIdentity
// =============================================================================

// IdentityRegister POSTs /admin/identity/register.
func (c *Client) IdentityRegister(ctx context.Context, req IdentityRegisterRequest) (IdentityRegisterResponse, error) {
	var res IdentityRegisterResponse
	err := c.postJSON(ctx, "/admin/identity/register", req, &res)
	return res, err
}

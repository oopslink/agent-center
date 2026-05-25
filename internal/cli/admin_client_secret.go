// Package cli — admin_client_secret.go: Client methods for the
// SecretManagement BC admin surface (user_secret CRUD). Mirrors
// internal/admin/api/secret.go 1:1.
//
// Resolve is intentionally NOT exposed on the Client; the admin
// endpoint stubs it as 501 because UserSecretSvc.Resolve returns
// plaintext and is gated behind SecretResolutionService (v2.3 review).
package cli

import "context"

// =============================================================================
// DTOs — JSON shape returned by admin/api/secret.go::secretMap.
// =============================================================================

// UserSecretDTO mirrors admin api secretMap.
type UserSecretDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`
	Version        int    `json:"version"`
	RevokedAt      string `json:"revoked_at,omitempty"`
	RevokedBy      string `json:"revoked_by,omitempty"`
	RevokedReason  string `json:"revoked_reason,omitempty"`
	RevokedMessage string `json:"revoked_message,omitempty"`
	RotatedAt      string `json:"rotated_at,omitempty"`
	LastUsedAt     string `json:"last_used_at,omitempty"`
}

// =============================================================================
// Request payloads — match admin/api request structs.
// =============================================================================

// SecretCreateRequest mirrors api secretCreateReq.
type SecretCreateRequest struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Plaintext string `json:"plaintext"`
}

// SecretCreateResponse mirrors api success body (id + name + event_id).
type SecretCreateResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	EventID string `json:"event_id"`
}

// SecretRotateRequest mirrors api secretRotateReq.
type SecretRotateRequest struct {
	ID           string `json:"id"`
	NewPlaintext string `json:"new_plaintext"`
	Version      int    `json:"version"`
}

// SecretRevokeRequest mirrors api secretRevokeReq.
type SecretRevokeRequest struct {
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Version int    `json:"version"`
}

// =============================================================================
// UserSecretRepo — FindAll / FindByID / FindByName
// =============================================================================

// SecretFindAll GETs /admin/secret/user-secret/find-all?kind=…&state=…
func (c *Client) SecretFindAll(ctx context.Context, kind, state string) ([]UserSecretDTO, error) {
	var out []UserSecretDTO
	err := c.getJSON(ctx, "/admin/secret/user-secret/find-all"+
		buildQuery("kind", kind, "state", state), &out)
	return out, err
}

// SecretFindByID GETs /admin/secret/user-secret/find-by-id?id=…
func (c *Client) SecretFindByID(ctx context.Context, id string) (UserSecretDTO, error) {
	var out UserSecretDTO
	err := c.getJSON(ctx, "/admin/secret/user-secret/find-by-id"+buildQuery("id", id), &out)
	return out, err
}

// SecretFindByName GETs /admin/secret/user-secret/find-by-name?name=…
func (c *Client) SecretFindByName(ctx context.Context, name string) (UserSecretDTO, error) {
	var out UserSecretDTO
	err := c.getJSON(ctx, "/admin/secret/user-secret/find-by-name"+buildQuery("name", name), &out)
	return out, err
}

// =============================================================================
// UserSecretSvc — Create / Rotate / Revoke
// =============================================================================

// SecretCreate POSTs /admin/secret/user-secret/create.
func (c *Client) SecretCreate(ctx context.Context, req SecretCreateRequest) (SecretCreateResponse, error) {
	var res SecretCreateResponse
	err := c.postJSON(ctx, "/admin/secret/user-secret/create", req, &res)
	return res, err
}

// SecretRotate POSTs /admin/secret/user-secret/rotate.
func (c *Client) SecretRotate(ctx context.Context, req SecretRotateRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/secret/user-secret/rotate", req, &res)
	return res, err
}

// SecretRevoke POSTs /admin/secret/user-secret/revoke.
func (c *Client) SecretRevoke(ctx context.Context, req SecretRevokeRequest) (EventIDResponse, error) {
	var res EventIDResponse
	err := c.postJSON(ctx, "/admin/secret/user-secret/revoke", req, &res)
	return res, err
}

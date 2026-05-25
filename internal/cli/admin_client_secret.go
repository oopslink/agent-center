// Package cli — admin_client_secret.go: Client methods for the
// SecretManagement BC admin surface (user_secret CRUD + Resolve).
// Mirrors internal/admin/api/secret.go 1:1.
//
// SecretResolve is v2.3-3b (task #29) and gated server-side by the
// `secret:resolve` scope. CLI tokens generally don't carry this scope —
// the method is here for test parity with the worker-daemon AdminClient
// (and so any future CLI command like `secret reveal` can plug in).
package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
)

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

// SecretResolveResponse mirrors the server's secret resolve envelope.
// PlaintextBase64 is std base64 of the raw bytes (NOT URL-safe).
type SecretResolveResponse struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	PlaintextBase64 string `json:"plaintext_base64"`
}

// SecretResolve POSTs /admin/secret/user-secret/resolve. Returns the
// decoded plaintext bytes; caller is responsible for wiping them after
// use (ADR-0026 § 5). Requires `secret:resolve` scope on the bearer.
func (c *Client) SecretResolve(ctx context.Context, name string) ([]byte, error) {
	if name == "" {
		return nil, errors.New("admin client: secret name required")
	}
	var res SecretResolveResponse
	if err := c.postJSON(ctx, "/admin/secret/user-secret/resolve",
		map[string]any{"name": name}, &res); err != nil {
		return nil, err
	}
	plain, err := base64.StdEncoding.DecodeString(res.PlaintextBase64)
	if err != nil {
		return nil, fmt.Errorf("admin client: decode resolve plaintext: %w", err)
	}
	return plain, nil
}

// Package api — admintoken.go: HTTP handlers for the AdminToken BC
// management surface (create / list / revoke). All three handlers gate
// on the `admin:token` scope so an operator who holds a narrow-scope
// token can't escalate by minting a fresh `*` token.
//
// Plaintext rule (v2.3-3a task #28):
//   - Create: returns plaintext exactly once.
//   - List / show: never echoes plaintext nor value_hash. Only id,
//     owner, scopes, timestamps, version.
package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
)

// admintokenScopeRequired is the gate scope every endpoint in this file
// requires. Kept as a const so a test or future refactor that loosens
// the policy has one location to update.
const admintokenScopeRequired admintoken.Scope = "admin:token"

// =============================================================================
// Request / response DTOs
// =============================================================================

// admintokenCreateReq mirrors the CLI payload for `admin token create`.
// CreatedBy is optional — middleware doesn't override it (the request
// caller may attribute creation to a different operator).
type admintokenCreateReq struct {
	Owner     string   `json:"owner"`
	Scopes    []string `json:"scopes"`
	CreatedBy string   `json:"created_by"`
}

// admintokenCreateResp returns the ID + plaintext. Plaintext is echoed
// EXACTLY ONCE here; the operator is responsible for capturing it.
type admintokenCreateResp struct {
	ID        string `json:"id"`
	Plaintext string `json:"plaintext"`
}

// admintokenRevokeReq mirrors `admin token revoke`. By is auto-derived
// from the request's AuthContext owner.
type admintokenRevokeReq struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// =============================================================================
// Handlers
// =============================================================================

func (s *Server) admintokenCreateHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireScope(w, r, admintokenScopeRequired) {
		return
	}
	d := hd(r)
	if d.AdminTokenSvc == nil {
		writeError(w, http.StatusNotImplemented, "admintoken_svc_not_wired", "")
		return
	}
	var req admintokenCreateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Owner == "" {
		writeError(w, http.StatusBadRequest, "missing_owner", "")
		return
	}
	if len(req.Scopes) == 0 {
		writeError(w, http.StatusBadRequest, "missing_scopes", "at least one scope required")
		return
	}
	scopes := make([]admintoken.Scope, 0, len(req.Scopes))
	for _, s := range req.Scopes {
		scopes = append(scopes, admintoken.Scope(s))
	}
	createdBy := req.CreatedBy
	if createdBy == "" {
		// Auto-attribute to the requesting bearer when unspecified.
		if auth, ok := AuthFromContext(r.Context()); ok {
			createdBy = string(auth.Owner)
		}
	}
	res, err := d.AdminTokenSvc.Create(r.Context(), admintokensvc.CreateCommand{
		Owner:     admintoken.Owner(req.Owner),
		Scopes:    scopes,
		CreatedBy: createdBy,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, admintokenCreateResp{
		ID:        string(res.ID),
		Plaintext: res.Plaintext,
	})
}

func (s *Server) admintokenListHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireScope(w, r, admintokenScopeRequired) {
		return
	}
	d := hd(r)
	if d.AdminTokenSvc == nil {
		writeError(w, http.StatusNotImplemented, "admintoken_svc_not_wired", "")
		return
	}
	tokens, err := d.AdminTokenSvc.FindAll(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(tokens))
	for i, t := range tokens {
		out[i] = admintokenMap(t)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) admintokenRevokeHandler(w http.ResponseWriter, r *http.Request) {
	if !RequireScope(w, r, admintokenScopeRequired) {
		return
	}
	d := hd(r)
	if d.AdminTokenSvc == nil {
		writeError(w, http.StatusNotImplemented, "admintoken_svc_not_wired", "")
		return
	}
	var req admintokenRevokeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	by := ""
	if auth, ok := AuthFromContext(r.Context()); ok {
		by = string(auth.Owner)
	}
	if err := d.AdminTokenSvc.Revoke(r.Context(), admintokensvc.RevokeCommand{
		ID:     admintoken.TokenID(req.ID),
		By:     by,
		Reason: req.Reason,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": req.ID, "revoked": true})
}

// =============================================================================
// Projection helper
// =============================================================================

// admintokenMap returns the JSON shape consumed by the CLI. NEVER
// includes value_hash or plaintext per task brief.
func admintokenMap(t *admintoken.AdminToken) map[string]any {
	scopes := make([]string, 0, len(t.Scopes()))
	for _, s := range t.Scopes() {
		scopes = append(scopes, string(s))
	}
	m := map[string]any{
		"id":         string(t.ID()),
		"owner":      string(t.Owner()),
		"scopes":     scopes,
		"created_at": t.CreatedAt().Format(time.RFC3339Nano),
		"created_by": t.CreatedBy(),
		"version":    t.Version(),
	}
	if rv := t.RevokedAt(); rv != nil {
		m["revoked_at"] = rv.Format(time.RFC3339Nano)
		m["revoked_by"] = t.RevokedBy()
		m["revoked_reason"] = t.RevokedReason()
	}
	if lu := t.LastUsedAt(); lu != nil {
		m["last_used_at"] = lu.Format(time.RFC3339Nano)
	}
	return m
}

package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/secretmgmt"
	secretservice "github.com/oopslink/agent-center/internal/secretmgmt/service"
)

// =============================================================================
// UserSecretRepo — FindAll / FindByID / FindByName
// =============================================================================

func (s *Server) secretFindAllHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretRepo == nil {
		writeError(w, http.StatusNotImplemented, "secret_repo_not_wired", "")
		return
	}
	filter := secretmgmt.UserSecretFilter{}
	if v := r.URL.Query().Get("kind"); v != "" {
		k := secretmgmt.UserSecretKind(v)
		filter.Kind = &k
	}
	if v := r.URL.Query().Get("state"); v != "" {
		st := secretmgmt.UserSecretState(v)
		filter.State = &st
	}
	list, err := d.UserSecretRepo.FindAll(r.Context(), filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, sec := range list {
		out[i] = secretMap(sec)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) secretFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretRepo == nil {
		writeError(w, http.StatusNotImplemented, "secret_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	sec, err := d.UserSecretRepo.FindByID(r.Context(), secretmgmt.UserSecretID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, secretMap(sec))
}

func (s *Server) secretFindByNameHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretRepo == nil {
		writeError(w, http.StatusNotImplemented, "secret_repo_not_wired", "")
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing_name", "")
		return
	}
	sec, err := d.UserSecretRepo.FindByName(r.Context(), name)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, secretMap(sec))
}

// =============================================================================
// UserSecretSvc — Create / Rotate / Revoke / Resolve (via separate resolution
// service if you wired it; but per audit deps don't include ResolutionSvc).
//
// We expose Create / Rotate / Revoke on UserSecretSvc directly. Resolve is
// admin-policy-sensitive (returns plaintext) — gated behind a separate
// SecretResolutionService in production wiring; v2.2-A2 keeps Resolve OUT
// of the admin transport (CLI never calls it).
// =============================================================================

type secretCreateReq struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Plaintext string `json:"plaintext"`
}

func (s *Server) secretCreateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretSvc == nil {
		writeError(w, http.StatusNotImplemented, "secret_svc_not_wired", "")
		return
	}
	var req secretCreateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Plaintext == "" {
		writeError(w, http.StatusBadRequest, "missing_plaintext", "")
		return
	}
	plain := []byte(req.Plaintext)
	res, err := d.UserSecretSvc.Create(r.Context(), secretservice.CreateSecretCommand{
		Name:          req.Name,
		Kind:          secretmgmt.UserSecretKind(req.Kind),
		Plaintext:     plain,
		ActorIdentity: d.Actor,
	})
	// Best-effort wipe of plaintext.
	for i := range plain {
		plain[i] = 0
	}
	req.Plaintext = ""
	if err != nil {
		mapDomainError(w, err)
		return
	}
	// ADR-0026 § 5: never echo plaintext.
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       string(res.ID),
		"name":     res.Name,
		"event_id": string(res.EventID),
	})
}

type secretRotateReq struct {
	ID           string `json:"id"`
	NewPlaintext string `json:"new_plaintext"`
	Version      int    `json:"version"`
}

func (s *Server) secretRotateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretSvc == nil {
		writeError(w, http.StatusNotImplemented, "secret_svc_not_wired", "")
		return
	}
	var req secretRotateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	plain := []byte(req.NewPlaintext)
	evID, err := d.UserSecretSvc.Rotate(r.Context(), secretservice.RotateSecretCommand{
		ID:            secretmgmt.UserSecretID(req.ID),
		NewPlaintext:  plain,
		Version:       req.Version,
		ActorIdentity: d.Actor,
	})
	for i := range plain {
		plain[i] = 0
	}
	req.NewPlaintext = ""
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

type secretRevokeReq struct {
	ID      string `json:"id"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Version int    `json:"version"`
}

func (s *Server) secretRevokeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.UserSecretSvc == nil {
		writeError(w, http.StatusNotImplemented, "secret_svc_not_wired", "")
		return
	}
	var req secretRevokeReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	reason := secretmgmt.UserSecretRevokedReason(req.Reason)
	if reason == "" {
		reason = secretmgmt.UserSecretRevokedReasonManual
	}
	evID, err := d.UserSecretSvc.Revoke(r.Context(), secretservice.RevokeSecretCommand{
		ID:            secretmgmt.UserSecretID(req.ID),
		Reason:        reason,
		Message:       req.Message,
		Version:       req.Version,
		ActorIdentity: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": string(evID)})
}

// secretResolveHandler is intentionally out of scope. The audit lists
// UserSecretSvc.Resolve, but the production Resolve lives on the
// SecretResolutionService (separate type) and returns plaintext —
// admin transport adoption is gated on v2.3 (security review). This stub
// returns 501 so the route slot is reserved.
func (s *Server) secretResolveHandler(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented, "resolve_not_admin_exposed",
		"UserSecretService.Resolve returns plaintext and is wired through "+
			"SecretResolutionService in worker daemon — admin transport "+
			"intentionally omits it pending v2.3 security review.")
}

// =============================================================================
// Projection helpers
// =============================================================================

func secretMap(s *secretmgmt.UserSecret) map[string]any {
	m := map[string]any{
		"id":         string(s.ID()),
		"name":       s.Name(),
		"kind":       string(s.Kind()),
		"state":      string(s.State()),
		"created_at": s.CreatedAt().Format(time.RFC3339Nano),
		"created_by": s.CreatedBy(),
		"version":    s.Version(),
	}
	if r := s.RevokedAt(); r != nil {
		m["revoked_at"] = r.Format(time.RFC3339Nano)
		m["revoked_by"] = s.RevokedBy()
		m["revoked_reason"] = string(s.RevokedReason())
		m["revoked_message"] = s.RevokedMessage()
	}
	if ru := s.RotatedAt(); ru != nil {
		m["rotated_at"] = ru.Format(time.RFC3339Nano)
	}
	if lu := s.LastUsedAt(); lu != nil {
		m["last_used_at"] = lu.Format(time.RFC3339Nano)
	}
	return m
}

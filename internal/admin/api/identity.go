package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/conversation/identity"
)

// =============================================================================
// IdentityRepo — Find
// =============================================================================

func (s *Server) identityFindHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IdentityRepo == nil {
		writeError(w, http.StatusNotImplemented, "identity_repo_not_wired", "")
		return
	}
	filter := identity.IdentityFilter{}
	if v := r.URL.Query().Get("kind"); v != "" {
		k := identity.Kind(v)
		filter.Kind = &k
	}
	list, err := d.IdentityRepo.Find(r.Context(), filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, ident := range list {
		out[i] = identityMap(ident)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// IdentityRegistration — RegisterIdentity
// =============================================================================

type identityRegisterReq struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name"`
}

func (s *Server) identityRegisterHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.IdentityRegistration == nil {
		writeError(w, http.StatusNotImplemented, "identity_registration_not_wired", "")
		return
	}
	var req identityRegisterReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	kind := identity.Kind(req.Kind)
	if kind == "" {
		derived, kerr := identity.KindFromID(identity.IdentityID(req.ID))
		if kerr != nil {
			writeError(w, http.StatusBadRequest, "invalid_id", kerr.Error())
			return
		}
		kind = derived
	}
	res, err := d.IdentityRegistration.RegisterIdentity(r.Context(), identity.RegisterIdentityCommand{
		ID:          identity.IdentityID(req.ID),
		Kind:        kind,
		DisplayName: req.DisplayName,
		Actor:       d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"identity": identityMap(res.Identity),
		"event_id": string(res.EventID),
	})
}

// =============================================================================
// Projection helpers
// =============================================================================

func identityMap(i *identity.Identity) map[string]any {
	return map[string]any{
		"id":           string(i.ID()),
		"kind":         string(i.Kind()),
		"display_name": i.DisplayName(),
		"version":      i.Version(),
		"created_at":   i.CreatedAt().Format(time.RFC3339Nano),
	}
}

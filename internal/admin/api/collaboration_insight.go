package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/observability/collaborationeffect"
)

func (s *Server) collaborationEffectsQueryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CollaborationInsight == nil {
		writeError(w, http.StatusNotImplemented, "collaboration_insight_not_wired", "")
		return
	}
	var f collaborationeffect.Filter
	if err := decodeJSON(r, &f); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := d.CollaborationInsight.Query(r.Context(), f)
	if errors.Is(err, collaborationeffect.ErrInvalidQuery) {
		writeError(w, http.StatusBadRequest, "invalid_query", err.Error())
		return
	}
	if errors.Is(err, collaborationeffect.ErrInvalidCursor) {
		writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error())
		return
	}
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) collaborationEffectEvidenceHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.CollaborationInsight == nil {
		writeError(w, http.StatusNotImplemented, "collaboration_insight_not_wired", "")
		return
	}
	res, err := d.CollaborationInsight.Evidence(r.Context(), strings.TrimSpace(r.PathValue("effect_id")), strings.TrimSpace(r.URL.Query().Get("project_id")))
	if errors.Is(err, collaborationeffect.ErrEffectNotFound) {
		writeError(w, http.StatusNotFound, "effect_not_found", "effect not found")
		return
	}
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

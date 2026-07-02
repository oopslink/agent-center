package api

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// templateMap is the wire DTO for a template (list and detail views).
// The content field is only included in the detail (getTemplate) response.
func templateMap(t *pm.Template, includeContent bool) map[string]any {
	m := map[string]any{
		"id":          string(t.ID()),
		"name":        t.Name(),
		"description": t.Description(),
		"builtin":     t.IsBuiltin(),
		"created_at":  t.CreatedAt().Format(time.RFC3339Nano),
	}
	if includeContent {
		m["content"] = t.Content()
		m["updated_at"] = t.UpdatedAt().Format(time.RFC3339Nano)
		m["version"] = t.Version()
	}
	return m
}

// mapTemplateError maps domain-level template errors to HTTP status codes.
func mapTemplateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrTemplateNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pm.ErrTemplateExists):
		writeError(w, http.StatusConflict, "template_exists", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}

// generateTemplateID returns a unique template ID prefixed with "tmpl-".
func generateTemplateID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("tmpl-%x", b[:]), nil
}

// listTemplatesHandler serves GET /api/orgs/{slug}/templates.
// Member-readable: returns id, name, description, builtin, created_at.
// The response includes both org-owned and builtin templates (builtin are global).
func (s *Server) listTemplatesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TemplateRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "template repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	templates, err := d.TemplateRepo.ListByOrg(r.Context(), orgID)
	if err != nil {
		mapTemplateError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(templates))
	for _, t := range templates {
		out = append(out, templateMap(t, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": out})
}

// getTemplateHandler serves GET /api/orgs/{slug}/templates/{id}.
// Member-readable: returns the full template including content.
// Builtin templates are accessible from any org; org-owned templates require org membership.
func (s *Server) getTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TemplateRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "template repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	tmplID := r.PathValue("id")
	t, err := d.TemplateRepo.FindByID(r.Context(), pm.TemplateID(tmplID))
	if err != nil {
		mapTemplateError(w, err)
		return
	}
	// Org access check: non-builtin templates belong to a specific org only.
	// Existence is not leaked across workspaces (cross-org → 404).
	if !t.IsBuiltin() && t.OrgID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	writeJSON(w, http.StatusOK, templateMap(t, true))
}

// createTemplateHandler serves POST /api/orgs/{slug}/templates.
// Org-member scoped: any member can create an org-owned template.
// Body: {name, description, content}
func (s *Server) createTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TemplateRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "template repo not wired")
		return
	}
	callerID, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20) // 2 MiB limit
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := generateTemplateID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "id_gen_failed", err.Error())
		return
	}
	t, err := pm.NewTemplate(pm.NewTemplateInput{
		ID:          pm.TemplateID(id),
		OrgID:       orgID,
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Builtin:     false,
		CreatedBy:   pm.IdentityRef("user:" + callerID.ID()),
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := d.TemplateRepo.Save(r.Context(), t); err != nil {
		mapTemplateError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, templateMap(t, true))
}

// updateTemplateHandler serves PUT /api/orgs/{slug}/templates/{id}.
// Org-member scoped. Builtin templates may not be edited (403).
// Body: {name, description, content}
func (s *Server) updateTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TemplateRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "template repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	tmplID := r.PathValue("id")
	t, err := d.TemplateRepo.FindByID(r.Context(), pm.TemplateID(tmplID))
	if err != nil {
		mapTemplateError(w, err)
		return
	}
	// Org boundary: cross-org id → 404 (existence non-disclosure).
	if t.OrgID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	// Builtin templates are immutable.
	if t.IsBuiltin() {
		writeError(w, http.StatusForbidden, "forbidden", "builtin templates cannot be modified")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20) // 2 MiB limit
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := t.Update(req.Name, req.Description, req.Content, time.Now().UTC()); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := d.TemplateRepo.Update(r.Context(), t); err != nil {
		mapTemplateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateMap(t, true))
}

// deleteTemplateHandler serves DELETE /api/orgs/{slug}/templates/{id}.
// Org-member scoped. Builtin templates may not be deleted (403).
func (s *Server) deleteTemplateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.TemplateRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "template repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	tmplID := r.PathValue("id")
	t, err := d.TemplateRepo.FindByID(r.Context(), pm.TemplateID(tmplID))
	if err != nil {
		mapTemplateError(w, err)
		return
	}
	// Org boundary: cross-org id → 404.
	if t.OrgID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	// Builtin templates are immutable.
	if t.IsBuiltin() {
		writeError(w, http.StatusForbidden, "forbidden", "builtin templates cannot be deleted")
		return
	}
	if err := d.TemplateRepo.Delete(r.Context(), pm.TemplateID(tmplID)); err != nil {
		mapTemplateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

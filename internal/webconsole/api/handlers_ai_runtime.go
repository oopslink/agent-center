package api

import (
	"errors"
	"net/http"

	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/identity"
)

type runtimeWrite[T any] struct {
	ExpectedRevision int64 `json:"expected_revision"`
	Value            T     `json:"value"`
}

func aiRuntimeDeps(w http.ResponseWriter, r *http.Request, admin bool) (HandlerDeps, *identity.Identity, string, bool) {
	d := hd(r)
	if d.RuntimeCatalog == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "AI Runtime Catalog is not configured")
		return d, nil, "", false
	}
	id, member, org, ok := requireOrgMember(w, r, d)
	if !ok {
		return d, nil, "", false
	}
	if admin && !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can manage AI Runtime Catalog")
		return d, nil, "", false
	}
	return d, id, org, true
}

func writeRuntimeError(w http.ResponseWriter, err error) {
	var runtimeErr *airuntime.Error
	switch {
	case errors.As(err, &runtimeErr):
		status := http.StatusBadRequest
		if runtimeErr.Reason == airuntime.ReasonRevisionConflict {
			status = http.StatusConflict
		}
		writeJSON(w, status, runtimeErr)
	case errors.Is(err, airuntime.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	}
}

func (s *Server) getRuntimeCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), org)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func (s *Server) listRuntimeCLIsHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	c, err := d.RuntimeCatalog.Catalog(r.Context(), org)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": c.Revision, "entries": c.CLIs})
}
func (s *Server) listRuntimeModelsHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	c, err := d.RuntimeCatalog.Catalog(r.Context(), org)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": c.Revision, "entries": c.Models})
}
func (s *Server) listRuntimeProfilesHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	c, err := d.RuntimeCatalog.Catalog(r.Context(), org)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": c.Revision, "default_runtime_profile_id": c.DefaultProfileID, "entries": c.Profiles})
}

func (s *Server) createRuntimeCLIHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeWrite[airuntime.CLIDefinition]
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	entry, rev, err := d.RuntimeCatalog.CreateCLI(r.Context(), org, "user:"+id.ID(), req.ExpectedRevision, req.Value)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"revision": rev, "entry": entry})
}
func (s *Server) updateRuntimeCLIHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeWrite[airuntime.CLIDefinition]
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.Value.ID = r.PathValue("id")
	entry, rev, err := d.RuntimeCatalog.UpdateCLI(r.Context(), org, "user:"+id.ID(), req.ExpectedRevision, req.Value)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev, "entry": entry})
}
func (s *Server) createRuntimeModelHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeWrite[airuntime.ModelDefinition]
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	entry, rev, err := d.RuntimeCatalog.CreateModel(r.Context(), org, "user:"+id.ID(), req.ExpectedRevision, req.Value)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"revision": rev, "entry": entry})
}
func (s *Server) updateRuntimeModelHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeWrite[airuntime.ModelDefinition]
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.Value.ID = r.PathValue("id")
	entry, rev, err := d.RuntimeCatalog.UpdateModel(r.Context(), org, "user:"+id.ID(), req.ExpectedRevision, req.Value)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev, "entry": entry})
}
func (s *Server) createRuntimeProfileHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeWrite[airuntime.RuntimeProfile]
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	entry, rev, err := d.RuntimeCatalog.CreateProfile(r.Context(), org, "user:"+id.ID(), req.ExpectedRevision, req.Value)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"revision": rev, "entry": entry})
}
func (s *Server) updateRuntimeProfileHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeWrite[airuntime.RuntimeProfile]
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.Value.ID = r.PathValue("id")
	entry, rev, err := d.RuntimeCatalog.UpdateProfile(r.Context(), org, "user:"+id.ID(), req.ExpectedRevision, req.Value)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev, "entry": entry})
}
func (s *Server) setRuntimeDefaultProfileHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expected_revision"`
		ProfileID        string `json:"profile_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	rev, err := d.RuntimeCatalog.SetDefaultProfile(r.Context(), org, "user:"+id.ID(), req.ProfileID, req.ExpectedRevision)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev, "default_runtime_profile_id": req.ProfileID})
}

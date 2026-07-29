package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/identity"
)

const maxRuntimeBundleBytes = 4 << 20

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

func (s *Server) exportRuntimeCatalogHandler(w http.ResponseWriter, r *http.Request) {
	// Export and import expose the same organization-wide configuration
	// boundary, so both require the catalog write/admin permission.
	d, _, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	data, err := d.RuntimeCatalog.Export(r.Context(), org, format)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	if format == "yaml" || format == "yml" {
		w.Header().Set("Content-Type", "application/yaml")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("Content-Disposition", `attachment; filename="ai-runtime-catalog.`+map[bool]string{true: "yaml", false: "json"}[format == "yaml" || format == "yml"]+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func readRuntimeBundle(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRuntimeBundleBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_bundle", err.Error())
		return nil, false
	}
	if len(data) > maxRuntimeBundleBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "bundle_too_large", "AI Runtime bundle exceeds 4 MiB")
		return nil, false
	}
	return data, true
}

func (s *Server) previewRuntimeImportHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	data, ok := readRuntimeBundle(w, r)
	if !ok {
		return
	}
	preview, err := d.RuntimeCatalog.PreviewImport(r.Context(), org, r.URL.Query().Get("mode"), data)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) applyRuntimeImportHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	data, ok := readRuntimeBundle(w, r)
	if !ok {
		return
	}
	token := r.Header.Get("X-AI-Runtime-Validation-Token")
	catalog, err := d.RuntimeCatalog.ApplyImport(r.Context(), org, "user:"+id.ID(), r.URL.Query().Get("mode"), data, token)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

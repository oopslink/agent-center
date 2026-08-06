package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/identity"
	"gopkg.in/yaml.v3"
)

type runtimeWrite[T any] struct {
	ExpectedRevision int64                        `json:"expected_revision"`
	Value            T                            `json:"value"`
	Rollout          airuntime.RuntimeRolloutPlan `json:"rollout,omitempty"`
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
		if runtimeErr.Reason == airuntime.ReasonRevisionConflict || runtimeErr.Reason == airuntime.ReasonImportConflict {
			status = http.StatusConflict
		}
		writeJSON(w, status, runtimeErr)
	case errors.Is(err, airuntime.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	}
}

func (s *Server) exportRuntimeCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	query := r.URL.Query()
	includeDependencies := true
	if raw := query.Get("include_dependencies"); raw != "" {
		var err error
		includeDependencies, err = strconv.ParseBool(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", "include_dependencies must be true or false")
			return
		}
	}
	doc, err := d.RuntimeCatalog.ExportWithOptions(r.Context(), org, airuntime.ExportOptions{
		Scope:               airuntime.ExportScope(query.Get("scope")),
		CLIKeys:             splitRuntimeKeys(query.Get("cli_keys")),
		ModelKeys:           splitRuntimeKeys(query.Get("model_keys")),
		ProfileKeys:         splitRuntimeKeys(query.Get("profile_keys")),
		IncludeDependencies: includeDependencies,
	})
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	format := strings.ToLower(query.Get("format"))
	if format == "" {
		format = "yaml"
	}
	switch format {
	case "json":
		writeJSON(w, http.StatusOK, doc)
	case "yaml", "yml":
		var value any
		raw, _ := json.Marshal(doc)
		if err := json.Unmarshal(raw, &value); err != nil {
			writeRuntimeError(w, err)
			return
		}
		out, err := yaml.Marshal(value)
		if err != nil {
			writeRuntimeError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	default:
		writeError(w, http.StatusBadRequest, "invalid_input", "format must be yaml or json")
	}
}

func (s *Server) previewRuntimeCatalogImportHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req airuntime.PreviewRequest
	if err := decodeRuntimeDocument(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_document", err.Error())
		return
	}
	result, err := d.RuntimeCatalog.PreviewImport(r.Context(), org, req)
	if err != nil {
		writeRuntimeImportError(w, result.Report, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) applyRuntimeCatalogImportHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req airuntime.ApplyRequest
	if err := decodeRuntimeDocument(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_document", err.Error())
		return
	}
	report, err := d.RuntimeCatalog.ApplyImport(r.Context(), org, "user:"+id.ID(), req)
	if err != nil {
		writeRuntimeImportError(w, report, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func writeRuntimeImportError(w http.ResponseWriter, report airuntime.ImportReport, err error) {
	var runtimeErr *airuntime.Error
	if errors.As(err, &runtimeErr) {
		status := http.StatusBadRequest
		if runtimeErr.Reason == airuntime.ReasonRevisionConflict || runtimeErr.Reason == airuntime.ReasonImportConflict {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"report": report, "error": runtimeErr})
		return
	}
	writeRuntimeError(w, err)
}

func decodeRuntimeDocument(r *http.Request, dst any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	contentType := strings.ToLower(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if contentType == "application/yaml" || contentType == "text/yaml" || contentType == "application/x-yaml" {
		var value any
		if err := yaml.Unmarshal(raw, &value); err != nil {
			return err
		}
		normalized, err := json.Marshal(value)
		if err != nil {
			return err
		}
		raw = normalized
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return err
	}
	preview, ok := dst.(*airuntime.PreviewRequest)
	if !ok {
		return nil
	}
	preview.Warnings = runtimeUnknownFieldWarnings(value)
	return nil
}

func runtimeUnknownFieldWarnings(value any) []airuntime.Diagnostic {
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	var out []airuntime.Diagnostic
	visitUnknown := func(object map[string]any, path string, allowed map[string]bool) {
		for key := range object {
			if !allowed[key] {
				out = append(out, airuntime.Diagnostic{
					Code: airuntime.ReasonImportUnknownField, Severity: "warning",
					Path: path + "." + key, Message: "unknown schema v1 field was ignored",
				})
			}
		}
	}
	visitUnknown(root, "$", map[string]bool{"strategy": true, "document": true})
	doc, _ := root["document"].(map[string]any)
	visitUnknown(doc, "$.document", map[string]bool{"schema_version": true, "kind": true, "exported_at": true, "runtime": true, "warnings": true})
	runtime, _ := doc["runtime"].(map[string]any)
	visitUnknown(runtime, "$.document.runtime", map[string]bool{"default_profile_key": true, "clis": true, "models": true, "profiles": true})
	scanList := func(raw any, path string, allowed map[string]bool) {
		items, _ := raw.([]any)
		for i, item := range items {
			object, _ := item.(map[string]any)
			visitUnknown(object, fmt.Sprintf("%s[%d]", path, i), allowed)
		}
	}
	scanList(runtime["clis"], "$.document.runtime.clis", map[string]bool{
		"key": true, "display_name": true, "executable": true, "version_constraint": true,
		"required_features": true, "parameter_schema": true, "enabled": true,
	})
	scanList(runtime["models"], "$.document.runtime.models", map[string]bool{
		"key": true, "model_key": true, "display_name": true, "compatible_cli_keys": true,
		"default_parameters": true, "enabled": true, "context_window": true,
		"input_cost_per_mtok": true, "output_cost_per_mtok": true, "tier": true,
	})
	scanList(runtime["profiles"], "$.document.runtime.profiles", map[string]bool{
		"key": true, "name": true, "description": true, "cli_key": true,
		"model_key": true, "parameters": true, "enabled": true,
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func splitRuntimeKeys(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, ",")
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

func (s *Server) runtimeImpactPreviewHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	query := r.URL.Query()
	percent := 0
	if raw := query.Get("rollout_percent"); raw != "" {
		var err error
		percent, err = strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", "rollout_percent must be an integer")
			return
		}
	}
	impact, err := d.RuntimeCatalog.ImpactPreview(r.Context(), org, airuntime.RuntimeImpactRequest{
		EntityType: query.Get("entity_type"),
		EntityID:   query.Get("entity_id"),
		Action:     query.Get("action"),
		Rollout: airuntime.RuntimeRolloutPlan{
			Enabled: query.Get("rollout") == "canary" || query.Get("rollout") == "gray" || query.Get("rollout_enabled") == "true",
			Percent: percent,
			Label:   query.Get("rollout"),
		},
	})
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, impact)
}

func (s *Server) runtimeAuditHandler(w http.ResponseWriter, r *http.Request) {
	d, _, org, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", "limit must be an integer")
			return
		}
		limit = n
	}
	events, err := d.RuntimeCatalog.AuditLog(r.Context(), org, limit)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": events})
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
	impact, err := d.RuntimeCatalog.ImpactPreview(r.Context(), org, airuntime.RuntimeImpactRequest{
		EntityType: "profile", EntityID: req.Value.ID, Action: "update", Rollout: req.Rollout,
	})
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	entry, rev, err := d.RuntimeCatalog.UpdateProfile(r.Context(), org, "user:"+id.ID(), req.ExpectedRevision, req.Value)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev, "entry": entry, "impact": impact})
}
func (s *Server) setRuntimeDefaultProfileHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req struct {
		ExpectedRevision int64                        `json:"expected_revision"`
		ProfileID        string                       `json:"profile_id"`
		Rollout          airuntime.RuntimeRolloutPlan `json:"rollout,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	impact, err := d.RuntimeCatalog.ImpactPreview(r.Context(), org, airuntime.RuntimeImpactRequest{
		EntityType: "profile", EntityID: req.ProfileID, Action: "set_default", Rollout: req.Rollout,
	})
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	rev, err := d.RuntimeCatalog.SetDefaultProfile(r.Context(), org, "user:"+id.ID(), req.ProfileID, req.ExpectedRevision)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev, "default_runtime_profile_id": req.ProfileID, "impact": impact, "rollout": impact.Rollout})
}

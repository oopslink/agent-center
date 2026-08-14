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
	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/identity"
	"gopkg.in/yaml.v3"
)

type runtimeWrite[T any] struct {
	ExpectedRevision int64 `json:"expected_revision"`
	Value            T     `json:"value"`
}

type runtimeDelete struct {
	ExpectedRevision int64 `json:"expected_revision"`
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
	if d.Authorizer != nil {
		permission := authz.PermissionKey("ai_runtime.catalog.read")
		if admin {
			permission = "ai_runtime.catalog.manage"
		}
		decision, err := d.Authorizer.CheckMigrated(r.Context(), authz.CheckRequest{
			SubjectRef: authz.UserSubject(id.ID()),
			Transport:  authz.TransportWeb,
			Permission: permission,
			Resource:   authz.ResourceScope{Kind: "org", ID: org},
		}, authz.LegacyDecision{
			Allowed:     true,
			Reason:      "legacy ai runtime org role",
			Source:      authz.SourceOrgRole,
			EvidenceRef: "members:" + member.ID(),
		})
		if err != nil || !decision.Allowed {
			writeAuthorizationError(w, decision, err)
			return d, nil, "", false
		}
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
	if err := rejectRetiredRuntimeProfileFields(value); err != nil {
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

func rejectRetiredRuntimeProfileFields(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	doc, _ := root["document"].(map[string]any)
	runtime, _ := doc["runtime"].(map[string]any)
	if _, ok := runtime["default_profile_key"]; ok {
		return errors.New("runtime.default_profile_key is retired; AI Runtime Profile import is no longer supported")
	}
	if _, ok := runtime["profiles"]; ok {
		return errors.New("runtime.profiles is retired; AI Runtime Profile import is no longer supported")
	}
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
	visitUnknown(runtime, "$.document.runtime", map[string]bool{"clis": true, "models": true})
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
func (s *Server) deleteRuntimeCLIHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeDelete
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	rev, err := d.RuntimeCatalog.DeleteCLI(r.Context(), org, "user:"+id.ID(), r.PathValue("id"), req.ExpectedRevision)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev})
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
func (s *Server) deleteRuntimeModelHandler(w http.ResponseWriter, r *http.Request) {
	d, id, org, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req runtimeDelete
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	rev, err := d.RuntimeCatalog.DeleteModel(r.Context(), org, "user:"+id.ID(), r.PathValue("id"), req.ExpectedRevision)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": rev})
}

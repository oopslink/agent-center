package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/identity"
)

// Org model catalog webconsole endpoints are now compatibility adapters over
// AI Runtime models. The old OrgModelCatalog page redirects to AI Runtime, and
// these legacy endpoints no longer write pm_model_catalog as a second source.

func runtimeModelCatalogMap(m airuntime.ModelDefinition) map[string]any {
	return map[string]any{
		"id":             m.ID,
		"model_id":       m.ModelKey,
		"display_name":   m.DisplayName,
		"input_cost":     m.InputCost,
		"output_cost":    m.OutputCost,
		"context_window": m.ContextWindow,
		"tier":           m.Tier,
		"version":        1,
		"updated_at":     m.UpdatedAt.Format(time.RFC3339Nano),
	}
}

// catalogFieldsReq is the create/update body (also one element of an import array).
type catalogFieldsReq struct {
	ModelID       string  `json:"model_id"`
	DisplayName   string  `json:"display_name"`
	InputCost     float64 `json:"input_cost"`
	OutputCost    float64 `json:"output_cost"`
	ContextWindow int     `json:"context_window"`
	Tier          string  `json:"tier"`
}

func validateLegacyModelCatalogFields(req catalogFieldsReq) error {
	if req.ModelID == "" || req.DisplayName == "" || req.InputCost < 0 || req.OutputCost < 0 || req.ContextWindow < 0 {
		return fmt.Errorf("invalid catalog fields")
	}
	return nil
}

func modelDefinitionFromLegacy(req catalogFieldsReq) airuntime.ModelDefinition {
	return airuntime.ModelDefinition{
		Key:               req.ModelID,
		ModelKey:          req.ModelID,
		DisplayName:       req.DisplayName,
		CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{},
		Enabled:           true,
		ContextWindow:     req.ContextWindow,
		InputCost:         req.InputCost,
		OutputCost:        req.OutputCost,
		Tier:              req.Tier,
	}
}

// listModelCatalogHandler serves GET /api/orgs/{slug}/model-catalog.
func (s *Server) listModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d, _, orgID, ok := aiRuntimeDeps(w, r, false)
	if !ok {
		return
	}
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(catalog.Models))
	for _, model := range catalog.Models {
		if model.Enabled {
			out = append(out, runtimeModelCatalogMap(model))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// createModelCatalogHandler serves POST /api/orgs/{slug}/model-catalog.
func (s *Server) createModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d, callerID, orgID, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	var req catalogFieldsReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := validateLegacyModelCatalogFields(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	model, _, err := d.RuntimeCatalog.CreateModel(r.Context(), orgID, "user:"+callerID.ID(), catalog.Revision, modelDefinitionFromLegacy(req))
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, runtimeModelCatalogMap(model))
}

// updateModelCatalogHandler serves PUT /api/orgs/{slug}/model-catalog/{id}.
func (s *Server) updateModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d, callerID, orgID, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	catalog, model, ok := loadRuntimeModelFromCatalog(w, r, d, orgID)
	if !ok {
		return
	}
	var req catalogFieldsReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := validateLegacyModelCatalogFields(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	model.ModelKey = req.ModelID
	model.DisplayName = req.DisplayName
	model.ContextWindow = req.ContextWindow
	model.InputCost = req.InputCost
	model.OutputCost = req.OutputCost
	model.Tier = req.Tier
	updated, _, err := d.RuntimeCatalog.UpdateModel(r.Context(), orgID, "user:"+callerID.ID(), catalog.Revision, model)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, runtimeModelCatalogMap(updated))
}

// deleteModelCatalogHandler serves DELETE /api/orgs/{slug}/model-catalog/{id}.
func (s *Server) deleteModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d, callerID, orgID, ok := aiRuntimeDeps(w, r, true)
	if !ok {
		return
	}
	catalog, model, ok := loadRuntimeModelFromCatalog(w, r, d, orgID)
	if !ok {
		return
	}
	model.Enabled = false
	if _, _, err := d.RuntimeCatalog.UpdateModel(r.Context(), orgID, "user:"+callerID.ID(), catalog.Revision, model); err != nil {
		writeRuntimeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func loadRuntimeModelFromCatalog(w http.ResponseWriter, r *http.Request, d HandlerDeps, orgID string) (airuntime.Catalog, airuntime.ModelDefinition, bool) {
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return airuntime.Catalog{}, airuntime.ModelDefinition{}, false
	}
	for _, model := range catalog.Models {
		if model.ID == r.PathValue("id") {
			return catalog, model, true
		}
	}
	writeError(w, http.StatusNotFound, "not_found", airuntime.ErrNotFound.Error())
	return airuntime.Catalog{}, airuntime.ModelDefinition{}, false
}

// importModelCatalogHandler serves POST /api/orgs/{slug}/model-catalog/import.
// Body: {mode: upsert|replace, json: "<raw array string>"} OR {mode, entries: [...]}.
// Whole-batch validation: any invalid field, negative cost, or duplicate model_id
// rejects the entire import (no half-swallow).
func (s *Server) importModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.RuntimeCatalog == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "model catalog adapter is not configured")
		return
	}
	callerID, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "only owner or admin can manage AI Runtime Catalog")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20) // 4 MiB
	var req struct {
		Mode    string             `json:"mode"`
		JSON    string             `json:"json"`
		Entries []catalogFieldsReq `json:"entries"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = "upsert"
	}
	if mode != "upsert" && mode != "replace" {
		writeError(w, http.StatusBadRequest, "invalid_mode", "mode must be upsert or replace")
		return
	}
	dtos := req.Entries
	if req.JSON != "" {
		dtos = nil
		if err := json.Unmarshal([]byte(req.JSON), &dtos); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_import_json", "json must be an array of catalog entries: "+err.Error())
			return
		}
	}
	seen := make(map[string]struct{}, len(dtos))
	models := make([]airuntime.ExportModel, 0, len(dtos))
	for i, dto := range dtos {
		if dto.ModelID == "" || dto.DisplayName == "" || dto.InputCost < 0 || dto.OutputCost < 0 || dto.ContextWindow < 0 {
			writeError(w, http.StatusBadRequest, "invalid_import", fmt.Sprintf("entry[%d]: invalid catalog fields (whole batch rejected)", i))
			return
		}
		if _, dup := seen[dto.ModelID]; dup {
			writeError(w, http.StatusBadRequest, "invalid_import", fmt.Sprintf("duplicate model_id %q (whole batch rejected)", dto.ModelID))
			return
		}
		seen[dto.ModelID] = struct{}{}
		models = append(models, airuntime.ExportModel{
			Key: dto.ModelID, ModelKey: dto.ModelID, DisplayName: dto.DisplayName,
			CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{},
			Enabled: true, ContextWindow: dto.ContextWindow, InputCost: dto.InputCost,
			OutputCost: dto.OutputCost, Tier: dto.Tier,
		})
	}
	doc, err := d.RuntimeCatalog.Export(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	if mode == "replace" {
		doc.Runtime.Models = models
	} else {
		byKey := make(map[string]airuntime.ExportModel, len(doc.Runtime.Models)+len(models))
		for _, model := range doc.Runtime.Models {
			byKey[model.Key] = model
		}
		for _, model := range models {
			byKey[model.Key] = model
		}
		doc.Runtime.Models = doc.Runtime.Models[:0]
		for _, model := range byKey {
			doc.Runtime.Models = append(doc.Runtime.Models, model)
		}
		sort.Slice(doc.Runtime.Models, func(i, j int) bool { return doc.Runtime.Models[i].Key < doc.Runtime.Models[j].Key })
	}
	// The legacy replace scope is only the legacy model-catalog projection. It
	// must not disable unrelated Runtime Catalog models.
	strategy := airuntime.ImportStrategy(airuntime.StrategyMerge)
	preview, err := d.RuntimeCatalog.PreviewImport(r.Context(), orgID, airuntime.PreviewRequest{Strategy: strategy, Document: doc})
	if err != nil {
		writeRuntimeImportError(w, preview.Report, err)
		return
	}
	_, err = d.RuntimeCatalog.ApplyImport(r.Context(), orgID, "user:"+callerID.ID(), airuntime.ApplyRequest{
		Strategy: strategy, Document: doc, ValidationToken: preview.ValidationToken,
	})
	if err != nil {
		writeRuntimeImportError(w, airuntime.ImportReport{}, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "imported": len(models)})
}

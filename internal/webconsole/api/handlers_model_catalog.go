package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/oopslink/agent-center/internal/airuntime"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Org model catalog webconsole endpoints (issue-93dd8daa ①) — the browser-facing
// CRUD + JSON import behind the org settings "模型类目" panel. Mirrors the template
// handlers: requireOrgMember gates + resolves the org from {slug}; no built-in rows.

func modelCatalogMap(e *pm.ModelCatalogEntry) map[string]any {
	return map[string]any{
		"id":             string(e.ID()),
		"model_id":       e.ModelID(),
		"display_name":   e.DisplayName(),
		"input_cost":     e.InputCost(),
		"output_cost":    e.OutputCost(),
		"context_window": e.ContextWindow(),
		"tier":           e.Tier(),
		"version":        e.Version(),
		"updated_at":     e.UpdatedAt().Format(time.RFC3339Nano),
	}
}

func mapModelCatalogWebError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrModelCatalogEntryNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pm.ErrModelCatalogEntryExists):
		writeError(w, http.StatusConflict, "model_id_exists", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}

func generateModelCatalogID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("mdl-%x", b[:]), nil
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

func (c catalogFieldsReq) fields() pm.ModelCatalogFields {
	return pm.ModelCatalogFields{
		ModelID: c.ModelID, DisplayName: c.DisplayName, InputCost: c.InputCost,
		OutputCost: c.OutputCost, ContextWindow: c.ContextWindow, Tier: c.Tier,
	}
}

// listModelCatalogHandler serves GET /api/orgs/{slug}/model-catalog.
func (s *Server) listModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "model catalog repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	entries, err := d.ModelCatalogRepo.ListByOrg(r.Context(), orgID)
	if err != nil {
		mapModelCatalogWebError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, modelCatalogMap(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// createModelCatalogHandler serves POST /api/orgs/{slug}/model-catalog.
func (s *Server) createModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "model catalog repo not wired")
		return
	}
	callerID, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	var req catalogFieldsReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	id, err := generateModelCatalogID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "id_gen_failed", err.Error())
		return
	}
	e, err := pm.NewModelCatalogEntry(pm.NewModelCatalogEntryInput{
		ID: pm.ModelCatalogEntryID(id), OrgID: orgID, Fields: req.fields(),
		CreatedBy: pm.IdentityRef("user:" + callerID.ID()), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := d.ModelCatalogRepo.Save(r.Context(), e); err != nil {
		mapModelCatalogWebError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, modelCatalogMap(e))
}

// updateModelCatalogHandler serves PUT /api/orgs/{slug}/model-catalog/{id}.
func (s *Server) updateModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "model catalog repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	e, ok := s.loadOwnedCatalogEntry(w, r, d, orgID)
	if !ok {
		return
	}
	var req catalogFieldsReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := e.Update(req.fields(), time.Now().UTC()); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := d.ModelCatalogRepo.Update(r.Context(), e); err != nil {
		mapModelCatalogWebError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, modelCatalogMap(e))
}

// deleteModelCatalogHandler serves DELETE /api/orgs/{slug}/model-catalog/{id}.
func (s *Server) deleteModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "model catalog repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	e, ok := s.loadOwnedCatalogEntry(w, r, d, orgID)
	if !ok {
		return
	}
	if err := d.ModelCatalogRepo.Delete(r.Context(), e.ID()); err != nil {
		mapModelCatalogWebError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// loadOwnedCatalogEntry loads {id} and enforces the org boundary (cross-org → 404).
func (s *Server) loadOwnedCatalogEntry(w http.ResponseWriter, r *http.Request, d HandlerDeps, orgID string) (*pm.ModelCatalogEntry, bool) {
	e, err := d.ModelCatalogRepo.FindByID(r.Context(), pm.ModelCatalogEntryID(r.PathValue("id")))
	if err != nil {
		mapModelCatalogWebError(w, err)
		return nil, false
	}
	if e.OrgID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "model catalog entry not found")
		return nil, false
	}
	return e, true
}

// importModelCatalogHandler serves POST /api/orgs/{slug}/model-catalog/import.
// Body: {mode: upsert|replace, json: "<raw array string>"} OR {mode, entries: [...]}.
// Whole-batch validation: any invalid field, negative cost, or duplicate model_id
// rejects the entire import (no half-swallow).
func (s *Server) importModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.RuntimeCatalog == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "AI Runtime Catalog is not configured")
		return
	}
	callerID, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
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
	strategy := airuntime.ImportStrategy(airuntime.StrategyMerge)
	if mode == "replace" {
		strategy = airuntime.StrategyReplace
	}
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

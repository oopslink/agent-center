package api

import (
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// =============================================================================
// Org model catalog agent-tools (issue-93dd8daa phase ①). The org-level, user-
// managed catalog of models an org's agents may run — the single source of truth
// the phase-② difficulty judge will select from. Mirrors the template CRUD auth +
// shape: requireAgentOnWorker gates the operating agent, org resolves from the
// agent, no built-in rows. Tools: create/update/delete/list_model_catalog_entry
// + import_model_catalog (JSON bulk upsert|replace, whole-batch validation).
// =============================================================================

// modelCatalogEntryDTO is the wire shape of an entry's user-supplied fields, shared
// by create/update and the JSON import array.
type modelCatalogEntryDTO struct {
	ModelID       string  `json:"model_id"`
	DisplayName   string  `json:"display_name"`
	InputCost     float64 `json:"input_cost"`
	OutputCost    float64 `json:"output_cost"`
	ContextWindow int     `json:"context_window"`
	Tier          string  `json:"tier"`
}

func (d modelCatalogEntryDTO) fields() pm.ModelCatalogFields {
	return pm.ModelCatalogFields{
		ModelID:       d.ModelID,
		DisplayName:   d.DisplayName,
		InputCost:     d.InputCost,
		OutputCost:    d.OutputCost,
		ContextWindow: d.ContextWindow,
		Tier:          d.Tier,
	}
}

func newModelCatalogID() (string, error) {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return "", err
	}
	return "mdl-" + hex.EncodeToString(b[:]), nil
}

func modelCatalogEntryMap(e *pm.ModelCatalogEntry) map[string]any {
	return map[string]any{
		"id":             string(e.ID()),
		"model_id":       e.ModelID(),
		"display_name":   e.DisplayName(),
		"input_cost":     e.InputCost(),
		"output_cost":    e.OutputCost(),
		"context_window": e.ContextWindow(),
		"tier":           e.Tier(),
		"version":        e.Version(),
	}
}

// mapModelCatalogError maps the domain errors to HTTP.
func mapModelCatalogError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrModelCatalogEntryExists):
		writeError(w, http.StatusConflict, "model_catalog_conflict", err.Error())
	case errors.Is(err, pm.ErrModelCatalogEntryNotFound):
		writeError(w, http.StatusNotFound, "model_catalog_not_found", err.Error())
	default:
		mapDomainError(w, err)
	}
}

// --- list_model_catalog_entry ------------------------------------------------

type listModelCatalogReq struct {
	AgentID string `json:"agent_id"`
}

func (s *Server) listModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listModelCatalogReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "model_catalog_not_wired", "")
		return
	}
	entries, err := d.ModelCatalogRepo.ListByOrg(r.Context(), string(a.OrganizationID()))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		items = append(items, modelCatalogEntryMap(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": items})
}

// --- create_model_catalog_entry ----------------------------------------------

type createModelCatalogReq struct {
	AgentID string `json:"agent_id"`
	modelCatalogEntryDTO
}

func (s *Server) createModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createModelCatalogReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "model_catalog_not_wired", "")
		return
	}
	id, err := newModelCatalogID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "id_gen_failed", err.Error())
		return
	}
	e, err := pm.NewModelCatalogEntry(pm.NewModelCatalogEntryInput{
		ID:        pm.ModelCatalogEntryID(id),
		OrgID:     string(a.OrganizationID()),
		Fields:    req.modelCatalogEntryDTO.fields(),
		CreatedBy: pm.IdentityRef(agentActor(a)),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := d.ModelCatalogRepo.Save(r.Context(), e); err != nil {
		mapModelCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, modelCatalogEntryMap(e))
}

// --- update_model_catalog_entry ----------------------------------------------

type updateModelCatalogReq struct {
	AgentID string `json:"agent_id"`
	ID      string `json:"id"`
	modelCatalogEntryDTO
}

func (s *Server) updateModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req updateModelCatalogReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "model_catalog_not_wired", "")
		return
	}
	e, err := s.findOwnedCatalogEntry(w, r, d, a, req.ID)
	if e == nil {
		return // findOwned already wrote the error
	}
	_ = err
	if err := e.Update(req.modelCatalogEntryDTO.fields(), time.Now().UTC()); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := d.ModelCatalogRepo.Update(r.Context(), e); err != nil {
		mapModelCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, modelCatalogEntryMap(e))
}

// --- delete_model_catalog_entry ----------------------------------------------

type deleteModelCatalogReq struct {
	AgentID string `json:"agent_id"`
	ID      string `json:"id"`
}

func (s *Server) deleteModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req deleteModelCatalogReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "model_catalog_not_wired", "")
		return
	}
	e := s.findOwnedCatalogEntryVal(w, r, d, a, req.ID)
	if e == nil {
		return
	}
	if err := d.ModelCatalogRepo.Delete(r.Context(), e.ID()); err != nil {
		mapModelCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": string(e.ID())})
}

// findOwnedCatalogEntry loads an entry by id and verifies it belongs to the agent's
// org (else 404 — no cross-org read/write). Writes the error + returns nil on miss.
func (s *Server) findOwnedCatalogEntry(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, id string) (*pm.ModelCatalogEntry, error) {
	e, err := d.ModelCatalogRepo.FindByID(r.Context(), pm.ModelCatalogEntryID(id))
	if err != nil {
		if errors.Is(err, pm.ErrModelCatalogEntryNotFound) {
			writeError(w, http.StatusNotFound, "model_catalog_not_found", err.Error())
			return nil, err
		}
		mapDomainError(w, err)
		return nil, err
	}
	if e.OrgID() != string(a.OrganizationID()) {
		writeError(w, http.StatusNotFound, "model_catalog_not_found", "not found")
		return nil, pm.ErrModelCatalogEntryNotFound
	}
	return e, nil
}

func (s *Server) findOwnedCatalogEntryVal(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, id string) *pm.ModelCatalogEntry {
	e, _ := s.findOwnedCatalogEntry(w, r, d, a, id)
	return e
}

// --- import_model_catalog ----------------------------------------------------

type importModelCatalogReq struct {
	AgentID string `json:"agent_id"`
	Mode    string `json:"mode"` // "upsert" (default) | "replace"
	// JSON is the raw JSON array of entries. Entries is the already-parsed array
	// (used by the web console); when JSON is non-empty it is parsed and takes
	// precedence, so a malformed JSON string is rejected here (whole batch).
	JSON    string                 `json:"json"`
	Entries []modelCatalogEntryDTO `json:"entries"`
}

func (s *Server) importModelCatalogHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req importModelCatalogReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ModelCatalogRepo == nil {
		writeError(w, http.StatusNotImplemented, "model_catalog_not_wired", "")
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
	// Resolve the entry DTOs: a raw JSON string (parsed here → malformed rejects the
	// whole batch) takes precedence over the structured array.
	dtos := req.Entries
	if req.JSON != "" {
		dtos = nil
		if err := json.Unmarshal([]byte(req.JSON), &dtos); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_import_json", "json must be an array of catalog entries: "+err.Error())
			return
		}
	}
	// Validate the WHOLE batch up front — any invalid field or duplicate model_id
	// rejects the entire import (no half-swallow). Build the domain entries only after
	// every row passes.
	now := time.Now().UTC()
	actor := pm.IdentityRef(agentActor(a))
	orgID := string(a.OrganizationID())
	seen := make(map[string]struct{}, len(dtos))
	entries := make([]*pm.ModelCatalogEntry, 0, len(dtos))
	for i, dto := range dtos {
		id, err := newModelCatalogID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "id_gen_failed", err.Error())
			return
		}
		e, err := pm.NewModelCatalogEntry(pm.NewModelCatalogEntryInput{
			ID: pm.ModelCatalogEntryID(id), OrgID: orgID, Fields: dto.fields(), CreatedBy: actor, CreatedAt: now,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_import", fmt.Sprintf("entry[%d]: %s (whole batch rejected)", i, err.Error()))
			return
		}
		if _, dup := seen[e.ModelID()]; dup {
			writeError(w, http.StatusBadRequest, "invalid_import", fmt.Sprintf("duplicate model_id %q in import (whole batch rejected)", e.ModelID()))
			return
		}
		seen[e.ModelID()] = struct{}{}
		entries = append(entries, e)
	}
	var err error
	if mode == "replace" {
		err = d.ModelCatalogRepo.ReplaceForOrg(r.Context(), orgID, entries)
	} else {
		err = d.ModelCatalogRepo.UpsertForOrg(r.Context(), orgID, entries)
	}
	if err != nil {
		mapModelCatalogError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode, "imported": len(entries)})
}

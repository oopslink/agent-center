package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
)

// v2.7 E1 #138 — Environment domain web surface (org-scoped READS).
//
// The Environment page shows the org's WORKERS from the CONTROL-CONNECTED view:
// these are environment.Worker ARs (created when a worker connects the control
// channel — D1, ADR-0050), carrying control-channel state (status / last-acked
// offset / heartbeat). This is DISTINCT from the Fleet page's workers segment,
// which derives from the legacy workforce.Worker (enrolled set). The two models
// are being converged in the workforce carve-out; until then the UI labels this
// page explicitly as the control-connected view so operators don't expect the
// full enrolled set here.
//
// Agents-on-worker are NOT a new endpoint: the page reuses the already org-scoped
// GET /api/agents (each Agent carries worker_id) and groups client-side.
// File-transfer sessions are slice-2 (#139).

// envWorkerMap serializes an environment.Worker (control-connected view) to JSON.
func envWorkerMap(w *environment.Worker) map[string]any {
	m := map[string]any{
		"worker_id":         string(w.ID()),
		"organization_id":   w.OrganizationID(),
		"name":              w.Name(),
		"status":            string(w.Status()), // online | offline (control-connection state)
		"last_acked_offset": w.LastAckedOffset(),
		"created_at":        w.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":        w.UpdatedAt().Format(time.RFC3339Nano),
		"version":           w.Version(),
	}
	if hb := w.LastHeartbeatAt(); !hb.IsZero() {
		m["last_heartbeat_at"] = hb.Format(time.RFC3339Nano)
	}
	return m
}

// listWorkersHandler serves GET /api/workers — the org's control-connected
// workers. Org-scoped at the source via environment.WorkerRepository.ListByOrg,
// so a caller only ever sees their own org's workers (no cross-org leak).
func (s *Server) listWorkersHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvWorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "env_workers_not_wired", "environment worker repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	workers, err := d.EnvWorkerRepo.ListByOrg(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "env_workers_error", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(workers))
	for _, wk := range workers {
		out = append(out, envWorkerMap(wk))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workers": out})
}

// getWorkerHandler serves GET /api/workers/{id} — one control-connected worker.
// Org isolation is enforced by FETCH-then-CHECK (not a scoped query): the worker
// is fetched by id and a cross-org (or unknown) id returns 404, so an attacker
// cannot probe another org's worker ids. (E-10b hard invariant.)
func (s *Server) getWorkerHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvWorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "env_workers_not_wired", "environment worker repo not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	wk, err := d.EnvWorkerRepo.FindByID(r.Context(), environment.WorkerID(r.PathValue("id")))
	if err != nil || wk.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "worker not found in this organization")
		return
	}
	writeJSON(w, http.StatusOK, envWorkerMap(wk))
}

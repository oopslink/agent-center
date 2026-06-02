package api

import (
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

type toggleCapabilityReq struct {
	Enabled bool `json:"enabled"`
}

// workerSetCapabilityEnabledHandler backs
// PATCH /admin/workforce/worker/{id}/capabilities/{name}/enabled (v2.7 #147 D4).
//
// This is an OPERATOR action: a worker auto-reports detected CLIs (default
// enabled), but enabling/disabling a CLI is an operator policy decision. A
// worker-owned token ("worker:<id>") is therefore rejected 403 — workers may
// report capabilities (POST .../capabilities) but not toggle them.
func (s *Server) workerSetCapabilityEnabledHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerConfigSvc == nil {
		writeError(w, http.StatusNotImplemented, "worker_config_svc_not_wired", "")
		return
	}
	workerID := r.PathValue("id")
	agentCLI := r.PathValue("name")
	if workerID == "" || agentCLI == "" {
		writeError(w, http.StatusBadRequest, "missing_path_param", "worker id + capability name required")
		return
	}
	// Operator-only: reject worker-owned callers.
	if auth, ok := AuthFromContext(r.Context()); ok && strings.HasPrefix(string(auth.Owner), "worker:") {
		writeError(w, http.StatusForbidden, "operator_only",
			"toggling a worker capability is an operator action")
		return
	}
	var req toggleCapabilityReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// Load for the optimistic-lock version.
	wk, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(workerID))
	if err != nil {
		mapDomainError(w, err) // ErrWorkerNotFound → 404
		return
	}
	res, err := d.WorkerConfigSvc.SetCapabilityEnabled(r.Context(), wfservice.SetCapabilityEnabledCommand{
		WorkerID:      workforce.WorkerID(workerID),
		AgentCLI:      agentCLI,
		Enabled:       req.Enabled,
		Version:       wk.Version(),
		ActorIdentity: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err) // ErrWorkerCapabilityNotFound / version conflict
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worker_id": string(res.WorkerID),
		"agent_cli": agentCLI,
		"enabled":   req.Enabled,
		"version":   res.NewVersion,
		"event_id":  string(res.EventID),
	})
}

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/workforce"
)

// =============================================================================
// Environment BC — worker-initiated control channel (v2.7 D1, ADR-0050,
// task #102). ADDITIVE on the existing admin API: these endpoints ride the
// SAME bearer auth + TLS as the legacy /admin/workforce/... worker surface
// (the worker daemon sends Authorization: Bearer <admin-token>, owner
// worker:<id>; AuthMiddleware verifies it before any handler runs). No new
// enrollment / auth is introduced, and the legacy dispatch routes are
// untouched.
//
// These prove the LOG layer: ordered + replayable command stream, cumulative
// ack, per-command idempotency. Process control (executing commands) is D2.
// =============================================================================

// envConnectReq is the body for /admin/environment/worker/connect.
type envConnectReq struct {
	WorkerID string `json:"worker_id"`
}

func (s *Server) envWorkerConnectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	var req envConnectReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "")
		return
	}
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	// Resolve the worker's org from the workforce.Worker the daemon enrolled
	// under. This is org PROVENANCE (org-stamping), NOT a tight Agent↔Worker
	// map: an unknown workforce worker → 404 (the daemon must enroll first).
	wfw, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(req.WorkerID))
	if err != nil {
		mapDomainError(w, err) // workforce.ErrWorkerNotFound → 404
		return
	}
	// v2.7 #148: a worker that enrolled via a bare admin token (not the
	// org-domain install-command path) has no organization_id. Connecting it to
	// the control channel is a missing precondition, not a server fault — reject
	// with a clear 409 instead of letting the empty-org error bubble through
	// ConnectWorker → environment.NewWorker into an opaque 500.
	if wfw.OrganizationID() == "" {
		writeError(w, http.StatusConflict, "worker_not_org_enrolled",
			"worker is not enrolled in an organization; enroll it via the org install command before connecting")
		return
	}
	// v2.7 #140 step-3: org is no longer stored on the control-channel Worker AR.
	// The org-enrolled precondition (409 above) + E-10b are still enforced here
	// from the canonical workforce.Worker; ConnectWorker only manages control state.
	worker, err := d.EnvControlSvc.ConnectWorker(r.Context(), environment.WorkerID(req.WorkerID))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worker_id":         string(worker.ID()),
		"last_acked_offset": worker.LastAckedOffset(),
		"status":            string(worker.Status()),
	})
}

func (s *Server) envWorkerCommandsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	workerID := r.URL.Query().Get("worker_id")
	if workerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "")
		return
	}
	var after int64
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_after", err.Error())
			return
		}
		after = n
	}
	cmds, err := d.EnvControlSvc.CommandsAfter(r.Context(), environment.WorkerID(workerID), after)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(cmds))
	for i, c := range cmds {
		out[i] = controlEventMap(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": out})
}

// envAckReq is the body for /admin/environment/worker/ack.
type envAckReq struct {
	WorkerID string `json:"worker_id"`
	Offset   int64  `json:"offset"`
}

func (s *Server) envWorkerAckHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	var req envAckReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "")
		return
	}
	worker, err := d.EnvControlSvc.AckWorker(r.Context(), environment.WorkerID(req.WorkerID), req.Offset)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worker_id":         string(worker.ID()),
		"last_acked_offset": worker.LastAckedOffset(),
	})
}

// envHeartbeatReq is the body for /admin/environment/worker/heartbeat.
type envHeartbeatReq struct {
	WorkerID string `json:"worker_id"`
}

func (s *Server) envWorkerHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnvControlSvc == nil {
		writeError(w, http.StatusNotImplemented, "env_control_svc_not_wired", "")
		return
	}
	var req envHeartbeatReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "")
		return
	}
	if err := d.EnvControlSvc.Heartbeat(r.Context(), environment.WorkerID(req.WorkerID)); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// controlEventMap projects a WorkerControlEvent to the JSON wire shape. Each
// entry CARRIES its idempotency_key — the Worker (D2) uses it to skip
// re-executing a destructive command seen again after a reconnect.
func controlEventMap(e *environment.WorkerControlEvent) map[string]any {
	return map[string]any{
		"id":              e.ID(),
		"offset":          e.Offset(),
		"idempotency_key": e.IdempotencyKey(),
		"command_type":    e.CommandType(),
		"payload":         e.Payload(),
		"created_at":      e.CreatedAt().Format(time.RFC3339Nano),
	}
}

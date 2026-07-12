package api

import (
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	admintokensvc "github.com/oopslink/agent-center/internal/admintoken/service"
	"github.com/oopslink/agent-center/internal/concurrency"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// workerLongTermTokenScopes is the scope set minted for a worker's
// post-enroll long-term bearer. Mirrors every admin endpoint the
// worker daemon exercises during its main loop (heartbeat, dispatch
// + kill pull, exec report, secret resolve, blob put). Kept explicit
// (rather than `*`) so a leaked worker token can't escalate to e.g.
// admin:token operations.
var workerLongTermTokenScopes = []admintoken.Scope{
	"workforce:enroll", // heartbeat / re-enroll
	"dispatch:pull",    // /admin/dispatch/queue/pull
	"task:*",           // exec/notify-working, report-success, etc.
	"secret:resolve",   // /admin/secret/user-secret/resolve
	"blob:put",         // /admin/blob/put
}

// =============================================================================
// EnrollSvc — POST /admin/workforce/worker/enroll
// =============================================================================

type enrollReq struct {
	WorkerID     string   `json:"worker_id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

func (s *Server) workerEnrollHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnrollSvc == nil {
		writeError(w, http.StatusNotImplemented, "enroll_svc_not_wired", "")
		return
	}
	var req enrollReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := d.EnrollSvc.Enroll(r.Context(), wfservice.EnrollCommand{
		WorkerID:      workforce.WorkerID(req.WorkerID),
		Name:          req.Name,
		Capabilities:  req.Capabilities,
		ActorIdentity: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	// v2.4-D-X1 fix B5: mint a long-term worker token so the daemon
	// can stop using its one-time enroll token (which the
	// AuthMiddleware already burned during this request). Returned
	// once in the response; the worker persists it locally and uses
	// it for every subsequent admin call (heartbeat, pull, report).
	resp := map[string]any{
		"worker_id": string(res.WorkerID),
		"event_id":  string(res.EventID),
		"version":   res.Version,
	}
	if d.AdminTokenSvc != nil {
		tokRes, terr := d.AdminTokenSvc.Create(r.Context(), admintokensvc.CreateCommand{
			Owner:     admintoken.Owner("worker:" + req.WorkerID),
			Scopes:    workerLongTermTokenScopes,
			CreatedBy: "workforce.enroll",
		})
		if terr != nil {
			// Don't leak token-mint failures up as 5xx — the enroll
			// itself already committed. Surface as a partial response
			// so the daemon can fail loudly with a clear diagnostic
			// instead of silently 401-looping on the burnt enroll
			// token.
			resp["admin_token_error"] = terr.Error()
		} else {
			resp["admin_token"] = tokRes.Plaintext
			resp["admin_token_id"] = string(tokRes.ID)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// renameReq is the body for /admin/workforce/worker/rename
// (v2.4-D-X1 @oopslink ask). worker_id identifies the target; name
// is the new friendly label (non-empty after trim).
type renameReq struct {
	WorkerID string `json:"worker_id"`
	Name     string `json:"name"`
}

func (s *Server) workerRenameHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnrollSvc == nil {
		writeError(w, http.StatusNotImplemented, "enroll_svc_not_wired", "")
		return
	}
	var req renameReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.EnrollSvc.Rename(r.Context(), wfservice.RenameCommand{
		WorkerID: workforce.WorkerID(req.WorkerID),
		Name:     req.Name,
		Actor:    d.Actor,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"worker_id": req.WorkerID, "name": req.Name})
}

// v2.3-1 (task #24): dedicated heartbeat endpoint. Replaces the v2.2
// worker-daemon hack that re-called /enroll per tick and swallowed
// the 409 already_exists as "alive". With this endpoint the daemon
// asserts liveness cleanly; the 409 path collapses to a real 200.
type heartbeatReq struct {
	WorkerID                 string `json:"worker_id"`
	AdditionalWorkingSeconds int64  `json:"additional_working_seconds"`
	// AgentConcurrencySnapshots is the optional v2.19.0 per-agent live executor view
	// (agent_id → snapshot). Absent from pre-v2.19 workers → no live state written
	// (back-compat: the field is purely additive and a missing field is not an error).
	AgentConcurrencySnapshots map[string]concurrency.AgentSnapshot `json:"agent_concurrency_snapshots,omitempty"`
}

func (s *Server) workerHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnrollSvc == nil {
		writeError(w, http.StatusNotImplemented, "enroll_svc_not_wired", "")
		return
	}
	var req heartbeatReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.EnrollSvc.Heartbeat(r.Context(), wfservice.HeartbeatCommand{
		WorkerID:                 workforce.WorkerID(req.WorkerID),
		AdditionalWorkingSeconds: req.AdditionalWorkingSeconds,
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	// v2.19.0: record the per-agent live executor snapshots (when the store is wired
	// and the worker sent any). Best-effort + after the liveness write — a snapshot is
	// transient view state, never a reason to fail the heartbeat.
	if d.LiveState != nil && len(req.AgentConcurrencySnapshots) > 0 {
		now := time.Now()
		for agentID, snap := range req.AgentConcurrencySnapshots {
			d.LiveState.Put(agentID, snap, now)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"worker_id": req.WorkerID})
}

// =============================================================================
// ProjectRepo — FindAll / FindByID
// =============================================================================

// projectFindAllHandler is the operator/admin-token project list. v2.7 #131
// PR-3: repointed from the retired workforce.Project model to the new
// pm.Project model via the operator-global ListAll (no org filter — these
// admin find-* endpoints are operator-scoped, A9-consistent global-visible).
func (s *Server) projectFindAllHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PMProjectRepo == nil {
		writeError(w, http.StatusNotImplemented, "project_repo_not_wired", "")
		return
	}
	list, err := d.PMProjectRepo.ListAll(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, p := range list {
		out[i] = pmProjectMap(p)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) projectFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PMProjectRepo == nil {
		writeError(w, http.StatusNotImplemented, "project_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	p, err := d.PMProjectRepo.FindByID(r.Context(), pm.ProjectID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pmProjectMap(p))
}

// =============================================================================
// WorkerRepo — FindAll / FindByID / FindByStatus
// =============================================================================

func (s *Server) workerFindAllHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	list, err := d.WorkerRepo.FindAll(r.Context())
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, ww := range list {
		out[i] = workerMap(ww)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) workerFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	ww, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workerMap(ww))
}

func (s *Server) workerFindByStatusHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.WorkerRepo == nil {
		writeError(w, http.StatusNotImplemented, "worker_repo_not_wired", "")
		return
	}
	st := r.URL.Query().Get("status")
	if st == "" {
		writeError(w, http.StatusBadRequest, "missing_status", "")
		return
	}
	list, err := d.WorkerRepo.FindByStatus(r.Context(), workforce.WorkerStatus(st))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, ww := range list {
		out[i] = workerMap(ww)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// Projection helpers
// =============================================================================

func workerMap(w *workforce.Worker) map[string]any {
	m := map[string]any{
		"worker_id":    string(w.ID()),
		"status":       string(w.Status()),
		"capabilities": w.Capabilities(),
		"version":      w.Version(),
		"enrolled_at":  w.EnrolledAt().Format(time.RFC3339Nano),
	}
	if hb := w.LastHeartbeatAt(); hb != nil {
		m["last_heartbeat_at"] = hb.Format(time.RFC3339Nano)
	}
	return m
}

// pmProjectMap projects the new pm.Project model for the operator/admin-token
// project find-* responses (v2.7 #131 PR-3). The CLI Client's ProjectDTO is
// the live consumer: keys mirror that DTO. Tags dropped (pm.Project has none);
// organization_id surfaced.
func pmProjectMap(p *pm.Project) map[string]any {
	return map[string]any{
		"id":              string(p.ID()),
		"name":            p.Name(),
		"description":     p.Description(),
		"organization_id": p.OrganizationID(),
		"version":         p.Version(),
		"created_at":      p.CreatedAt().Format(time.RFC3339Nano),
	}
}

package api

import (
	"net/http"

	"github.com/oopslink/agent-center/internal/workforce"
	wfservice "github.com/oopslink/agent-center/internal/workforce/service"
)

// reportCapabilitiesReq is the worker's online-time probe upload (v2.7 #147).
// Capabilities ride as the rich workforce.Capability shape (D2) so probe
// version + feature flags survive the wire — never downgraded to bare names.
type reportCapabilitiesReq struct {
	WorkerID     string                 `json:"worker_id"`
	Capabilities []workforce.Capability `json:"capabilities"`
	// SystemInfo is the worker-reported host + build identity (T752). Optional
	// and additive — a pre-T752 worker omits it and the field stays zero.
	SystemInfo workforce.SystemInfo `json:"system_info"`
}

// workerReportCapabilitiesHandler backs POST /admin/workforce/worker/capabilities.
// The worker daemon calls this on every online after ProbeAllAdapters.
//
// §-1①: a worker may only report ITS OWN capabilities. The caller's long-term
// token Owner is "worker:<id>" (minted at enroll); we require it to equal
// "worker:"+body.worker_id, else 403 — no cross-worker capability overwrite.
func (s *Server) workerReportCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EnrollSvc == nil {
		writeError(w, http.StatusNotImplemented, "enroll_svc_not_wired", "")
		return
	}
	var req reportCapabilitiesReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.WorkerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "")
		return
	}
	// §-1① ownership: caller token must own this worker.
	auth, ok := AuthFromContext(r.Context())
	if !ok || string(auth.Owner) != "worker:"+req.WorkerID {
		writeError(w, http.StatusForbidden, "worker_mismatch",
			"a worker may only report its own capabilities")
		return
	}
	res, err := d.EnrollSvc.ReportCapabilities(r.Context(), wfservice.ReportCapabilitiesCommand{
		WorkerID:      workforce.WorkerID(req.WorkerID),
		Capabilities:  req.Capabilities,
		SystemInfo:    req.SystemInfo,
		ActorIdentity: d.Actor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"worker_id": string(res.WorkerID),
		"version":   res.NewVersion,
		"event_id":  string(res.EventID),
	})
}

package api

import (
	"net/http"

	"github.com/oopslink/agent-center/internal/admin/dispatchq"
)

// Dispatch queue endpoints (v2.2-A3 ↔ Phase C worker daemon).
//
// Worker daemons drain via these endpoints to discover pending
// dispatches + kill requests addressed to their worker_id. The center
// stays the system of record — the queue is fire-and-forget once the
// worker pulls (worker reports ack/nack via separate
// /admin/taskruntime/exec/report-* endpoints, which DispatchService
// reads back as the canonical state machine input).

func (s *Server) dispatchQueuePullHandler(w http.ResponseWriter, r *http.Request) {
	if s.deps.Queue == nil {
		writeError(w, http.StatusNotImplemented, "queue_not_wired", "")
		return
	}
	workerID := r.URL.Query().Get("worker_id")
	if workerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "worker_id query param required")
		return
	}
	pending := s.deps.Queue.DrainDispatches(workerID)
	writeJSON(w, http.StatusOK, pending)
}

func (s *Server) killQueuePullHandler(w http.ResponseWriter, r *http.Request) {
	if s.deps.Queue == nil {
		writeError(w, http.StatusNotImplemented, "queue_not_wired", "")
		return
	}
	// v2.2 semantic: workers pull the unrouted slot ("") and filter
	// kills against their own claimed executions on the daemon side.
	// See dispatchq.KillSender for the rationale (TaskExecutionID
	// → worker_id resolution lives on the daemon).
	pending := s.deps.Queue.AllKills()
	writeJSON(w, http.StatusOK, pending)
}

// queuePeekHandler is an observability helper — returns the pending
// counts per worker. Useful for `agent-center ps` style debug + for
// e2e tests asserting "did the dispatch land in the queue".
func (s *Server) queuePeekHandler(w http.ResponseWriter, r *http.Request) {
	if s.deps.Queue == nil {
		writeError(w, http.StatusNotImplemented, "queue_not_wired", "")
		return
	}
	workerID := r.URL.Query().Get("worker_id")
	if workerID == "" {
		writeError(w, http.StatusBadRequest, "missing_worker_id", "worker_id query param required")
		return
	}
	d, k := s.deps.Queue.Pending(workerID)
	writeJSON(w, http.StatusOK, map[string]any{
		"worker_id":      workerID,
		"dispatch_count": d,
		"kill_count":     k,
	})
}

// dispatchq is intentionally referenced once to satisfy import; the
// real handlers use s.deps.Queue (typed as *dispatchq.Queue). Keeping
// the package alias prevents future "unused import" surprises if
// someone refactors the handlers above.
var _ = (*dispatchq.Queue)(nil)

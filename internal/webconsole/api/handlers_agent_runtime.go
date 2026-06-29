package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/runtimefs"
	"github.com/oopslink/agent-center/internal/workforce"
)

// handlers_agent_runtime.go (issue-921db054 / I5) — the org-scoped, READ-ONLY Agent
// Runtime file browser endpoints:
//
//	GET /api/orgs/{slug}/agents/{id}/runtime/list?path=
//	GET /api/orgs/{slug}/agents/{id}/runtime/read?path=
//	GET /api/orgs/{slug}/agents/{id}/runtime/gitlog?path=memory&limit=
//
// There is NO reverse RPC into a worker, so each call rides the EXISTING control-loop:
// resolve the agent (org-member gated) → if its worker is offline return {unavailable}
// at once → otherwise enqueue an `agent.runtime_fs` command and BLOCK on the worker's
// correlated feedback reply (by req_id, via the shared RuntimeFsDispatcher) up to a
// short timeout. On timeout the call also degrades to {unavailable} (never hangs the
// page). The worker builds the FE-facing DTO; the Center passes it through verbatim.

// runtimeFsRequestTimeout bounds how long a runtime read waits for the worker's reply
// before degrading to {unavailable, reason:"timeout"} (the design's 3s budget).
const runtimeFsRequestTimeout = 3 * time.Second

func (s *Server) agentRuntimeListHandler(w http.ResponseWriter, r *http.Request) {
	s.agentRuntimeOp(w, r, runtimefs.OpList)
}

func (s *Server) agentRuntimeReadHandler(w http.ResponseWriter, r *http.Request) {
	s.agentRuntimeOp(w, r, runtimefs.OpRead)
}

func (s *Server) agentRuntimeGitlogHandler(w http.ResponseWriter, r *http.Request) {
	s.agentRuntimeOp(w, r, runtimefs.OpGitLog)
}

// agentRuntimeOp is the shared request→correlated-response flow for the three ops.
func (s *Server) agentRuntimeOp(w http.ResponseWriter, r *http.Request, op string) {
	d := hd(r)
	// Org-member gate + agent-in-org (cross-org → 404).
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}

	// Availability FIRST (cheap, no transport needed): an archived/unbound agent or an
	// offline worker degrades to {unavailable} immediately — no point enqueuing a
	// command no worker will ever pull.
	workerID := strings.TrimSpace(a.WorkerID())
	if workerID == "" {
		writeRuntimeUnavailable(w, "agent_not_on_worker")
		return
	}
	if d.WorkerRepo == nil {
		writeRuntimeUnavailable(w, "not_wired")
		return
	}
	wk, err := d.WorkerRepo.FindByID(r.Context(), workforce.WorkerID(workerID))
	if err != nil || wk == nil || wk.Status() != workforce.WorkerOnline {
		writeRuntimeUnavailable(w, "worker_offline")
		return
	}

	// Online → the read needs the control-loop transport.
	if d.EnvControl == nil || d.RuntimeFsDispatcher == nil {
		writeRuntimeUnavailable(w, "not_wired")
		return
	}

	reqID, err := newRuntimeReqID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil {
			limit = n
		}
	}
	cmd := runtimefs.Command{
		ReqID:   reqID,
		AgentID: string(a.ID()),
		Op:      op,
		Path:    r.URL.Query().Get("path"),
		Limit:   limit,
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	// Register the correlation slot BEFORE enqueuing so a fast worker reply can never
	// race ahead of the waiter.
	ch, release := d.RuntimeFsDispatcher.Register(reqID)
	defer release()

	if _, err := d.EnvControl.EnqueueCommand(r.Context(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    runtimefs.CommandType,
		Payload:        string(payload),
		IdempotencyKey: reqID, // unique per request → never collapsed by the log's dedup
	}); err != nil {
		writeRuntimeUnavailable(w, "enqueue_failed")
		return
	}

	select {
	case resp := <-ch:
		if resp.Code != "" {
			writeRuntimeOpError(w, resp.Code, resp.Message)
			return
		}
		// Pass the worker-built DTO through verbatim (the Center adds nothing).
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp.Result)
	case <-time.After(runtimeFsRequestTimeout):
		writeRuntimeUnavailable(w, "timeout")
	case <-r.Context().Done():
		// Client disconnected — nothing to write.
	}
}

// writeRuntimeUnavailable renders the {unavailable:true, reason} degrade DTO (HTTP 200
// — the FE checks the flag, not the status, and shows the "Runtime unavailable" panel).
func writeRuntimeUnavailable(w http.ResponseWriter, reason string) {
	writeJSON(w, http.StatusOK, map[string]any{"unavailable": true, "reason": reason})
}

// writeRuntimeOpError maps a worker op-error code to an HTTP status. These are
// defense/edge cases (the FE drives valid paths); a path escape is a hard 403.
func writeRuntimeOpError(w http.ResponseWriter, code, msg string) {
	status := http.StatusBadRequest
	switch code {
	case runtimefs.ErrCodePathEscape:
		status = http.StatusForbidden
	case runtimefs.ErrCodeNotFound:
		status = http.StatusNotFound
	case runtimefs.ErrCodeInternal:
		status = http.StatusInternalServerError
	}
	writeError(w, status, code, msg)
}

// newRuntimeReqID mints a random 128-bit hex correlation id (uniqueness is all that's
// required; it is also the command's idempotency key).
func newRuntimeReqID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

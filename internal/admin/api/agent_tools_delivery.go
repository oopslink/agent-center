package api

import (
	"net/http"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// report_delivery (issue-f30b7e7b) — the worker-initiated per-executor delivery-status
// ingest.
//
// WHO CALLS IT. Like report_usage, this is a WORKER-initiated agent-tool, not an
// LLM-facing one: the worker's CenterWriteback probes git when a forked executor
// finishes and POSTs the terminal FinalizedGitStatus here. The model never calls it, so
// it is deliberately NOT in the agent-facing MCP set. Auth rides the standard
// worker-bearer + agent-binding gate (requireAgentOnWorker).
//
// WHAT IT DOES. Persists the git status onto the task (pm.Task.Delivery) so the
// writeback auto-block (B②) and audit can tell a durable pushed delivery (Probed &&
// Pushed) from a zero-delivery run (committed-but-not-pushed / no-commit) that must be
// auto-blocked rather than re-nudged. Best-effort by design: the worker swallows any
// non-2xx, so a lost signal degrades to "no delivery" (the safe side) and never breaks
// the agent loop.

// reportDeliveryReq is the body for POST /admin/agent-tools/report_delivery. git carries
// the verbatim 9 FinalizedGitStatus fields; project_id is derived from the task, never
// trusted from the wire.
type reportDeliveryReq struct {
	AgentID string          `json:"agent_id"`
	TaskID  string          `json:"task_id"`
	Git     *deliveryGitReq `json:"git"`
}

// deliveryGitReq mirrors agentruntime executor.FinalizedGitStatus (9 fields verbatim —
// push_error is the 9th, added when the eager supervisor-push failed; it MUST be relayed so
// the DURABLE center-side Task.Delivery records WHY a delivery was not pushed, not just the
// live conversation/logs).
type deliveryGitReq struct {
	Branch      string `json:"branch"`
	HeadSHA     string `json:"head_sha"`
	Dirty       bool   `json:"dirty"`
	Pushed      bool   `json:"pushed"`
	Probed      bool   `json:"probed"`
	BaseRef     string `json:"base_ref"`
	BaseKnown   bool   `json:"base_known"`
	AheadOfBase int    `json:"ahead_of_base"`
	PushError   string `json:"push_error"`
}

func (s *Server) reportDeliveryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req reportDeliveryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}

	// nil git → nil delivery (never-reported / probe absent): a valid best-effort no-op
	// signal, still recorded as "no delivery" (the safe side).
	var delivery *pm.Delivery
	if g := req.Git; g != nil {
		delivery = &pm.Delivery{
			Branch:      g.Branch,
			HeadSHA:     g.HeadSHA,
			Dirty:       g.Dirty,
			Pushed:      g.Pushed,
			Probed:      g.Probed,
			BaseRef:     g.BaseRef,
			BaseKnown:   g.BaseKnown,
			AheadOfBase: g.AheadOfBase,
			PushError:   g.PushError,
		}
	}
	if err := d.PMService.RecordDelivery(r.Context(), pm.TaskID(taskID), pm.IdentityRef(agentActor(a)), delivery); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task_id": taskID})
}

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
// the verbatim FinalizedGitStatus fields; project_id is derived from the task, never
// trusted from the wire.
type reportDeliveryReq struct {
	AgentID    string          `json:"agent_id"`
	TaskID     string          `json:"task_id"`
	Source     string          `json:"source"`
	ExecutorID string          `json:"executor_id"`
	Worktree   string          `json:"worktree"`
	Evidence   string          `json:"evidence"`
	Reason     string          `json:"reason"`
	Git        *deliveryGitReq `json:"git"`
}

// deliveryGitReq mirrors agentruntime executor.FinalizedGitStatus (fields verbatim —
// push_error is the 9th, added when the eager supervisor-push failed; it MUST be relayed so
// the DURABLE center-side Task.Delivery records WHY a delivery was not pushed, not just the
// live conversation/logs).
type deliveryGitReq struct {
	Branch      string   `json:"branch"`
	HeadSHA     string   `json:"head_sha"`
	Dirty       bool     `json:"dirty"`
	DirtyPaths  []string `json:"dirty_paths"`
	Worktree    string   `json:"worktree"`
	Pushed      bool     `json:"pushed"`
	Probed      bool     `json:"probed"`
	BaseRef     string   `json:"base_ref"`
	BaseKnown   bool     `json:"base_known"`
	AheadOfBase int      `json:"ahead_of_base"`
	PushError   string   `json:"push_error"`
}

func (s *Server) reportDeliveryHandler(w http.ResponseWriter, r *http.Request) {
	var req reportDeliveryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	s.recordDeliveryFromRequest(w, r, req)
}

func (s *Server) reportManualRecoveryDeliveryHandler(w http.ResponseWriter, r *http.Request) {
	var req reportDeliveryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	req.Source = "manual_recovery"
	s.recordDeliveryFromRequest(w, r, req)
}

func (s *Server) recordDeliveryFromRequest(w http.ResponseWriter, r *http.Request, req reportDeliveryReq) {
	d := hd(r)
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
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "executor"
	}
	if source != "executor" && source != "manual_recovery" {
		writeError(w, http.StatusBadRequest, "invalid_delivery_source", "delivery source must be executor or manual_recovery")
		return
	}
	if source == "manual_recovery" {
		if strings.TrimSpace(req.Reason) == "" {
			writeError(w, http.StatusBadRequest, "missing_manual_recovery_reason", "manual recovery delivery requires reason")
			return
		}
		if strings.TrimSpace(req.Evidence) == "" {
			writeError(w, http.StatusBadRequest, "missing_manual_recovery_evidence", "manual recovery delivery requires test/evidence summary")
			return
		}
		if req.Git == nil {
			writeError(w, http.StatusBadRequest, "missing_manual_recovery_git", "manual recovery delivery requires git delivery snapshot")
			return
		}
	}

	// nil git → nil delivery (never-reported / probe absent): a valid best-effort no-op
	// signal, still recorded as "no delivery" (the safe side).
	var delivery *pm.Delivery
	if g := req.Git; g != nil {
		delivery = &pm.Delivery{
			Branch:      g.Branch,
			HeadSHA:     g.HeadSHA,
			Dirty:       g.Dirty,
			DirtyPaths:  append([]string(nil), g.DirtyPaths...),
			Pushed:      g.Pushed,
			Probed:      g.Probed,
			BaseRef:     g.BaseRef,
			BaseKnown:   g.BaseKnown,
			AheadOfBase: g.AheadOfBase,
			PushError:   g.PushError,
			Source:      source,
			ExecutorID:  strings.TrimSpace(req.ExecutorID),
			Worktree:    firstNonEmptyString(strings.TrimSpace(req.Worktree), strings.TrimSpace(g.Worktree)),
			Evidence:    strings.TrimSpace(req.Evidence),
			Reason:      strings.TrimSpace(req.Reason),
		}
	}
	if err := d.PMService.RecordDelivery(r.Context(), pm.TaskID(taskID), pm.IdentityRef(agentActor(a)), delivery); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task_id": taskID})
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

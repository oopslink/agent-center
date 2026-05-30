package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentservice "github.com/oopslink/agent-center/internal/agent/service"
)

// =============================================================================
// Environment BC — controller→center feedback (v2.7 D2-c-i, ADR-0049/0050).
//
// The daemon AgentController (D2-c-ii, future) has NO DB. It reports back to the
// center via these ADDITIVE admin HTTP endpoints under /admin/environment/agent/.
// They are worker/daemon-facing (like the D1 /admin/environment/worker/* control
// endpoints) — NOT the agent MCP tool surface. Each is gated by the SAME
// per-agent guardrail the agent tools use (requireAgentOnWorker): the worker is
// taken from the TOKEN OWNER, never the body, and the target Agent MUST be bound
// to that worker, else 403.
//
// CRITICAL loop-avoidance (lifecycle-feedback): these are RESULT feedback, not
// intent changes. They MUST NOT emit agent.lifecycle_changed — that event is
// consumed by the Environment AgentControlProjector, which would enqueue a NEW
// reconcile command (feedback loop). The AppService methods invoked here
// (MarkAgentStopped / MarkAgentError) are PERSIST-ONLY (no outbox emit).
//
// D2-c-i is additive plumbing: nothing is activated (the daemon controller is
// D2-c-ii; execution cutover is D2-f). The legacy taskruntime path is untouched.
// =============================================================================

// agentActivityReq is the body for POST /admin/environment/agent/activity.
type agentActivityReq struct {
	AgentID        string `json:"agent_id"`
	EventType      string `json:"event_type"`
	Payload        string `json:"payload"`
	WorkItemRef    string `json:"work_item_ref,omitempty"`
	InteractionRef string `json:"interaction_ref,omitempty"`
	OccurredAt     string `json:"occurred_at,omitempty"`
}

// envAgentActivityHandler is the stdout→activity sink: it appends an
// AgentActivityEvent (observation only — it does NOT post to any Conversation).
func (s *Server) envAgentActivityHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req agentActivityReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.EventType) == "" {
		writeError(w, http.StatusBadRequest, "missing_event_type", "")
		return
	}
	occurredAt, err := parseOptionalTime(req.OccurredAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_occurred_at", err.Error())
		return
	}
	id, err := d.AgentSvc.AppendActivity(r.Context(), agent.NewActivityEventInput{
		AgentID:        a.ID(),
		WorkItemRef:    req.WorkItemRef,
		InteractionRef: req.InteractionRef,
		EventType:      req.EventType,
		Payload:        req.Payload,
		OccurredAt:     occurredAt, // zero → AppService stamps clock.Now()
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// agentLifecycleFeedbackReq is the body for
// POST /admin/environment/agent/lifecycle-feedback.
type agentLifecycleFeedbackReq struct {
	AgentID string `json:"agent_id"`
	State   string `json:"state"` // "stopped" | "error"
	Error   string `json:"error,omitempty"`
	At      string `json:"at,omitempty"`
}

// envAgentLifecycleFeedbackHandler records controller-reported lifecycle RESULT
// feedback. PERSIST-ONLY via the AppService — it MUST NOT emit
// agent.lifecycle_changed (that would re-trigger the reconcile projector).
func (s *Server) envAgentLifecycleFeedbackHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req agentLifecycleFeedbackReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	at, err := parseOptionalTime(req.At)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_at", err.Error())
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	switch strings.ToLower(strings.TrimSpace(req.State)) {
	case "stopped":
		err = d.AgentSvc.MarkAgentStopped(r.Context(), a.ID(), at)
	case "error":
		err = d.AgentSvc.MarkAgentError(r.Context(), a.ID(), req.Error, at)
	default:
		writeError(w, http.StatusBadRequest, "invalid_state",
			"state must be 'stopped' or 'error'")
		return
	}
	if err != nil {
		mapDomainError(w, err) // ErrIllegalLifecycle → 409
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// agentWorkItemStateReq is the body for
// POST /admin/environment/agent/work-item-state.
type agentWorkItemStateReq struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
	State      string `json:"state"` // "active" | "done" | "failed"
	At         string `json:"at,omitempty"`
}

// envAgentWorkItemStateHandler applies a controller-reported WorkItem
// transition (active/done/failed). The AppService verifies the WorkItem belongs
// to the agent (ownership guardrail) and the AR rejects illegal transitions
// (→ 409). PERSIST-ONLY (the WorkItem AR emits no outbox event).
func (s *Server) envAgentWorkItemStateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req agentWorkItemStateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.WorkItemID) == "" {
		writeError(w, http.StatusBadRequest, "missing_work_item_id", "")
		return
	}
	at, err := parseOptionalTime(req.At)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_at", err.Error())
		return
	}
	var state agentservice.WorkItemFeedbackState
	switch strings.ToLower(strings.TrimSpace(req.State)) {
	case "active":
		state = agentservice.WorkItemFeedbackActive
	case "done":
		state = agentservice.WorkItemFeedbackDone
	case "failed":
		state = agentservice.WorkItemFeedbackFailed
	default:
		writeError(w, http.StatusBadRequest, "invalid_state",
			"state must be 'active', 'done' or 'failed'")
		return
	}
	if err := d.AgentSvc.MarkWorkItemState(r.Context(), a.ID(), req.WorkItemID, state, at); err != nil {
		mapDomainError(w, err) // illegal move / not-owned → 409 / 404
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// parseOptionalTime parses an optional RFC3339 timestamp. An empty string
// yields the zero time (callers treat it as "use server clock").
func parseOptionalTime(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

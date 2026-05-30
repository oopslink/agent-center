package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentservice "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
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

// agentMarkSeenReq is the body for POST /admin/environment/agent/mark-seen.
type agentMarkSeenReq struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	At             string `json:"at,omitempty"`
}

// envAgentMarkSeenHandler MONOTONICALLY advances the agent participant's
// read-state cursor in a task conversation to message_id (v2.7 D2-e-ii / OQ5).
// The controller calls this after a wake inject so the next batch flush won't
// re-deliver the messages it already injected. Reuses the Conversation
// ReadStateService.MarkSeen (only-forward: an older/equal id is a no-op; absent
// row → insert). Same per-agent guardrail as the other feedback endpoints
// (requireAgentOnWorker — worker from the token owner, target agent bound to it).
func (s *Server) envAgentMarkSeenHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req agentMarkSeenReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ReadStateSvc == nil {
		writeError(w, http.StatusNotImplemented, "read_state_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ConversationID) == "" {
		writeError(w, http.StatusBadRequest, "missing_conversation_id", "")
		return
	}
	if strings.TrimSpace(req.MessageID) == "" {
		writeError(w, http.StatusBadRequest, "missing_message_id", "")
		return
	}
	participant := conversation.IdentityRef("agent:" + string(a.ID()))
	if _, err := d.ReadStateSvc.MarkSeen(r.Context(), convservice.MarkSeenCommand{
		UserID:            participant,
		ConversationID:    conversation.ConversationID(req.ConversationID),
		LastSeenMessageID: conversation.MessageID(req.MessageID),
		Actor:             observability.Actor(participant),
	}); err != nil {
		mapDomainError(w, err) // message-not-in-conv → 422; not found → 404
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

// =============================================================================
// Worker boot-resume state (v2.7 D2-f s4, ADR-0049/0050). The boot half of the
// execution cutover: when a worker daemon (re)starts with the control-stream
// path active, it asks the center "which agents on THIS worker should be running
// + their in-flight WorkItems" and reconciles their claude sessions (re-attach a
// survivor / relaunch a dead one / stop an unwanted one). This preserves
// worker/agent lifecycle independence — a worker restart does NOT lose running
// agents.
//
// 🔴 AUTHZ (worker-level, NOT per-agent): the worker is taken from the
// AUTHENTICATED token owner (worker:<id>), and the body worker_id MUST equal it,
// else 403 — a worker may only ask about ITSELF (no cross-worker leak). Same
// security spine as requireAgentOnWorker, but worker-scoped: resolve the worker
// from the token, never trust the body beyond an equality check.
//
// ADDITIVE + DORMANT: the daemon only calls this when the control loop is active
// (the D2-f cutover flag, default off). Nothing is activated by default.
// =============================================================================

// resumeStateReq is the body for POST /admin/environment/worker/resume-state.
type resumeStateReq struct {
	WorkerID string `json:"worker_id"`
}

// envWorkerResumeStateHandler returns this worker's resumable agents + their
// in-flight WorkItems so the daemon can reconcile their claude sessions on boot.
//
// AUTHZ: worker = token owner (strip `worker:` prefix; non-worker owner → 403);
// body.worker_id MUST == that worker, else 403 (worker_mismatch).
//
// Computed set: AgentRepo.ListByWorker(worker) → for each agent, include it iff
// it SHOULD be running (lifecycle == running) OR it has ≥1 in-flight WorkItem
// (status ∈ {active, waiting_input}); a stopped/stopping/resetting/error agent
// with no in-flight work is SKIPPED (nothing to resume). Each included agent
// carries desired_lifecycle + version (+ reset_scope reserved for f-3) and its
// in-flight WorkItems only.
func (s *Server) envWorkerResumeStateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentRepo == nil || d.AgentWorkItemRepo == nil {
		writeError(w, http.StatusNotImplemented, "agent_repo_not_wired", "")
		return
	}
	auth, ok := AuthFromContext(r.Context())
	if !ok || !strings.HasPrefix(string(auth.Owner), "worker:") {
		writeError(w, http.StatusForbidden, "not_a_worker_token",
			"resume-state requires a worker:<id> bearer")
		return
	}
	worker := strings.TrimPrefix(string(auth.Owner), "worker:")
	if worker == "" {
		writeError(w, http.StatusForbidden, "not_a_worker_token",
			"worker token has empty worker id")
		return
	}
	var req resumeStateReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// 🔴 Only-ask-self: the body worker_id must equal the authenticated worker.
	if strings.TrimSpace(req.WorkerID) != worker {
		writeError(w, http.StatusForbidden, "worker_mismatch",
			"a worker may only query its own resume-state")
		return
	}

	agents, err := d.AgentRepo.ListByWorker(r.Context(), worker)
	if err != nil {
		mapDomainError(w, err)
		return
	}

	out := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		items, err := d.AgentWorkItemRepo.ListByAgent(r.Context(), a.ID())
		if err != nil {
			mapDomainError(w, err)
			return
		}
		inflight := make([]map[string]any, 0, len(items))
		for _, it := range items {
			switch it.Status() {
			case agent.WorkItemActive, agent.WorkItemWaitingInput:
				inflight = append(inflight, map[string]any{
					"work_item_id": it.ID(),
					"task_ref":     it.TaskRef(),
					"status":       string(it.Status()),
				})
			}
		}
		// Include the agent only if it SHOULD be running OR it has in-flight work.
		// A stopped agent with no in-flight WorkItem has nothing to resume → skip.
		if a.Lifecycle() != agent.LifecycleRunning && len(inflight) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"agent_id":          string(a.ID()),
			"desired_lifecycle": string(a.Lifecycle()),
			"version":           a.Version(),
			"reset_scope":       "", // reserved for f-3 (rollback/reset semantics)
			"work_items":        inflight,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

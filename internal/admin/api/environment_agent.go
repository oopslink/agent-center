package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/observability"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/runtimefs"
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
// (MarkAgentRecovered / MarkAgentStopped / MarkAgentError / MarkAgentFailed) are
// PERSIST-ONLY (no outbox emit).
//
// D2-c-i is additive plumbing: nothing is activated (the daemon controller is
// D2-c-ii; execution cutover is D2-f). The legacy taskruntime path is untouched.
// =============================================================================

// agentActivityReq is the body for POST /admin/environment/agent/activity.
type agentActivityReq struct {
	AgentID        string `json:"agent_id"`
	EventType      string `json:"event_type"`
	Payload        string `json:"payload"`
	TaskRef        string `json:"task_ref,omitempty"`
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
		TaskRef:        req.TaskRef,
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
	State   string `json:"state"` // "running" (recovery) | "stopped" | "error" | "failed"
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
	case "running":
		// issue I13 auto-recovery: the daemon reports a CRASHED (error) agent's session
		// is back up (boot reattach/relaunch or mid-run self-heal) → clear error →
		// running so the agent is AVAILABLE for dispatch again + the UI stops showing it
		// crashed. NO-OP on any non-error state (MarkAgentRecovered), so it can never
		// resurrect a deliberately-stopped or terminal agent.
		err = d.AgentSvc.MarkAgentRecovered(r.Context(), a.ID(), at)
	case "stopped":
		err = d.AgentSvc.MarkAgentStopped(r.Context(), a.ID(), at)
	case "error":
		err = d.AgentSvc.MarkAgentError(r.Context(), a.ID(), req.Error, at)
	case "failed":
		// Terminal crash-loop circuit-breaker (v2.7 GATE-7 Mode-B self-heal cap).
		err = d.AgentSvc.MarkAgentFailed(r.Context(), a.ID(), req.Error, at)
	default:
		writeError(w, http.StatusBadRequest, "invalid_state",
			"state must be 'running', 'stopped', 'error' or 'failed'")
		return
	}
	if err != nil {
		mapDomainError(w, err) // ErrIllegalLifecycle → 409
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
		Trigger:           convservice.MarkSeenTriggerDelivery,
	}); err != nil {
		mapDomainError(w, err) // message-not-in-conv → 422; not found → 404
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// agentConverseErrorReq is the body for POST /admin/environment/agent/converse-error.
type agentConverseErrorReq struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	Error          string `json:"error"`
	At             string `json:"at,omitempty"`
}

// envAgentConverseErrorHandler posts a VISIBLE system message into the
// conversation when an agent.converse turn ended is_error (#185 follow-up / UX
// Rule 9 — no silent black hole: a DM/channel reply that failed, e.g. invalid
// model → claude 404, must tell the human instead of leaving them waiting). The
// controller (no DB) calls this after detecting the failed turn. Same per-agent
// guardrail as the other feedback endpoints (requireAgentOnWorker); the agent
// must be an active participant of the conversation. The message is posted as
// `system` (not the agent), mirroring the stopped-agent notice the WakeProjector
// emits.
func (s *Server) envAgentConverseErrorHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req agentConverseErrorReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.MessageWriter == nil || d.ConvRepo == nil {
		writeError(w, http.StatusNotImplemented, "conversation_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ConversationID) == "" {
		writeError(w, http.StatusBadRequest, "missing_conversation_id", "")
		return
	}
	conv, err := d.ConvRepo.FindByID(r.Context(), conversation.ConversationID(req.ConversationID))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	if !agentIsActiveParticipant(conv, a) {
		writeError(w, http.StatusForbidden, "not_a_participant",
			"agent is not an active participant of this conversation")
		return
	}
	name := strings.TrimSpace(a.Profile().Name)
	if name == "" {
		name = string(a.ID())
	}
	msg := "⚠️ @" + name + " couldn't process the message"
	if summary := strings.TrimSpace(req.Error); summary != "" {
		msg += " (" + summary + ")"
	}
	msg += "."
	if _, err := d.MessageWriter.AddMessage(r.Context(), convservice.AddMessageCommand{
		ConversationID:   conv.ID(),
		SenderIdentityID: conversation.IdentityRef("system"),
		ContentKind:      conversation.MessageContentSystem,
		Direction:        conversation.DirectionOutbound,
		Content:          msg,
		Actor:            observability.Actor("system"),
	}); err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// agentReplyNudgesReq is the body for POST /admin/environment/agent/reply-nudges.
type agentReplyNudgesReq struct {
	AgentID string `json:"agent_id"`
}

// agentReplyNudgesResp returns the re-inject prompts the worker should inject.
type agentReplyNudgesResp struct {
	Prompts []string `json:"prompts"`
}

// envAgentReplyNudgesHandler is the reply-guardrail server hook (T341). The
// worker calls it at turn-end + TrueIdle; the server resolves the agent's
// org/display-name/identity-member, derives the directed replies it still owes,
// gates agent-authored ones through the SHARED wake-guardrail (a hop the guardrail
// drops is released — "被 wake-guardrail 拦下就不用回"), bounds the rest by
// max_nudges + cooldown, and returns the prompts to inject (方案 A — the agent
// itself discharges the obligation). Same per-agent guardrail as the other
// feedback endpoints (requireAgentOnWorker). nil ReplyNudgeSvc → 501 (feature off).
func (s *Server) envAgentReplyNudgesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req agentReplyNudgesReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, ok := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if d.ReplyNudgeSvc == nil {
		writeError(w, http.StatusNotImplemented, "reply_guardrail_not_wired", "")
		return
	}
	nudges, err := d.ReplyNudgeSvc.NudgesForAgent(
		r.Context(), string(a.ID()), a.IdentityMemberID(), a.OrganizationID(), strings.TrimSpace(a.Profile().Name))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reply_nudge_failed", err.Error())
		return
	}
	prompts := make([]string, 0, len(nudges))
	for _, n := range nudges {
		prompts = append(prompts, n.Prompt)
	}
	writeJSON(w, http.StatusOK, agentReplyNudgesResp{Prompts: prompts})
}

// agentLeaseHeartbeatReq is the body for POST /admin/environment/agent/lease/heartbeat.
type agentLeaseHeartbeatReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

// envAgentLeaseHeartbeatHandler is the WORKER-driven process-alive lease auto-renew
// (T456 / issue-21ba5b78 I30 P0 #1). The daemon AgentController renews the lease for
// every live session's current task on a periodic tick — decoupled from the agent's
// LLM turn — so a long build/test never lets the lease lapse and get the task
// nudged/recovered. PERSIST-ONLY (a lease touch, no status change → no outbox emit).
// Same per-agent guardrail as the other feedback endpoints (requireAgentOnWorker: the
// worker is the token owner, the target agent must be bound to it). The PM layer
// (WorkerRenewLease) still verifies the task is THIS agent's running, non-blocked task
// before renewing, so a stale worker view (reassigned/blocked task) is a safe no-op.
func (s *Server) envAgentLeaseHeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req agentLeaseHeartbeatReq
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
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	revoked, reason, err := d.PMService.WorkerRenewLease(r.Context(), pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	// REVOCATION (issue-88e32d98, P0 block-fuse): the lease was NOT renewed because this
	// agent's execution should stop (blocked / reassigned / terminal). Tell the worker so
	// it circuit-breaks the in-flight executor at this heartbeat, instead of the executor
	// racing on until the lease silently lapses. PERSIST-ONLY either way (a lease touch).
	if revoked {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": true, "reason": reason})
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

// envWorkerResumeStateHandler returns this worker's resumable agents so the
// daemon can reconcile their claude sessions on boot.
//
// AUTHZ: worker = token owner (strip `worker:` prefix; non-worker owner → 403);
// body.worker_id MUST == that worker, else 403 (worker_mismatch).
//
// Computed set: AgentRepo.ListByWorker(worker) → include each agent whose
// operator INTENT is to be running: lifecycle == running (the normal signal) OR
// lifecycle == error (issue I13: a daemon-reported CRASH — transient, the operator
// never asked to stop it). A stopped/stopping/resetting agent and the TERMINAL
// `failed` circuit-breaker are SKIPPED (nothing to resume / manual recovery only).
// Each included agent carries desired_lifecycle + version (+ reset_scope reserved
// for f-3).
//
// v2.14.0 F7 (issue I14): the per-agent in-flight WorkItem list was removed —
// AgentWorkItem retired. The resumable set is now exactly the running agents; the
// response's per-agent "tasks" array is always empty (the daemon's resume
// parser still accepts it, now a no-op).
func (s *Server) envWorkerResumeStateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentRepo == nil {
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
		// Include the agent if its operator intent is to be running:
		//   - running: the normal resumable signal (v2.14.0 F7 / issue I14 — the
		//     "OR has in-flight WorkItem" condition was dropped with AgentWorkItem).
		//   - error:   the daemon reported a CRASH (issue I13). `error` is the
		//     TRANSIENT crash state (vs the TERMINAL `failed` circuit-breaker), and
		//     the operator never asked to stop it, so its desired intent is still
		//     running. It MUST be resumable: on a daemon (re)boot the agent's
		//     supervisor often SURVIVED, and omitting it here makes boot-reconcile see
		//     a live supervisor with NO center record and stop+reap it as an orphan
		//     (killing the surviving claude, never relaunching) — the
		//     "挂了不能自动恢复" trap. Listing it lets boot-reconcile reattach the
		//     survivor / relaunch a dead one, and lets a new-work wake bring it back.
		lc := a.Lifecycle()
		if lc != agent.LifecycleRunning && lc != agent.LifecycleError {
			continue
		}
		p := a.Profile()
		out = append(out, map[string]any{
			"agent_id": string(a.ID()),
			// agent_ref (T872): the agent's identity-member ref (bare, e.g. "agent-20d5e05c")
			// — the id namespace task.assignee uses ("agent:"+ref). The runtime keys on the
			// bare ULID a.ID() everywhere else, but the executor self-recovery should-continue
			// check must compare a task's assignee against THIS ref, not the ULID, or every
			// crashed executor is misjudged "reassigned" and never tier-1 resumed.
			"agent_ref": a.IdentityMemberID(),
			// Report the INTENT, not the literal lifecycle: the daemon keys on
			// desired_lifecycle=="running" (boot_reconcile.wantsRunning) to reattach /
			// relaunch. A running agent's lifecycle already == "running"; an error agent
			// must also map to "running" here, or boot-reconcile would fall through to
			// stop+reap (the recovery trap above).
			"desired_lifecycle": string(agent.LifecycleRunning),
			"model":             p.Model, // v2.7 Model plumbing: boot-reconcile relaunch spawns claude with it
			"display_name":      p.Name,  // T469: boot-reconcile relaunch injects it as git author NAME (② AgentEnv seam)
			// T728: the already-gated description text to inject into the system prompt,
			// carried like display_name so a boot-reconcile relaunch (worker restart) keeps
			// the persona段 instead of silently dropping it (issue-5e39b7dc-style regression).
			"prompt_description": resumePromptDescription(p),
			// Concurrency config: carried so a boot-reconcile relaunch (worker restart)
			// can RE-ATTACH the executor engine and keep a concurrency-enabled agent
			// CONCURRENT across the restart, instead of silently degrading to
			// single-active (the executor engine is only attached on a fresh reconcile
			// command, which a boot/self-heal relaunch never gets).
			"cli":                    p.CLI,
			"max_concurrent_tasks":   p.MaxConcurrentTasks,
			"allowed_executors":      executorProfilesToMaps(annotateExecutorsFromCatalog(r.Context(), d.ModelCatalogRepo, a.OrganizationID(), p.AllowedExecutors)),
			"orchestrator_model":     p.OrchestratorModel,
			"judge_enabled":          p.JudgeEnabled, // T950 ②: per-agent judge opt-in (default OFF)
			"default_executor_model": p.DefaultExecutorModel,
			"env_vars":               p.EnvVars,
			"version":                a.Version(),
			"reset_scope":            "",                 // reserved for f-3 (rollback/reset semantics)
			"tasks":                  []map[string]any{}, // F7: always empty (AgentWorkItem retired)
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// executorProfilesToMaps serializes the agent's allowed-executor candidates into the
// {cli, model} JSON the worker's ResumeAgent.AllowedExecutors decodes. Returns an
// empty (non-nil) slice for no candidates so the field is an array, never null.
func executorProfilesToMaps(execs []agent.ExecutorProfile) []map[string]any {
	out := make([]map[string]any, 0, len(execs))
	for _, e := range execs {
		m := map[string]any{"cli": e.CLI, "model": e.Model}
		// T950 ②: emit the catalog annotations (omit empties so an unannotated pool
		// stays byte-identical to the pre-catalog {cli,model} wire). The worker decodes
		// these back into agent.ExecutorProfile via the matching json tags.
		if e.DisplayName != "" {
			m["display_name"] = e.DisplayName
		}
		if e.InputCost > 0 {
			m["input_cost"] = e.InputCost
		}
		if e.OutputCost > 0 {
			m["output_cost"] = e.OutputCost
		}
		if e.ContextWindow > 0 {
			m["context_window"] = e.ContextWindow
		}
		if e.Tier != "" {
			m["tier"] = e.Tier
		}
		out = append(out, m)
	}
	return out
}

// annotateExecutorsFromCatalog joins the agent's allowed-executor pool with the org's
// model catalog (pm_model_catalog) by model id (T950 ②), so the difficulty judge sees
// each candidate's tier/cost/context. Catalog-derived + transient: it returns COPIES
// with the annotation fields filled. A model with no catalog row is left NEUTRAL (all
// zero) and logged fail-loud — never silently dropped. A nil repo / empty pool / empty
// org / lookup error returns execs unchanged (judge degrades to name-only routing).
func annotateExecutorsFromCatalog(ctx context.Context, repo pm.ModelCatalogRepository, orgID string, execs []agent.ExecutorProfile) []agent.ExecutorProfile {
	if repo == nil || len(execs) == 0 || strings.TrimSpace(orgID) == "" {
		return execs
	}
	entries, err := repo.ListByOrg(ctx, orgID)
	if err != nil {
		slog.Warn("resume-state: model-catalog lookup failed; executor pool left unannotated (judge sees neutral costs)",
			"org", orgID, "err", err)
		return execs
	}
	byModel := make(map[string]*pm.ModelCatalogEntry, len(entries))
	for _, e := range entries {
		byModel[e.ModelID()] = e
	}
	out := make([]agent.ExecutorProfile, len(execs))
	for i, p := range execs {
		out[i] = p
		e, ok := byModel[p.Model]
		if !ok {
			slog.Warn("resume-state: allowed-executor model not in org model-catalog; judge sees neutral annotation",
				"org", orgID, "cli", p.CLI, "model", p.Model)
			continue
		}
		out[i].DisplayName = e.DisplayName()
		out[i].InputCost = e.InputCost()
		out[i].OutputCost = e.OutputCost()
		out[i].ContextWindow = e.ContextWindow()
		out[i].Tier = e.Tier()
	}
	return out
}

// resumePromptDescription collapses the per-agent inject-description switch into the
// effective text carried in the resume-state (T728): trimmed Description when the
// agent opts in, else "". Mirrors the center-side emit() gate so the boot-reconcile
// relaunch path injects exactly what the reconcile path would.
func resumePromptDescription(p agent.Profile) string {
	if !p.IncludeDescriptionInSystemPrompt {
		return ""
	}
	return strings.TrimSpace(p.Description)
}

// envAgentRuntimeFSResponseHandler — POST /admin/environment/agent/runtime-fs/response
// (issue-921db054 / I5). The WORKER posts the correlated reply to an agent.runtime_fs
// read command here. requireAgentOnWorker proves the posting worker (token owner) owns
// resp.AgentID, then the in-process RuntimeFsDispatcher matches the reply to the
// waiting Web Console request by req_id. An unmatched req_id (the waiter already timed
// out, or a duplicate/late reply) is acknowledged with matched=false — never an error,
// so a slow worker reply after the Center gave up is a harmless no-op.
func (s *Server) envAgentRuntimeFSResponseHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var resp runtimefs.Response
	if err := decodeJSON(r, &resp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// Hard gate: the agent must be bound to the worker proven by the bearer token.
	if _, ok := s.requireAgentOnWorker(w, r, d, resp.AgentID); !ok {
		return
	}
	if strings.TrimSpace(resp.ReqID) == "" {
		writeError(w, http.StatusBadRequest, "missing_req_id", "")
		return
	}
	if d.RuntimeFsDispatcher == nil {
		writeError(w, http.StatusNotImplemented, "runtime_fs_not_wired", "")
		return
	}
	matched := d.RuntimeFsDispatcher.Resolve(resp)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "matched": matched})
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// agentFacingID returns the business-layer id for an agent: its identity-member
// id ("agent-<ulid>"), the ONLY agent id that crosses the BC boundary (v2.7 #185
// — the execution-entity ULID is internal). Falls back to the entity id only for
// a standalone agent with no member (should not occur after no-middle-state;
// defensive so a response never carries an empty id).
func agentFacingID(a *agentbc.Agent) string {
	if m := strings.TrimSpace(a.IdentityMemberID()); m != "" {
		return m
	}
	return string(a.ID())
}

// v2.7 C3 Agent HTTP surface (ADR-0049). The new Agent BC is ORG-scoped (an
// Agent belongs to an Org and can take tasks across projects — it is NOT
// project-nested), so routes live at /api/agents + /api/agents/{id}/... gated by
// requireOrgMember + an agent-in-org check. Lifecycle verbs are INTENT only —
// they transition the Agent AR + emit an outbox event; the Environment BC (D2
// AgentController) reconciles the real worker process. (This surface replaces
// the legacy workforce.AgentInstance /api/agents routes; that backend retires
// with the rest of the legacy execution stack in #107.)

// agentCallerRef maps an authenticated webconsole identity to an Agent-BC ref.
func agentCallerRef(id *identity.Identity) agentbc.IdentityRef {
	if id == nil {
		return ""
	}
	if id.Kind() == "agent" {
		return agentbc.IdentityRef("agent:" + id.ID())
	}
	return agentbc.IdentityRef("user:" + id.ID())
}

// mapAgentError translates Agent-BC errors to HTTP responses.
func mapAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentbc.ErrAgentNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, agentbc.ErrResetRequiresStopped):
		// v2.16 W5: Reset on a non-settled agent. 409 conflict with the current
		// runtime state — the operator stops it first, then resets.
		writeError(w, http.StatusConflict, "reset_requires_stopped", err.Error())
	case errors.Is(err, agentbc.ErrIllegalLifecycle):
		writeError(w, http.StatusConflict, "illegal_transition", err.Error())
	case errors.Is(err, agentbc.ErrAgentNotStopped):
		// v2.7 #197: must stop the agent before deleting it.
		writeError(w, http.StatusConflict, "agent_running", err.Error())
	case errors.Is(err, agentbc.ErrAgentNotStoppedForArchive):
		// v2.8 #272 (b) strict: must stop the agent before archiving it.
		writeError(w, http.StatusConflict, "invalid_state", err.Error())
	case errors.Is(err, agentbc.ErrAgentArchived):
		// v2.8 #272: archived is terminal — e.g. cannot Start an archived agent.
		// 400 (fundamentally invalid), not 409 (transient conflict).
		writeError(w, http.StatusBadRequest, "agent_archived", err.Error())
	case errors.Is(err, agentbc.ErrAgentHasActiveWork):
		writeError(w, http.StatusConflict, "agent_has_active_work", err.Error())
	case errors.Is(err, agentsvc.ErrWorkerNotInOrg):
		writeError(w, http.StatusBadRequest, "worker_not_in_org", err.Error())
	case errors.Is(err, agentbc.ErrUnsupportedCLI):
		// v2.7 #181 / FINDING-F: cli not in the execution allowlist.
		writeError(w, http.StatusBadRequest, "invalid_cli", err.Error())
	case errors.Is(err, agentbc.ErrUnsupportedReasoning):
		// T236: reasoning effort outside the allowlist.
		writeError(w, http.StatusBadRequest, "invalid_reasoning", err.Error())
	case errors.Is(err, agentsvc.ErrResetNotConfirmed),
		errors.Is(err, agentbc.ErrInvalidResetScope),
		errors.Is(err, agentbc.ErrWorkerRequired),
		errors.Is(err, agentbc.ErrInvalidLifecycle):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "agent_error", err.Error())
	}
}

// --- serializers ------------------------------------------------------------

func agentMap(a *agentbc.Agent, availability agentbc.Availability) map[string]any {
	p := a.Profile()
	// v2.7 #183 (FINDING): emit skills/env_vars as [] / {} never null. Go nil
	// slices/maps marshal to JSON null, but the SPA types them non-null
	// (skills: string[]) and reads a.skills.length — a freshly-created agent
	// with no skills sent "skills": null → AgentDetail crashed
	// (Cannot read properties of null (reading 'length')). Honor the contract
	// at the serializer so every consumer (get/list/create) is safe.
	skills := a.Skills()
	if skills == nil {
		skills = []string{}
	}
	// T461: capability tags — emit [] never null (same contract as skills) so the
	// SPA can read .length safely.
	tags := a.CapabilityTags()
	if tags == nil {
		tags = []string{}
	}
	envVars := p.EnvVars
	if envVars == nil {
		envVars = map[string]string{}
	}
	m := map[string]any{
		// v2.7 #185: the business-layer id is the identity-member id; the
		// execution-entity ULID is internal and must NOT appear in API responses.
		"id": agentFacingID(a), "organization_id": a.OrganizationID(),
		"name": p.Name, "description": p.Description, "model": p.Model, "cli": p.CLI,
		// T236: real LLM tuning fields (were hardcoded placeholders in the UI).
		"reasoning": p.Reasoning, "mode": p.Mode, "provider": p.Provider,
		"env_vars": envVars, "skills": skills, "capability_tags": tags, "worker_id": a.WorkerID(),
		"lifecycle": string(a.Lifecycle()), "availability": string(availability),
		"created_by": string(a.CreatedBy()), "version": a.Version(),
		// v2.7 #157: kept for back-compat (equals id now). Lets the Members page
		// navigate an agent member → AgentDetail.
		"identity_member_id": a.IdentityMemberID(),
		"created_at":         a.CreatedAt().Format(time.RFC3339Nano),
		"updated_at":         a.UpdatedAt().Format(time.RFC3339Nano),
	}
	if le := a.LifecycleError(); le != "" {
		m["lifecycle_error"] = le
	}
	return m
}

// bareRefID strips the "user:"/"agent:" kind prefix from an ADR-0033 actor ref
// → the bare identity-member id (mirrors refBareID in handlers.go, kept local to
// avoid importing the conversation BC just for the string op).
func bareRefID(s string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// agentDetailEnrich adds the AgentDetail-only Profile fields (v2.7.1 #228) onto
// the base agentMap: the creator's display name, the bound worker's computer
// info, and the agents this agent created. Read-time + best-effort — a miss
// omits the field so the UI falls back. Kept OUT of agentMap so the list
// endpoint stays lean (no per-row worker/identity/org-list fan-out); only the
// single-agent detail load pays for it.
//
// NOTE the deliberate v2.7.1 omissions (frontend shows static/fallback, real
// values are v2.8 schema work): runtime config reasoning_level/mode/provider
// (#229 — Profile only models Model+CLI), skills global/local indicator + path
// (#230 — skills is a name list, origin is a worker FS property), and the
// worker's daemon version (no Worker BC field). We never fabricate these here.
func (s *Server) agentDetailEnrich(ctx context.Context, d HandlerDeps, a *agentbc.Agent, m map[string]any) {
	// Creator display name (created_by is a "user:"/"agent:" actor ref → bare id).
	if d.IdentityRepo != nil {
		if id := bareRefID(string(a.CreatedBy())); id != "" {
			if ident, err := d.IdentityRepo.GetByID(ctx, id); err == nil && ident != nil {
				m["created_by_display_name"] = ident.DisplayName()
			}
		}
	}
	// Computer: the bound worker's label + connected state. daemon version is NOT
	// a Worker BC field → omitted (the UI does not fabricate it).
	if wid := a.WorkerID(); wid != "" && d.WorkerRepo != nil {
		if wk, err := d.WorkerRepo.FindByID(ctx, workforce.WorkerID(wid)); err == nil && wk != nil {
			m["computer"] = map[string]any{
				"worker_id": wid,
				"name":      wk.Name(),
				"status":    string(wk.Status()),
				"connected": wk.Status() == workforce.WorkerOnline,
			}
		}
	}
	// Created agents: the sub-agents this agent created (created_by == "agent:"+self).
	// Always a slice, never null (#183 contract); empty → "No created agents" in UI.
	created := []map[string]any{}
	if d.AgentSvc != nil {
		if siblings, err := d.AgentSvc.ListAgents(ctx, a.OrganizationID()); err == nil {
			self := agentFacingID(a)
			for _, c := range siblings {
				ref := string(c.CreatedBy())
				if strings.HasPrefix(ref, "agent:") && bareRefID(ref) == self {
					created = append(created, map[string]any{"id": agentFacingID(c), "name": c.Profile().Name})
				}
			}
		}
	}
	m["created_agents"] = created
}

// agentTaskExecMap renders one of an agent's active executions — a non-terminal
// assigned task — in the work_items wire shape the Web Console panel expects
// (v2.14.0 F7 / issue I14: the AgentWorkItem model was removed, so the task IS
// the unit of work — id == task id). agentFacingID is the business-layer id of
// the owning agent (the records are always for one agent, so the caller passes
// it). status maps the task's running/blocked annotation onto the legacy
// queued/active/waiting_input vocabulary the panel still renders.
func agentTaskExecMap(t *pm.Task, agentFacingID string) map[string]any {
	status := "queued"
	if t.BlockedReason() != "" {
		status = "waiting_input"
	} else if t.Status() == pm.TaskRunning {
		status = "active"
	}
	m := map[string]any{
		"id": string(t.ID()), "agent_id": agentFacingID,
		"task_ref": "pm://tasks/" + string(t.ID()),
		"status":   status, "version": t.Version(),
		"created_at": t.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": t.UpdatedAt().Format(time.RFC3339Nano),
		// task enrichment is direct now (the row IS a task): bare task_id (hover/
		// link), task_title (display), project_id (the
		// /projects/{project_id}/tasks/{task_id} link). Omitted when empty so the
		// UI falls back (zero-raw-id invariant preserved).
		"task_id": string(t.ID()),
	}
	if title := t.Title(); title != "" {
		m["task_title"] = title
	}
	if projectID := string(t.ProjectID()); projectID != "" {
		m["project_id"] = projectID
	}
	// the task's org_ref ("T<n>") so the UI shows T84 instead of an id-tail.
	// Omitted when the task has no org_number (UI falls back).
	if orgRef := orgRefToken("T", t.OrgNumber()); orgRef != "" {
		m["org_ref"] = orgRef
	}
	return m
}

// agentActivityMap renders an activity event. agentFacingID is the owning
// agent's business-layer id (v2.7 #185), passed by the caller (single-agent
// listing) so the entity e.AgentID() never leaks.
func agentActivityMap(e *agentbc.AgentActivityEvent, agentFacingID string) map[string]any {
	m := map[string]any{
		"id": e.ID(), "agent_id": agentFacingID, "event_type": e.EventType(),
		"payload": e.Payload(), "occurred_at": e.OccurredAt().Format(time.RFC3339Nano),
	}
	if r := e.TaskRef(); r != "" {
		m["task_ref"] = r
	}
	if r := e.InteractionRef(); r != "" {
		m["interaction_ref"] = r
	}
	return m
}

// enrichAgentLastActivity writes the v2.8.1 #278 agents-list fields onto an agent
// DTO row: last_activity_at (RFC3339Nano | null) + last_activity_content (a plain-
// text truncated preview | null). A nil event (agent with no activity) leaves both
// explicitly null so the FE renders an empty state rather than a missing key.
func enrichAgentLastActivity(m map[string]any, e *agentbc.AgentActivityEvent) {
	if e == nil {
		m["last_activity_at"] = nil
		m["last_activity_content"] = nil
		return
	}
	m["last_activity_at"] = e.OccurredAt().UTC().Format(time.RFC3339Nano)
	if c := plainTextPreview(activityPreviewText(e)); c != "" {
		m["last_activity_content"] = c
	} else {
		m["last_activity_content"] = nil
	}
}

// enrichAgentLoad attaches the agent-load metric (T342) to an agent DTO:
// running_tasks ("doing"), pending_tasks ("open"), and task_load =
// running/(running+pending) ∈ [0,1] (0 when the agent has no active task). The
// frontend colors the load by pressure level. Always emitted (0/0/0 when idle)
// so the SPA can render a stable column.
func enrichAgentLoad(m map[string]any, l pm.AgentTaskLoad) {
	total := l.Running + l.Pending
	load := 0.0
	if total > 0 {
		load = float64(l.Running) / float64(total)
	}
	m["running_tasks"] = l.Running
	m["pending_tasks"] = l.Pending
	m["task_load"] = load
}

// activityPreviewText derives a human-readable content string from an activity
// event's JSON payload (v2.8.1 #278). The payload schema is per event_type
// (activity_event.go): it pulls the first present human-meaningful field
// (text / result / tool_name / event) and, failing that, the raw payload. Never
// panics on malformed JSON — a parse miss falls back to the raw payload string.
func activityPreviewText(e *agentbc.AgentActivityEvent) string {
	payload := strings.TrimSpace(e.Payload())
	if payload == "" || payload == "{}" {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		return "" // not a JSON object → no readable preview (never leak the raw string)
	}
	for _, key := range []string{"text", "result", "tool_name", "event"} {
		if v, ok := fields[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	// E2E finding F-5: an event with no human-meaningful top-level field (e.g. a
	// "system"/"commands_changed" line whose only keys are type/subtype/raw)
	// previously fell through to `return payload`, dumping the raw stream-json blob
	// ({"raw":{...}}) verbatim into the agents-list "Last activity" cell. Return
	// empty so the row renders its empty state instead of internal JSON.
	return ""
}

// --- gate -------------------------------------------------------------------

// agentRequireInOrg resolves {id}, requires org membership, and verifies the
// Agent belongs to the caller's org (cross-org → 404).
func (s *Server) agentRequireInOrg(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*agentbc.Agent, string, bool) {
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_not_wired", "Agent service not wired")
		return nil, "", false
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return nil, "", false
	}
	// v2.7 #185: {id} is the business-layer member id; ResolveAgent bridges it to
	// the execution entity (also accepts a raw entity id for back-compat).
	a, err := d.AgentSvc.ResolveAgent(r.Context(), r.PathValue("id"))
	if err != nil || a.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "agent not found in this organization")
		return nil, "", false
	}
	return a, orgID, true
}

// agentWriteJSON writes an agent with its derived availability.
func (s *Server) agentWriteJSON(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agentbc.Agent) {
	avail, err := d.AgentSvc.Availability(r.Context(), a)
	if err != nil {
		mapAgentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentMap(a, avail))
}

// --- handlers ---------------------------------------------------------------

func (s *Server) agentListHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_not_wired", "")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	as, err := d.AgentSvc.ListAgents(r.Context(), orgID)
	if err != nil {
		mapAgentError(w, err)
		return
	}
	// v2.8 #272: the list excludes archived agents by default (they are retired
	// from the user surface); pass ?include_archived=true to include them.
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	shown := make([]*agentbc.Agent, 0, len(as))
	for _, a := range as {
		if a.Lifecycle() == agentbc.LifecycleArchived && !includeArchived {
			continue
		}
		shown = append(shown, a)
	}
	// v2.8.1 #278 agents-list enrich: batch-fetch the latest activity event for the
	// WHOLE page in ONE window-function query (NO N+1 — query count is constant
	// regardless of list size). Keyed by the execution-entity AgentID (the
	// agent_activity_events partition key). Fail-soft: a batch error → no enrich
	// (last_activity_* stay null), never a 500.
	latestActivity := map[agentbc.AgentID]*agentbc.AgentActivityEvent{}
	if len(shown) > 0 {
		ids := make([]agentbc.AgentID, len(shown))
		for i, a := range shown {
			ids[i] = a.ID()
		}
		if m, lerr := d.AgentSvc.LatestActivityByAgents(r.Context(), ids); lerr == nil {
			latestActivity = m
		}
	}
	// T342 agent-load: batch-fetch the per-assignee active-task split for the whole
	// page in ONE grouped query (no N+1). Fail-soft — a load error → no enrich
	// (task_load stays 0), never a 500.
	loads := map[pm.IdentityRef]pm.AgentTaskLoad{}
	if d.PM != nil {
		if lm, lerr := d.PM.AgentTaskLoads(r.Context()); lerr == nil {
			loads = lm
		}
	}
	out := make([]map[string]any, 0, len(shown))
	for _, a := range shown {
		avail, aerr := d.AgentSvc.Availability(r.Context(), a)
		if aerr != nil {
			mapAgentError(w, aerr)
			return
		}
		m := agentMap(a, avail)
		enrichAgentLastActivity(m, latestActivity[a.ID()])
		enrichAgentLoad(m, loads[pm.IdentityRef("agent:"+agentFacingID(a))])
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// NOTE: agentCreateHandler (POST /api/agents) was removed in v2.7 (#185 /
// no-middle-state). Agents are created ONLY via POST /api/members/agent
// (addAgentMemberHandler), which atomically provisions the identity member AND
// the execution agent (#157) so every agent carries a member id.

func (s *Server) agentGetHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	// v2.7.1 #228: AgentDetail loads the single agent here — enrich with the
	// Profile-only fields (creator name / computer / created agents). The list
	// + lifecycle paths keep using agentWriteJSON (lean agentMap).
	avail, err := d.AgentSvc.Availability(r.Context(), a)
	if err != nil {
		mapAgentError(w, err)
		return
	}
	m := agentMap(a, avail)
	s.agentDetailEnrich(r.Context(), d, a, m)
	// T342 agent-load on the detail DTO (single-agent; fail-soft → 0).
	if d.PM != nil {
		if lm, lerr := d.PM.AgentTaskLoads(r.Context()); lerr == nil {
			enrichAgentLoad(m, lm[pm.IdentityRef("agent:"+agentFacingID(a))])
		}
	}
	writeJSON(w, http.StatusOK, m)
}

// agentDeleteHandler hard-deletes an agent (v2.7 #197). Guards (in the service):
// the agent must be Stopped (else 409 agent_running) and idle (no active work
// item, else 409 agent_has_active_work). In one tx it deletes the agent row
// (releasing the worker binding) AND cascade-deletes the agent's identity-member
// + identity (symmetric to #157's atomic create — no orphan member). Lingering
// pm/conversation refs to the deleted agent render as "(deleted)" + a v2.8 sweep.
func (s *Server) agentDeleteHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_not_wired", "Agent service not wired")
		return
	}
	// v2.8 #272: hard-delete is ADMIN-ONLY. The user-facing delete is archive
	// (soft, POST /api/agents/{id}/archive); the destructive hard-delete cascade is
	// retained only as an admin backdoor (GDPR / test cleanup), unreachable by
	// ordinary members (= "no user-reachable hard-delete path", Tester gate).
	_, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if member == nil || !member.Role().AtLeast(identity.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "hard delete is admin-only; use archive instead")
		return
	}
	a, err := d.AgentSvc.ResolveAgent(r.Context(), r.PathValue("id"))
	if err != nil || a.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "agent not found in this organization")
		return
	}
	if d.IdentityRepo == nil || d.MemberRepo == nil || d.DB == nil {
		writeError(w, http.StatusNotImplemented, "not_wired", "agent delete deps not wired")
		return
	}
	memberID := strings.TrimSpace(a.IdentityMemberID())
	// v2.8.1 force-delete (@oopslink): ?force=true skips the stopped/idle guards and
	// sweeps the agent's non-terminal WorkItems (orphan-sweep). Without it, an
	// active/non-stopped agent returns 409 (mapAgentError) so the FE can offer force.
	force := r.URL.Query().Get("force") == "true"
	if err := persistence.RunInTx(r.Context(), d.DB, func(txCtx context.Context) error {
		if err := d.AgentSvc.DeleteAgent(txCtx, a.ID(), force); err != nil {
			return err
		}
		if memberID != "" {
			if m, merr := d.MemberRepo.GetByOrganizationAndIdentity(txCtx, orgID, memberID); merr == nil && m != nil {
				if derr := d.MemberRepo.Delete(txCtx, m.ID()); derr != nil {
					return derr
				}
			}
			if derr := d.IdentityRepo.Delete(txCtx, memberID); derr != nil {
				return derr
			}
		}
		return nil
	}); err != nil {
		mapAgentError(w, err)
		return
	}
	// v2.8.1: audit a force-delete (the original force-delete spec's "emit
	// force_deleted event"). Best-effort — never fails the request.
	if force && d.EventSink != nil {
		_, _ = d.EventSink.Emit(r.Context(), observability.EmitCommand{
			EventType: observability.EventType("agent.force_deleted"),
			Refs:      observability.EventRefs{AgentID: string(a.ID()), OrganizationID: orgID, MemberID: memberID},
			Actor:     d.Actor,
			Payload:   map[string]any{"force": true},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": agentFacingID(a)})
}

// agentLifecycleAction runs a lifecycle verb then returns the refreshed agent.
func (s *Server) agentLifecycleAction(w http.ResponseWriter, r *http.Request, run func(id agentbc.AgentID) error) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	if err := run(a.ID()); err != nil {
		mapAgentError(w, err)
		return
	}
	got, _ := d.AgentSvc.GetAgent(r.Context(), a.ID())
	s.agentWriteJSON(w, r, d, got)
}

func (s *Server) agentStartHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error { return d.AgentSvc.StartAgent(r.Context(), id) })
}

func (s *Server) agentStopHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error { return d.AgentSvc.StopAgent(r.Context(), id) })
}

// agentArchiveHandler soft-deletes (archives) an agent (v2.8 #272) — the sole
// user-facing delete. Guard (b strict): running/transitioning → 409 invalid_state.
// Idempotent: re-archiving an already-archived agent → 200 no-op. Clears the
// worker binding (worker freed to re-bind); the agent row is retained (history).
// The second confirmation (ConfirmModal) is enforced by the frontend (#270).
func (s *Server) agentArchiveHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error { return d.AgentSvc.ArchiveAgent(r.Context(), id) })
}

func (s *Server) agentRestartHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error { return d.AgentSvc.RestartAgent(r.Context(), id) })
}

func (s *Server) agentResetHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope   string `json:"scope"`
		Confirm bool   `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	s.agentLifecycleAction(w, r, func(id agentbc.AgentID) error {
		return d.AgentSvc.ResetAgent(r.Context(), id, agentbc.ResetScope(req.Scope), req.Confirm)
	})
}

// agentUpdateConfigHandler edits an agent's LLM config (T236): model / cli /
// reasoning / mode / provider. Persist-only — the change applies on the next
// spawn, so the UI pairs this with a restart (the second confirmation "this will
// restart the agent" is enforced by the frontend). Returns the refreshed agent.
func (s *Server) agentUpdateConfigHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model     string `json:"model"`
		CLI       string `json:"cli"`
		Reasoning string `json:"reasoning"`
		Mode      string `json:"mode"`
		Provider  string `json:"provider"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	err := d.AgentSvc.UpdateAgentConfig(r.Context(), a.ID(), agentsvc.UpdateAgentConfigCommand{
		Model: req.Model, CLI: req.CLI, Reasoning: req.Reasoning, Mode: req.Mode, Provider: req.Provider,
	})
	if err != nil {
		mapAgentError(w, err)
		return
	}
	got, _ := d.AgentSvc.GetAgent(r.Context(), a.ID())
	s.agentWriteJSON(w, r, d, got)
}

// agentUpdateTagsHandler replaces an agent's capability tags (T461) — the
// dispatch labels (FE / BE / platform / test / integration / docs ...) the PD
// reads via find_org_agent to assign work by capability + load. Free-form: any
// string list is accepted; the domain normalizes (trim / drop-blank / de-dupe).
// Pure metadata, no restart implication. Returns the refreshed agent.
func (s *Server) agentUpdateTagsHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CapabilityTags []string `json:"capability_tags"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	if err := d.AgentSvc.SetAgentCapabilityTags(r.Context(), a.ID(), req.CapabilityTags); err != nil {
		mapAgentError(w, err)
		return
	}
	got, _ := d.AgentSvc.GetAgent(r.Context(), a.ID())
	s.agentWriteJSON(w, r, d, got)
}

func (s *Server) agentTasksHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	// v2.14.0 F7 (issue I14): the AgentWorkItem model was removed; an agent's
	// "work items" are now its non-terminal assigned tasks. Source the same
	// runnable open/running queue the MCP list_my_tasks pull uses (the panel keeps
	// the work_items wire shape — see agentTaskExecMap).
	facing := agentFacingID(a)
	out := make([]map[string]any, 0)
	if d.PM != nil {
		tasks, err := d.PM.ListRunnableAgentTasks(r.Context(), pm.IdentityRef("agent:"+facing))
		if err != nil {
			mapAgentError(w, err)
			return
		}
		for _, t := range tasks {
			out = append(out, agentTaskExecMap(t, facing))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

func (s *Server) agentActivityHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, _, ok := s.agentRequireInOrg(w, r, d)
	if !ok {
		return
	}
	// v2.8 #274 cursor pagination (a-presence-check, day-0 locked):
	//   - limit omitted   → default 50 (the frontend always passes explicit 50)
	//   - limit=0 present  → unlimited (admin/debug/test full history)
	//   - limit>0 present  → that value
	//   - negative/non-int → 400 invalid_limit
	//   - before=<event-id> → only events older than that cursor (id < before)
	// next_cursor = the last (oldest) event id of this page when a further page
	// exists, else null. (We over-fetch limit+1 to detect "more".)
	const defaultActivityLimit = 50
	limit := defaultActivityLimit
	unlimited := false
	if r.URL.Query().Has("limit") {
		n, perr := strconv.Atoi(r.URL.Query().Get("limit"))
		if perr != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a non-negative integer")
			return
		}
		if n == 0 {
			unlimited, limit = true, 0
		} else {
			limit = n
		}
	}
	before := r.URL.Query().Get("before")
	fetch := limit + 1 // over-fetch to detect a next page
	if unlimited {
		fetch = 0 // no cap
	}
	events, err := d.AgentSvc.ListActivity(r.Context(), a.ID(), fetch, before)
	if err != nil {
		mapAgentError(w, err)
		return
	}
	var nextCursor any // nil → JSON null (no more pages)
	if !unlimited && len(events) > limit {
		nextCursor = events[limit-1].ID() // oldest event on this page = next `before`
		events = events[:limit]
	}
	facing := agentFacingID(a)
	out := make([]map[string]any, 0, len(events))
	for _, e := range events {
		out = append(out, agentActivityMap(e, facing))
	}
	writeJSON(w, http.StatusOK, map[string]any{"activity": out, "next_cursor": nextCursor})
}

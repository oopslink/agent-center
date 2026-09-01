package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	authz "github.com/oopslink/agent-center/internal/authorization"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// v2.9 Plan Orchestration HTTP surface (#285, design §3/§9). Plans nest under
// /api/projects/{project_id}/plans so membership gating is uniform. Read endpoints
// require project membership; writes rely on the PM service's member gate. The Plan DTO carries
// the DERIVED node read model (§9.2): per-node node_status, the ready-set,
// has_failed, and {done,total} progress — node status is never stored.

// --- serializers ------------------------------------------------------------

// pmPlanMap renders the bare Plan AR (list view — no derived nodes).
func pmPlanMap(p *pm.Plan) map[string]any {
	m := map[string]any{
		"id": string(p.ID()), "project_id": string(p.ProjectID()), "name": p.Name(),
		"description": p.Description(), "status": string(p.Status()),
		"creator_ref": string(p.CreatorRef()), "conversation_id": p.ConversationID(),
		"created_at": p.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": p.UpdatedAt().Format(time.RFC3339Nano),
		"version":    p.Version(),
		"is_builtin": p.IsBuiltin(), // ADR-0047: the per-project assignment pool (vs a structured plan)
	}
	// v2.10.1 [T99]: the human Plan id (org_ref "P123"); omitted when org_number
	// is 0 (builtin pool / pre-allocator rows) — the UI falls back to the hash handle.
	if ref := orgRefToken("P", p.OrgNumber()); ref != "" {
		m["org_ref"] = ref
	}
	if p.ActiveGenerationID() != "" {
		m["active_generation_id"] = string(p.ActiveGenerationID())
	}
	if d := p.TargetDate(); d != nil {
		m["target_date"] = d.Format(time.RFC3339Nano)
	}
	if at := p.ArchivedAt(); at != nil {
		m["archived_at"] = at.Format(time.RFC3339Nano)
		m["archived_by"] = string(p.ArchivedBy())
	}
	return m
}

// pmPlanNodeMap renders ONE PlanNodeView to the canonical Plan-node JSON shape
// (§9.2): {task_id,title,assignee_ref,task_status,node_status,depends_on,
// dispatched_at?}. It is the SINGLE source of the node contract — both the detail
// DTO (pmPlanDetailMap) and the list-row preview (pmPlanSummaryMap) build their
// nodes through this helper, so a list preview node is byte-identical in shape to
// a detail node and the two can never drift. titleOf/assigneeOf are the per-Plan
// task lookups built once by the caller.
func pmPlanNodeMap(n pm.PlanNodeView, l planNodeLookup) map[string]any {
	depends := make([]string, 0, len(n.DependsOn))
	for _, d := range n.DependsOn {
		depends = append(depends, string(d))
	}
	node := map[string]any{
		"task_id":      string(n.TaskID),
		"title":        l.titleOf[n.TaskID],
		"assignee_ref": string(l.assigneeOf[n.TaskID]),
		"task_status":  string(n.TaskStatus),
		"node_status":  string(n.NodeStatus),
		"depends_on":   depends,
		"effective":    n.Effective,
		// The task's creation time (always present) so the Plan detail task list
		// can show a "Created" column with a full local timestamp. RFC3339Nano.
		"created_at": l.createdAtOf[n.TaskID],
		// v2.9 P3 Stage B: orthogonal archived state (ArchivePlan cascades to every
		// task) so the DAG-node / task-list "已归档" badge renders here too — not just
		// on board cards (which read the task DTO). Coexists with task_status.
		"archived": l.archivedOf[n.TaskID],
		// ADR-0047: the DERIVED claimable predicate, computed where the plan view is
		// available. True iff the task can be claimed (open→running) right now: not
		// archived, open, assigned, in this plan, node dispatched (e.g. built-in pool).
		"claimable": pm.Claimable(l.archivedOf[n.TaskID], n.TaskStatus, l.assigneeOf[n.TaskID], l.planID, n.NodeStatus),
		// v2.18.3 BE-2 (issue-577a7b0e): the auto-assign STARVED signal — true iff this
		// ownerless pool task carries required_capabilities but NO eligible online agent
		// can take it (a capability-supply gap, NOT mere transient busy-ness). FE renders
		// a "waiting for an eligible agent" badge. Always present; false for non-pool
		// nodes / tasks with no requirement / when no candidate gap exists.
		"starved": l.starvedOf[n.TaskID],
	}
	// v2.9.2 (task-0543ece9): the human Task id (org_ref "T123") rides on the node
	// DTO so the Work Board card + agent-facing list show it WITHOUT a second
	// task-list resolver query. Omitted (not ""-emitted) when unallocated (orgNumber
	// 0 for pre-allocator rows), mirroring the task DTO's omit-when-empty contract.
	if ref := l.orgRefOf[n.TaskID]; ref != "" {
		node["org_ref"] = ref
	}
	if at := l.archivedAtOf[n.TaskID]; at != "" {
		node["archived_at"] = at
	}
	if n.Dispatched && !n.DispatchedAt.IsZero() {
		node["dispatched_at"] = n.DispatchedAt.Format(time.RFC3339Nano)
	}
	if len(n.SupersededBy) > 0 {
		by := make([]string, 0, len(n.SupersededBy))
		for _, id := range n.SupersededBy {
			by = append(by, string(id))
		}
		node["superseded_by"] = by
	}
	if n.SupersededReason != "" {
		node["superseded_reason"] = n.SupersededReason
	}
	// T570 (+ follow-up): a completed task carries its authoritative completion
	// time (task.CompletedAt, set on →completed and cleared on reopen). Emitted
	// only when present — a never-completed / reopened task has no completed_at.
	if at := l.completedAtOf[n.TaskID]; at != "" {
		node["completed_at"] = at
	}
	return node
}

// planNodeLookup is the per-Plan task lookups used to enrich derived nodes (which
// carry only task_id) into the full node JSON — title/assignee plus the orthogonal
// archived state (#283/Stage B) so the badge renders on DAG nodes + task list.
type planNodeLookup struct {
	planID       pm.PlanID
	titleOf      map[pm.TaskID]string
	assigneeOf   map[pm.TaskID]pm.IdentityRef
	archivedOf   map[pm.TaskID]bool
	archivedAtOf map[pm.TaskID]string
	orgRefOf     map[pm.TaskID]string
	// T570 (+ follow-up): the task's authoritative completion time
	// (task.CompletedAt) — set on →completed, cleared on reopen. Surfaced as
	// completed_at so the task list shows WHEN a DONE node finished. Empty when the
	// task is not currently completed.
	completedAtOf map[pm.TaskID]string
	// createdAtOf maps a task id → its creation time (RFC3339Nano). Always present
	// (a task always has a CreatedAt); surfaced as the node's created_at so the
	// Plan detail task list can render a "Created" column.
	createdAtOf map[pm.TaskID]string
	// starvedOf (v2.18.3 BE-2) maps a task id → true when it is auto-assign STARVED.
	// Sourced from PlanDetail.Starved (populated by the FE-facing reads for builtin
	// pool plans); nil/absent ⇒ false (the common case for structured-plan nodes).
	starvedOf map[pm.TaskID]bool
}

func planNodeLookups(detail *pmservice.PlanDetail) planNodeLookup {
	l := planNodeLookup{
		planID:        detail.Plan.ID(),
		titleOf:       make(map[pm.TaskID]string, len(detail.Tasks)),
		assigneeOf:    make(map[pm.TaskID]pm.IdentityRef, len(detail.Tasks)),
		archivedOf:    make(map[pm.TaskID]bool, len(detail.Tasks)),
		archivedAtOf:  make(map[pm.TaskID]string, len(detail.Tasks)),
		orgRefOf:      make(map[pm.TaskID]string, len(detail.Tasks)),
		completedAtOf: make(map[pm.TaskID]string, len(detail.Tasks)),
		createdAtOf:   make(map[pm.TaskID]string, len(detail.Tasks)),
		starvedOf:     detail.Starved,
	}
	for _, t := range detail.Tasks {
		l.titleOf[t.ID()] = t.Title()
		l.assigneeOf[t.ID()] = t.Assignee()
		l.archivedOf[t.ID()] = t.IsArchived()
		l.archivedAtOf[t.ID()] = rfc3339OrEmptyPtr(t.ArchivedAt())
		l.orgRefOf[t.ID()] = orgRefToken("T", t.OrgNumber())
		l.createdAtOf[t.ID()] = t.CreatedAt().Format(time.RFC3339Nano)
		if at := t.CompletedAt(); !at.IsZero() {
			l.completedAtOf[t.ID()] = at.UTC().Format(time.RFC3339Nano)
		}
	}
	return l
}

// pmPlanDetailMap renders the full Plan DTO with the DERIVED node read model
// (§9.2): nodes[{task_id,title,assignee_ref,task_status,node_status,depends_on,
// dispatched_at?}] + ready_set + has_failed + progress{done,total}.
func pmPlanDetailMap(detail *pmservice.PlanDetail) map[string]any {
	p := detail.Plan
	m := pmPlanMap(p)

	lookups := planNodeLookups(detail)

	nodes := make([]map[string]any, 0, len(detail.View.Nodes))
	for _, n := range detail.View.Nodes {
		nodes = append(nodes, pmPlanNodeMap(n, lookups))
	}
	readySet := make([]string, 0, len(detail.View.ReadySet))
	for _, id := range detail.View.ReadySet {
		readySet = append(readySet, string(id))
	}

	m["nodes"] = nodes
	m["ready_set"] = readySet
	m["has_failed"] = detail.View.HasFailed
	m["progress"] = map[string]any{"done": detail.View.Progress.Done, "total": detail.View.Progress.Total}
	if len(detail.View.HistoricalFailures) > 0 {
		m["historical_failures"] = taskIDsToStrings(detail.View.HistoricalFailures)
	}
	if len(detail.View.ActiveFailures) > 0 {
		m["active_failures"] = taskIDsToStrings(detail.View.ActiveFailures)
	}
	if len(detail.GateVerdicts) > 0 {
		m["gate_verdicts"] = detail.GateVerdicts
	}
	if len(detail.Continuations) > 0 {
		m["continuations"] = detail.Continuations
	}
	if len(detail.BlockedOn) > 0 {
		blockedOn := make([]map[string]any, 0, len(detail.BlockedOn))
		for _, b := range detail.BlockedOn {
			blockedOn = append(blockedOn, map[string]any{
				"event_id":          string(b.TaskID),
				"task_id":           string(b.TaskID),
				"node_id":           b.NodeID,
				"wait_type":         string(b.WaitType),
				"wait_keys":         b.WaitKeys,
				"trigger_condition": b.TriggerCondition,
				"waited_since":      b.WaitedSince.Format(time.RFC3339Nano),
				"deadline":          rfc3339OrEmpty(b.Deadline),
				"on_timeout":        b.OnTimeout,
				"last_probe_at":     rfc3339OrEmpty(b.LastProbeAt),
				"probe_count":       b.ProbeCount,
			})
		}
		m["blocked_on"] = blockedOn
	}
	if detail.ProgressControl != nil {
		m["progress_control"] = pmProgressControlMap(detail.ProgressControl)
	}
	return m
}

func pmProgressControlMap(snap *pm.ProgressControlSnapshot) map[string]any {
	if snap == nil {
		return nil
	}
	holds := make([]map[string]any, 0, len(snap.OpenHolds))
	for _, h := range snap.OpenHolds {
		holds = append(holds, map[string]any{
			"id": h.ID, "task_id": string(h.TaskID), "node_id": h.NodeID,
			"reason_kind": h.ReasonKind, "reason_id": h.ReasonID,
			"owner_ref": h.OwnerRef, "entered_at": h.EnteredAt.Format(time.RFC3339Nano),
			"hold_ack_deadline":    h.HoldAckDeadline.Format(time.RFC3339Nano),
			"max_hold_duration_ms": h.MaxHoldDuration.Milliseconds(),
			"escalation_level":     h.EscalationLevel,
			"next_escalation_at":   h.NextEscalationAt.Format(time.RFC3339Nano),
			"blocks_dispatch":      h.BlocksDispatch,
			"blocks_acceptance":    h.BlocksAcceptance,
			"blocks_completion":    h.BlocksCompletion,
		})
	}
	obligations := make([]map[string]any, 0, len(snap.OpenObligations))
	for _, o := range snap.OpenObligations {
		obligations = append(obligations, map[string]any{
			"id": o.ID, "task_id": string(o.TaskID), "node_id": o.NodeID,
			"kind": o.Kind, "owner_ref": string(o.OwnerRef), "deadline_at": o.DeadlineAt.Format(time.RFC3339Nano),
			"ack_required": o.AckRequired, "escalate_to_ref": o.EscalateToRef,
			"escalation_deadline_at": o.EscalationDeadlineAt.Format(time.RFC3339Nano),
			"source_fact_refs":       o.SourceFactRefs,
			"status":                 o.Status,
		})
	}
	incidents := make([]map[string]any, 0, len(snap.OpenIncidents))
	for _, i := range snap.OpenIncidents {
		incidents = append(incidents, map[string]any{
			"id": i.ID, "task_id": string(i.TaskID), "node_id": i.NodeID,
			"kind": i.Kind, "severity": i.Severity, "owner_ref": i.OwnerRef,
			"summary": i.Summary, "source_ref": i.SourceRef, "status": i.Status,
		})
	}
	actions := make([]map[string]any, 0, len(snap.RequiredActions))
	for _, a := range snap.RequiredActions {
		actions = append(actions, map[string]any{
			"id": a.ID, "source_type": a.SourceType, "source_id": a.SourceID,
			"category": a.Category, "action": a.Action, "owner_ref": a.OwnerRef,
			"owner_display": a.OwnerDisplay, "deadline_at": rfc3339OrEmpty(a.DeadlineAt),
			"trigger_fact_refs": a.TriggerFactRefs, "options": a.Options,
		})
	}
	return map[string]any{
		"as_of": snap.AsOf.Format(time.RFC3339Nano), "decision": string(snap.Decision),
		"observation_vector_id": snap.ObservationVectorID, "quality": string(snap.Quality),
		"freshness":  map[string]any{"state": snap.Freshness.State, "watermark_lag_ms": snap.Freshness.WatermarkLagMS, "threshold_ms": snap.Freshness.ThresholdMS},
		"open_holds": holds, "open_obligations": obligations, "open_incidents": incidents,
		"required_actions": actions,
	}
}

// pmPlanSummaryMap renders a Plan for the Work Board's kanban LIST view: the bare
// Plan fields (same as pmPlanMap) PLUS the DERIVED board summary (§9.1/§9.2) —
// progress{done,total}, has_failed, node_count, and nodes_preview.
//
// v2.9.2 (task-0543ece9): the preview is NO LONGER capped — it carries EVERY node,
// so the Work Board card shows the whole task set without a silent "…and N more"
// truncation. This aligns the board with T41's "no silent truncation" principle
// (which fixed the Plan DETAIL page; the board card was the remaining gap). The
// board renders the full list in a scrollable column. node_count stays == the
// node count (now == len(nodes_preview)); a degraded/partial payload that still
// sends fewer preview nodes than node_count keeps the FE overflow hint as a
// belt-and-braces safety net. Each preview node is built through the SAME
// pmPlanNodeMap helper the detail DTO uses, so it is byte-identical in shape to a
// detail node and the two views can never drift.
func pmPlanSummaryMap(detail *pmservice.PlanDetail) map[string]any {
	m := pmPlanMap(detail.Plan)

	lookups := planNodeLookups(detail)

	nodes := detail.View.Nodes
	preview := make([]map[string]any, 0, len(nodes))
	for _, nd := range nodes {
		preview = append(preview, pmPlanNodeMap(nd, lookups))
	}

	m["progress"] = map[string]any{"done": detail.View.Progress.Done, "total": detail.View.Progress.Total}
	m["has_failed"] = detail.View.HasFailed
	if len(detail.View.HistoricalFailures) > 0 {
		m["historical_failures"] = taskIDsToStrings(detail.View.HistoricalFailures)
	}
	if len(detail.View.ActiveFailures) > 0 {
		m["active_failures"] = taskIDsToStrings(detail.View.ActiveFailures)
	}
	m["node_count"] = len(nodes)
	m["nodes_preview"] = preview
	return m
}

func taskIDsToStrings(ids []pm.TaskID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

// mapPlanError extends mapPMError with the Plan-specific status mappings.
func mapPlanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pmservice.ErrStageGateReopenForbidden):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, pm.ErrPlanRunning), errors.Is(err, pm.ErrPlanArchived),
		errors.Is(err, pm.ErrPlanNotPending), errors.Is(err, pm.ErrPlanNotRunning),
		errors.Is(err, pm.ErrPlanNotTerminal), errors.Is(err, pm.ErrPlanNotPaused),
		errors.Is(err, pm.ErrPlanNotDone),
		errors.Is(err, pm.ErrProjectArchived),
		errors.Is(err, pm.ErrPlanVersionConflict), errors.Is(err, pm.ErrPlanNodeInFlight),
		errors.Is(err, pm.ErrPlanGenerationConflict), errors.Is(err, pm.ErrIdempotencyConflict),
		errors.Is(err, pm.ErrPlanHasRunningTasks), errors.Is(err, pm.ErrPlanNotComplete):
		// v2.9 P3: STATE-conflict class — the plan's status blocks the op (running
		// can't delete/archive; already-archived can't re-archive; not-draft can't
		// edit task-set/DAG; not-running can't advance/stop). v2.9 #297: a plan op on
		// an ARCHIVED PARENT PROJECT also conflicts; #299: archive rejected while a
		// member task is still running. All → 409, consistent across
		// webconsole + MCP. Validation-class (cycle/self/no-tasks) stays 400.
		writeError(w, http.StatusConflict, "plan_conflict", err.Error())
	case errors.Is(err, pm.ErrGateAlreadyVerdicted):
		writeError(w, http.StatusConflict, "gate_already_verdicted", err.Error())
	case errors.Is(err, pm.ErrRemediationBudgetExhausted):
		writeError(w, http.StatusConflict, "remediation_budget_exhausted", err.Error())
	case errors.Is(err, pm.ErrRemediationProposalStale):
		writeError(w, http.StatusConflict, "remediation_proposal_stale", err.Error())
	case errors.Is(err, pm.ErrRemediationUnavailable):
		writeError(w, http.StatusNotImplemented, "pm_not_wired", err.Error())
	case errors.Is(err, pm.ErrRemediationProposalInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_remediation_proposal", err.Error())
	case errors.Is(err, pmservice.ErrPlansUnavailable), errors.Is(err, pmservice.ErrDispatcherUnavailable):
		writeError(w, http.StatusNotImplemented, "pm_not_wired", err.Error())
	case errors.Is(err, pm.ErrIllegalPlanTransition), errors.Is(err, pm.ErrInvalidPlanStatus),
		errors.Is(err, pm.ErrPlanCycle), errors.Is(err, pm.ErrSelfDependency),
		errors.Is(err, pm.ErrInvalidLoopback), errors.Is(err, pm.ErrConditionalNeedsWhen),
		errors.Is(err, pm.ErrInvalidEdgeKind),
		errors.Is(err, pm.ErrPlanNoTasks), errors.Is(err, pm.ErrPlanUnassignedTask),
		errors.Is(err, pm.ErrPlanUnresolvableAssignee), errors.Is(err, pm.ErrCrossOrgAssignee),
		errors.Is(err, pm.ErrPlanProjectMismatch), errors.Is(err, pm.ErrTaskInOtherPlan),
		errors.Is(err, pm.ErrEmptyPlanName), errors.Is(err, pm.ErrPlanExists),
		errors.Is(err, pm.ErrPlanGenerationDisconnected):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		mapPMError(w, err)
	}
}

// --- handlers ---------------------------------------------------------------

func (s *Server) pmListPlansHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequireProjectMemberInOrg(w, r, d)
	if !ok {
		return
	}
	// T302: the project plan LIST panel sends pagination/sort params (page_size).
	// In that mode use the SQL-paginated path (ListOrgPlansPage scoped to this one
	// project) — which EXCLUDES the builtin pool and supports sort/q/page — and
	// return a total. Without page params, keep the legacy path: every plan INCL.
	// the builtin pool (the Work Board / usePlans consumers depend on that).
	if r.URL.Query().Get("page_size") != "" {
		q := pm.OrgListQuery{ProjectIDs: []pm.ProjectID{p.ID()}}
		if err := applyListFilters(r, &q, planTerminalStatus); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_filter", err.Error())
			return
		}
		details, total, err := d.PM.ListOrgPlansPage(r.Context(), q)
		if err != nil {
			mapPlanError(w, err)
			return
		}
		out := make([]map[string]any, 0, len(details))
		for _, detail := range details {
			out = append(out, pmPlanSummaryMap(detail))
		}
		writeJSON(w, http.StatusOK, map[string]any{"plans": out, "total": total})
		return
	}
	summaries, err := d.PM.ListPlanSummaries(r.Context(), p.ID())
	if err != nil {
		mapPlanError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(summaries))
	for _, detail := range summaries {
		out = append(out, pmPlanSummaryMap(detail))
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": out})
}

func (s *Server) pmCreatePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		TargetDate  string `json:"target_date"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	var td *time.Time
	if req.TargetDate != "" {
		t, perr := time.Parse(time.RFC3339Nano, req.TargetDate)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "target_date must be RFC3339")
			return
		}
		td = &t
	}
	id, err := d.PM.CreatePlan(r.Context(), pmservice.CreatePlanCommand{
		ProjectID: p.ID(), Name: req.Name, Description: req.Description, TargetDate: td, CreatedBy: caller,
	})
	if err != nil {
		mapPlanError(w, err)
		return
	}
	detail, derr := d.PM.GetPlanDetail(r.Context(), id)
	if derr != nil {
		mapPlanError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

// pmRequirePlanInProject resolves {project_id}+{plan_id}, verifying org
// membership and that the Plan belongs to the path project. Returns the Plan +
// caller ref.
func (s *Server) pmRequirePlanInProject(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*pm.Plan, pm.IdentityRef, bool) {
	p, caller, ok := s.pmRequireProjectMemberInOrg(w, r, d)
	if !ok {
		return nil, "", false
	}
	pl, err := d.PM.GetPlan(r.Context(), pm.PlanID(r.PathValue("plan_id")))
	if err != nil || pl.ProjectID() != p.ID() {
		writeError(w, http.StatusNotFound, "not_found", "plan not found in this project")
		return nil, "", false
	}
	return pl, caller, true
}

func (s *Server) pmGetPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, _, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	detail, err := d.PM.GetPlanDetail(r.Context(), pl.ID())
	if err != nil {
		mapPlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

// pmGetPlanGenerationsHandler exposes the immutable generation ledger rooted at
// Plan.active_generation_id. Stage.generation is deliberately not consulted.
func (s *Server) pmGetPlanGenerationsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, _, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	view, err := d.PM.GetPlanGenerations(r.Context(), pl.ID())
	if err != nil {
		mapPlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pmPlanGenerationReadMap(view))
}

func pmPlanGenerationReadMap(view *pmservice.PlanGenerationRead) map[string]any {
	generations := make([]map[string]any, 0, len(view.Generations))
	for _, revision := range view.Generations {
		generations = append(generations, pmPlanGenerationRevisionMap(revision))
	}
	nodes := make([]map[string]any, 0, len(view.Nodes))
	for _, node := range view.Nodes {
		nodes = append(nodes, map[string]any{
			"task_id":           string(node.TaskID),
			"node_id":           node.NodeID,
			"stage_id":          string(node.StageID),
			"generation_id":     string(node.GenerationID),
			"revision":          node.Revision,
			"present_in_active": node.PresentInActive,
		})
	}
	return map[string]any{
		"plan_id":              string(view.PlanID),
		"active_generation_id": string(view.ActiveGenerationID),
		"plan_version":         view.PlanVersion,
		"generations":          generations,
		"nodes":                nodes,
	}
}

func pmPlanGenerationRevisionMap(revision pmservice.PlanGenerationRevision) map[string]any {
	generation := revision.Generation
	dispatched := make([]string, 0, len(generation.DispatchedTaskIDs))
	for _, id := range generation.DispatchedTaskIDs {
		dispatched = append(dispatched, string(id))
	}
	return map[string]any{
		"id":                   string(generation.ID),
		"plan_id":              string(generation.PlanID),
		"parent_generation_id": string(generation.ParentGenerationID),
		"revision":             revision.Revision,
		"active":               revision.Active,
		"reason":               generation.Reason,
		"evidence":             generation.Evidence,
		"creator_ref":          string(generation.CreatorRef),
		"diff":                 pmPlanGenerationDiffMap(generation.Diff),
		"snapshot":             generation.Snapshot,
		"snapshot_progress": map[string]any{
			"done": revision.Progress.Done, "total": revision.Progress.Total,
		},
		"idempotency_key":     generation.IdempotencyKey,
		"dispatched_task_ids": dispatched,
		"created_at":          generation.CreatedAt.Format(time.RFC3339Nano),
	}
}

func pmPlanGenerationDiffMap(diff pm.PlanGenerationDiff) map[string]any {
	decisions := make([]pm.PlanGenerationNodeDecision, 0, len(diff.NodeDecisions))
	decisions = append(decisions, diff.NodeDecisions...)
	tasks := make([]pm.PlanGenerationTaskDraft, 0, len(diff.Tasks))
	tasks = append(tasks, diff.Tasks...)
	edges := make([]pm.PlanGenerationEdgeDraft, 0, len(diff.Edges))
	edges = append(edges, diff.Edges...)
	return map[string]any{
		"node_decisions": decisions,
		"tasks":          tasks,
		"edges":          edges,
	}
}

// pmGetPlanGraphHandler — GET …/plans/{plan_id}/graph (T769). Returns the plan's
// orchestration-engine GRAPH read model: control nodes (Start/End/Condition) +
// business nodes bound to tasks (status/org_ref) + edges by kind
// (seq/conditional/loopback). The plan-detail DAG reads THIS to reflect the real
// engine graph rather than the client-side DerivePlanView reconstruction.
//
// NON-BREAKING: a plan with NO graph_id (pre-T768 / draft / never-started, or the
// engine unwired) yields ErrPlanHasNoGraph → a 200 {has_graph:false} body, and the
// FE falls back to the legacy plan-DAG rendering. Membership + plan-in-project
// gated exactly like pmGetPlanHandler.
func (s *Server) pmGetPlanGraphHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, _, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	view, err := d.PM.GetPlanGraph(r.Context(), pl.ID())
	if err != nil {
		if errors.Is(err, pmservice.ErrPlanHasNoGraph) {
			// The non-breaking fallback signal — tell the FE to render the legacy DAG.
			writeJSON(w, http.StatusOK, map[string]any{"has_graph": false})
			return
		}
		mapPlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pmPlanGraphMap(view))
}

// pmPlanGraphMap renders a PlanGraphView to the graph-read JSON shape:
// {has_graph:true, graph_id, status, nodes[{id,category,control_kind,title,status,
// task_id?,task_status?,org_ref?,assignee_ref?}], edges[{from,to,kind}]}.
func pmPlanGraphMap(v *pmservice.PlanGraphView) map[string]any {
	nodes := make([]map[string]any, 0, len(v.Nodes))
	for _, n := range v.Nodes {
		node := map[string]any{
			"id":       n.ID,
			"category": n.Category,
			"title":    n.Title,
			"status":   n.Status,
		}
		if n.ControlKind != "" {
			node["control_kind"] = n.ControlKind
		}
		if n.TaskID != "" {
			node["task_id"] = n.TaskID
			node["task_status"] = n.TaskStatus
			node["assignee_ref"] = n.Assignee
			if n.StageID != "" {
				node["stage_id"] = n.StageID
			}
			if n.FollowsTaskID != "" {
				node["follows_task_id"] = n.FollowsTaskID
			}
		}
		if n.TaskOrgRef != "" {
			node["org_ref"] = n.TaskOrgRef
		}
		nodes = append(nodes, node)
	}
	edges := make([]map[string]any, 0, len(v.Edges))
	for _, e := range v.Edges {
		edges = append(edges, map[string]any{"from": e.From, "to": e.To, "kind": e.Kind})
	}
	return map[string]any{
		"has_graph": true,
		"graph_id":  v.GraphID,
		"status":    v.Status,
		"nodes":     nodes,
		"edges":     edges,
	}
}

// pmListPlanStagesHandler — GET …/plans/{plan_id}/stages (T981, plan-stage-model §7).
// Returns the plan's stage-level DERIVED read model for the detail page's stage
// rendering: each stage's projected status / retry rounds / member nodes, so the FE can
// group the sub-DAG by member task_id and show "Stage 2/3 · running". Membership +
// plan-in-project gated exactly like pmGetPlanGraphHandler.
//
// NON-BREAKING (§8): a plan with NO stages yields {stages:[]}, and the FE falls back to
// the legacy no-stage plan rendering (byte-identical to before). The projection reuses
// pmservice.ListStagesForPlan, which shares GetStage's single projection path.
func (s *Server) pmListPlanStagesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, _, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	details, err := d.PM.ListStagesForPlan(r.Context(), pl.ID())
	if err != nil {
		// Stages unwired (no StageRepository) is a non-breaking empty, mirroring the
		// graph handler's has_graph:false fallback — the FE renders the legacy view.
		if errors.Is(err, pmservice.ErrStagesUnavailable) {
			writeJSON(w, http.StatusOK, map[string]any{"stages": []any{}})
			return
		}
		mapPlanError(w, err)
		return
	}
	stages := make([]map[string]any, 0, len(details))
	for _, det := range details {
		stages = append(stages, pmStageDetailMap(det))
	}
	writeJSON(w, http.StatusOK, map[string]any{"stages": stages})
}

// pmStageDetailMap renders one StageDetail to the stage-read JSON shape:
// {id,name,status,rounds,max_rounds,depends_on_stages,gate_node_id,
//
//	members[{task_id,title,task_status}]}. Status/rounds/members are the DERIVED
//
// projection (never stored). Mirrors the agent-tool stageDetailMap shape.
func pmStageDetailMap(det *pmservice.StageDetail) map[string]any {
	st := det.Stage
	deps := make([]string, 0, len(st.DependsOnStages()))
	for _, dep := range st.DependsOnStages() {
		deps = append(deps, string(dep))
	}
	members := make([]map[string]any, 0, len(det.Members))
	for _, m := range det.Members {
		members = append(members, map[string]any{
			"task_id":     string(m.TaskID),
			"title":       m.Title,
			"task_status": string(m.TaskStatus),
		})
	}
	return map[string]any{
		"id":                   string(st.ID()),
		"name":                 st.Name(),
		"status":               string(det.Status),
		"rounds":               det.Rounds,
		"max_rounds":           st.MaxRounds(),
		"depends_on_stages":    deps,
		"gate_node_id":         st.GateNodeID(),
		"gate_task_id":         string(st.GateTaskID()),
		"gate_spec":            st.GateSpec(),
		"gate_outcome":         det.GateOutcome,
		"gate_evidence":        det.GateEvidence,
		"gate_reviewed_sha":    det.GateReviewedSHA,
		"origin_verdict_id":    string(st.OriginVerdictID()),
		"continuation_id":      string(st.ContinuationID()),
		"generation":           st.Generation(),
		"acceptance_contract":  st.AcceptanceContract(),
		"topology_fingerprint": st.TopologyFingerprint(),
		"diagnostics":          det.Diagnostics,
		"members":              members,
	}
}

// pmRelatedPlansHandler — GET …/plans/{plan_id}/related-plans (T581). The OTHER
// structured plans derived from the SAME source issue as this plan, for the plan
// detail rail's "Related Plans" list. Membership + plan-in-project gated like
// pmGetPlanHandler. Response mirrors the plan list shape ({plans:[...]}); each row is
// the base plan DTO (the rail renders ref + name + status). Empty array when the plan
// has no source issue / no siblings.
func (s *Server) pmRelatedPlansHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	plans, err := d.PM.ListRelatedPlans(r.Context(), pl.ID(), caller)
	if err != nil {
		mapPlanError(w, err)
		return
	}
	rows := make([]map[string]any, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, pmPlanMap(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": rows})
}

// pmRelatedIssuesHandler — GET …/plans/{plan_id}/related-issues. The DISTINCT source
// issues this plan's tasks derive from, for the plan detail rail's "Related Issues" list
// (the issue-side mirror of the issue sidebar's Derived Tasks). Membership + plan-in-
// project gated like pmGetPlanHandler. Response mirrors the issue list shape
// ({issues:[...]}); each row is the base Issue DTO (the rail renders ref + title +
// status). Empty array when no task derives from an issue.
func (s *Server) pmRelatedIssuesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	issues, err := d.PM.ListRelatedIssues(r.Context(), pl.ID(), caller)
	if err != nil {
		mapPlanError(w, err)
		return
	}
	rows := make([]map[string]any, 0, len(issues))
	for _, i := range issues {
		rows = append(rows, pmIssueMap(i))
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": rows})
}

// pmIssueRelatedPlansHandler — GET …/issues/{issue_id}/related-plans. The DISTINCT
// non-builtin plans derived from this issue, for the issue detail's "Related Plans"
// panel (the plan-side mirror of Derived Tasks; the reverse of pmRelatedIssuesHandler).
// Membership + issue-in-project gated like pmGetIssueHandler. Response mirrors the plan
// list shape ({plans:[...]}); each row is the base plan DTO. Empty array when no plan is
// derived from the issue.
func (s *Server) pmIssueRelatedPlansHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	i, caller, ok := s.pmRequireIssueInProject(w, r, d)
	if !ok {
		return
	}
	plans, err := d.PM.ListPlansForIssue(r.Context(), i.ID(), caller)
	if err != nil {
		mapPlanError(w, err)
		return
	}
	rows := make([]map[string]any, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, pmPlanMap(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"plans": rows})
}

func (s *Server) pmUpdatePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		TargetDate  *string `json:"target_date"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	cmd := pmservice.UpdatePlanCommand{PlanID: pl.ID(), Name: req.Name, Description: req.Description, Actor: caller}
	if req.TargetDate != nil {
		cmd.TargetDateSet = true
		if *req.TargetDate != "" {
			t, perr := time.Parse(time.RFC3339Nano, *req.TargetDate)
			if perr != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "target_date must be RFC3339 or empty")
				return
			}
			cmd.TargetDate = &t
		}
	}
	if err := d.PM.UpdatePlan(r.Context(), cmd); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmSelectTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.SelectTaskIntoPlan(r.Context(), pl.ID(), pm.TaskID(req.TaskID), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmRemoveTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.RemoveTaskFromPlan(r.Context(), pl.ID(), pm.TaskID(r.PathValue("task_id")), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmAddDependencyHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	var req struct {
		FromTaskID string `json:"from_task_id"`
		ToTaskID   string `json:"to_task_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.AddPlanDependency(r.Context(), pl.ID(), pm.TaskID(req.FromTaskID), pm.TaskID(req.ToTaskID), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmRemoveDependencyHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	// DELETE carries the edge in the query string (the FE api.del client is
	// path/query-only, no body) — reading the body here left from/to empty so the
	// edge was never removed. Query params are the correct REST shape for DELETE.
	fromTaskID := r.URL.Query().Get("from_task_id")
	toTaskID := r.URL.Query().Get("to_task_id")
	if err := d.PM.RemovePlanDependency(r.Context(), pl.ID(), pm.TaskID(fromTaskID), pm.TaskID(toTaskID), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

type pmPlanGenerationDiffReq struct {
	NodeDecisions *[]pm.PlanGenerationNodeDecision `json:"node_decisions"`
	Tasks         *[]pm.PlanGenerationTaskDraft    `json:"tasks"`
	Edges         *[]pm.PlanGenerationEdgeDraft    `json:"edges"`
}

type pmPlanEvolutionReq struct {
	ParentGenerationID  string                   `json:"parent_generation_id"`
	BaseVersion         int                      `json:"base_version"`
	IdempotencyKey      string                   `json:"idempotency_key"`
	Reason              string                   `json:"reason"`
	Evidence            string                   `json:"evidence"`
	Diff                *pmPlanGenerationDiffReq `json:"diff"`
	ResolveBlockEventID string                   `json:"resolve_block_event_id"`
	ResolutionKind      string                   `json:"resolution_kind"`
	ResolutionNote      string                   `json:"resolution_note"`
}

// pmCommitPlanEvolutionHandler is the product Evolution write surface. It uses
// the same EvolvePlanGeneration AppService command as agent tools so parent/CAS,
// idempotency, immutable snapshot, and transaction-wide in-flight rejection are
// one domain contract.
func (s *Server) pmCommitPlanEvolutionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if !requireWebSubjectAuthorization(w, r, d, authz.SubjectRef(caller), "plan.write", authz.ResourceScope{Kind: "plan", ID: string(pl.ID()), ProjectID: string(pl.ProjectID())}) {
		return
	}
	if pl.CreatorRef() != caller {
		writeError(w, http.StatusForbidden, "forbidden", "plan owner is required to commit evolution")
		return
	}
	var req pmPlanEvolutionReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(req.ParentGenerationID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "parent_generation_id is required")
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "reason is required")
		return
	}
	if strings.TrimSpace(req.Evidence) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "evidence is required")
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "idempotency_key is required")
		return
	}
	if req.Diff == nil || req.Diff.NodeDecisions == nil || req.Diff.Tasks == nil || req.Diff.Edges == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "diff must contain node_decisions, tasks, and edges arrays")
		return
	}
	diff := pm.PlanGenerationDiff{
		NodeDecisions: *req.Diff.NodeDecisions,
		Tasks:         *req.Diff.Tasks,
		Edges:         *req.Diff.Edges,
	}
	result, err := d.PM.EvolvePlanGeneration(r.Context(), pmservice.EvolvePlanGenerationCommand{
		PlanID:              pl.ID(),
		ParentGenerationID:  pm.PlanGenerationID(req.ParentGenerationID),
		BaseVersion:         req.BaseVersion,
		IdempotencyKey:      req.IdempotencyKey,
		Reason:              req.Reason,
		Evidence:            req.Evidence,
		Creator:             caller,
		Diff:                diff,
		ResolveBlockEventID: req.ResolveBlockEventID,
		ResolutionKind:      req.ResolutionKind,
		ResolutionNote:      req.ResolutionNote,
	})
	if err != nil {
		mapPlanError(w, err)
		return
	}
	dispatched := make([]string, 0, len(result.Dispatched))
	for _, id := range result.Dispatched {
		dispatched = append(dispatched, string(id))
	}
	view, err := d.PM.GetPlanGenerations(r.Context(), pl.ID())
	if err != nil {
		mapPlanError(w, err)
		return
	}
	var active map[string]any
	for _, revision := range view.Generations {
		if revision.Generation.ID == result.Generation.ID {
			active = pmPlanGenerationRevisionMap(revision)
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"duplicate":            result.Duplicate,
		"active_generation_id": string(view.ActiveGenerationID),
		"version":              result.Generation.Snapshot.PlanVersion,
		"dispatched":           dispatched,
		"generation":           active,
	})
}

func (s *Server) pmStartPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.StartPlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmStopPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.StopPlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

// pmPausePlanHandler is the canonical running→paused lifecycle action. The
// legacy /stop endpoint delegates to the same command during migration.
func (s *Server) pmPausePlanHandler(w http.ResponseWriter, r *http.Request) {
	s.pmStopPlanHandler(w, r)
}

func (s *Server) pmResumePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.ResumePlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmReopenPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.ReopenPlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmCompletePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.CompletePlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmDiscardPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.DiscardPlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

// pmResumePausedNodeHandler is the T53 operator recovery action for the owner: a
// project member resumes a plan node whose agent paused its work item and went idle
// (the node shows `paused`). pm authorizes (project member + plan running + task in
// plan), resumes the node's work item, and wakes its agent. Returns the refreshed
// plan detail so the DAG reflects the node leaving `paused`.
func (s *Server) pmResumePausedNodeHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	taskID := pm.TaskID(r.PathValue("task_id"))
	if err := d.PM.ResumePausedNode(r.Context(), pl.ID(), taskID, caller); err != nil {
		switch {
		case errors.Is(err, pmservice.ErrNodeNotPaused):
			writeError(w, http.StatusConflict, "node_not_paused", "the plan node has no paused work item to resume")
		case errors.Is(err, agent.ErrAgentHasActiveWork):
			writeError(w, http.StatusConflict, "agent_busy", "the node's agent is busy on another work item; try again after it settles")
		case errors.Is(err, pmservice.ErrTaskNotInPlan):
			writeError(w, http.StatusNotFound, "not_found", "the task is not a node of this plan")
		// T101: parity with the agent-tools (MCP) path — give a SPECIFIC plan_not_running
		// code/message instead of the generic plan_conflict from mapPlanError, so the
		// operator UI can render an accurate hint.
		case errors.Is(err, pm.ErrPlanNotRunning):
			writeError(w, http.StatusConflict, "plan_not_running", "the plan is not running, so its nodes can't be resumed")
		case errors.Is(err, pmservice.ErrNodeResumerUnavailable):
			writeError(w, http.StatusNotImplemented, "pm_not_wired", "paused-node resume is not available")
		default:
			mapPlanError(w, err)
		}
		return
	}
	detail, _ := d.PM.GetPlanDetail(r.Context(), pl.ID())
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

// pmDeletePlanHandler hard-deletes a non-running Plan (v2.9 P3): its tasks are
// unloaded back to the backlog, its deps/dispatch-records + the plan row are
// removed, and its 1:1 conversation is hard-deleted (event-driven). A running
// plan is rejected 409 (stop it first). The plan is gone, so it returns a bare
// deletion confirmation (no detail to re-fetch).
func (s *Server) pmDeletePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.DeletePlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "plan_id": string(pl.ID())})
}

// pmArchivePlanHandler archives a non-running Plan + CASCADE-archives its tasks
// (v2.9 P3, irreversible). A running plan is rejected 409 (stop it first); an
// already-archived plan is rejected 409. Returns the archived Plan detail.
func (s *Server) pmArchivePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.ArchivePlan(r.Context(), pl.ID(), caller); err != nil {
		mapPlanError(w, err)
		return
	}
	detail, derr := d.PM.GetPlanDetail(r.Context(), pl.ID())
	if derr != nil {
		mapPlanError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, pmPlanDetailMap(detail))
}

func (s *Server) pmAdvancePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, caller, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	dispatchedIDs, err := d.PM.AdvancePlan(r.Context(), pl.ID(), caller)
	if err != nil {
		mapPlanError(w, err)
		return
	}
	dispatched := make([]string, 0, len(dispatchedIDs))
	for _, id := range dispatchedIDs {
		dispatched = append(dispatched, string(id))
	}
	detail, derr := d.PM.GetPlanDetail(r.Context(), pl.ID())
	if derr != nil {
		mapPlanError(w, derr)
		return
	}
	resp := pmPlanDetailMap(detail)
	resp["dispatched"] = dispatched
	writeJSON(w, http.StatusOK, resp)
}

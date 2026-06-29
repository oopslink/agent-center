package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// v2.9 Plan Orchestration HTTP surface (#285, design §3/§9). Plans nest under
// /api/projects/{project_id}/plans so membership gating is uniform
// (pmRequireProjectInOrg → requireProjectMember on writes). The Plan DTO carries
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
	if d := p.TargetDate(); d != nil {
		m["target_date"] = d.Format(time.RFC3339Nano)
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
		starvedOf:     detail.Starved,
	}
	for _, t := range detail.Tasks {
		l.titleOf[t.ID()] = t.Title()
		l.assigneeOf[t.ID()] = t.Assignee()
		l.archivedOf[t.ID()] = t.IsArchived()
		l.archivedAtOf[t.ID()] = rfc3339OrEmptyPtr(t.ArchivedAt())
		l.orgRefOf[t.ID()] = orgRefToken("T", t.OrgNumber())
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
	return m
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
	m["node_count"] = len(nodes)
	m["nodes_preview"] = preview
	return m
}

// mapPlanError extends mapPMError with the Plan-specific status mappings.
func mapPlanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pm.ErrPlanRunning), errors.Is(err, pm.ErrPlanArchived),
		errors.Is(err, pm.ErrPlanNotDraft), errors.Is(err, pm.ErrPlanNotRunning),
		errors.Is(err, pm.ErrProjectArchived),
		errors.Is(err, pm.ErrPlanHasRunningTasks):
		// v2.9 P3: STATE-conflict class — the plan's status blocks the op (running
		// can't delete/archive; already-archived can't re-archive; not-draft can't
		// edit task-set/DAG; not-running can't advance/stop). v2.9 #297: a plan op on
		// an ARCHIVED PARENT PROJECT also conflicts; #299: archive rejected while a
		// member task is still running. All → 409, consistent across
		// webconsole + MCP. Validation-class (cycle/self/no-tasks) stays 400.
		writeError(w, http.StatusConflict, "plan_conflict", err.Error())
	case errors.Is(err, pmservice.ErrPlansUnavailable), errors.Is(err, pmservice.ErrDispatcherUnavailable):
		writeError(w, http.StatusNotImplemented, "pm_not_wired", err.Error())
	case errors.Is(err, pm.ErrIllegalPlanTransition), errors.Is(err, pm.ErrInvalidPlanStatus),
		errors.Is(err, pm.ErrPlanCycle), errors.Is(err, pm.ErrSelfDependency),
		errors.Is(err, pm.ErrPlanNoTasks), errors.Is(err, pm.ErrPlanUnassignedTask),
		errors.Is(err, pm.ErrPlanUnresolvableAssignee), errors.Is(err, pm.ErrCrossOrgAssignee),
		errors.Is(err, pm.ErrPlanProjectMismatch), errors.Is(err, pm.ErrTaskInOtherPlan),
		errors.Is(err, pm.ErrEmptyPlanName), errors.Is(err, pm.ErrPlanExists):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		mapPMError(w, err)
	}
}

// --- handlers ---------------------------------------------------------------

func (s *Server) pmListPlansHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequireProjectInOrg(w, r, d)
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
	p, caller, ok := s.pmRequireProjectInOrg(w, r, d)
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

// pmUnmergedBoardMap renders the F4 unmerged-branch board DTO (v2.13.0 / I18):
// the plan's `Integrate(T)` nodes that have NOT yet merged back into the trunk
// (un-done Integrate nodes). Each row resolves its title/org_ref/assignee from the
// SAME PlanDetail load so the board mirrors the plan node shape. all_merged is the
// ship-gate-clear signal (no Integrate node still open).
func pmUnmergedBoardMap(board *pmservice.UnmergedBoard) map[string]any {
	lookups := planNodeLookups(board.Detail)
	rows := make([]map[string]any, 0, len(board.Unmerged))
	for _, u := range board.Unmerged {
		row := map[string]any{
			"task_id":          string(u.TaskID),
			"title":            lookups.titleOf[u.TaskID],
			"assignee_ref":     string(lookups.assigneeOf[u.TaskID]),
			"node_status":      string(u.NodeStatus),
			"branch":           u.Branch,
			"base":             u.Base,
			"skip_merge_check": u.SkipMergeCheck,
		}
		if ref := lookups.orgRefOf[u.TaskID]; ref != "" {
			row["org_ref"] = ref
		}
		rows = append(rows, row)
	}
	return map[string]any{
		"plan_id":        string(board.Detail.Plan.ID()),
		"project_id":     string(board.Detail.Plan.ProjectID()),
		"plan_name":      board.Detail.Plan.Name(),
		"plan_status":    string(board.Detail.Plan.Status()),
		"all_merged":     board.AllMerged(),
		"unmerged_count": len(rows),
		"unmerged":       rows,
	}
}

// pmListUnmergedBranchesHandler — GET …/plans/{plan_id}/unmerged-branches. The F4
// ship-gate board: lists the plan's un-done Integrate nodes (unmerged feature
// branches). Membership + plan-in-project gated like pmGetPlanHandler. Empty
// (all_merged) when the plan has no cycle metadata (non-scaffolded / F2 not wired).
func (s *Server) pmListUnmergedBranchesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	pl, _, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	board, err := d.PM.ListUnmergedIntegrations(r.Context(), pl.ID())
	if err != nil {
		mapPlanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pmUnmergedBoardMap(board))
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

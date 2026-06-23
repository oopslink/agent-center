package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// Agent MCP passthrough tools — Plan operations (v2.9 P3 Stage C). Thin wrappers
// over the pm Plan AppServices so a PM-agent can drive Plan orchestration via MCP
// tools (an agent-created plan becomes real). MIRRORS agent_tools_passthrough.go
// EXACTLY: parse args → call the pm AppService with actor=agent → map result/error
// to the MCP tool response. NO new domain logic.
//
// Auth/identity is REUSED, not reinvented:
//   - Every tool goes through requireAgentOnWorker (the b1 guardrail: worker proven
//     by the TOKEN OWNER, target agent bound to it — the SAME gate the task tools
//     use). A wrong-org / wrong-worker caller is rejected there (403) before any
//     AppService call.
//   - The actor passed into each WRITE AppService is the agent's business identity
//     ref `agent:<member-id>` (agentActor) — the SAME actor the create_task /
//     assign_task tools pass. The AppService's own requireProjectMember is the
//     write-gate: an agent member of the plan's project passes; a foreign project
//     yields ErrNotMember (→403). No extra membership layer is added on top.
//
// Plan domain errors the AppServices already enforce (ErrPlanNotDraft,
// ErrPlanNotRunning, ErrPlanCycle, ErrSelfDependency, ErrPlanNoTasks, …) are
// surfaced as tool errors via mapPlanToolError (the admin mirror of webconsole's
// mapPlanError).
//
// NOTE on task assignment: assign_task ALREADY exists in agent_tools_passthrough.go
// (→ pm.AssignTask). A plan node's assignee is just the underlying Task's assignee,
// so there is NO plan-specific assign tool here — the existing assign_task suffices.
//
// NOTE on delete/archive: delete_plan / archive_plan are thin wrappers over the pm
// DeletePlan / ArchivePlan AppServices (Stage B), which guard a RUNNING plan with
// ErrPlanRunning (stop it first) and re-archival with ErrPlanArchived — both
// surfaced via mapPlanToolError as 409 plan_conflict (mirroring webconsole).
// =============================================================================

// mapPlanToolError translates Plan-specific sentinel errors to the tool-error
// envelope, then defers to the shared mapDomainError for everything else. It is
// the admin-package mirror of webconsole/api.mapPlanError (same sentinels, same
// status classes): plan-not-found → 404, plans/dispatcher-unwired → 501, the
// draft/running/cycle/validation guards → 422 invalid_transition (a state
// precondition the agent must observe), and ErrNotMember/ErrCrossProject fall
// through to mapDomainError's 403.
func mapPlanToolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pm.ErrPlanRunning), errors.Is(err, pm.ErrPlanArchived),
		errors.Is(err, pm.ErrPlanNotDraft), errors.Is(err, pm.ErrPlanNotRunning),
		errors.Is(err, pm.ErrProjectArchived),
		errors.Is(err, pm.ErrPlanHasRunningTasks):
		// v2.9 #297: a plan op on an ARCHIVED PARENT PROJECT also conflicts → 409.
		// v2.9 #299: archive rejected while a member task is still running → 409.
		// v2.9 P3: STATE-conflict class — the plan's status blocks the op → 409
		// plan_conflict, CONSISTENT with webconsole mapPlanError (ErrPlanNotDraft was
		// 422 here / 400 there; unified to 409 = same domain-error-class same code
		// cross-surface). Validation-class (cycle/self/no-tasks) stays 422 below.
		writeError(w, http.StatusConflict, "plan_conflict", err.Error())
	case errors.Is(err, pmservice.ErrPlansUnavailable), errors.Is(err, pmservice.ErrDispatcherUnavailable):
		writeError(w, http.StatusNotImplemented, "pm_not_wired", err.Error())
	case errors.Is(err, pm.ErrIllegalPlanTransition), errors.Is(err, pm.ErrInvalidPlanStatus),
		errors.Is(err, pm.ErrPlanCycle), errors.Is(err, pm.ErrSelfDependency),
		errors.Is(err, pm.ErrPlanNoTasks), errors.Is(err, pm.ErrPlanUnassignedTask),
		errors.Is(err, pm.ErrPlanUnresolvableAssignee), errors.Is(err, pm.ErrCrossOrgAssignee),
		errors.Is(err, pm.ErrPlanProjectMismatch), errors.Is(err, pm.ErrTaskInOtherPlan),
		errors.Is(err, pm.ErrEmptyPlanName), errors.Is(err, pm.ErrPlanExists),
		errors.Is(err, pmservice.ErrScaffoldVersionRequired), errors.Is(err, pmservice.ErrScaffoldNoFeatures),
		errors.Is(err, pmservice.ErrScaffoldFeatureNameRequired):
		writeError(w, http.StatusUnprocessableEntity, "invalid_transition", err.Error())
	default:
		mapDomainError(w, err)
	}
}

// --- create_plan -------------------------------------------------------------

type createPlanReq struct {
	AgentID     string `json:"agent_id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TargetDate  string `json:"target_date"`
}

// createPlanHandler creates a draft Plan via pm.CreatePlan with actor=agent. The
// AppService's requireProjectMember bounds the agent to its own projects (a
// foreign project → ErrNotMember → 403). target_date, when present, is RFC3339Nano.
func (s *Server) createPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req createPlanReq
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
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	var td *time.Time
	if strings.TrimSpace(req.TargetDate) != "" {
		t, perr := time.Parse(time.RFC3339Nano, req.TargetDate)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid_target_date", "target_date must be RFC3339")
			return
		}
		td = &t
	}
	planID, err := d.PMService.CreatePlan(r.Context(), pmservice.CreatePlanCommand{
		ProjectID:   pm.ProjectID(req.ProjectID),
		Name:        req.Name,
		Description: req.Description,
		TargetDate:  td,
		CreatedBy:   pm.IdentityRef(agentActor(a)),
	})
	if err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan_id": string(planID)})
}

// --- scaffold_cycle_plan -----------------------------------------------------

type scaffoldFeatureReq struct {
	Name    string `json:"name"`
	Branch  string `json:"branch"`
	DocOnly bool   `json:"doc_only"`
}

type scaffoldCyclePlanReq struct {
	AgentID         string               `json:"agent_id"`
	ProjectID       string               `json:"project_id"`
	Version         string               `json:"version"`
	Features        []scaffoldFeatureReq `json:"features"`
	MaxReviewRounds int                  `json:"max_review_rounds"`
	// SkipMergeCheck marks every Integrate node skip_merge_check at build time (T330),
	// standing F3's merge guard down for this cycle. Default false keeps merge-check on.
	SkipMergeCheck bool `json:"skip_merge_check"`
}

// scaffoldCyclePlanHandler builds a whole cycle CONTROL-FLOW graph (S0 → (Dev→
// Review→Decision{pass→Integrate, reject→Dev 有界})×N → 集成完成 Gate → Accept →
// Ship) in one call via pm.ScaffoldCyclePlan (actor=agent). Nodes are created
// UNASSIGNED (PD assigns owners next) and carry branch/base/role cycle metadata.
// Returns the draft plan id + created nodes + control-flow edges. Validation
// (version/features) is surfaced via mapPlanToolError.
func (s *Server) scaffoldCyclePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req scaffoldCyclePlanReq
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
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	features := make([]pmservice.CycleFeature, 0, len(req.Features))
	for _, f := range req.Features {
		features = append(features, pmservice.CycleFeature{
			Name: f.Name, Branch: f.Branch, DocOnly: f.DocOnly,
		})
	}
	res, err := d.PMService.ScaffoldCyclePlan(r.Context(), pmservice.ScaffoldCyclePlanCommand{
		ProjectID:       pm.ProjectID(req.ProjectID),
		Version:         req.Version,
		Features:        features,
		MaxReviewRounds: req.MaxReviewRounds,
		SkipMergeCheck:  req.SkipMergeCheck,
		CreatedBy:       pm.IdentityRef(agentActor(a)),
	})
	if err != nil {
		mapPlanToolError(w, err)
		return
	}
	nodes := make([]map[string]any, 0, len(res.Nodes))
	for _, n := range res.Nodes {
		nodes = append(nodes, map[string]any{
			"task_id": string(n.TaskID), "title": n.Title, "branch": n.Branch,
			"base": n.Base, "kind": n.Kind, "feature": n.Feature,
		})
	}
	edges := make([]map[string]any, 0, len(res.Edges))
	for _, e := range res.Edges {
		edges = append(edges, map[string]any{
			"from": string(e.From), "to": string(e.To),
			"kind": e.Kind, "when": e.When, "max_rounds": e.MaxRounds,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan_id": string(res.PlanID), "nodes": nodes, "edges": edges})
}

// --- add_task_to_plan --------------------------------------------------------

type planTaskReq struct {
	AgentID string `json:"agent_id"`
	PlanID  string `json:"plan_id"`
	TaskID  string `json:"task_id"`
}

// addTaskToPlanHandler selects a backlog task into a draft Plan via
// pm.SelectTaskIntoPlan (actor=agent). Draft-gating + project-scope guards
// (ErrPlanNotDraft / ErrPlanProjectMismatch / ErrTaskInOtherPlan) are enforced by
// the AppService and surfaced as tool errors.
func (s *Server) addTaskToPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanTask(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.SelectTaskIntoPlan(r.Context(), pm.PlanID(req.PlanID),
		pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// removeTaskFromPlanHandler removes a task from its Plan via
// pm.RemoveTaskFromPlan (actor=agent).
func (s *Server) removeTaskFromPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanTask(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.RemoveTaskFromPlan(r.Context(), pm.PlanID(req.PlanID),
		pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- add_plan_dependency / remove_plan_dependency ----------------------------

type planDepReq struct {
	AgentID    string `json:"agent_id"`
	PlanID     string `json:"plan_id"`
	FromTaskID string `json:"from_task_id"`
	ToTaskID   string `json:"to_task_id"`
}

// addPlanDependencyHandler adds a depends_on edge to a draft Plan's DAG via
// pm.AddPlanDependency (actor=agent). The repo rejects self-edges/cycles
// (ErrSelfDependency / ErrPlanCycle) before persisting → surfaced as tool errors.
func (s *Server) addPlanDependencyHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanDep(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.AddPlanDependency(r.Context(), pm.PlanID(req.PlanID),
		pm.TaskID(req.FromTaskID), pm.TaskID(req.ToTaskID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// removePlanDependencyHandler removes a depends_on edge from a draft Plan's DAG
// via pm.RemovePlanDependency (actor=agent). Idempotent (removing a missing edge
// is a no-op).
func (s *Server) removePlanDependencyHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanDep(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.RemovePlanDependency(r.Context(), pm.PlanID(req.PlanID),
		pm.TaskID(req.FromTaskID), pm.TaskID(req.ToTaskID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- start_plan / stop_plan --------------------------------------------------

type planIDReq struct {
	AgentID string `json:"agent_id"`
	PlanID  string `json:"plan_id"`
}

// startPlanHandler validates + moves a draft Plan to running via pm.StartPlan
// (actor=agent). Start-validation guards (ErrPlanNoTasks, ErrPlanCycle,
// ErrPlanUnassignedTask, ErrPlanUnresolvableAssignee, …) are enforced by the
// AppService and surfaced as tool errors.
func (s *Server) startPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanID(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.StartPlan(r.Context(), pm.PlanID(req.PlanID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// stopPlanHandler moves a running Plan back to draft via pm.StopPlan (actor=agent).
func (s *Server) stopPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanID(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.StopPlan(r.Context(), pm.PlanID(req.PlanID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// --- delete_plan / archive_plan ----------------------------------------------

// deletePlanHandler HARD-deletes a non-running Plan via pm.DeletePlan (actor=agent):
// its tasks are unloaded back to the backlog, its deps/dispatch-records + the plan
// row are removed, and its 1:1 conversation is hard-deleted (event-driven). A running
// plan is rejected (ErrPlanRunning → 409 plan_conflict; stop it first). The plan is
// gone, so it returns a bare deletion confirmation.
func (s *Server) deletePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanID(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.DeletePlan(r.Context(), pm.PlanID(req.PlanID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "plan_id": req.PlanID})
}

// archivePlanHandler archives a non-running Plan + CASCADE-archives its tasks via
// pm.ArchivePlan (actor=agent, irreversible). A running plan is rejected
// (ErrPlanRunning → 409); an already-archived plan is rejected (ErrPlanArchived →
// 409). Returns the archived Plan detail (mirrors get_plan / webconsole archive).
func (s *Server) archivePlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, req, ok := s.decodePlanID(w, r, d)
	if !ok {
		return
	}
	if err := d.PMService.ArchivePlan(r.Context(), pm.PlanID(req.PlanID), pm.IdentityRef(agentActor(a))); err != nil {
		mapPlanToolError(w, err)
		return
	}
	detail, derr := d.PMService.GetPlanDetail(r.Context(), pm.PlanID(req.PlanID))
	if derr != nil {
		mapPlanToolError(w, derr)
		return
	}
	writeJSON(w, http.StatusOK, planDetailMap(detail))
}

// --- get_plan / list_plans ---------------------------------------------------

type getPlanReq struct {
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
}

// getPlanHandler returns the full Plan DTO (the DERIVED node read model, §9.2) for
// a plan via pm.GetPlanDetail. It first resolves the plan and verifies it belongs
// to project_id (the same plan-in-project check the web handler does) so a caller
// cannot read a plan outside the project it named.
func (s *Server) getPlanHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getPlanReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// The guardrail (worker-from-token + agent-bound) is the read gate; the
	// resolved agent itself isn't needed for the project-scoped plan reads.
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return
	}
	detail, err := d.PMService.GetPlanDetail(r.Context(), pm.PlanID(req.PlanID))
	if err != nil {
		mapPlanToolError(w, err)
		return
	}
	// Plan-in-project check (mirrors the web pmRequirePlanInProject): a plan named
	// under the wrong project is not found here.
	if string(detail.Plan.ProjectID()) != req.ProjectID {
		writeError(w, http.StatusNotFound, "not_found", "plan not found in this project")
		return
	}
	writeJSON(w, http.StatusOK, planDetailMap(detail))
}

type listPlansReq struct {
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
	PageSize  int    `json:"page_size"` // optional; page window (default 50, max 100)
	Offset    int    `json:"offset"`    // optional; plans to skip (default 0)
}

// listPlansHandler returns the project's Plan summaries (the DERIVED board read
// model, §9.1/§9.2) via pm.ListPlanSummariesPage. SQL-windowed (page_size default
// 50 / max 100, offset) so a project with many plans can't overflow the
// tool-result token cap; returns {plans,total,page_size,offset,has_more}.
func (s *Server) listPlansHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listPlansReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	// The guardrail (worker-from-token + agent-bound) is the read gate; the
	// resolved agent itself isn't needed for the project-scoped plan reads.
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = agentListDefaultPageSize
	}
	if pageSize > agentListMaxPageSize {
		pageSize = agentListMaxPageSize
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	summaries, total, err := d.PMService.ListPlanSummariesPage(r.Context(), pm.ProjectID(req.ProjectID), pageSize, offset)
	if err != nil {
		mapPlanToolError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(summaries))
	for _, detail := range summaries {
		out = append(out, planSummaryMap(detail))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plans":     out,
		"total":     total,
		"page_size": pageSize,
		"offset":    offset,
		"has_more":  offset+len(out) < total,
	})
}

// --- list_unmerged_branches (v2.13.0 / I18 F4) -------------------------------

type listUnmergedReq struct {
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
}

// listUnmergedBranchesHandler returns the F4 unmerged-branch board for a plan: the
// `Integrate(T)` nodes that have NOT yet merged back into the integration trunk
// (un-done Integrate nodes, F1 spec §2.5/§8) — the PD's ship-gate reconciliation
// list. Like getPlanHandler it is project-scoped (a plan named under the wrong
// project is not found). When no cycle metadata is wired/present the board is
// empty (all_merged=true) rather than wrong.
func (s *Server) listUnmergedBranchesHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listUnmergedReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if _, ok := s.requireAgentOnWorker(w, r, d, req.AgentID); !ok {
		return
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		writeError(w, http.StatusBadRequest, "missing_project_id", "")
		return
	}
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return
	}
	board, err := d.PMService.ListUnmergedIntegrations(r.Context(), pm.PlanID(req.PlanID))
	if err != nil {
		mapPlanToolError(w, err)
		return
	}
	// Plan-in-project check (mirrors getPlanHandler): a plan named under the wrong
	// project is not found here.
	if string(board.Detail.Plan.ProjectID()) != req.ProjectID {
		writeError(w, http.StatusNotFound, "not_found", "plan not found in this project")
		return
	}
	writeJSON(w, http.StatusOK, unmergedBoardMap(board))
}

// unmergedBoardMap renders the F4 board DTO. Each row resolves its title / org_ref
// / assignee from the SAME PlanDetail load (planNodeLookups), so the board mirrors
// the plan node DTO without a second query.
func unmergedBoardMap(board *pmservice.UnmergedBoard) map[string]any {
	detail := board.Detail
	titleOf, assigneeOf, _, orgRefOf := planNodeLookups(detail)
	rows := make([]map[string]any, 0, len(board.Unmerged))
	for _, u := range board.Unmerged {
		row := map[string]any{
			"task_id":          string(u.TaskID),
			"title":            titleOf[u.TaskID],
			"assignee_ref":     string(assigneeOf[u.TaskID]),
			"node_status":      string(u.NodeStatus),
			"branch":           u.Branch,
			"base":             u.Base,
			"skip_merge_check": u.SkipMergeCheck,
		}
		if ref := orgRefOf[u.TaskID]; ref != "" {
			row["org_ref"] = ref
		}
		rows = append(rows, row)
	}
	return map[string]any{
		"plan_id":        string(detail.Plan.ID()),
		"project_id":     string(detail.Plan.ProjectID()),
		"plan_name":      detail.Plan.Name(),
		"plan_status":    string(detail.Plan.Status()),
		"all_merged":     board.AllMerged(),
		"unmerged_count": len(rows),
		"unmerged":       rows,
	}
}

// --- decode helpers ----------------------------------------------------------
//
// Each runs the SAME prologue every passthrough write tool uses: decode → require
// the agent on the worker (guardrail) → assert PMService wired → validate the ids.
// They return the resolved Agent (the actor source) + the parsed req on success;
// on any failure they have already written the error envelope.

func (s *Server) decodePlanTask(w http.ResponseWriter, r *http.Request, d HandlerDeps) (a *agent.Agent, req planTaskReq, ok bool) {
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return nil, req, false
	}
	ag, gateOK := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !gateOK {
		return nil, req, false
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return nil, req, false
	}
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return nil, req, false
	}
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return nil, req, false
	}
	return ag, req, true
}

func (s *Server) decodePlanDep(w http.ResponseWriter, r *http.Request, d HandlerDeps) (a *agent.Agent, req planDepReq, ok bool) {
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return nil, req, false
	}
	ag, gateOK := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !gateOK {
		return nil, req, false
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return nil, req, false
	}
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return nil, req, false
	}
	if strings.TrimSpace(req.FromTaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_from_task_id", "")
		return nil, req, false
	}
	if strings.TrimSpace(req.ToTaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_to_task_id", "")
		return nil, req, false
	}
	return ag, req, true
}

func (s *Server) decodePlanID(w http.ResponseWriter, r *http.Request, d HandlerDeps) (a *agent.Agent, req planIDReq, ok bool) {
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return nil, req, false
	}
	ag, gateOK := s.requireAgentOnWorker(w, r, d, req.AgentID)
	if !gateOK {
		return nil, req, false
	}
	if d.PMService == nil {
		writeError(w, http.StatusNotImplemented, "pm_not_wired", "")
		return nil, req, false
	}
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return nil, req, false
	}
	return ag, req, true
}

// --- serializers -------------------------------------------------------------
//
// planMap / planNodeMap / planDetailMap / planSummaryMap reproduce the canonical
// Plan wire shape the webconsole emits (handlers_pm_plans.go pmPlan*Map). The web
// mappers live in the webconsole/api package and can't be imported here, so these
// mirror them exactly (same keys, same RFC3339Nano timestamps) — mirroring the
// way agentTaskMap/agentIssueMap mirror webconsole's pmTaskMap/pmIssueMap.

func planMap(p *pm.Plan) map[string]any {
	m := map[string]any{
		"id": string(p.ID()), "project_id": string(p.ProjectID()), "name": p.Name(),
		"description": p.Description(), "status": string(p.Status()),
		"creator_ref": string(p.CreatorRef()), "conversation_id": p.ConversationID(),
		"created_at": p.CreatedAt().Format(time.RFC3339Nano),
		"updated_at": p.UpdatedAt().Format(time.RFC3339Nano),
		"version":    p.Version(),
		"is_builtin": p.IsBuiltin(), // ADR-0047: the per-project assignment pool (vs a structured plan)
	}
	if d := p.TargetDate(); d != nil {
		m["target_date"] = d.Format(time.RFC3339Nano)
	}
	return m
}

func planNodeMap(planID pm.PlanID, n pm.PlanNodeView, titleOf map[pm.TaskID]string, assigneeOf map[pm.TaskID]pm.IdentityRef, archivedOf map[pm.TaskID]bool, orgRefOf map[pm.TaskID]string) map[string]any {
	depends := make([]string, 0, len(n.DependsOn))
	for _, d := range n.DependsOn {
		depends = append(depends, string(d))
	}
	node := map[string]any{
		"task_id":      string(n.TaskID),
		"title":        titleOf[n.TaskID],
		"assignee_ref": string(assigneeOf[n.TaskID]),
		"task_status":  string(n.TaskStatus),
		"node_status":  string(n.NodeStatus),
		"depends_on":   depends,
		// ADR-0047: the DERIVED claimable predicate, computed where the plan view is
		// available (the node already carries node_status; the lookups supply the
		// archived/assignee inputs). True iff the task can be claimed (open→running)
		// right now: not archived, open, assigned, in this plan, node dispatched.
		"claimable": pm.Claimable(archivedOf[n.TaskID], n.TaskStatus, assigneeOf[n.TaskID], planID, n.NodeStatus),
	}
	// v2.9.2 (task-0543ece9): the human Task id (org_ref "T123") rides on the node
	// DTO so the agent-facing plan list mirrors the web board (T-number shown
	// without a second resolver). Omitted when unallocated (orgNumber 0).
	if ref := orgRefOf[n.TaskID]; ref != "" {
		node["org_ref"] = ref
	}
	if n.Dispatched && !n.DispatchedAt.IsZero() {
		node["dispatched_at"] = n.DispatchedAt.Format(time.RFC3339Nano)
	}
	return node
}

func planNodeLookups(detail *pmservice.PlanDetail) (map[pm.TaskID]string, map[pm.TaskID]pm.IdentityRef, map[pm.TaskID]bool, map[pm.TaskID]string) {
	titleOf := make(map[pm.TaskID]string, len(detail.Tasks))
	assigneeOf := make(map[pm.TaskID]pm.IdentityRef, len(detail.Tasks))
	archivedOf := make(map[pm.TaskID]bool, len(detail.Tasks))
	orgRefOf := make(map[pm.TaskID]string, len(detail.Tasks))
	for _, t := range detail.Tasks {
		titleOf[t.ID()] = t.Title()
		assigneeOf[t.ID()] = t.Assignee()
		archivedOf[t.ID()] = t.IsArchived()
		// v2.7.1 #245 T<n> token, omitted (left "") when unallocated — matches the
		// agentTaskMap pattern in this package (no cross-package orgRefToken here).
		if n := t.OrgNumber(); n > 0 {
			orgRefOf[t.ID()] = "T" + strconv.Itoa(n)
		}
	}
	return titleOf, assigneeOf, archivedOf, orgRefOf
}

// planDetailMap renders the full Plan DTO with the DERIVED node read model (§9.2):
// nodes + ready_set + has_failed + progress{done,total}.
func planDetailMap(detail *pmservice.PlanDetail) map[string]any {
	m := planMap(detail.Plan)
	titleOf, assigneeOf, archivedOf, orgRefOf := planNodeLookups(detail)
	nodes := make([]map[string]any, 0, len(detail.View.Nodes))
	for _, n := range detail.View.Nodes {
		nodes = append(nodes, planNodeMap(detail.Plan.ID(), n, titleOf, assigneeOf, archivedOf, orgRefOf))
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

// planSummaryMap renders a Plan for the list tool: the bare Plan fields plus the
// DERIVED board summary (progress, has_failed, node_count, nodes_preview).
//
// v2.9.2 (task-0543ece9): the preview is NO LONGER capped — it carries EVERY node,
// mirroring the web list endpoint (pmPlanSummaryMap) so the agent-facing list and
// the human Work Board stay byte-identical and neither silently truncates the task
// set. node_count stays == len(nodes_preview).
func planSummaryMap(detail *pmservice.PlanDetail) map[string]any {
	m := planMap(detail.Plan)
	titleOf, assigneeOf, archivedOf, orgRefOf := planNodeLookups(detail)
	nodes := detail.View.Nodes
	preview := make([]map[string]any, 0, len(nodes))
	for _, nd := range nodes {
		preview = append(preview, planNodeMap(detail.Plan.ID(), nd, titleOf, assigneeOf, archivedOf, orgRefOf))
	}
	m["progress"] = map[string]any{"done": detail.View.Progress.Done, "total": detail.View.Progress.Total}
	m["has_failed"] = detail.View.HasFailed
	m["node_count"] = len(nodes)
	m["nodes_preview"] = preview
	return m
}

// --- claim_task (T83: open-claim of a built-in assignment-pool task) ---------

type claimTaskReq struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

// claimTaskHandler is the agent's claim entry point for the built-in assignment
// pool (T83). The agent sees an open pool task in get_my_work and claims it here
// (pool tasks have no WorkItem, so start_work does not apply). ClaimPoolTask
// atomically assigns it to the caller + moves it open→running, fail-closed on
// project membership, holding cap, and concurrency.
func (s *Server) claimTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req claimTaskReq
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
	// T190: a backlog (inert) task is not claimable — return the unified
	// add-to-plan/pool guidance rather than the generic not_claimable. A
	// structured-plan node / already-claimed / wrong-status task still falls through
	// to ClaimPoolTask's own not_claimable / already_claimed below.
	if s.rejectIfBacklog(w, r, d, req.TaskID, "claiming") {
		return
	}
	if err := d.PMService.ClaimPoolTask(r.Context(), pm.TaskID(req.TaskID), pm.IdentityRef(agentActor(a))); err != nil {
		writeClaimError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": req.TaskID,
		"status":  string(pm.TaskRunning),
		"claimed": true,
	})
}

// writeClaimError maps ClaimPoolTask errors. Authz + existence errors collapse to
// ONE opaque 404 (T83 §4.3 — never reveal whether a task exists or sits in a
// project the agent can't see). Claimability / concurrency / cap are explicit 409s.
func writeClaimError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pmservice.ErrNotMember),
		errors.Is(err, pm.ErrProjectNotFound),
		errors.Is(err, pm.ErrTaskNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, pm.ErrTaskNotClaimable):
		writeError(w, http.StatusConflict, "not_claimable", err.Error())
	case errors.Is(err, pm.ErrTaskAlreadyClaimed):
		writeError(w, http.StatusConflict, "already_claimed", err.Error())
	case errors.Is(err, pm.ErrPoolClaimLimitReached):
		writeError(w, http.StatusConflict, "pool_claim_limit_reached", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "claim_error", err.Error())
	}
}

// list_assignment_pool was removed in WS2 (#issue-e346e5ec). v2.14.0 F7 (issue
// I14): get_my_work was removed too (AgentWorkItem retired); the agent's runnable
// work — including the claimable pool — is now served by list_my_tasks.

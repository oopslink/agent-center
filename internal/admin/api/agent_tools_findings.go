package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// =============================================================================
// Agent MCP passthrough tools — Plan Shared Findings (v2.10, ADR-0053 — the DeLM
// "shared verified context" minimal slice). Thin wrappers over the pm Finding
// AppServices so an agent can record a compact finding (gist) back to its Plan and
// read a plan's findings via MCP. MIRRORS agent_tools_plans.go EXACTLY: parse args
// → require the agent on the worker (the b1 guardrail) → call the pm AppService with
// actor = agent business identity (agentActor) → map result/error. NO new domain
// logic. The ADMISSION gate (author == the source task's assignee) lives in the
// AppService (RecordFinding), not here.
// =============================================================================

// mapFindingToolError translates Finding-specific sentinel errors to the tool-error
// envelope, then defers to mapDomainError for everything else (e.g. ErrNotMember →
// 403). not-found → 404; admission/validation guards → 422; archived project → 409;
// retract-forbidden → 403; unwired services → 501.
func mapFindingToolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pm.ErrPlanNotFound), errors.Is(err, pm.ErrTaskNotFound),
		errors.Is(err, pm.ErrPlanFindingNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pmservice.ErrFindingsUnavailable), errors.Is(err, pmservice.ErrPlansUnavailable):
		writeError(w, http.StatusNotImplemented, "pm_not_wired", err.Error())
	case errors.Is(err, pm.ErrProjectArchived), errors.Is(err, pm.ErrPlanFindingExists):
		writeError(w, http.StatusConflict, "plan_conflict", err.Error())
	case errors.Is(err, pm.ErrFindingNotTaskAssignee), errors.Is(err, pm.ErrFindingTaskNotInPlan),
		errors.Is(err, pm.ErrInvalidFindingKind), errors.Is(err, pm.ErrEmptyFindingContent),
		errors.Is(err, pm.ErrFindingContentTooLong):
		writeError(w, http.StatusUnprocessableEntity, "invalid_finding", err.Error())
	case errors.Is(err, pm.ErrFindingForbidden):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	default:
		mapDomainError(w, err)
	}
}

// --- record_finding ----------------------------------------------------------

type recordFindingReq struct {
	AgentID string `json:"agent_id"`
	PlanID  string `json:"plan_id"`
	TaskID  string `json:"task_id"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// recordFindingHandler records a finding via pm.RecordFinding with actor = the
// calling agent. The AppService enforces the admission gate (author == the source
// task's assignee, task in plan, member, project mutable). Returns {finding_id}.
func (s *Server) recordFindingHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req recordFindingReq
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
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return
	}
	if strings.TrimSpace(req.TaskID) == "" {
		writeError(w, http.StatusBadRequest, "missing_task_id", "")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "missing_content", "")
		return
	}
	id, err := d.PMService.RecordFinding(r.Context(), pmservice.RecordFindingCommand{
		PlanID:    pm.PlanID(req.PlanID),
		TaskID:    pm.TaskID(req.TaskID),
		AuthorRef: pm.IdentityRef(agentActor(a)),
		Kind:      pm.PlanFindingKind(req.Kind),
		Content:   req.Content,
	})
	if err != nil {
		mapFindingToolError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"finding_id": string(id)})
}

// --- list_findings -----------------------------------------------------------

type listFindingsReq struct {
	AgentID string `json:"agent_id"`
	PlanID  string `json:"plan_id"`
}

// listFindingsHandler returns a plan's shared findings (oldest-first) via
// pm.ListPlanFindings. Auth is two-layer: the worker-bound guardrail proves the
// caller, and the AppService requires the agent be a MEMBER of the plan's project
// (review #2) — a worker-bound agent cannot read another project's findings by
// guessing a plan_id.
func (s *Server) listFindingsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listFindingsReq
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
	if strings.TrimSpace(req.PlanID) == "" {
		writeError(w, http.StatusBadRequest, "missing_plan_id", "")
		return
	}
	findings, err := d.PMService.ListPlanFindings(r.Context(), pm.PlanID(req.PlanID), pm.IdentityRef(agentActor(a)))
	if err != nil {
		mapFindingToolError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		out = append(out, findingToolMap(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": out})
}

// findingToolMap renders a finding for the list_findings tool wire shape.
func findingToolMap(f *pm.PlanFinding) map[string]any {
	return map[string]any{
		"finding_id": string(f.ID()),
		"plan_id":    string(f.PlanID()),
		"task_id":    string(f.TaskID()),
		"author_ref": string(f.AuthorRef()),
		"kind":       string(f.Kind()),
		"content":    f.Content(),
		"created_at": f.CreatedAt().Format(time.RFC3339Nano),
	}
}

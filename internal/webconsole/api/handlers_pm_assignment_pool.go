package api

import (
	"net/http"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func (s *Server) pmGetAssignmentPoolHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	project, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	detail, err := d.PM.GetAssignmentPool(r.Context(), project.ID(), caller)
	if err != nil {
		mapPMError(w, err)
		return
	}
	tasks := make([]map[string]any, 0, len(detail.Tasks))
	for _, view := range detail.Tasks {
		row := pmTaskMap(view.Task)
		row["priority"] = view.Membership.Priority
		row["claimable"] = view.Claimable
		row["starved"] = view.Starved
		row["claimed_by"] = string(view.Membership.ClaimedBy)
		row["claim_expires_at"] = ""
		if !view.Membership.ClaimExpiresAt.IsZero() {
			row["claim_expires_at"] = view.Membership.ClaimExpiresAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
		}
		tasks = append(tasks, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": string(detail.Pool.ID()), "project_id": string(detail.Pool.ProjectID()),
		"scheduling_class": detail.Pool.SchedulingClass(), "auto_assign_enabled": detail.Pool.AutoAssignEnabled(),
		"holding_cap": detail.Pool.HoldingCap(), "tasks": tasks,
	})
}

func (s *Server) pmAddAssignmentPoolTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	project, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	var req struct {
		TaskID   string `json:"task_id"`
		Priority int    `json:"priority"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if err := d.PM.AddTaskToAssignmentPool(r.Context(), project.ID(), pm.TaskID(req.TaskID), req.Priority, caller); err != nil {
		mapPMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) pmRemoveAssignmentPoolTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	project, caller, ok := s.pmRequireProjectInOrg(w, r, d)
	if !ok {
		return
	}
	if err := d.PM.RemoveTaskFromAssignmentPool(r.Context(), project.ID(), pm.TaskID(r.PathValue("task_id")), caller); err != nil {
		mapPMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

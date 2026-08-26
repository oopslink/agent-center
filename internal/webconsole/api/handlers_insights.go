package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

const insightWindow = 24 * time.Hour

type insightTaskExecution struct {
	TaskID            string
	OrgRef            string
	Title             string
	ProjectID         string
	ProjectName       string
	AgentID           string
	AgentName         string
	Status            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	StatusChangedAt   time.Time
	CompletedAt       time.Time
	AssignedAt        time.Time
	StartedAt         time.Time
	TerminalAt        time.Time
	QueueWaitSeconds  *int64
	DurationSeconds   *int64
	ConstituentScopes []string
}

func (s *Server) insightsOverviewHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PM == nil || d.PMTaskActions == nil {
		writeError(w, http.StatusNotImplemented, "insights_not_wired", "insights task query is not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	rows, projects, agents, window, err := s.insightRows(r, d, orgID)
	if err != nil {
		mapPMError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.insightOverviewJSON(rows, agents, projects, window))
}

func (s *Server) insightsTaskExecutionsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.PM == nil || d.PMTaskActions == nil {
		writeError(w, http.StatusNotImplemented, "insights_not_wired", "insights task query is not wired")
		return
	}
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	rows, _, _, window, err := s.insightRows(r, d, orgID)
	if err != nil {
		mapPMError(w, err)
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))
	value := strings.TrimSpace(r.URL.Query().Get("value"))
	rows = filterInsightRows(rows, filter, value)
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, insightTaskExecutionJSON(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window":       window,
		"refreshed_at": window["refreshed_at"],
		"freshness":    "fresh",
		"stale":        false,
		"items":        items,
		"total":        len(items),
		"filter":       filter,
		"value":        value,
	})
}

func (s *Server) insightRows(r *http.Request, d HandlerDeps, orgID string) ([]insightTaskExecution, map[string]*pm.Project, map[string]*agentbc.Agent, map[string]any, error) {
	now := time.Now().UTC()
	start := now.Add(-insightWindow)
	window := map[string]any{
		"label":        "past_24h",
		"started_at":   start.Format(time.RFC3339Nano),
		"ended_at":     now.Format(time.RFC3339Nano),
		"refreshed_at": now.Format(time.RFC3339Nano),
	}
	projects, err := d.PM.ListProjects(r.Context(), orgID)
	if err != nil {
		return nil, nil, nil, window, err
	}
	q := pm.OrgListQuery{
		ProjectIDs:      projectIDs(projects),
		Statuses:        []string{string(pm.TaskOpen), string(pm.TaskRunning), string(pm.TaskBlocked), string(pm.TaskCompleted), string(pm.TaskDiscarded)},
		UpdatedAfter:    &start,
		SortColumn:      "updated_at",
		SortDesc:        true,
		Limit:           10000,
		IncludeArchived: false,
		ExcludeStatuses: nil,
		CreatedAfter:    nil,
		CreatedBefore:   nil,
		UpdatedBefore:   nil,
	}
	tasks, _, err := d.PM.ListTasksOrgPage(r.Context(), q)
	if err != nil {
		return nil, nil, nil, window, err
	}
	projectByID := projectByID(projects)
	agents := map[string]*agentbc.Agent{}
	if d.AgentSvc != nil {
		if as, aerr := d.AgentSvc.ListAgents(r.Context(), orgID); aerr == nil {
			for _, a := range as {
				if a.IdentityMemberID() != "" {
					agents[a.IdentityMemberID()] = a
				}
			}
		}
	}
	rows := make([]insightTaskExecution, 0, len(tasks))
	for _, t := range tasks {
		agentID := agentMemberIDFromRef(t.Assignee())
		if agentID == "" {
			continue
		}
		p := projectByID[string(t.ProjectID())]
		if p == nil {
			continue
		}
		logs, err := d.PMTaskActions.ListByTask(r.Context(), t.ID())
		if err != nil {
			return nil, nil, nil, window, err
		}
		row := insightRowFromTask(t, p, agents[agentID], logs)
		if row.UpdatedAt.Before(start) {
			continue
		}
		rows = append(rows, row)
	}
	return rows, projectByID, agents, window, nil
}

func insightRowFromTask(t *pm.Task, p *pm.Project, a *agentbc.Agent, logs []pm.TaskActionLog) insightTaskExecution {
	row := insightTaskExecution{
		TaskID:          string(t.ID()),
		Title:           t.Title(),
		ProjectID:       string(p.ID()),
		ProjectName:     p.Name(),
		AgentID:         agentMemberIDFromRef(t.Assignee()),
		Status:          string(t.Status()),
		CreatedAt:       t.CreatedAt().UTC(),
		UpdatedAt:       t.UpdatedAt().UTC(),
		StatusChangedAt: t.StatusChangedAt().UTC(),
		CompletedAt:     t.CompletedAt().UTC(),
	}
	if n := t.OrgNumber(); n > 0 {
		row.OrgRef = "T" + strconv.Itoa(n)
	}
	if a != nil {
		row.AgentName = a.Profile().Name
	}
	for _, lg := range logs {
		switch lg.Action {
		case pm.TaskActionAssigned, pm.TaskActionReassigned:
			row.AssignedAt = lg.OccurredAt.UTC()
		case pm.TaskActionAgentStarted:
			if row.StartedAt.IsZero() {
				row.StartedAt = lg.OccurredAt.UTC()
			}
		}
	}
	if row.AssignedAt.IsZero() {
		row.AssignedAt = row.CreatedAt
	}
	if row.StartedAt.IsZero() && t.Status() == pm.TaskRunning {
		row.StartedAt = row.StatusChangedAt
	}
	if row.StartedAt.IsZero() && !row.StatusChangedAt.IsZero() && (t.Status() == pm.TaskCompleted || t.Status() == pm.TaskDiscarded) {
		row.StartedAt = row.CreatedAt
	}
	if !row.StartedAt.IsZero() && !row.AssignedAt.IsZero() && !row.StartedAt.Before(row.AssignedAt) {
		v := int64(row.StartedAt.Sub(row.AssignedAt).Seconds())
		row.QueueWaitSeconds = &v
	}
	if t.Status() == pm.TaskCompleted {
		row.TerminalAt = row.CompletedAt
		if row.TerminalAt.IsZero() {
			row.TerminalAt = row.StatusChangedAt
		}
	} else if t.Status() == pm.TaskDiscarded {
		row.TerminalAt = row.StatusChangedAt
	}
	if !row.TerminalAt.IsZero() && !row.StartedAt.IsZero() && !row.TerminalAt.Before(row.StartedAt) {
		v := int64(row.TerminalAt.Sub(row.StartedAt).Seconds())
		row.DurationSeconds = &v
	}
	row.ConstituentScopes = insightScopes(row)
	return row
}

func (s *Server) insightOverviewJSON(rows []insightTaskExecution, agents map[string]*agentbc.Agent, projects map[string]*pm.Project, window map[string]any) map[string]any {
	execCount := len(rows)
	failures := 0
	var waits, durations []int64
	agentRows := map[string][]insightTaskExecution{}
	projectRows := map[string][]insightTaskExecution{}
	running := 0
	for _, row := range rows {
		if row.Status == string(pm.TaskDiscarded) {
			failures++
		}
		if row.QueueWaitSeconds != nil {
			waits = append(waits, *row.QueueWaitSeconds)
		}
		if row.DurationSeconds != nil {
			durations = append(durations, *row.DurationSeconds)
		}
		if row.Status == string(pm.TaskRunning) {
			running++
		}
		agentRows[row.AgentID] = append(agentRows[row.AgentID], row)
		projectRows[row.ProjectID] = append(projectRows[row.ProjectID], row)
	}
	capacity := 0
	for _, a := range agents {
		capacity += a.Profile().EffectiveConcurrencyCap()
	}
	var utilization any
	if capacity > 0 {
		utilization = float64(running) / float64(capacity)
	}
	return map[string]any{
		"window":       window,
		"refreshed_at": window["refreshed_at"],
		"freshness":    "fresh",
		"stale":        false,
		"summary": map[string]any{
			"executions":   execCount,
			"failures":     failures,
			"failure_rate": nullableRatio(failures, execCount),
			"slot_utilization": map[string]any{
				"running":     running,
				"capacity":    capacity,
				"utilization": utilization,
			},
			"queue_wait": map[string]any{
				"p50_seconds": percentile(waits, 50),
				"p95_seconds": percentile(waits, 95),
			},
			"execution_duration": map[string]any{
				"p50_seconds": percentile(durations, 50),
				"p95_seconds": percentile(durations, 95),
			},
		},
		"leaderboards": map[string]any{
			"agents": leaderboard(agentRows, func(id string) string {
				if a := agents[id]; a != nil {
					return a.Profile().Name
				}
				return id
			}),
			"projects": leaderboard(projectRows, func(id string) string {
				if p := projects[id]; p != nil {
					return p.Name()
				}
				return id
			}),
		},
	}
}

func projectIDs(projects []*pm.Project) []pm.ProjectID {
	ids := make([]pm.ProjectID, 0, len(projects))
	for _, p := range projects {
		if p.Status() != pm.ProjectArchived {
			ids = append(ids, p.ID())
		}
	}
	return ids
}

func agentMemberIDFromRef(ref pm.IdentityRef) string {
	const prefix = "agent:"
	s := string(ref)
	if strings.HasPrefix(s, prefix) {
		return strings.TrimPrefix(s, prefix)
	}
	return ""
}

func insightScopes(row insightTaskExecution) []string {
	scopes := []string{"executions", "agent:" + row.AgentID, "project:" + row.ProjectID}
	if row.Status == string(pm.TaskDiscarded) {
		scopes = append(scopes, "failures")
	}
	if row.QueueWaitSeconds != nil {
		scopes = append(scopes, "queue_wait")
	}
	if row.DurationSeconds != nil {
		scopes = append(scopes, "execution_duration")
	}
	if row.Status == string(pm.TaskRunning) {
		scopes = append(scopes, "slot_utilization")
	}
	return scopes
}

func filterInsightRows(rows []insightTaskExecution, filter, value string) []insightTaskExecution {
	if filter == "" {
		return rows
	}
	out := make([]insightTaskExecution, 0, len(rows))
	for _, row := range rows {
		if filter == "agent" && row.AgentID == value {
			out = append(out, row)
		}
		if filter == "project" && row.ProjectID == value {
			out = append(out, row)
		}
		if filter == "metric" {
			for _, scope := range row.ConstituentScopes {
				if scope == value {
					out = append(out, row)
					break
				}
			}
		}
	}
	return out
}

func insightTaskExecutionJSON(row insightTaskExecution) map[string]any {
	m := map[string]any{
		"task_id":            row.TaskID,
		"org_ref":            row.OrgRef,
		"title":              row.Title,
		"project_id":         row.ProjectID,
		"project_name":       row.ProjectName,
		"agent_id":           row.AgentID,
		"agent_name":         row.AgentName,
		"status":             row.Status,
		"created_at":         row.CreatedAt.Format(time.RFC3339Nano),
		"updated_at":         row.UpdatedAt.Format(time.RFC3339Nano),
		"status_changed_at":  row.StatusChangedAt.Format(time.RFC3339Nano),
		"constituent_scopes": row.ConstituentScopes,
	}
	if !row.CompletedAt.IsZero() {
		m["completed_at"] = row.CompletedAt.Format(time.RFC3339Nano)
	}
	if !row.AssignedAt.IsZero() {
		m["assigned_at"] = row.AssignedAt.Format(time.RFC3339Nano)
	}
	if !row.StartedAt.IsZero() {
		m["started_at"] = row.StartedAt.Format(time.RFC3339Nano)
	}
	if !row.TerminalAt.IsZero() {
		m["terminal_at"] = row.TerminalAt.Format(time.RFC3339Nano)
	}
	if row.QueueWaitSeconds != nil {
		m["queue_wait_seconds"] = *row.QueueWaitSeconds
	}
	if row.DurationSeconds != nil {
		m["duration_seconds"] = *row.DurationSeconds
	}
	return m
}

func nullableRatio(n, d int) any {
	if d == 0 {
		return nil
	}
	return float64(n) / float64(d)
}

func percentile(values []int64, pct int) any {
	if len(values) == 0 {
		return nil
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := (pct*len(cp) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(cp) {
		idx = len(cp)
	}
	return cp[idx-1]
}

func leaderboard(groups map[string][]insightTaskExecution, name func(string) string) []map[string]any {
	rows := make([]map[string]any, 0, len(groups))
	for id, executions := range groups {
		failures := 0
		for _, row := range executions {
			if row.Status == string(pm.TaskDiscarded) {
				failures++
			}
		}
		rows = append(rows, map[string]any{
			"id":           id,
			"name":         name(id),
			"executions":   len(executions),
			"failures":     failures,
			"failure_rate": nullableRatio(failures, len(executions)),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		ai, _ := rows[i]["executions"].(int)
		aj, _ := rows[j]["executions"].(int)
		if ai != aj {
			return ai > aj
		}
		return rows[i]["name"].(string) < rows[j]["name"].(string)
	})
	if len(rows) > 5 {
		return rows[:5]
	}
	return rows
}

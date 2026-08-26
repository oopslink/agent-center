package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const insightsWindow = 24 * time.Hour

type insightsExecutionRow struct {
	ExecutionID       string `json:"execution_id"`
	TaskID            string `json:"task_id"`
	TaskTitle         string `json:"task_title,omitempty"`
	TaskOrgRef        string `json:"task_org_ref,omitempty"`
	ProjectID         string `json:"project_id,omitempty"`
	ProjectName       string `json:"project_name,omitempty"`
	AgentID           string `json:"agent_id,omitempty"`
	Status            string `json:"status"`
	StatusReason      string `json:"status_reason,omitempty"`
	StatusDetail      string `json:"status_detail,omitempty"`
	Attempt           int    `json:"attempt"`
	SubmittedAt       string `json:"submitted_at"`
	StartedAt         string `json:"started_at,omitempty"`
	CompletedAt       string `json:"completed_at,omitempty"`
	QueueWaitMs       int64  `json:"queue_wait_ms"`
	DurationMs        int64  `json:"duration_ms"`
	CommandID         string `json:"command_id,omitempty"`
	WorkerID          string `json:"worker_id,omitempty"`
	RefreshedAt       string `json:"refreshed_at"`
	Freshness         string `json:"freshness"`
	CurrentActivity   string `json:"current_activity,omitempty"`
	TotalToolCalls    int64  `json:"total_tool_calls"`
	TotalTokensInput  int64  `json:"total_tokens_input"`
	TotalTokensOutput int64  `json:"total_tokens_output"`
}

type insightsOverviewResponse struct {
	Window      map[string]string      `json:"window"`
	RefreshedAt string                 `json:"refreshed_at"`
	Freshness   string                 `json:"freshness"`
	Metrics     map[string]any         `json:"metrics"`
	Agents      []insightsRankRow      `json:"agents"`
	Projects    []insightsRankRow      `json:"projects"`
	Executions  []insightsExecutionRow `json:"executions"`
}

type insightsRankRow struct {
	ID          string  `json:"id"`
	Name        string  `json:"name,omitempty"`
	Executions  int     `json:"executions"`
	Failures    int     `json:"failures"`
	FailureRate float64 `json:"failure_rate"`
}

func (s *Server) insightsOverviewHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if d.DB == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "insights requires database access")
		return
	}
	now := time.Now().UTC()
	from := now.Add(-insightsWindow)
	rows, err := loadInsightExecutions(r, d.DB, orgID, from, now, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insights_query_failed", err.Error())
		return
	}
	resp := buildInsightsOverview(rows, from, now)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) insightsExecutionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if d.DB == nil {
		writeError(w, http.StatusNotImplemented, "not_configured", "insights requires database access")
		return
	}
	executionID := strings.TrimSpace(r.PathValue("execution_id"))
	if executionID == "" {
		writeError(w, http.StatusBadRequest, "missing_execution_id", "execution_id required")
		return
	}
	now := time.Now().UTC()
	rows, err := loadInsightExecutions(r, d.DB, orgID, now.Add(-365*24*time.Hour), now, executionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "insights_query_failed", err.Error())
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "execution not found")
		return
	}
	writeJSON(w, http.StatusOK, rows[0])
}

func loadInsightExecutions(r *http.Request, db *sql.DB, orgID string, from, now time.Time, executionID string) ([]insightsExecutionRow, error) {
	q := `SELECT
		COALESCE(wce.execution_id, ''),
		wce.id,
		wce.worker_id,
		COALESCE(wce.agent_id, ''),
		COALESCE(wce.task_id, ''),
		COALESCE(wce.status, ''),
		COALESCE(wce.status_reason, ''),
		COALESCE(wce.status_detail, ''),
		COALESCE(wce.payload, ''),
		wce.created_at,
		COALESCE(wce.status_updated_at, ''),
		COALESCE(t.title, ''),
		COALESCE(t.org_number, 0),
		COALESCE(t.status, ''),
		COALESCE(t.blocked_reason, ''),
		COALESCE(t.updated_at, ''),
		COALESCE(t.completed_at, ''),
		COALESCE(p.id, ''),
		COALESCE(p.name, '')
	FROM worker_control_events wce
	JOIN agents a ON a.id = wce.agent_id
	LEFT JOIN pm_tasks t ON t.id = wce.task_id
	LEFT JOIN pm_projects p ON p.id = t.project_id
	WHERE a.organization_id = ?
	  AND wce.command_type IN ('agent.execute_task', 'agent.converse')
	  AND wce.created_at >= ?
	  AND wce.created_at <= ?`
	args := []any{orgID, from.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)}
	if executionID != "" {
		q += ` AND wce.execution_id = ?`
		args = append(args, executionID)
	}
	q += ` ORDER BY wce.created_at DESC, wce.id DESC LIMIT 200`
	sqlRows, err := db.QueryContext(r.Context(), q, args...)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()
	out := make([]insightsExecutionRow, 0)
	for sqlRows.Next() {
		var (
			row                                                                                        insightsExecutionRow
			payload, createdAt, statusUpdatedAt, taskStatus, blockedReason, taskUpdatedAt, completedAt string
			orgNumber                                                                                  int
		)
		if err := sqlRows.Scan(&row.ExecutionID, &row.CommandID, &row.WorkerID, &row.AgentID, &row.TaskID,
			&row.Status, &row.StatusReason, &row.StatusDetail, &payload, &createdAt, &statusUpdatedAt,
			&row.TaskTitle, &orgNumber, &taskStatus, &blockedReason, &taskUpdatedAt, &completedAt,
			&row.ProjectID, &row.ProjectName); err != nil {
			return nil, err
		}
		if row.ExecutionID == "" {
			row.ExecutionID = row.CommandID
		}
		if row.Status == "" {
			row.Status = taskStatus
		}
		row.Attempt = payloadAttempt(payload)
		if row.Attempt == 0 {
			row.Attempt = 1
		}
		if orgNumber > 0 {
			row.TaskOrgRef = "T" + strconv.Itoa(orgNumber)
		}
		row.SubmittedAt = createdAt
		submitted := parseInsightTime(createdAt)
		updated := parseInsightTime(statusUpdatedAt)
		if updated.IsZero() {
			updated = parseInsightTime(taskUpdatedAt)
		}
		if !updated.IsZero() {
			row.StartedAt = updated.Format(time.RFC3339Nano)
		}
		completed := parseInsightTime(completedAt)
		if isInsightTerminal(row.Status) {
			if completed.IsZero() {
				completed = updated
			}
			if !completed.IsZero() {
				row.CompletedAt = completed.Format(time.RFC3339Nano)
			}
		}
		if !submitted.IsZero() && !updated.IsZero() && updated.After(submitted) {
			row.QueueWaitMs = updated.Sub(submitted).Milliseconds()
		}
		if !submitted.IsZero() && !completed.IsZero() && completed.After(submitted) {
			row.DurationMs = completed.Sub(submitted).Milliseconds()
		}
		row.CurrentActivity = blockedReason
		row.RefreshedAt = now.Format(time.RFC3339Nano)
		row.Freshness = insightFreshness(now, updated)
		out = append(out, row)
	}
	return out, sqlRows.Err()
}

func payloadAttempt(payload string) int {
	if strings.TrimSpace(payload) == "" {
		return 0
	}
	var p struct {
		Attempt int `json:"attempt"`
	}
	_ = json.Unmarshal([]byte(payload), &p)
	return p.Attempt
}

func buildInsightsOverview(rows []insightsExecutionRow, from, now time.Time) insightsOverviewResponse {
	failures := 0
	queue := make([]int64, 0)
	durations := make([]int64, 0)
	active := 0
	agents := map[string]*insightsRankRow{}
	projects := map[string]*insightsRankRow{}
	for _, row := range rows {
		failed := isInsightFailure(row.Status)
		if failed {
			failures++
		}
		if row.QueueWaitMs > 0 {
			queue = append(queue, row.QueueWaitMs)
		}
		if row.DurationMs > 0 {
			durations = append(durations, row.DurationMs)
		}
		if strings.EqualFold(row.Status, "started") || strings.EqualFold(row.Status, "running") || strings.EqualFold(row.Status, "active") {
			active++
		}
		if row.AgentID != "" {
			r := agents[row.AgentID]
			if r == nil {
				r = &insightsRankRow{ID: row.AgentID, Name: row.AgentID}
				agents[row.AgentID] = r
			}
			r.Executions++
			if failed {
				r.Failures++
			}
		}
		if row.ProjectID != "" {
			r := projects[row.ProjectID]
			if r == nil {
				r = &insightsRankRow{ID: row.ProjectID, Name: row.ProjectName}
				projects[row.ProjectID] = r
			}
			r.Executions++
			if failed {
				r.Failures++
			}
		}
	}
	total := len(rows)
	return insightsOverviewResponse{
		Window: map[string]string{
			"from":  from.Format(time.RFC3339Nano),
			"to":    now.Format(time.RFC3339Nano),
			"label": "past_24h",
		},
		RefreshedAt: now.Format(time.RFC3339Nano),
		Freshness:   insightFreshness(now, newestInsightUpdate(rows)),
		Metrics: map[string]any{
			"execution_count":           total,
			"failure_rate":              ratio(failures, total),
			"slot_utilization":          ratio(active, max(total, 1)),
			"queue_wait_p50_ms":         percentile(queue, 50),
			"queue_wait_p95_ms":         percentile(queue, 95),
			"execution_duration_p50_ms": percentile(durations, 50),
			"execution_duration_p95_ms": percentile(durations, 95),
		},
		Agents:     rankRows(agents),
		Projects:   rankRows(projects),
		Executions: rows,
	}
}

func parseInsightTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func insightFreshness(now, updated time.Time) string {
	if updated.IsZero() || now.Sub(updated) > 5*time.Minute {
		return "stale"
	}
	return "fresh"
}

func newestInsightUpdate(rows []insightsExecutionRow) time.Time {
	var newest time.Time
	for _, row := range rows {
		for _, raw := range []string{row.CompletedAt, row.StartedAt, row.SubmittedAt} {
			t := parseInsightTime(raw)
			if t.After(newest) {
				newest = t
			}
		}
	}
	return newest
}

func isInsightFailure(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "failed" || s == "killed" || s == "canceled" || s == "cancelled" || strings.Contains(s, "fail")
}

func isInsightTerminal(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "completed" || s == "done" || s == "failed" || s == "killed" || s == "canceled" || s == "cancelled"
}

func ratio(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func percentile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := (len(cp)*p+99)/100 - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func rankRows(m map[string]*insightsRankRow) []insightsRankRow {
	out := make([]insightsRankRow, 0, len(m))
	for _, r := range m {
		r.FailureRate = ratio(r.Failures, r.Executions)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Executions == out[j].Executions {
			return out[i].ID < out[j].ID
		}
		return out[i].Executions > out[j].Executions
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

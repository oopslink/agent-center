package api

import (
	"net/http"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/usage"
)

// I28/F4 (issue-a7ff560e, v2.15.0): the per-agent analytics dashboard HTTP
// surface. One composed read (heatmap + overview cards + project/model trends +
// Top-Cost-Tasks) plus a per-task usage drill-down, both org-scoped and gated by
// decision-5 authz (owner/admin see any agent; a plain member sees only agents
// they created).
//
// Read-model split is enforced in the service (usage/sqlite.Analytics): the
// project-dimension trend comes from the rollup, the model-dimension trend +
// Top-Cost-Tasks from raw usage_events. The handler just composes + authorizes.

// defaultHeatmapDays is the window the composed read covers when no from/to is
// given: 53 weeks (the contribution-graph width), inclusive of today.
const defaultHeatmapDays = 53 * 7

// analyticsAuthorize resolves the target agent, enforces org membership, and
// applies decision-5 visibility (owner/admin → any agent in the org; member →
// only an agent they created). On success it returns the agent and its canonical
// "agent:<member-id>" usage ref. On any failure it writes the response and
// returns ok=false.
func (s *Server) analyticsAuthorize(w http.ResponseWriter, r *http.Request, d HandlerDeps) (*agentbc.Agent, string, bool) {
	if d.Analytics == nil {
		writeError(w, http.StatusNotImplemented, "analytics_not_wired", "analytics service not wired")
		return nil, "", false
	}
	if d.AgentSvc == nil {
		writeError(w, http.StatusNotImplemented, "agent_not_wired", "agent service not wired")
		return nil, "", false
	}
	callerID, member, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return nil, "", false
	}
	a, err := d.AgentSvc.ResolveAgent(r.Context(), r.PathValue("id"))
	if err != nil || a.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "agent not found in this organization")
		return nil, "", false
	}
	// decision 5: owner/PM (admin+) see every agent; a plain member may only view
	// analytics for an agent they created (created_by == their identity).
	if member == nil || !member.Role().AtLeast(identity.RoleAdmin) {
		if callerID == nil || bareRefID(string(a.CreatedBy())) != string(callerID.ID()) {
			writeError(w, http.StatusForbidden, "forbidden",
				"members may only view analytics for agents they created")
			return nil, "", false
		}
	}
	return a, "agent:" + agentFacingID(a), true
}

// agentAnalyticsHandler serves GET /api/orgs/{slug}/agents/{id}/analytics — the
// composed dashboard payload. Query params: from / to ("YYYY-MM-DD", default = a
// 53-week window ending today UTC) and top (Top-Cost-Tasks limit).
func (s *Server) agentAnalyticsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	a, agentRef, ok := s.analyticsAuthorize(w, r, d)
	if !ok {
		return
	}
	now := time.Now().UTC()
	fromDay, toDay, ok := analyticsWindow(w, r, now)
	if !ok {
		return
	}
	limit := atoiDefault(r.URL.Query().Get("top"), 0) // 0 → service default

	ctx := r.Context()
	heatmap, err := d.Analytics.Heatmap(ctx, agentRef, fromDay, toDay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}
	overview, err := d.Analytics.Overview(ctx, agentRef, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}
	projectTrend, err := d.Analytics.ProjectTrend(ctx, agentRef, fromDay, toDay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}
	modelTrend, err := d.Analytics.ModelTrend(ctx, agentRef, fromDay, toDay)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}
	topTasks, err := d.Analytics.TopTasks(ctx, agentRef, fromDay, toDay, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":  agentFacingID(a),
		"agent_ref": agentRef,
		"from":      fromDay,
		"to":        toDay,
		"heatmap":   heatmapJSON(heatmap),
		"overview":  overviewJSON(overview),
		"trends": map[string]any{
			"by_project": projectTrendJSON(projectTrend),
			"by_model":   modelTrendJSON(modelTrend),
		},
		"top_tasks": topTasksJSON(topTasks),
	})
}

// agentAnalyticsTaskHandler serves GET
// /api/orgs/{slug}/agents/{id}/analytics/tasks/{taskId} — the Top-Cost-Tasks
// drill-down: the agent's raw usage events on one task, ordered by ts. Filtered
// to the resolved agent so it never leaks another agent's events on a shared task.
func (s *Server) agentAnalyticsTaskHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	_, agentRef, ok := s.analyticsAuthorize(w, r, d)
	if !ok {
		return
	}
	taskID := r.PathValue("taskId")
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing task id")
		return
	}
	events, err := d.Analytics.TaskDrilldown(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "analytics_error", err.Error())
		return
	}
	arr := make([]map[string]any, 0, len(events))
	for _, e := range events {
		if e.AgentRef != agentRef { // agent-scoped drill-down
			continue
		}
		arr = append(arr, usageEventJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "events": arr})
}

// analyticsWindow resolves the [from, to] day window from query params, defaulting
// to a 53-week window ending today. Malformed dates → 400.
func analyticsWindow(w http.ResponseWriter, r *http.Request, now time.Time) (string, string, bool) {
	toDay := now.Format("2006-01-02")
	fromDay := now.AddDate(0, 0, -(defaultHeatmapDays - 1)).Format("2006-01-02")
	if q := r.URL.Query().Get("to"); q != "" {
		if !validDay(q) {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid 'to' date (want YYYY-MM-DD)")
			return "", "", false
		}
		toDay = q
	}
	if q := r.URL.Query().Get("from"); q != "" {
		if !validDay(q) {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid 'from' date (want YYYY-MM-DD)")
			return "", "", false
		}
		fromDay = q
	}
	if fromDay > toDay {
		writeError(w, http.StatusBadRequest, "bad_request", "'from' must be <= 'to'")
		return "", "", false
	}
	return fromDay, toDay, true
}

// validDay reports whether s is a well-formed "YYYY-MM-DD" UTC calendar date.
func validDay(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// atoiDefault parses a base-10 int, returning def on empty/garbage.
func atoiDefault(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// --- JSON shaping (explicit maps so the wire contract is stable) ---

func heatmapJSON(cells []usage.HeatmapCell) []map[string]any {
	out := make([]map[string]any, 0, len(cells))
	for _, c := range cells {
		out = append(out, map[string]any{
			"day": c.Day, "events": c.Events, "completed": c.Completed,
			"tokens_in": c.TokensIn, "tokens_out": c.TokensOut,
			"cache_tokens": c.CacheTokens, "cost_micros": c.CostMicros,
		})
	}
	return out
}

func windowJSON(w usage.WindowStat) map[string]any {
	return map[string]any{
		"tokens_in": w.TokensIn, "tokens_out": w.TokensOut,
		"cache_tokens": w.CacheTokens, "cost_micros": w.CostMicros,
		"completed_tasks": w.CompletedTasks,
	}
}

func overviewJSON(o usage.Overview) map[string]any {
	return map[string]any{
		"today": windowJSON(o.Today), "week": windowJSON(o.Week), "month": windowJSON(o.Month),
		"active_days": o.ActiveDays, "streak": o.Streak,
	}
}

func projectTrendJSON(pts []usage.ProjectTrendPoint) []map[string]any {
	out := make([]map[string]any, 0, len(pts))
	for _, p := range pts {
		out = append(out, map[string]any{
			"day": p.Day, "project_id": p.ProjectID, "events": p.Events,
			"tokens_in": p.TokensIn, "tokens_out": p.TokensOut,
			"cache_tokens": p.CacheTokens, "cost_micros": p.CostMicros,
		})
	}
	return out
}

func modelTrendJSON(pts []usage.ModelTrendPoint) []map[string]any {
	out := make([]map[string]any, 0, len(pts))
	for _, p := range pts {
		out = append(out, map[string]any{
			"day": p.Day, "model": p.Model,
			"tokens_in": p.TokensIn, "tokens_out": p.TokensOut,
			"cache_tokens": p.CacheTokens, "cost_micros": p.CostMicros,
		})
	}
	return out
}

func topTasksJSON(tasks []usage.TaskCost) []map[string]any {
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, map[string]any{
			"task_id": t.TaskID, "title": t.Title, "dominant_model": t.DominantModel,
			"events":    t.Events,
			"tokens_in": t.TokensIn, "tokens_out": t.TokensOut,
			"cache_tokens": t.CacheTokens, "cost_micros": t.CostMicros,
		})
	}
	return out
}

func usageEventJSON(e usage.UsageEvent) map[string]any {
	return map[string]any{
		"id": e.ID, "project_id": e.ProjectID, "task_id": e.TaskID, "model": e.Model,
		"tokens_in": e.Tokens.Input, "tokens_out": e.Tokens.Output,
		"cache_read_tokens": e.Tokens.CacheRead, "cache_write_tokens": e.Tokens.CacheWrite,
		"cost_micros": e.CostMicros, "ts": e.TS.Format(time.RFC3339Nano), "source": string(e.Source),
	}
}

package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/insight"
)

func (s *Server) insightsOverviewHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.Insight == nil {
		writeError(w, http.StatusNotImplemented, "insight_not_wired", "")
		return
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !requireWebAuthorization(w, r, d, caller, "org.analytics.read", authz.ResourceScope{Kind: "org", ID: orgID, OrgID: orgID}) {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.Overview(r.Context(), orgID, time.Now().UTC())
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) insightsExecutionsHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.Insight == nil {
		writeError(w, http.StatusNotImplemented, "insight_not_wired", "")
		return
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !requireWebAuthorization(w, r, d, caller, "org.analytics.read", authz.ResourceScope{Kind: "org", ID: orgID, OrgID: orgID}) {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be 1..100")
			return
		}
		limit = n
	}
	res, err := d.Insight.Executions(r.Context(), orgID, insight.ExecutionFilter{
		AgentRef:  strings.TrimSpace(r.URL.Query().Get("agent_ref")),
		ProjectID: strings.TrimSpace(r.URL.Query().Get("project_id")),
		Cursor:    strings.TrimSpace(r.URL.Query().Get("cursor")),
		Limit:     limit,
		AsOf:      time.Now().UTC(),
	})
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) insightsExecutionHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.Insight == nil {
		writeError(w, http.StatusNotImplemented, "insight_not_wired", "")
		return
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if !requireWebAuthorization(w, r, d, caller, "org.analytics.read", authz.ResourceScope{Kind: "org", ID: orgID, OrgID: orgID}) {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.Execution(r.Context(), orgID, strings.TrimSpace(r.PathValue("execution_id")), time.Now().UTC())
	if errors.Is(err, insight.ErrExecutionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "execution not found")
		return
	}
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) insightsV2OverviewHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2Overview(r.Context(), orgID, time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2AgentsHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2Agents(r.Context(), orgID, time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2AgentHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2Agent(r.Context(), orgID, strings.TrimSpace(r.PathValue("agent_ref")), time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2ProjectsHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2Projects(r.Context(), orgID, time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2ProjectHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2Project(r.Context(), orgID, strings.TrimSpace(r.PathValue("project_id")), time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2ProjectDeliveryHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2ProjectDelivery(r.Context(), orgID, strings.TrimSpace(r.PathValue("project_id")), time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2ProjectEvolutionHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2ProjectEvolution(r.Context(), orgID, strings.TrimSpace(r.PathValue("project_id")), time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2PlanLineageHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.V2PlanLineage(r.Context(), orgID, strings.TrimSpace(r.PathValue("project_id")), strings.TrimSpace(r.PathValue("plan_id")), time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2ExecutionsHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("agent_ref")) == "" && strings.TrimSpace(r.URL.Query().Get("project_id")) == "" {
		writeError(w, http.StatusBadRequest, "execution_context_required", "agent_ref or project_id is required")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be 1..100")
			return
		}
		limit = n
	}
	res, err := d.Insight.Executions(r.Context(), orgID, insight.ExecutionFilter{
		AgentRef: strings.TrimSpace(r.URL.Query().Get("agent_ref")), ProjectID: strings.TrimSpace(r.URL.Query().Get("project_id")),
		Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")), Limit: limit, AsOf: time.Now().UTC(),
	})
	writeInsightResult(w, res, err)
}

func (s *Server) insightsV2ExecutionHandler(w http.ResponseWriter, r *http.Request) {
	d, orgID, ok := s.requireInsightRead(w, r)
	if !ok {
		return
	}
	if !requireInsightWindow(w, r) {
		return
	}
	res, err := d.Insight.Execution(r.Context(), orgID, strings.TrimSpace(r.PathValue("execution_id")), time.Now().UTC())
	writeInsightResult(w, res, err)
}

func (s *Server) requireInsightRead(w http.ResponseWriter, r *http.Request) (HandlerDeps, string, bool) {
	d := hd(r)
	if d.Insight == nil {
		writeError(w, http.StatusNotImplemented, "insight_not_wired", "")
		return HandlerDeps{}, "", false
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return HandlerDeps{}, "", false
	}
	if !requireWebAuthorization(w, r, d, caller, "org.analytics.read", authz.ResourceScope{Kind: "org", ID: orgID, OrgID: orgID}) {
		return HandlerDeps{}, "", false
	}
	return d, orgID, true
}

func writeInsightResult(w http.ResponseWriter, v any, err error) {
	if errors.Is(err, insight.ErrExecutionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func writeInsightUnavailable(w http.ResponseWriter, message string) {
	now := time.Now().UTC()
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":        map[string]string{"code": "insight_unavailable", "message": message},
		"window":       insight.Window{Kind: "rolling", Duration: insight.Window24h, Start: now.Add(-24 * time.Hour).Format(time.RFC3339Nano), End: now.Format(time.RFC3339Nano)},
		"refreshed_at": "",
		"freshness":    insight.Freshness{State: "unavailable", AgeMS: 0, ThresholdMS: insight.DefaultFreshnessSLA.Milliseconds()},
	})
}

func requireInsightWindow(w http.ResponseWriter, r *http.Request) bool {
	if v := strings.TrimSpace(r.URL.Query().Get("window")); v != "" && v != insight.Window24h {
		writeError(w, http.StatusBadRequest, "invalid_window", "window must be absent or 24h")
		return false
	}
	return true
}

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
		writeInsightUnavailable(w, r, d, err)
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
		writeInsightUnavailable(w, r, d, err)
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
		writeInsightUnavailable(w, r, d, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func writeInsightUnavailable(w http.ResponseWriter, r *http.Request, d HandlerDeps, err error) {
	asOf := time.Now().UTC()
	ref, fresh := "", insight.Freshness{State: "unavailable"}
	if d.Insight != nil {
		ref, fresh = d.Insight.Freshness(r.Context(), asOf)
		if fresh.State == "fresh" || fresh.State == "stale" {
			fresh.State = "unavailable"
		}
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":        "insight_unavailable",
		"message":      err.Error(),
		"as_of":        asOf.Format(time.RFC3339Nano),
		"refreshed_at": ref,
		"freshness":    fresh,
	})
}

func requireInsightWindow(w http.ResponseWriter, r *http.Request) bool {
	if v := strings.TrimSpace(r.URL.Query().Get("window")); v != "" && v != insight.Window24h {
		writeError(w, http.StatusBadRequest, "invalid_window", "window must be absent or 24h")
		return false
	}
	return true
}

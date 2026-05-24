package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/query"
)

// =============================================================================
// EventRepo — FindByID / Find
// =============================================================================

func (s *Server) eventFindByIDHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EventRepo == nil {
		writeError(w, http.StatusNotImplemented, "event_repo_not_wired", "")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "")
		return
	}
	e, err := d.EventRepo.FindByID(r.Context(), observability.EventID(id))
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, eventMap(e))
}

func (s *Server) eventFindHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.EventRepo == nil {
		writeError(w, http.StatusNotImplemented, "event_repo_not_wired", "")
		return
	}
	filter := observability.EventQueryFilter{Limit: 200}
	if v := r.URL.Query().Get("type"); v != "" {
		et := observability.EventType(v)
		filter.EventType = &et
	}
	if v := r.URL.Query().Get("task_id"); v != "" {
		filter.Refs.TaskID = v
	}
	if v := r.URL.Query().Get("execution_id"); v != "" {
		filter.Refs.ExecutionID = v
	}
	if v := r.URL.Query().Get("issue_id"); v != "" {
		filter.Refs.IssueID = v
	}
	if v := r.URL.Query().Get("conversation_id"); v != "" {
		filter.Refs.ConversationID = v
	}
	if v := r.URL.Query().Get("worker_id"); v != "" {
		filter.Refs.WorkerID = v
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	list, err := d.EventRepo.Find(r.Context(), filter)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	out := make([]map[string]any, len(list))
	for i, e := range list {
		out[i] = eventMap(e)
	}
	writeJSON(w, http.StatusOK, out)
}

// =============================================================================
// QuerySvc — Inspect / Query
// =============================================================================

type queryReq struct {
	Resource    string `json:"resource"`
	Status      string `json:"status"`
	ProjectID   string `json:"project_id"`
	WorkerID    string `json:"worker_id"`
	TaskID      string `json:"task_id"`
	ExecutionID string `json:"execution_id"`
	IssueID     string `json:"issue_id"`
	Opener      string `json:"opener"`
	EventType   string `json:"event_type"`
	Limit       int    `json:"limit"`
	Cursor      string `json:"cursor"`
}

func (s *Server) queryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.QuerySvc == nil {
		writeError(w, http.StatusNotImplemented, "query_svc_not_wired", "")
		return
	}
	var req queryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Resource == "" {
		writeError(w, http.StatusBadRequest, "missing_resource", "")
		return
	}
	res, err := d.QuerySvc.Query(r.Context(), req.Resource, query.QueryFilter{
		Status:      req.Status,
		ProjectID:   req.ProjectID,
		WorkerID:    req.WorkerID,
		TaskID:      req.TaskID,
		ExecutionID: req.ExecutionID,
		IssueID:     req.IssueID,
		Opener:      req.Opener,
		EventType:   req.EventType,
		Limit:       req.Limit,
		Cursor:      req.Cursor,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) inspectHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.QuerySvc == nil {
		writeError(w, http.StatusNotImplemented, "query_svc_not_wired", "")
		return
	}
	kind := r.URL.Query().Get("kind")
	id := r.URL.Query().Get("id")
	if kind == "" || id == "" {
		writeError(w, http.StatusBadRequest, "missing_kind_or_id", "")
		return
	}
	res, err := d.QuerySvc.Inspect(r.Context(), kind, id)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// =============================================================================
// FleetSvc — Snapshot
// =============================================================================

func (s *Server) fleetSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.FleetSvc == nil {
		writeError(w, http.StatusNotImplemented, "fleet_svc_not_wired", "")
		return
	}
	snap := d.FleetSvc.Snapshot(r.Context(), query.SnapshotFilter{
		ProjectID: r.URL.Query().Get("project_id"),
	})
	writeJSON(w, http.StatusOK, snap)
}

// =============================================================================
// StatsSvc — Aggregate
// =============================================================================

func (s *Server) statsAggregateHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.StatsSvc == nil {
		writeError(w, http.StatusNotImplemented, "stats_svc_not_wired", "")
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = string(query.StatsScopeTasks)
	}
	var sincePtr *time.Time
	if v := r.URL.Query().Get("since"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			t := time.Now().Add(-d)
			sincePtr = &t
		} else if t, err := time.Parse(time.RFC3339, v); err == nil {
			sincePtr = &t
		}
	}
	res, err := d.StatsSvc.Aggregate(r.Context(), scope, sincePtr)
	if err != nil {
		mapDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// =============================================================================
// LogsSvc — Open (streams the gzipped blob body as-is)
// =============================================================================

func (s *Server) logsOpenHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	if d.LogsSvc == nil {
		writeError(w, http.StatusNotImplemented, "logs_svc_not_wired", "")
		return
	}
	kind := r.URL.Query().Get("kind")
	id := r.URL.Query().Get("id")
	if kind == "" || id == "" {
		writeError(w, http.StatusBadRequest, "missing_kind_or_id", "")
		return
	}
	rc, ref, err := d.LogsSvc.Open(r.Context(), query.LogsRequest{
		Kind: query.LogsKind(kind), ID: id, Follow: false,
	})
	if err != nil {
		mapDomainError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("X-Blob-Ref", ref)
	// Stream the gzipped body straight through; client decompresses.
	buf := make([]byte, 32*1024)
	for {
		n, rerr := rc.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
		}
		if rerr != nil {
			return
		}
	}
}

// =============================================================================
// Projection helpers
// =============================================================================

func eventMap(e *observability.Event) map[string]any {
	return map[string]any{
		"id":             string(e.ID()),
		"event_type":     string(e.Type()),
		"actor":          string(e.Actor()),
		"refs":           e.Refs(),
		"payload":        e.Payload(),
		"correlation_id": e.CorrelationID(),
		"decision_id":    e.DecisionID(),
		"occurred_at":    e.OccurredAt().Format(time.RFC3339Nano),
	}
}

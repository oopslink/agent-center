package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// handlers_pm_audit.go — the read side of the 变更记录 / audit-trail (change-log
// design §6). One handler per object kind (issue / task / plan); each resolves the
// object-in-project (which enforces project membership — 只有 project 成员可读) then
// pages that object's change ledger newest-first. The DTO ships STRUCTURED fields
// (change_type / field / from / to / actor / detail / occurred_at); the human-readable
// sentence is composed on the frontend (design §7).

// pmAuditEntryMap renders one AuditEntry as a frontend-friendly JSON object. detail is
// re-parsed from its stored JSON string into a nested object (falling back to an empty
// object when unparseable) so the client consumes structured extras directly.
func pmAuditEntryMap(e pm.AuditEntry) map[string]any {
	var detail any = map[string]any{}
	if e.Detail != "" {
		var parsed any
		if err := json.Unmarshal([]byte(e.Detail), &parsed); err == nil {
			detail = parsed
		}
	}
	return map[string]any{
		"id":          e.ID,
		"object_type": string(e.ObjectType),
		"object_id":   e.ObjectID,
		"change_type": string(e.ChangeType),
		"field":       e.Field,
		"from":        e.FromValue,
		"to":          e.ToValue,
		"actor":       string(e.ActorRef),
		"detail":      detail,
		"occurred_at": e.OccurredAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	}
}

// pmWriteAudit is the shared page+render for the three audit handlers. It reads the
// ?cursor / ?limit query params, lists the object's ledger, and writes
// {entries, next_cursor}. A bad ?limit is a 400.
func (s *Server) pmWriteAudit(w http.ResponseWriter, r *http.Request, objType pm.AuditObjectType, objID string) {
	d := hd(r)
	cursor := r.URL.Query().Get("cursor")
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a non-negative integer")
			return
		}
		limit = n
	}
	entries, next, err := d.PM.ListObjectAudit(r.Context(), objType, objID, cursor, limit)
	if err != nil {
		mapPMError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, pmAuditEntryMap(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "next_cursor": next})
}

func (s *Server) pmIssueAuditHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	i, _, ok := s.pmRequireIssueInProject(w, r, d)
	if !ok {
		return
	}
	s.pmWriteAudit(w, r, pm.AuditObjectIssue, string(i.ID()))
}

func (s *Server) pmTaskAuditHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	t, _, ok := s.pmRequireTaskInProject(w, r, d)
	if !ok {
		return
	}
	s.pmWriteAudit(w, r, pm.AuditObjectTask, string(t.ID()))
}

func (s *Server) pmPlanAuditHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	p, _, ok := s.pmRequirePlanInProject(w, r, d)
	if !ok {
		return
	}
	s.pmWriteAudit(w, r, pm.AuditObjectPlan, string(p.ID()))
}

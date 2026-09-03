package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/observability/collaborationeffect"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func (s *Server) requireCollaborationProject(w http.ResponseWriter, r *http.Request, projectID string) (HandlerDeps, bool) {
	d := hd(r)
	if d.CollaborationInsight == nil || d.PM == nil {
		writeError(w, http.StatusNotImplemented, "collaboration_insight_not_wired", "")
		return d, false
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return d, false
	}
	p, err := d.PM.GetProject(r.Context(), pm.ProjectID(projectID))
	if err != nil || p.OrganizationID() != orgID {
		writeError(w, http.StatusNotFound, "not_found", "project not found in this organization")
		return d, false
	}
	if !requireWebAuthorization(w, r, d, caller, "project.read", authz.ResourceScope{Kind: "project", ID: projectID, OrgID: orgID}) {
		return d, false
	}
	return d, true
}

func parseEffectFilter(r *http.Request) (collaborationeffect.Filter, error) {
	q := r.URL.Query()
	f := collaborationeffect.Filter{ProjectID: strings.TrimSpace(q.Get("project_id")), PlanID: strings.TrimSpace(q.Get("plan_id")), TaskID: strings.TrimSpace(q.Get("task_id")), StageID: strings.TrimSpace(q.Get("stage_id")), AgentRef: strings.TrimSpace(q.Get("agent_ref")), RelationType: collaborationeffect.RelationType(strings.TrimSpace(q.Get("relation_type"))), Polarity: collaborationeffect.Polarity(strings.TrimSpace(q.Get("polarity"))), Cursor: strings.TrimSpace(q.Get("cursor")), LOD: strings.TrimSpace(q.Get("lod")), GraphVersion: strings.TrimSpace(q.Get("graph_version"))}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > collaborationeffect.MaxQueryLimit {
			return f, collaborationeffect.ErrInvalidQuery
		}
		f.Limit = n
	}
	if raw := strings.TrimSpace(q.Get("max_nodes")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return f, collaborationeffect.ErrInvalidQuery
		}
		f.MaxNodes = n
	}
	for raw, dst := range map[string]**time.Time{"since": &f.Since, "until": &f.Until} {
		if v := strings.TrimSpace(q.Get(raw)); v != "" {
			parsed, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return f, collaborationeffect.ErrInvalidQuery
			}
			*dst = &parsed
		}
	}
	validRel := map[collaborationeffect.RelationType]bool{"": true, collaborationeffect.RelationAssign: true, collaborationeffect.RelationReassign: true, collaborationeffect.RelationBlock: true, collaborationeffect.RelationUnblock: true, collaborationeffect.RelationComplete: true, collaborationeffect.RelationDependencyRelease: true, collaborationeffect.RelationReviewAccept: true, collaborationeffect.RelationReviewReject: true}
	validPol := map[collaborationeffect.Polarity]bool{"": true, collaborationeffect.PolarityPositive: true, collaborationeffect.PolarityNegative: true, collaborationeffect.PolarityNeutral: true, collaborationeffect.PolarityMixed: true}
	if !validRel[f.RelationType] || !validPol[f.Polarity] {
		return f, collaborationeffect.ErrInvalidQuery
	}
	return f, nil
}

func (s *Server) requireCollaborationScope(w http.ResponseWriter, r *http.Request, f *collaborationeffect.Filter) (HandlerDeps, bool) {
	d := hd(r)
	if d.CollaborationInsight == nil || d.PM == nil {
		writeError(w, http.StatusNotImplemented, "collaboration_insight_not_wired", "")
		return d, false
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return d, false
	}
	f.OrganizationID = orgID
	if f.ProjectID != "" {
		p, err := d.PM.GetProject(r.Context(), pm.ProjectID(f.ProjectID))
		if err != nil || p.OrganizationID() != orgID {
			writeError(w, http.StatusNotFound, "not_found", "project not found in this organization")
			return d, false
		}
		if !requireWebAuthorization(w, r, d, caller, "project.read", authz.ResourceScope{Kind: "project", ID: f.ProjectID, OrgID: orgID}) {
			return d, false
		}
		return d, true
	}
	projects, err := d.PM.ListProjects(r.Context(), orgID)
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return d, false
	}
	for _, p := range projects {
		projectID := string(p.ID())
		if !requireWebAuthorization(w, r, d, caller, "project.read", authz.ResourceScope{Kind: "project", ID: projectID, OrgID: orgID}) {
			return d, false
		}
	}
	return d, true
}

func (s *Server) collaborationEffectsHandler(w http.ResponseWriter, r *http.Request) {
	f, err := parseEffectFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", "invalid collaboration effect query")
		return
	}
	d, ok := s.requireCollaborationScope(w, r, &f)
	if !ok {
		return
	}
	res, err := d.CollaborationInsight.Query(r.Context(), f)
	if errors.Is(err, collaborationeffect.ErrInvalidCursor) {
		writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error())
		return
	}
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) collaborationEffectEvidenceHandler(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if projectID == "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "project_id is required")
		return
	}
	d, ok := s.requireCollaborationProject(w, r, projectID)
	if !ok {
		return
	}
	res, err := d.CollaborationInsight.Evidence(r.Context(), strings.TrimSpace(r.PathValue("effect_id")), projectID)
	if errors.Is(err, collaborationeffect.ErrEffectNotFound) {
		writeError(w, http.StatusNotFound, "effect_not_found", "effect not found")
		return
	}
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

package api

import (
	"errors"
	"net/http"
	"sort"
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
	f := collaborationeffect.Filter{ProjectID: strings.TrimSpace(q.Get("project_id")), PlanID: strings.TrimSpace(q.Get("plan_id")), TaskID: strings.TrimSpace(q.Get("task_id")), AgentRef: strings.TrimSpace(q.Get("agent_ref")), RelationType: collaborationeffect.RelationType(strings.TrimSpace(q.Get("relation_type"))), Polarity: collaborationeffect.Polarity(strings.TrimSpace(q.Get("polarity"))), Cursor: strings.TrimSpace(q.Get("cursor"))}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > collaborationeffect.MaxQueryLimit {
			return f, collaborationeffect.ErrInvalidQuery
		}
		f.Limit = n
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

func (s *Server) collaborationEffectsHandler(w http.ResponseWriter, r *http.Request) {
	f, err := parseEffectFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_query", "invalid collaboration effect query")
		return
	}
	d := hd(r)
	if d.CollaborationInsight == nil || d.PM == nil {
		writeError(w, http.StatusNotImplemented, "collaboration_insight_not_wired", "")
		return
	}
	caller, _, orgID, ok := requireOrgMember(w, r, d)
	if !ok {
		return
	}
	if f.ProjectID != "" {
		if _, ok := s.requireCollaborationProject(w, r, f.ProjectID); !ok {
			return
		}
	} else if f.PlanID != "" {
		plan, err := d.PM.GetPlan(r.Context(), pm.PlanID(f.PlanID))
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "plan not found in this organization")
			return
		}
		f.ProjectID = string(plan.ProjectID())
		if _, ok := s.requireCollaborationProject(w, r, f.ProjectID); !ok {
			return
		}
	} else if !requireWebAuthorization(w, r, d, caller, "org.analytics.read", authz.ResourceScope{Kind: "org", ID: orgID, OrgID: orgID}) {
		return
	}
	if f.ProjectID == "" {
		res, err := s.collaborationOrganizationGraph(r, d, orgID, f)
		if err != nil {
			s.writeCollaborationQueryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
		return
	}
	res, err := d.CollaborationInsight.Query(r.Context(), f)
	if err != nil {
		s.writeCollaborationQueryError(w, err)
		return
	}
	res = s.enrichCollaborationGraph(r, d, res, f)
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) writeCollaborationQueryError(w http.ResponseWriter, err error) {
	if errors.Is(err, collaborationeffect.ErrInvalidCursor) {
		writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error())
		return
	}
	if err != nil {
		writeInsightUnavailable(w, err.Error())
		return
	}
}

func (s *Server) collaborationOrganizationGraph(r *http.Request, d HandlerDeps, orgID string, f collaborationeffect.Filter) (collaborationeffect.QueryResult, error) {
	projects, err := d.PM.ListProjects(r.Context(), orgID)
	if err != nil {
		return collaborationeffect.QueryResult{}, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	all := make([]collaborationeffect.Effect, 0, limit)
	truncated := false
	var asOf time.Time
	ruleVersion := ""
	for _, p := range projects {
		next := f
		next.ProjectID = string(p.ID())
		next.Limit = limit + 1
		res, qerr := d.CollaborationInsight.Query(r.Context(), next)
		if qerr != nil {
			return collaborationeffect.QueryResult{}, qerr
		}
		if asOf.IsZero() || res.AsOf.After(asOf) {
			asOf = res.AsOf
		}
		if ruleVersion == "" {
			ruleVersion = res.RuleVersion
		}
		if res.Truncated {
			truncated = true
		}
		all = append(all, res.Effects...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].EffectID < all[j].EffectID })
	if len(all) > limit {
		all = all[:limit]
		truncated = true
	}
	out := collaborationeffect.QueryResult{
		Effects:     all,
		AsOf:        asOf,
		RuleVersion: ruleVersion,
		Truncated:   truncated,
		Graph:       collaborationeffect.Graph{Nodes: []collaborationeffect.GraphNode{}, Edges: []collaborationeffect.GraphEdge{}},
	}
	if truncated && len(all) > 0 {
		out.NextCursor = all[len(all)-1].EffectID
	}
	return s.enrichCollaborationGraph(r, d, out, f), nil
}

func (s *Server) enrichCollaborationGraph(r *http.Request, d HandlerDeps, res collaborationeffect.QueryResult, f collaborationeffect.Filter) collaborationeffect.QueryResult {
	topology := collaborationeffect.GraphTopology{
		Plans:  map[string]collaborationeffect.GraphPlan{},
		Stages: map[string]collaborationeffect.GraphStage{},
		Tasks:  map[string]collaborationeffect.GraphTask{},
	}
	projectIDs := map[string]struct{}{}
	for _, e := range res.Effects {
		if e.ProjectID != "" {
			projectIDs[e.ProjectID] = struct{}{}
		}
	}
	if f.ProjectID != "" {
		projectIDs[f.ProjectID] = struct{}{}
	}
	for projectID := range projectIDs {
		plans, err := d.PM.ListPlans(r.Context(), pm.ProjectID(projectID))
		if err == nil {
			for _, plan := range plans {
				if f.PlanID != "" && string(plan.ID()) != f.PlanID {
					continue
				}
				topology.Plans[string(plan.ID())] = collaborationeffect.GraphPlan{ID: string(plan.ID()), Label: plan.Name(), ProjectID: string(plan.ProjectID())}
				if stages, serr := d.PM.ListStagesForPlan(r.Context(), plan.ID()); serr == nil {
					for _, detail := range stages {
						stage := detail.Stage
						topology.Stages[string(stage.ID())] = collaborationeffect.GraphStage{ID: string(stage.ID()), Label: stage.Name(), ProjectID: string(plan.ProjectID()), PlanID: string(plan.ID())}
					}
				}
			}
		}
		tasks, err := d.PM.ListTasks(r.Context(), pm.ProjectID(projectID))
		if err != nil {
			continue
		}
		for _, task := range tasks {
			if f.PlanID != "" && string(task.PlanID()) != f.PlanID {
				continue
			}
			topology.Tasks[string(task.ID())] = collaborationeffect.GraphTask{ID: string(task.ID()), Label: task.Title(), ProjectID: string(task.ProjectID()), PlanID: string(task.PlanID()), StageID: string(task.StageID())}
		}
	}
	if f.PlanID != "" {
		filtered := res.Effects[:0]
		for _, e := range res.Effects {
			if topology.Tasks[e.TargetTaskID].PlanID == f.PlanID {
				filtered = append(filtered, e)
			}
		}
		res.Effects = filtered
	}
	if res.GraphVersion == "" {
		res.GraphVersion = res.RuleVersion
	}
	res.Graph, res.Summary = collaborationeffect.BuildGraph(res.Effects, res.GraphVersion, topology)
	return res
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

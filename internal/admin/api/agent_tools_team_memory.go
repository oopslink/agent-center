package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/cognition/memory/teammemory"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/team"
)

type proposeTeamMemoryReq struct {
	AgentID        string                `json:"agent_id"`
	Operation      string                `json:"operation"`
	TargetKind     string                `json:"target_kind"`
	Kind           string                `json:"kind"`
	Target         *teammemory.TargetRef `json:"target"`
	Candidate      *teammemory.Candidate `json:"candidate"`
	Rationale      string                `json:"rationale"`
	EvidenceRefs   []string              `json:"evidence_refs"`
	IdempotencyKey string                `json:"idempotency_key"`
}

func (s *Server) proposeTeamMemoryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req proposeTeamMemoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, svc, projector, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if _, ok := s.requireAgentCurrentTeamMemoryPermission(w, r, d, a, "team.memory.propose"); !ok {
		return
	}
	kind := strings.TrimSpace(req.TargetKind)
	if kind == "" {
		kind = req.Kind
	}
	res, err := svc.Propose(r.Context(), teammemory.ProposeCommand{
		ActorRef:       operatingAgentMemberRef(a).String(),
		IdempotencyKey: req.IdempotencyKey,
		Operation:      teammemory.Operation(strings.ToLower(strings.TrimSpace(req.Operation))),
		TargetKind:     teammemory.TargetKind(strings.ToLower(strings.TrimSpace(kind))),
		Target:         req.Target,
		Candidate:      req.Candidate,
		Rationale:      req.Rationale,
		EvidenceRefs:   req.EvidenceRefs,
	})
	if err != nil {
		mapTeamMemoryError(w, err)
		return
	}
	_ = projector.ReconcileTeam(r.Context(), res.TeamID)
	writeJSON(w, http.StatusCreated, teamMemoryResultPayload(res))
}

type listTeamMemoryReq struct {
	AgentID  string   `json:"agent_id"`
	Status   string   `json:"status"`
	Statuses []string `json:"statuses"`
	Kind     string   `json:"kind"`
	Limit    int      `json:"limit"`
	Offset   int      `json:"offset"`
}

func (s *Server) listTeamMemoryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listTeamMemoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, svc, projector, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	res, err := svc.List(r.Context(), teammemory.ListCommand{
		ActorRef:   operatingAgentMemberRef(a).String(),
		Status:     parseTeamMemoryStatuses(req.Status, req.Statuses),
		TargetKind: teammemory.TargetKind(strings.ToLower(strings.TrimSpace(req.Kind))),
		Limit:      req.Limit,
		Offset:     req.Offset,
	})
	if err != nil {
		mapTeamMemoryError(w, err)
		return
	}
	_ = projector.ReconcileTeam(r.Context(), res.TeamID)
	out := make([]map[string]any, 0, len(res.Proposals))
	for _, proposal := range res.Proposals {
		out = append(out, teamMemoryViewPayload(proposal))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"team_id":     res.TeamID,
		"repo_commit": res.RepoCommit,
		"proposals":   out,
		"total":       res.Total,
		"has_more":    res.HasMore,
	})
}

type getTeamMemoryReq struct {
	AgentID    string `json:"agent_id"`
	ProposalID string `json:"proposal_id"`
}

func (s *Server) getTeamMemoryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req getTeamMemoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, svc, projector, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	view, err := svc.Get(r.Context(), teammemory.GetCommand{
		ActorRef:   operatingAgentMemberRef(a).String(),
		ProposalID: req.ProposalID,
	})
	if err != nil {
		mapTeamMemoryError(w, err)
		return
	}
	_ = projector.ReconcileTeam(r.Context(), view.Proposal.TeamID)
	writeJSON(w, http.StatusOK, teamMemoryViewPayload(view))
}

type reviewTeamMemoryReq struct {
	AgentID                string   `json:"agent_id"`
	ProposalID             string   `json:"proposal_id"`
	Action                 string   `json:"action"`
	ExpectedRepoCommit     string   `json:"expected_repo_commit"`
	ExpectedProposalStatus string   `json:"expected_proposal_status"`
	Comment                string   `json:"comment"`
	AcknowledgeWarnings    []string `json:"acknowledge_warnings"`
}

func (s *Server) reviewTeamMemoryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req reviewTeamMemoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, svc, projector, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	if _, ok := s.requireAgentCurrentTeamMemoryPermission(w, r, d, a, "team.memory.review"); !ok {
		return
	}
	actorRef := operatingAgentMemberRef(a).String()
	cmd := teammemory.ReviewCommand{
		ActorRef:               actorRef,
		ProposalID:             req.ProposalID,
		Action:                 teammemory.ReviewAction(strings.ToLower(strings.TrimSpace(req.Action))),
		ExpectedRepoCommit:     req.ExpectedRepoCommit,
		ExpectedProposalStatus: teammemory.ProposalStatus(strings.ToLower(strings.TrimSpace(req.ExpectedProposalStatus))),
		Comment:                req.Comment,
		AcknowledgeWarnings:    req.AcknowledgeWarnings,
	}
	res, err := svc.Review(r.Context(), "", cmd)
	if err != nil {
		if errors.Is(err, teammemory.ErrTeamMemoryVersionConflict) {
			teamID, actual := s.resolveTeamMemoryConflictContext(r, d, a, svc, req.ProposalID)
			_ = projector.EmitPromotionConflict(r.Context(), teamID, req.ProposalID, actorRef, req.ExpectedRepoCommit, actual)
		}
		mapTeamMemoryError(w, err)
		return
	}
	_ = projector.ReconcileTeam(r.Context(), res.TeamID)
	writeJSON(w, http.StatusOK, teamMemoryResultPayload(res))
}

func (s *Server) requireTeamMemoryAgent(w http.ResponseWriter, r *http.Request, d HandlerDeps, agentID string) (*agent.Agent, *teammemory.Service, *teammemory.Projector, bool) {
	a, ok := s.requireTeamAgent(w, r, d, agentID)
	if !ok {
		return nil, nil, nil, false
	}
	if d.TeamGitHost == nil {
		writeError(w, http.StatusNotImplemented, "team_memory_not_wired", "team memory git host is not wired")
		return nil, nil, nil, false
	}
	repo := centergit.NewTeamMemoryRepository(d.TeamGitHost, nil)
	auth := teammemory.NewTeamPolicyAuthorizationFromService(d.TeamSvc, d.TeamMemberRepo)
	svc := teammemory.NewService(repo, auth)
	var projector *teammemory.Projector
	if seq, ok := d.EventRepo.(observability.SeqAllocator); ok && d.EventRepo != nil {
		projector = teammemory.NewProjector(d.DB, repo, d.EventRepo, seq)
	} else {
		projector = teammemory.NewProjector(nil, nil, nil, nil)
	}
	return a, svc, projector, true
}

func (s *Server) requireAgentCurrentTeamMemoryPermission(w http.ResponseWriter, r *http.Request, d HandlerDeps, a *agent.Agent, permission authz.PermissionKey) (string, bool) {
	teamID := ""
	if d.TeamSvc != nil {
		if id, ok, err := d.TeamSvc.FindAgentTeam(r.Context(), operatingAgentMemberRef(a)); err == nil && ok {
			teamID = id.String()
		}
	}
	if teamID == "" {
		writeError(w, http.StatusForbidden, "not_team_member", "agent is not a current team member")
		return "", false
	}
	return teamID, s.requireAgentTeamPermission(w, r, d, a, teamID, permission)
}

func (s *Server) resolveTeamMemoryConflictContext(r *http.Request, d HandlerDeps, a *agent.Agent, svc *teammemory.Service, proposalID string) (string, string) {
	teamID := ""
	if d.TeamSvc != nil {
		if id, ok, err := d.TeamSvc.FindAgentTeam(r.Context(), operatingAgentMemberRef(a)); err == nil && ok {
			teamID = id.String()
		}
	}
	actual := ""
	if view, err := svc.Get(r.Context(), teammemory.GetCommand{
		ActorRef:   operatingAgentMemberRef(a).String(),
		TeamID:     teamID,
		ProposalID: proposalID,
	}); err == nil {
		actual = view.RepoCommit
	}
	return teamID, actual
}

func parseTeamMemoryStatuses(single string, many []string) []teammemory.ProposalStatus {
	raw := append([]string{}, many...)
	if strings.TrimSpace(single) != "" {
		raw = append(raw, strings.Split(single, ",")...)
	}
	out := make([]teammemory.ProposalStatus, 0, len(raw))
	for _, item := range raw {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		out = append(out, teammemory.ProposalStatus(item))
	}
	return out
}

func teamMemoryResultPayload(res teammemory.Result) map[string]any {
	return map[string]any{
		"team_id":       res.TeamID,
		"proposal_id":   res.ProposalID,
		"status":        res.Status,
		"repo_commit":   res.RepoCommit,
		"source_path":   res.SourcePath,
		"warnings":      res.Warnings,
		"effective_for": res.EffectiveFor,
		"old_commit":    res.OldCommit,
		"new_commit":    res.NewCommit,
	}
}

func teamMemoryViewPayload(view teammemory.ProposalView) map[string]any {
	p := view.Proposal
	return map[string]any{
		"team_id":                  p.TeamID,
		"proposal_id":              p.ProposalID,
		"operation":                p.Operation,
		"target_kind":              p.TargetKind,
		"target":                   p.Target,
		"candidate":                p.Candidate,
		"rationale":                p.Rationale,
		"evidence_refs":            p.EvidenceRefs,
		"author_ref":               p.AuthorRef,
		"created_at":               p.CreatedAt,
		"idempotency_key":          p.IdempotencyKey,
		"status":                   p.Status,
		"warnings":                 p.Warnings,
		"reviewer_ref":             p.ReviewerRef,
		"review_comment":           p.ReviewComment,
		"reviewed_at":              p.ReviewedAt,
		"promotion_commit":         p.PromotionCommit,
		"source_path":              p.SourcePath,
		"repo_commit":              view.RepoCommit,
		"current_target_blob_hash": view.CurrentTargetBlobHash,
		"diff_preview":             view.DiffPreview,
	}
}

func mapTeamMemoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, centergit.ErrTeamRuleIndexTooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "team_rule_index_too_large", err.Error())
		return
	}
	reason := teammemory.Reason(err)
	switch reason {
	case "team_memory_not_wired":
		writeError(w, http.StatusNotImplemented, reason, err.Error())
	case "not_team_member", "proposal_not_found":
		writeError(w, http.StatusNotFound, reason, err.Error())
	case "not_memory_curator":
		writeError(w, http.StatusForbidden, reason, err.Error())
	case "invalid_candidate", "secret_detected":
		writeError(w, http.StatusBadRequest, reason, err.Error())
	case "warning_unacknowledged", "target_changed", "proposal_not_pending", "idempotency_conflict", "team_memory_version_conflict":
		writeError(w, http.StatusConflict, reason, err.Error())
	case "git_unavailable":
		writeError(w, http.StatusServiceUnavailable, reason, err.Error())
	default:
		if errors.Is(err, team.ErrTeamNotFound) {
			writeError(w, http.StatusNotFound, "not_team_member", "team not found")
			return
		}
		mapDomainError(w, err)
	}
}

package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/cognition/memory/teammemory"
	"github.com/oopslink/agent-center/internal/observability"
)

type proposeTeamMemoryReq struct {
	AgentID        string                       `json:"agent_id"`
	Operation      string                       `json:"operation"`
	TargetKind     string                       `json:"target_kind"`
	Kind           string                       `json:"kind"`
	Target         *centergit.ProposalTarget    `json:"target"`
	Candidate      *centergit.ProposalCandidate `json:"candidate"`
	Rationale      string                       `json:"rationale"`
	EvidenceRefs   []string                     `json:"evidence_refs"`
	IdempotencyKey string                       `json:"idempotency_key"`
}

func (s *Server) proposeTeamMemoryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req proposeTeamMemoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, svc, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	kind := req.TargetKind
	if kind == "" {
		kind = req.Kind
	}
	view, err := svc.Propose(r.Context(), teammemory.Actor{
		OrgID:     string(a.OrganizationID()),
		MemberRef: operatingAgentMemberRef(a),
	}, centergit.ProposeInput{
		IdempotencyKey: req.IdempotencyKey,
		Operation:      teammemory.OperationFromString(req.Operation),
		TargetKind:     teammemory.KindFromString(kind),
		Target:         req.Target,
		Candidate:      req.Candidate,
		Rationale:      req.Rationale,
		EvidenceRefs:   req.EvidenceRefs,
	})
	if err != nil {
		mapTeamMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, teammemory.ViewPayload(view))
}

type listTeamMemoryReq struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
	Kind    string `json:"kind"`
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
}

func (s *Server) listTeamMemoryHandler(w http.ResponseWriter, r *http.Request) {
	d := hd(r)
	var req listTeamMemoryReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	a, svc, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	items, commit, err := svc.List(r.Context(), teammemory.Actor{
		OrgID:     string(a.OrganizationID()),
		MemberRef: operatingAgentMemberRef(a),
	}, centergit.ProposalListFilter{
		Status: teammemory.StatusFromString(req.Status),
		Kind:   teammemory.KindFromString(req.Kind),
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		mapTeamMemoryError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, teammemory.ViewPayload(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo_commit": commit, "proposals": out})
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
	a, svc, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	view, err := svc.Get(r.Context(), teammemory.Actor{
		OrgID:     string(a.OrganizationID()),
		MemberRef: operatingAgentMemberRef(a),
	}, strings.TrimSpace(req.ProposalID))
	if err != nil {
		mapTeamMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, teammemory.ViewPayload(view))
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
	a, svc, ok := s.requireTeamMemoryAgent(w, r, d, req.AgentID)
	if !ok {
		return
	}
	view, err := svc.Review(r.Context(), teammemory.Actor{
		OrgID:     string(a.OrganizationID()),
		MemberRef: operatingAgentMemberRef(a),
	}, centergit.ReviewInput{
		ProposalID:             req.ProposalID,
		Action:                 req.Action,
		ExpectedRepoCommit:     req.ExpectedRepoCommit,
		ExpectedProposalStatus: teammemory.StatusFromString(req.ExpectedProposalStatus),
		Comment:                req.Comment,
		AcknowledgeWarnings:    req.AcknowledgeWarnings,
	})
	if err != nil {
		mapTeamMemoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, teammemory.ViewPayload(view))
}

func (s *Server) requireTeamMemoryAgent(w http.ResponseWriter, r *http.Request, d HandlerDeps, agentID string) (*agent.Agent, *teammemory.Service, bool) {
	a, ok := s.requireTeamAgent(w, r, d, agentID)
	if !ok {
		return nil, nil, false
	}
	if d.TeamGitHost == nil {
		writeError(w, http.StatusNotImplemented, "team_memory_not_wired", "team memory git host is not wired")
		return nil, nil, false
	}
	git := centergit.NewTeamMemoryGit(d.TeamGitHost, nil)
	var projector teammemory.EventProjector
	if seq, ok := d.EventRepo.(observability.SeqAllocator); ok && d.DB != nil {
		projector = teammemory.NewProjector(d.DB, git, d.EventRepo, seq)
	}
	return a, teammemory.NewService(d.TeamSvc, git, projector), true
}

func mapTeamMemoryError(w http.ResponseWriter, err error) {
	var tmerr *teammemory.Error
	if !errors.As(err, &tmerr) {
		mapDomainError(w, err)
		return
	}
	switch tmerr.Reason {
	case teammemory.ReasonNotTeamMember, teammemory.ReasonProposalNotFound:
		writeError(w, http.StatusNotFound, tmerr.Reason, tmerr.Error())
	case teammemory.ReasonNotMemoryCurator:
		writeError(w, http.StatusForbidden, tmerr.Reason, tmerr.Error())
	case teammemory.ReasonInvalidCandidate, teammemory.ReasonSecretDetected:
		writeError(w, http.StatusBadRequest, tmerr.Reason, tmerr.Error())
	case teammemory.ReasonWarningUnacknowledged,
		teammemory.ReasonTargetChanged,
		teammemory.ReasonProposalNotPending,
		teammemory.ReasonIdempotencyConflict,
		teammemory.ReasonTeamMemoryVersionConflict:
		writeError(w, http.StatusConflict, tmerr.Reason, tmerr.Error())
	case teammemory.ReasonGitUnavailable:
		writeError(w, http.StatusServiceUnavailable, tmerr.Reason, tmerr.Error())
	default:
		writeError(w, http.StatusBadRequest, tmerr.Reason, tmerr.Error())
	}
}

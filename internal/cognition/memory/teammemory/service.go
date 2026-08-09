package teammemory

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
)

const (
	ReasonNotTeamMember             = "not_team_member"
	ReasonNotMemoryCurator          = "not_memory_curator"
	ReasonInvalidCandidate          = "invalid_candidate"
	ReasonSecretDetected            = "secret_detected"
	ReasonWarningUnacknowledged     = "warning_unacknowledged"
	ReasonTargetChanged             = "target_changed"
	ReasonProposalNotPending        = "proposal_not_pending"
	ReasonProposalNotFound          = "proposal_not_found"
	ReasonIdempotencyConflict       = "idempotency_conflict"
	ReasonTeamMemoryVersionConflict = "team_memory_version_conflict"
	ReasonGitUnavailable            = "git_unavailable"
)

type Error struct {
	Reason  string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Reason
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func ReasonOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Reason
	}
	return ""
}

type Actor struct {
	OrgID     string
	MemberRef team.MemberRef
}

type EventProjector interface {
	ReconcileTeam(ctx context.Context, teamID string) error
	EmitPromotionConflict(ctx context.Context, teamID, proposalID, actorRef, expectedCommit, actualCommit string) error
}

type Service struct {
	teams     *teamservice.Service
	git       *centergit.TeamMemoryGit
	projector EventProjector
	now       func() time.Time
}

func NewService(teams *teamservice.Service, git *centergit.TeamMemoryGit, projector EventProjector) *Service {
	return &Service{teams: teams, git: git, projector: projector, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Propose(ctx context.Context, actor Actor, in centergit.ProposeInput) (centergit.ProposalView, error) {
	teamID, err := s.resolveTeam(ctx, actor)
	if err != nil {
		return centergit.ProposalView{}, err
	}
	if err := validatePropose(in); err != nil {
		return centergit.ProposalView{}, err
	}
	warnings := contentWarnings(proposalText(in))
	in.Warnings = warnings
	in.ActorRef = actor.MemberRef.String()
	if in.CreatedAt.IsZero() && s.now != nil {
		in.CreatedAt = s.now()
	}
	out, err := s.git.Propose(ctx, teamID.String(), in)
	if err != nil {
		return centergit.ProposalView{}, mapGitError(err)
	}
	out = annotateEffectiveFor(out)
	_ = s.reconcile(ctx, teamID.String())
	return out, nil
}

func (s *Service) List(ctx context.Context, actor Actor, filter centergit.ProposalListFilter) ([]centergit.ProposalView, string, error) {
	teamID, err := s.resolveTeam(ctx, actor)
	if err != nil {
		return nil, "", err
	}
	if filter.Status == "" {
		filter.Status = centergit.ProposalPending
	}
	out, commit, err := s.git.ListProposals(ctx, teamID.String(), filter)
	if err != nil {
		return nil, "", mapGitError(err)
	}
	_ = s.reconcile(ctx, teamID.String())
	return out, commit, nil
}

func (s *Service) Get(ctx context.Context, actor Actor, proposalID string) (centergit.ProposalView, error) {
	teamID, err := s.resolveTeam(ctx, actor)
	if err != nil {
		return centergit.ProposalView{}, err
	}
	out, err := s.git.GetProposal(ctx, teamID.String(), proposalID)
	if err != nil {
		return centergit.ProposalView{}, mapGitError(err)
	}
	out = annotateEffectiveFor(out)
	_ = s.reconcile(ctx, teamID.String())
	return out, nil
}

func (s *Service) Review(ctx context.Context, actor Actor, in centergit.ReviewInput) (centergit.ProposalView, error) {
	teamID, err := s.resolveTeam(ctx, actor)
	if err != nil {
		return centergit.ProposalView{}, err
	}
	policy, err := s.teams.GetMemoryPolicy(ctx, teamID)
	if err != nil {
		return centergit.ProposalView{}, err
	}
	if !policy.IsCurator(actor.MemberRef) {
		return centergit.ProposalView{}, &Error{Reason: ReasonNotMemoryCurator, Message: "only Team Memory curator agents may review through MCP"}
	}
	if err := validateReview(in); err != nil {
		return centergit.ProposalView{}, err
	}
	in.ActorRef = actor.MemberRef.String()
	if in.ReviewedAt.IsZero() && s.now != nil {
		in.ReviewedAt = s.now()
	}
	out, err := s.git.Review(ctx, teamID.String(), in)
	if err != nil {
		mapped := mapGitError(err)
		if ReasonOf(mapped) == ReasonTeamMemoryVersionConflict && s.projector != nil {
			current := ""
			if p, gerr := s.git.GetProposal(ctx, teamID.String(), in.ProposalID); gerr == nil {
				current = p.RepoCommit
			}
			_ = s.projector.EmitPromotionConflict(ctx, teamID.String(), in.ProposalID, actor.MemberRef.String(), in.ExpectedRepoCommit, current)
		}
		return centergit.ProposalView{}, mapped
	}
	out = annotateEffectiveFor(out)
	_ = s.reconcile(ctx, teamID.String())
	return out, nil
}

func annotateEffectiveFor(v centergit.ProposalView) centergit.ProposalView {
	if v.Status == centergit.ProposalPromoted {
		v.EffectiveFor = "new_sessions_and_forks"
	}
	return v
}

func (s *Service) resolveTeam(ctx context.Context, actor Actor) (team.TeamID, error) {
	if s == nil || s.teams == nil || s.git == nil {
		return "", &Error{Reason: ReasonGitUnavailable, Message: "team memory service is not wired"}
	}
	ref := team.MemberRef(strings.TrimSpace(actor.MemberRef.String()))
	if ref == "" {
		return "", &Error{Reason: ReasonNotTeamMember, Message: "agent is not a team member"}
	}
	teamID, ok, err := s.teams.FindAgentTeam(ctx, ref)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", &Error{Reason: ReasonNotTeamMember, Message: "agent is not a team member"}
	}
	t, err := s.teams.GetTeam(ctx, teamID)
	if err != nil {
		return "", err
	}
	if t.OrgID() != strings.TrimSpace(actor.OrgID) {
		return "", &Error{Reason: ReasonNotTeamMember, Message: "team not found"}
	}
	return teamID, nil
}

func (s *Service) reconcile(ctx context.Context, teamID string) error {
	if s.projector == nil {
		return nil
	}
	return s.projector.ReconcileTeam(ctx, teamID)
}

func mapGitError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, centergit.ErrIdempotencyConflict):
		return &Error{Reason: ReasonIdempotencyConflict, Message: "idempotency key was reused with a different payload", Err: err}
	case errors.Is(err, centergit.ErrWarningUnacknowledged):
		return &Error{Reason: ReasonWarningUnacknowledged, Message: err.Error(), Err: err}
	case errors.Is(err, centergit.ErrTargetChanged):
		return &Error{Reason: ReasonTargetChanged, Message: "target changed since proposal was created", Err: err}
	case errors.Is(err, centergit.ErrProposalNotPending):
		return &Error{Reason: ReasonProposalNotPending, Message: "proposal is not pending", Err: err}
	case errors.Is(err, centergit.ErrProposalNotFound):
		return &Error{Reason: ReasonProposalNotFound, Message: "proposal not found", Err: err}
	case errors.Is(err, centergit.ErrTeamMemoryVersionConflict):
		return &Error{Reason: ReasonTeamMemoryVersionConflict, Message: "team memory repository changed; refresh and retry", Err: err}
	case errors.Is(err, centergit.ErrInvalidProposal),
		errors.Is(err, centergit.ErrUnsupportedProposalAction),
		errors.Is(err, centergit.ErrUnsupportedProposalMutation),
		errors.Is(err, centergit.ErrInvalidEntry),
		errors.Is(err, centergit.ErrInvalidRule):
		return &Error{Reason: ReasonInvalidCandidate, Message: err.Error(), Err: err}
	case errors.Is(err, centergit.ErrGitOpFailed), errors.Is(err, centergit.ErrPushRetriesExhausted):
		return &Error{Reason: ReasonGitUnavailable, Message: "team memory git is unavailable", Err: err}
	default:
		return err
	}
}

func validatePropose(in centergit.ProposeInput) error {
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return invalid("idempotency_key is required")
	}
	if strings.TrimSpace(in.Rationale) == "" {
		return invalid("rationale is required")
	}
	if len(in.Rationale) > 4096 {
		return invalid("rationale exceeds 4096 bytes")
	}
	if len(in.EvidenceRefs) > 20 {
		return invalid("evidence_refs exceeds 20 items")
	}
	for _, e := range in.EvidenceRefs {
		if len(e) > 200 {
			return invalid("evidence_ref exceeds 200 bytes")
		}
	}
	if hasNUL(proposalText(in)) {
		return invalid("payload contains NUL")
	}
	if len(proposalText(in)) > 96*1024 {
		return invalid("payload exceeds 96 KiB")
	}
	if detectSecret(proposalText(in)) {
		return &Error{Reason: ReasonSecretDetected, Message: "team memory payload contains a secret-shaped credential"}
	}
	switch in.Operation {
	case centergit.ProposalAdd, centergit.ProposalUpdate:
		if in.Candidate == nil {
			return invalid("candidate is required")
		}
		if in.Operation == centergit.ProposalAdd && strings.TrimSpace(in.Candidate.Slug) == "" {
			return invalid("candidate.slug is required for add")
		}
		if in.Operation == centergit.ProposalUpdate && strings.TrimSpace(in.Candidate.Slug) != "" {
			return invalid("update cannot rename; omit candidate.slug")
		}
		if strings.TrimSpace(in.Candidate.Description) == "" {
			return invalid("candidate.description is required")
		}
		if len(in.Candidate.Title) > 200 || len(in.Candidate.Description) > 1000 || len(in.Candidate.Body) > 64*1024 {
			return invalid("candidate title, description, or body exceeds size limits")
		}
		if strings.HasPrefix(strings.TrimSpace(in.Candidate.Body), "---") || strings.Contains(in.Candidate.Body, "\n---\n") {
			return invalid("candidate body may not contain frontmatter fences")
		}
	case centergit.ProposalDisable, centergit.ProposalDelete:
		if in.Target == nil {
			return invalid("target is required")
		}
	default:
		return invalid("operation must be add, update, disable, or delete")
	}
	if in.TargetKind != centergit.ProposalTargetEntry && in.TargetKind != centergit.ProposalTargetRule {
		return invalid("target_kind must be entry or rule")
	}
	if in.Operation == centergit.ProposalDisable && in.TargetKind != centergit.ProposalTargetRule {
		return invalid("disable is only valid for rules")
	}
	if in.Target != nil {
		if strings.Contains(in.Target.SourcePath, "..") || strings.ContainsAny(in.Target.SourcePath, "\x00\\") {
			return invalid("target source_path is unsafe")
		}
	}
	if in.Candidate != nil && in.TargetKind == centergit.ProposalTargetRule {
		for _, p := range in.Candidate.AppliesTo {
			switch strings.ToLower(strings.TrimSpace(p)) {
			case "", "all", "plan", "execute", "execution", "review", "recovery", "recover":
			default:
				return invalid("candidate.applies_to contains an unknown phase")
			}
		}
	}
	return nil
}

func validateReview(in centergit.ReviewInput) error {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action != "promote" && action != "reject" {
		return invalid("action must be promote or reject")
	}
	if strings.TrimSpace(in.ProposalID) == "" || strings.TrimSpace(in.ExpectedRepoCommit) == "" {
		return invalid("proposal_id and expected_repo_commit are required")
	}
	if in.ExpectedProposalStatus != "" && in.ExpectedProposalStatus != centergit.ProposalPending {
		return invalid("expected_proposal_status must be pending")
	}
	if strings.TrimSpace(in.Comment) == "" {
		return invalid("review comment is required")
	}
	if len(in.Comment) > 4096 {
		return invalid("review comment exceeds 4096 bytes")
	}
	if hasNUL(in.Comment) || detectSecret(in.Comment) {
		return &Error{Reason: ReasonSecretDetected, Message: "review comment contains a secret-shaped credential"}
	}
	return nil
}

func invalid(msg string) error {
	return &Error{Reason: ReasonInvalidCandidate, Message: msg}
}

func proposalText(in centergit.ProposeInput) string {
	var b strings.Builder
	b.WriteString(string(in.Operation))
	b.WriteString("\n")
	b.WriteString(string(in.TargetKind))
	b.WriteString("\n")
	b.WriteString(in.IdempotencyKey)
	b.WriteString("\n")
	b.WriteString(in.Rationale)
	for _, e := range in.EvidenceRefs {
		b.WriteString("\n")
		b.WriteString(e)
	}
	if in.Target != nil {
		b.WriteString("\n")
		b.WriteString(in.Target.SourcePath)
		b.WriteString("\n")
		b.WriteString(in.Target.UUID)
		b.WriteString("\n")
		b.WriteString(in.Target.ExpectedBlobHash)
	}
	if in.Candidate != nil {
		b.WriteString("\n")
		b.WriteString(in.Candidate.Slug)
		b.WriteString("\n")
		b.WriteString(in.Candidate.Title)
		b.WriteString("\n")
		b.WriteString(in.Candidate.Description)
		b.WriteString("\n")
		b.WriteString(in.Candidate.Body)
		for _, p := range in.Candidate.AppliesTo {
			b.WriteString("\n")
			b.WriteString(p)
		}
	}
	return b.String()
}

func hasNUL(s string) bool { return strings.ContainsRune(s, 0) }

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bacat_[A-Za-z0-9_=-]{12,}`),
	regexp.MustCompile(`(?i)\bAC_MCP_WORKER_TOKEN\s*=`),
	regexp.MustCompile(`(?i)\bAGENT_CENTER_ADMIN_TOKEN\s*=`),
	regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9]{20,})\b`),
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|access[_-]?token|secret)\s*[:=]\s*['"]?[A-Za-z0-9_./+=-]{20,}`),
}

func detectSecret(s string) bool {
	for _, re := range secretPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

var warningMatchers = map[string]*regexp.Regexp{
	"repo_url":          regexp.MustCompile(`(?i)(https?://|ssh://|git@[^:]+:)`),
	"absolute_path":     regexp.MustCompile(`(?i)(^|\s)(/Users/|/home/|/var/|/etc/|[A-Z]:\\)`),
	"proprietary_token": regexp.MustCompile(`\b[A-Za-z0-9_-]{48,}\b`),
}

func contentWarnings(s string) []string {
	var out []string
	for code, re := range warningMatchers {
		if re.MatchString(s) {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

func ViewPayload(v centergit.ProposalView) map[string]any {
	payload := map[string]any{
		"team_id":           v.TeamID,
		"proposal_id":       v.ProposalID,
		"operation":         v.Operation,
		"target_kind":       v.TargetKind,
		"target":            v.Target,
		"candidate":         v.Candidate,
		"rationale":         v.Rationale,
		"evidence_refs":     v.EvidenceRefs,
		"warnings":          v.Warnings,
		"author_ref":        v.AuthorRef,
		"created_at":        v.CreatedAt,
		"status":            v.Status,
		"repo_commit":       v.RepoCommit,
		"current_blob_hash": v.CurrentBlobHash,
		"diff_preview":      v.DiffPreview,
		"effective_for":     v.EffectiveFor,
	}
	if v.ReviewerRef != "" {
		payload["reviewer_ref"] = v.ReviewerRef
		payload["review_action"] = v.ReviewAction
		payload["review_comment"] = v.ReviewComment
		payload["reviewed_at"] = v.ReviewedAt
	}
	return payload
}

func StatusFromString(s string) centergit.ProposalStatus {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "pending":
		return centergit.ProposalPending
	case "promoted":
		return centergit.ProposalPromoted
	case "rejected":
		return centergit.ProposalRejected
	default:
		return centergit.ProposalStatus(s)
	}
}

func KindFromString(s string) centergit.ProposalTargetKind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return ""
	case "entry":
		return centergit.ProposalTargetEntry
	case "rule":
		return centergit.ProposalTargetRule
	default:
		return centergit.ProposalTargetKind(s)
	}
}

func OperationFromString(s string) centergit.ProposalOperation {
	return centergit.ProposalOperation(strings.ToLower(strings.TrimSpace(s)))
}

func NewCandidate(slug, title, description, body string, enabled *bool, applies []string) *centergit.ProposalCandidate {
	return &centergit.ProposalCandidate{Slug: slug, Title: title, Description: description, Body: body, Enabled: enabled, AppliesTo: applies}
}

func Errorf(reason, format string, args ...any) error {
	return &Error{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

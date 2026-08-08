// Package teammemory owns the Team Memory aggregate and application service
// contracts for ADR-0057 controlled writes. Git is an adapter detail; this
// package keeps Proposal state and authorization flow independent of storage.
package teammemory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Operation is the canonical memory mutation proposed for review.
type Operation string

const (
	OperationAdd     Operation = "add"
	OperationUpdate  Operation = "update"
	OperationDisable Operation = "disable"
	OperationDelete  Operation = "delete"
)

// TargetKind distinguishes canonical directories.
type TargetKind string

const (
	TargetEntry TargetKind = "entry"
	TargetRule  TargetKind = "rule"
)

// ProposalStatus is the Proposal state machine.
type ProposalStatus string

const (
	StatusPending    ProposalStatus = "pending"
	StatusPromoted   ProposalStatus = "promoted"
	StatusRejected   ProposalStatus = "rejected"
	StatusSuperseded ProposalStatus = "superseded"
)

// ReviewAction is a terminal review transition requested by a reviewer.
type ReviewAction string

const (
	ActionPromote ReviewAction = "promote"
	ActionReject  ReviewAction = "reject"
)

// EffectiveFor documents runtime refresh semantics for successful promotion.
const EffectiveForNewSessionsAndForks = "new_sessions_and_forks"

// TargetRef pins an existing canonical file by repo path, file UUID, and git
// blob hash. update/disable/delete must satisfy all three.
type TargetRef struct {
	SourcePath       string `json:"source_path"`
	UUID             string `json:"uuid"`
	ExpectedBlobHash string `json:"expected_blob_hash"`
}

// Candidate is the proposed canonical content. Rules may carry Enabled and
// AppliesTo; entries may carry Type.
type Candidate struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description"`
	Body        string   `json:"body,omitempty"`
	Type        string   `json:"type,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	AppliesTo   []string `json:"applies_to,omitempty"`
}

// Proposal is an entity inside the TeamMemory aggregate.
type Proposal struct {
	TeamID          string         `json:"team_id"`
	ProposalID      string         `json:"proposal_id"`
	Operation       Operation      `json:"operation"`
	TargetKind      TargetKind     `json:"target_kind"`
	Target          *TargetRef     `json:"target,omitempty"`
	Candidate       *Candidate     `json:"candidate,omitempty"`
	Rationale       string         `json:"rationale"`
	EvidenceRefs    []string       `json:"evidence_refs,omitempty"`
	AuthorRef       string         `json:"author_ref"`
	CreatedAt       time.Time      `json:"created_at"`
	IdempotencyKey  string         `json:"idempotency_key"`
	Status          ProposalStatus `json:"status"`
	Warnings        []string       `json:"warnings,omitempty"`
	ReviewerRef     string         `json:"reviewer_ref,omitempty"`
	ReviewComment   string         `json:"review_comment,omitempty"`
	ReviewedAt      time.Time      `json:"reviewed_at,omitempty"`
	PromotionCommit string         `json:"promotion_commit,omitempty"`
	SourcePath      string         `json:"source_path,omitempty"`
	Supersedes      string         `json:"supersedes,omitempty"`
	PayloadHash     string         `json:"payload_hash,omitempty"`
}

// TeamMemory is the per-team aggregate root; the Git HEAD commit is the
// aggregate version.
type TeamMemory struct {
	TeamID string
	Commit string
}

// Promote applies the pending->promoted transition.
func (p *Proposal) Promote(reviewer, comment, commit string, now time.Time) error {
	if p == nil {
		return ErrProposalNotFound
	}
	if p.Status != StatusPending {
		return ErrProposalNotPending
	}
	p.Status = StatusPromoted
	p.ReviewerRef = strings.TrimSpace(reviewer)
	p.ReviewComment = strings.TrimSpace(comment)
	p.ReviewedAt = now.UTC()
	p.PromotionCommit = strings.TrimSpace(commit)
	return nil
}

// Reject applies the pending->rejected transition.
func (p *Proposal) Reject(reviewer, comment string, now time.Time) error {
	if p == nil {
		return ErrProposalNotFound
	}
	if p.Status != StatusPending {
		return ErrProposalNotPending
	}
	p.Status = StatusRejected
	p.ReviewerRef = strings.TrimSpace(reviewer)
	p.ReviewComment = strings.TrimSpace(comment)
	p.ReviewedAt = now.UTC()
	return nil
}

// Supersede applies the pending->superseded transition.
func (p *Proposal) Supersede(reviewer, comment, successor string, now time.Time) error {
	if p == nil {
		return ErrProposalNotFound
	}
	if p.Status != StatusPending {
		return ErrProposalNotPending
	}
	p.Status = StatusSuperseded
	p.ReviewerRef = strings.TrimSpace(reviewer)
	p.ReviewComment = strings.TrimSpace(comment)
	p.ReviewedAt = now.UTC()
	p.Supersedes = strings.TrimSpace(successor)
	return nil
}

// ProposeCommand is the agent-facing proposal payload. TeamID is intentionally
// absent from the MCP contract; trusted callers may use Service.ProposeForTeam.
type ProposeCommand struct {
	ActorRef       string
	IdempotencyKey string
	Operation      Operation
	TargetKind     TargetKind
	Target         *TargetRef
	Candidate      *Candidate
	Rationale      string
	EvidenceRefs   []string
}

// ReviewCommand is the promote/reject payload.
type ReviewCommand struct {
	ActorRef               string
	ProposalID             string
	Action                 ReviewAction
	ExpectedRepoCommit     string
	ExpectedProposalStatus ProposalStatus
	Comment                string
	AcknowledgeWarnings    []string
}

// ListCommand controls proposal listing.
type ListCommand struct {
	ActorRef   string
	TeamID     string
	Status     []ProposalStatus
	TargetKind TargetKind
	Limit      int
	Offset     int
}

// GetCommand reads one proposal.
type GetCommand struct {
	ActorRef   string
	TeamID     string
	ProposalID string
}

// Result is returned by propose/review commands.
type Result struct {
	TeamID       string         `json:"team_id"`
	ProposalID   string         `json:"proposal_id"`
	Status       ProposalStatus `json:"status"`
	RepoCommit   string         `json:"repo_commit"`
	SourcePath   string         `json:"source_path,omitempty"`
	Warnings     []string       `json:"warnings,omitempty"`
	EffectiveFor string         `json:"effective_for,omitempty"`
	OldCommit    string         `json:"old_commit,omitempty"`
	NewCommit    string         `json:"new_commit,omitempty"`
}

// ProposalView is returned by get/list. CurrentTargetBlobHash is populated when
// the canonical target still exists.
type ProposalView struct {
	Proposal              Proposal `json:"proposal"`
	RepoCommit            string   `json:"repo_commit"`
	CurrentTargetBlobHash string   `json:"current_target_blob_hash,omitempty"`
	DiffPreview           string   `json:"diff_preview,omitempty"`
}

// ListResult is a stable paginated proposal read model.
type ListResult struct {
	TeamID     string         `json:"team_id"`
	RepoCommit string         `json:"repo_commit"`
	Proposals  []ProposalView `json:"proposals"`
	Total      int            `json:"total"`
	HasMore    bool           `json:"has_more"`
}

// Filter is the repository-side listing filter after auth has resolved team.
type Filter struct {
	Status     []ProposalStatus
	TargetKind TargetKind
	Limit      int
	Offset     int
}

// Repository is the Team Memory storage port. Git-backed implementations are
// the write authority.
type Repository interface {
	Propose(ctx context.Context, teamID string, cmd ProposeCommand) (Result, error)
	List(ctx context.Context, teamID string, filter Filter) (ListResult, error)
	Get(ctx context.Context, teamID, proposalID string) (ProposalView, error)
	Review(ctx context.Context, teamID string, cmd ReviewCommand) (Result, error)
}

// AuthorizationPort is implemented by the Team/Identity adapters. It answers
// membership and explicit curator policy without giving Cognition direct access
// to Team SQLite internals.
type AuthorizationPort interface {
	ResolveActorTeam(ctx context.Context, actorRef string) (teamID string, ok bool, err error)
	CanPropose(ctx context.Context, teamID, actorRef string) (bool, error)
	CanRead(ctx context.Context, teamID, actorRef string) (bool, error)
	CanReview(ctx context.Context, teamID, actorRef string) (bool, error)
}

// Service is the TeamMemory application service. MCP/Web/bootstrap adapters
// should enter through this type rather than calling Git plumbing directly.
type Service struct {
	repo Repository
	auth AuthorizationPort
}

// NewService wires the application service.
func NewService(repo Repository, auth AuthorizationPort) *Service {
	return &Service{repo: repo, auth: auth}
}

// Propose resolves the caller's team from ActorRef and persists a pending
// proposal after membership authorization.
func (s *Service) Propose(ctx context.Context, cmd ProposeCommand) (Result, error) {
	if s == nil || s.repo == nil || s.auth == nil {
		return Result{}, ErrNotWired
	}
	teamID, ok, err := s.auth.ResolveActorTeam(ctx, cmd.ActorRef)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrNotTeamMember
	}
	return s.ProposeForTeam(ctx, teamID, cmd)
}

// ProposeForTeam is for trusted adapters that already resolved team scope (for
// example Web routes). It still checks CanPropose.
func (s *Service) ProposeForTeam(ctx context.Context, teamID string, cmd ProposeCommand) (Result, error) {
	if s == nil || s.repo == nil || s.auth == nil {
		return Result{}, ErrNotWired
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return Result{}, ErrNotTeamMember
	}
	ok, err := s.auth.CanPropose(ctx, teamID, cmd.ActorRef)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrNotTeamMember
	}
	return s.repo.Propose(ctx, teamID, cmd)
}

// List returns proposals visible to the actor. If TeamID is omitted the actor's
// team is resolved from membership.
func (s *Service) List(ctx context.Context, cmd ListCommand) (ListResult, error) {
	teamID, err := s.resolveReadableTeam(ctx, cmd.ActorRef, cmd.TeamID)
	if err != nil {
		return ListResult{}, err
	}
	return s.repo.List(ctx, teamID, Filter{Status: cmd.Status, TargetKind: cmd.TargetKind, Limit: cmd.Limit, Offset: cmd.Offset})
}

// Get returns a single proposal only within the actor's resolved team.
func (s *Service) Get(ctx context.Context, cmd GetCommand) (ProposalView, error) {
	teamID, err := s.resolveReadableTeam(ctx, cmd.ActorRef, cmd.TeamID)
	if err != nil {
		return ProposalView{}, err
	}
	return s.repo.Get(ctx, teamID, cmd.ProposalID)
}

// Review promotes or rejects a pending proposal after curator/human auth.
func (s *Service) Review(ctx context.Context, teamID string, cmd ReviewCommand) (Result, error) {
	if s == nil || s.repo == nil || s.auth == nil {
		return Result{}, ErrNotWired
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		resolved, ok, err := s.auth.ResolveActorTeam(ctx, cmd.ActorRef)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{}, ErrNotTeamMember
		}
		teamID = resolved
	}
	ok, err := s.auth.CanReview(ctx, teamID, cmd.ActorRef)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, ErrNotMemoryCurator
	}
	return s.repo.Review(ctx, teamID, cmd)
}

func (s *Service) resolveReadableTeam(ctx context.Context, actorRef, teamID string) (string, error) {
	if s == nil || s.repo == nil || s.auth == nil {
		return "", ErrNotWired
	}
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		resolved, ok, err := s.auth.ResolveActorTeam(ctx, actorRef)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", ErrNotTeamMember
		}
		teamID = resolved
	}
	ok, err := s.auth.CanRead(ctx, teamID, actorRef)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNotTeamMember
	}
	return teamID, nil
}

// Stable taxonomy. Sentinels intentionally use the wire reason as their error
// text so transports can split reason/message without string rewriting.
var (
	ErrNotWired                  = errors.New("team_memory_not_wired")
	ErrNotTeamMember             = errors.New("not_team_member")
	ErrNotMemoryCurator          = errors.New("not_memory_curator")
	ErrInvalidCandidate          = errors.New("invalid_candidate")
	ErrSecretDetected            = errors.New("secret_detected")
	ErrWarningUnacknowledged     = errors.New("warning_unacknowledged")
	ErrTargetChanged             = errors.New("target_changed")
	ErrProposalNotFound          = errors.New("proposal_not_found")
	ErrProposalNotPending        = errors.New("proposal_not_pending")
	ErrIdempotencyConflict       = errors.New("idempotency_conflict")
	ErrTeamMemoryVersionConflict = errors.New("team_memory_version_conflict")
	ErrGitUnavailable            = errors.New("git_unavailable")
)

// Reason returns the stable wire reason for a known Team Memory error.
func Reason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotWired):
		return "team_memory_not_wired"
	case errors.Is(err, ErrNotTeamMember):
		return "not_team_member"
	case errors.Is(err, ErrNotMemoryCurator):
		return "not_memory_curator"
	case errors.Is(err, ErrInvalidCandidate):
		return "invalid_candidate"
	case errors.Is(err, ErrSecretDetected):
		return "secret_detected"
	case errors.Is(err, ErrWarningUnacknowledged):
		return "warning_unacknowledged"
	case errors.Is(err, ErrTargetChanged):
		return "target_changed"
	case errors.Is(err, ErrProposalNotFound):
		return "proposal_not_found"
	case errors.Is(err, ErrProposalNotPending):
		return "proposal_not_pending"
	case errors.Is(err, ErrIdempotencyConflict):
		return "idempotency_conflict"
	case errors.Is(err, ErrTeamMemoryVersionConflict):
		return "team_memory_version_conflict"
	case errors.Is(err, ErrGitUnavailable):
		return "git_unavailable"
	default:
		return "team_memory_error"
	}
}

// Invalid wraps an invalid-candidate reason with a useful message.
func Invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCandidate, fmt.Sprintf(format, args...))
}

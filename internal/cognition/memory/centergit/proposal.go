package centergit

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/idgen"
)

const proposalsDir = "proposals"

var (
	ErrInvalidProposal             = errors.New("centergit: invalid team memory proposal")
	ErrProposalNotFound            = errors.New("centergit: team memory proposal not found")
	ErrProposalNotPending          = errors.New("centergit: team memory proposal not pending")
	ErrIdempotencyConflict         = errors.New("centergit: team memory idempotency conflict")
	ErrTargetChanged               = errors.New("centergit: team memory target changed")
	ErrWarningUnacknowledged       = errors.New("centergit: team memory warning unacknowledged")
	ErrTeamMemoryVersionConflict   = errors.New("centergit: team memory version conflict")
	ErrUnsupportedProposalAction   = errors.New("centergit: unsupported team memory review action")
	ErrUnsupportedProposalMutation = errors.New("centergit: unsupported team memory mutation")
)

type ProposalOperation string

const (
	ProposalAdd     ProposalOperation = "add"
	ProposalUpdate  ProposalOperation = "update"
	ProposalDisable ProposalOperation = "disable"
	ProposalDelete  ProposalOperation = "delete"
)

type ProposalTargetKind string

const (
	ProposalTargetEntry ProposalTargetKind = "entry"
	ProposalTargetRule  ProposalTargetKind = "rule"
)

type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalPromoted ProposalStatus = "promoted"
	ProposalRejected ProposalStatus = "rejected"
)

type ProposalCandidate struct {
	Slug        string   `json:"slug,omitempty"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Body        string   `json:"body,omitempty"`
	Enabled     *bool    `json:"enabled,omitempty"`
	AppliesTo   []string `json:"applies_to,omitempty"`
}

type ProposalTarget struct {
	SourcePath       string `json:"source_path,omitempty"`
	UUID             string `json:"uuid,omitempty"`
	ExpectedBlobHash string `json:"expected_blob_hash,omitempty"`
}

type ProposeInput struct {
	ProposalID     string
	ActorRef       string
	IdempotencyKey string
	Operation      ProposalOperation
	TargetKind     ProposalTargetKind
	Target         *ProposalTarget
	Candidate      *ProposalCandidate
	Rationale      string
	EvidenceRefs   []string
	Warnings       []string
	CreatedAt      time.Time
}

type ReviewInput struct {
	ActorRef               string
	ProposalID             string
	Action                 string
	ExpectedRepoCommit     string
	ExpectedProposalStatus ProposalStatus
	Comment                string
	AcknowledgeWarnings    []string
	ReviewedAt             time.Time
}

type ProposalView struct {
	TeamID           string             `json:"team_id"`
	ProposalID       string             `json:"proposal_id"`
	Operation        ProposalOperation  `json:"operation"`
	TargetKind       ProposalTargetKind `json:"target_kind"`
	Target           ProposalTarget     `json:"target"`
	Candidate        *ProposalCandidate `json:"candidate,omitempty"`
	Rationale        string             `json:"rationale"`
	EvidenceRefs     []string           `json:"evidence_refs"`
	Warnings         []string           `json:"warnings"`
	AuthorRef        string             `json:"author_ref"`
	CreatedAt        time.Time          `json:"created_at"`
	IdempotencyKey   string             `json:"idempotency_key,omitempty"`
	Status           ProposalStatus     `json:"status"`
	ReviewerRef      string             `json:"reviewer_ref,omitempty"`
	ReviewAction     string             `json:"review_action,omitempty"`
	ReviewComment    string             `json:"review_comment,omitempty"`
	ReviewedAt       time.Time          `json:"reviewed_at,omitempty"`
	RepoCommit       string             `json:"repo_commit"`
	CreatedCommit    string             `json:"created_commit,omitempty"`
	StatusCommit     string             `json:"status_commit,omitempty"`
	CurrentBlobHash  string             `json:"current_blob_hash,omitempty"`
	DiffPreview      string             `json:"diff_preview,omitempty"`
	EffectiveFor     string             `json:"effective_for,omitempty"`
	proposalFilePath string
	payloadHash      string
}

type ProposalListFilter struct {
	Status ProposalStatus
	Kind   ProposalTargetKind
	Limit  int
	Offset int
}

type TeamMemoryGit struct {
	host           *Host
	runner         memory.GitRunner
	author         Author
	newProposalID  func() string
	newCanonicalID func() string
}

type TeamMemoryGitOption func(*TeamMemoryGit)

func WithProposalIDGen(fn func() string) TeamMemoryGitOption {
	return func(g *TeamMemoryGit) { g.newProposalID = fn }
}

func WithCanonicalIDGen(fn func() string) TeamMemoryGitOption {
	return func(g *TeamMemoryGit) { g.newCanonicalID = fn }
}

func NewTeamMemoryGit(host *Host, runner memory.GitRunner, opts ...TeamMemoryGitOption) *TeamMemoryGit {
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	g := &TeamMemoryGit{
		host:           host,
		runner:         runner,
		author:         defaultSeedAuthor,
		newProposalID:  func() string { return "tmprop-" + idgen.MustNewULID() },
		newCanonicalID: idgen.MustNewULID,
	}
	for _, o := range opts {
		o(g)
	}
	return g
}

func (g *TeamMemoryGit) Propose(ctx context.Context, teamID string, in ProposeInput) (ProposalView, error) {
	if err := g.require(); err != nil {
		return ProposalView{}, err
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	if in.ProposalID == "" {
		in.ProposalID = g.newProposalID()
	}
	ref := TeamRepo(teamID)
	if err := g.host.EnsureRepo(ctx, ref); err != nil {
		return ProposalView{}, err
	}
	repoDir, work, err := g.clone(ctx, ref, "team-memory-propose-*")
	if err != nil {
		return ProposalView{}, err
	}
	defer os.RemoveAll(work)

	payloadHash, err := proposalPayloadHash(in)
	if err != nil {
		return ProposalView{}, err
	}
	if existing, ok, err := g.findByIdempotency(ctx, teamID, repoDir, in.ActorRef, in.IdempotencyKey); err != nil {
		return ProposalView{}, err
	} else if ok {
		if existing.payloadHash != payloadHash {
			return ProposalView{}, ErrIdempotencyConflict
		}
		return existing, nil
	}

	fm, body, err := g.newProposalDocument(in, payloadHash)
	if err != nil {
		return ProposalView{}, err
	}
	if err := writeProposalDocument(repoDir, fm, body); err != nil {
		return ProposalView{}, err
	}
	store := NewStore(repoDir, g.runner, WithHomeOverride(work))
	if err := store.Commit(ctx, g.author, "propose team memory "+fm.ProposalID); err != nil {
		return ProposalView{}, err
	}
	if err := g.pushWithProposalRetry(ctx, repoDir, work, teamID, in.ActorRef, in.IdempotencyKey, payloadHash); err != nil {
		return ProposalView{}, err
	}
	return g.getProposalFromWorktree(ctx, teamID, repoDir, fm.ProposalID, true)
}

func (g *TeamMemoryGit) ListProposals(ctx context.Context, teamID string, filter ProposalListFilter) ([]ProposalView, string, error) {
	if err := g.require(); err != nil {
		return nil, "", err
	}
	repoDir, work, err := g.cloneIfExists(ctx, TeamRepo(teamID), "team-memory-list-*")
	if err != nil || repoDir == "" {
		return nil, "", err
	}
	defer os.RemoveAll(work)
	head := g.head(ctx, repoDir, work)
	props, err := g.loadProposals(ctx, teamID, repoDir, true)
	if err != nil {
		return nil, "", err
	}
	out := make([]ProposalView, 0, len(props))
	for _, p := range props {
		if filter.Status != "" && p.Status != filter.Status {
			continue
		}
		if filter.Kind != "" && p.TargetKind != filter.Kind {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ProposalID > out[j].ProposalID
	})
	if filter.Offset > 0 {
		if filter.Offset >= len(out) {
			out = nil
		} else {
			out = out[filter.Offset:]
		}
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, head, nil
}

func (g *TeamMemoryGit) GetProposal(ctx context.Context, teamID, proposalID string) (ProposalView, error) {
	if err := g.require(); err != nil {
		return ProposalView{}, err
	}
	repoDir, work, err := g.cloneIfExists(ctx, TeamRepo(teamID), "team-memory-get-*")
	if err != nil || repoDir == "" {
		if err == nil {
			err = ErrProposalNotFound
		}
		return ProposalView{}, err
	}
	defer os.RemoveAll(work)
	return g.getProposalFromWorktree(ctx, teamID, repoDir, proposalID, true)
}

func (g *TeamMemoryGit) Review(ctx context.Context, teamID string, in ReviewInput) (ProposalView, error) {
	if err := g.require(); err != nil {
		return ProposalView{}, err
	}
	if in.ReviewedAt.IsZero() {
		in.ReviewedAt = time.Now().UTC()
	}
	if strings.TrimSpace(in.ExpectedRepoCommit) == "" {
		return ProposalView{}, fmt.Errorf("%w: expected_repo_commit is required", ErrInvalidProposal)
	}
	if in.ExpectedProposalStatus == "" {
		in.ExpectedProposalStatus = ProposalPending
	}
	repoDir, work, err := g.cloneIfExists(ctx, TeamRepo(teamID), "team-memory-review-*")
	if err != nil || repoDir == "" {
		if err == nil {
			err = ErrProposalNotFound
		}
		return ProposalView{}, err
	}
	defer os.RemoveAll(work)
	head := g.head(ctx, repoDir, work)
	if head != in.ExpectedRepoCommit {
		return ProposalView{}, ErrTeamMemoryVersionConflict
	}
	p, err := g.getProposalFromWorktree(ctx, teamID, repoDir, in.ProposalID, true)
	if err != nil {
		return ProposalView{}, err
	}
	if p.Status != ProposalPending || p.Status != in.ExpectedProposalStatus {
		return ProposalView{}, ErrProposalNotPending
	}
	if strings.TrimSpace(in.Comment) == "" {
		return ProposalView{}, fmt.Errorf("%w: review comment is required", ErrInvalidProposal)
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	switch action {
	case "reject":
		if err := updateProposalReview(repoDir, p, ProposalRejected, in); err != nil {
			return ProposalView{}, err
		}
	case "promote":
		if missing := missingWarningAcknowledgements(p.Warnings, in.AcknowledgeWarnings); len(missing) > 0 {
			return ProposalView{}, fmt.Errorf("%w: %s", ErrWarningUnacknowledged, strings.Join(missing, ","))
		}
		if err := g.applyPromotion(repoDir, p); err != nil {
			return ProposalView{}, err
		}
		if err := NewStore(repoDir, g.runner, WithHomeOverride(work)).RegenerateIndex(); err != nil {
			return ProposalView{}, err
		}
		if err := updateProposalReview(repoDir, p, ProposalPromoted, in); err != nil {
			return ProposalView{}, err
		}
	default:
		return ProposalView{}, ErrUnsupportedProposalAction
	}

	store := NewStore(repoDir, g.runner, WithHomeOverride(work))
	if err := store.Commit(ctx, g.author, action+" team memory "+in.ProposalID); err != nil {
		return ProposalView{}, err
	}
	out, err := g.runner.Run(ctx, repoDir, baseGitEnv(work, g.author.Name, g.author.Email), "push", "origin", "HEAD:main")
	if err != nil {
		if isNonFastForward(out) {
			return ProposalView{}, ErrTeamMemoryVersionConflict
		}
		return ProposalView{}, fmt.Errorf("%w: push: %v: %s", ErrGitOpFailed, err, out)
	}
	return g.getProposalFromWorktree(ctx, teamID, repoDir, in.ProposalID, true)
}

func (g *TeamMemoryGit) require() error {
	if g == nil || g.host == nil {
		return fmt.Errorf("%w: team memory git not wired", ErrGitOpFailed)
	}
	return nil
}

func (g *TeamMemoryGit) clone(ctx context.Context, ref RepoRef, prefix string) (repoDir, work string, err error) {
	bareDir, err := g.host.RepoDir(ref)
	if err != nil {
		return "", "", err
	}
	work, err = os.MkdirTemp("", prefix)
	if err != nil {
		return "", "", fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	repoDir = filepath.Join(work, "repo")
	if out, cErr := g.runner.Run(ctx, work, baseGitEnv(work, g.author.Name, g.author.Email), "clone", bareDir, repoDir); cErr != nil {
		_ = os.RemoveAll(work)
		return "", "", fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}
	return repoDir, work, nil
}

func (g *TeamMemoryGit) cloneIfExists(ctx context.Context, ref RepoRef, prefix string) (repoDir, work string, err error) {
	exists, err := g.host.RepoExists(ref)
	if err != nil || !exists {
		return "", "", err
	}
	return g.clone(ctx, ref, prefix)
}

func (g *TeamMemoryGit) head(ctx context.Context, repoDir, work string) string {
	out, err := g.runner.Run(ctx, repoDir, baseGitEnv(work, "", ""), "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (g *TeamMemoryGit) pushWithProposalRetry(ctx context.Context, repoDir, work, teamID, actorRef, idem, payloadHash string) error {
	env := baseGitEnv(work, g.author.Name, g.author.Email)
	for attempt := 0; attempt < 3; attempt++ {
		out, err := g.runner.Run(ctx, repoDir, env, "push", "origin", "HEAD:main")
		if err == nil {
			return nil
		}
		if !isNonFastForward(out) {
			return fmt.Errorf("%w: push: %v: %s", ErrGitOpFailed, err, out)
		}
		if out, rErr := g.runner.Run(ctx, repoDir, env, "-c", "rebase.autoStash=true", "pull", "--rebase", "origin", "main"); rErr != nil {
			return fmt.Errorf("%w: pull --rebase: %v: %s", ErrGitOpFailed, rErr, out)
		}
		if existing, ok, lErr := g.findByIdempotency(ctx, teamID, repoDir, actorRef, idem); lErr != nil {
			return lErr
		} else if ok && existing.payloadHash != payloadHash {
			return ErrIdempotencyConflict
		}
	}
	return ErrPushRetriesExhausted
}

type proposalFrontmatter struct {
	ProposalID       string   `yaml:"proposal_id"`
	Operation        string   `yaml:"operation"`
	TargetKind       string   `yaml:"target_kind"`
	TargetSourcePath string   `yaml:"target_source_path,omitempty"`
	TargetUUID       string   `yaml:"target_uuid,omitempty"`
	ExpectedBlobHash string   `yaml:"expected_blob_hash,omitempty"`
	AuthorRef        string   `yaml:"author_ref"`
	CreatedAt        string   `yaml:"created_at"`
	IdempotencyKey   string   `yaml:"idempotency_key"`
	Status           string   `yaml:"status"`
	Rationale        string   `yaml:"rationale"`
	EvidenceRefs     []string `yaml:"evidence_refs,omitempty"`
	Warnings         []string `yaml:"warnings,omitempty"`
	PayloadHash      string   `yaml:"payload_hash"`
	ReviewerRef      string   `yaml:"reviewer_ref,omitempty"`
	ReviewAction     string   `yaml:"review_action,omitempty"`
	ReviewComment    string   `yaml:"review_comment,omitempty"`
	ReviewedAt       string   `yaml:"reviewed_at,omitempty"`
}

type proposalBody struct {
	Candidate *ProposalCandidate `json:"candidate,omitempty"`
}

func (g *TeamMemoryGit) newProposalDocument(in ProposeInput, payloadHash string) (proposalFrontmatter, proposalBody, error) {
	op := in.Operation
	kind := in.TargetKind
	if !validProposalMutation(op, kind) {
		return proposalFrontmatter{}, proposalBody{}, ErrUnsupportedProposalMutation
	}
	target := ProposalTarget{}
	if in.Target != nil {
		target = *in.Target
	}
	if op == ProposalAdd {
		if in.Candidate == nil {
			return proposalFrontmatter{}, proposalBody{}, fmt.Errorf("%w: candidate is required", ErrInvalidProposal)
		}
		if err := validateSegment(in.Candidate.Slug); err != nil {
			return proposalFrontmatter{}, proposalBody{}, fmt.Errorf("%w: invalid slug: %v", ErrInvalidProposal, err)
		}
		target.UUID = g.newCanonicalID()
		dir := entriesDir
		if kind == ProposalTargetRule {
			dir = rulesDir
		}
		target.SourcePath = filepath.ToSlash(filepath.Join(dir, in.Candidate.Slug+"-"+target.UUID+".md"))
	} else {
		if in.Target == nil {
			return proposalFrontmatter{}, proposalBody{}, fmt.Errorf("%w: target is required", ErrInvalidProposal)
		}
		if err := validateTargetPath(kind, target.SourcePath); err != nil {
			return proposalFrontmatter{}, proposalBody{}, err
		}
		if strings.TrimSpace(target.UUID) == "" || strings.TrimSpace(target.ExpectedBlobHash) == "" {
			return proposalFrontmatter{}, proposalBody{}, fmt.Errorf("%w: target uuid and expected_blob_hash are required", ErrInvalidProposal)
		}
	}
	fm := proposalFrontmatter{
		ProposalID:       in.ProposalID,
		Operation:        string(op),
		TargetKind:       string(kind),
		TargetSourcePath: target.SourcePath,
		TargetUUID:       target.UUID,
		ExpectedBlobHash: target.ExpectedBlobHash,
		AuthorRef:        strings.TrimSpace(in.ActorRef),
		CreatedAt:        in.CreatedAt.UTC().Format(time.RFC3339Nano),
		IdempotencyKey:   strings.TrimSpace(in.IdempotencyKey),
		Status:           string(ProposalPending),
		Rationale:        strings.TrimSpace(in.Rationale),
		EvidenceRefs:     append([]string(nil), in.EvidenceRefs...),
		Warnings:         append([]string(nil), in.Warnings...),
		PayloadHash:      payloadHash,
	}
	if fm.ProposalID == "" || fm.AuthorRef == "" || fm.IdempotencyKey == "" || fm.Rationale == "" {
		return proposalFrontmatter{}, proposalBody{}, fmt.Errorf("%w: proposal_id, actor_ref, idempotency_key and rationale are required", ErrInvalidProposal)
	}
	return fm, proposalBody{Candidate: in.Candidate}, nil
}

func validProposalMutation(op ProposalOperation, kind ProposalTargetKind) bool {
	if kind != ProposalTargetEntry && kind != ProposalTargetRule {
		return false
	}
	switch op {
	case ProposalAdd, ProposalUpdate, ProposalDelete:
		return true
	case ProposalDisable:
		return kind == ProposalTargetRule
	default:
		return false
	}
}

func writeProposalDocument(repoDir string, fm proposalFrontmatter, body proposalBody) error {
	raw, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	content, err := renderMarkdown(fm, string(raw))
	if err != nil {
		return err
	}
	rel := filepath.ToSlash(filepath.Join(proposalsDir, fm.ProposalID+".md"))
	abs := filepath.Join(repoDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o600)
}

func parseProposalDocument(abs, rel string) (ProposalView, proposalFrontmatter, proposalBody, error) {
	var fm proposalFrontmatter
	bodyText, err := parseMarkdown(abs, &fm)
	if err != nil {
		return ProposalView{}, fm, proposalBody{}, err
	}
	var body proposalBody
	if strings.TrimSpace(bodyText) != "" {
		if err := json.Unmarshal([]byte(bodyText), &body); err != nil {
			return ProposalView{}, fm, body, fmt.Errorf("%w: proposal body json: %v", ErrInvalidProposal, err)
		}
	}
	created, _ := time.Parse(time.RFC3339Nano, fm.CreatedAt)
	reviewed, _ := time.Parse(time.RFC3339Nano, fm.ReviewedAt)
	view := ProposalView{
		ProposalID:       fm.ProposalID,
		Operation:        ProposalOperation(fm.Operation),
		TargetKind:       ProposalTargetKind(fm.TargetKind),
		Target:           ProposalTarget{SourcePath: fm.TargetSourcePath, UUID: fm.TargetUUID, ExpectedBlobHash: fm.ExpectedBlobHash},
		Candidate:        body.Candidate,
		Rationale:        fm.Rationale,
		EvidenceRefs:     nonNilStrings(fm.EvidenceRefs),
		Warnings:         nonNilStrings(fm.Warnings),
		AuthorRef:        fm.AuthorRef,
		CreatedAt:        created,
		IdempotencyKey:   fm.IdempotencyKey,
		Status:           ProposalStatus(fm.Status),
		ReviewerRef:      fm.ReviewerRef,
		ReviewAction:     fm.ReviewAction,
		ReviewComment:    fm.ReviewComment,
		ReviewedAt:       reviewed,
		payloadHash:      fm.PayloadHash,
		proposalFilePath: rel,
	}
	return view, fm, body, nil
}

func (g *TeamMemoryGit) loadProposals(ctx context.Context, teamID, repoDir string, includeDetails bool) ([]ProposalView, error) {
	dir := filepath.Join(repoDir, proposalsDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	head := g.head(ctx, repoDir, filepath.Dir(repoDir))
	out := make([]ProposalView, 0, len(ents))
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(proposalsDir, de.Name()))
		p, _, _, err := parseProposalDocument(filepath.Join(dir, de.Name()), rel)
		if err != nil {
			return nil, err
		}
		p.TeamID = teamID
		p.RepoCommit = head
		p.CreatedCommit, p.StatusCommit = g.proposalCommits(ctx, repoDir, rel)
		if includeDetails {
			g.attachTargetDetails(ctx, repoDir, &p)
		}
		out = append(out, p)
	}
	return out, nil
}

func (g *TeamMemoryGit) getProposalFromWorktree(ctx context.Context, teamID, repoDir, proposalID string, includeDetails bool) (ProposalView, error) {
	rel := filepath.ToSlash(filepath.Join(proposalsDir, proposalID+".md"))
	abs := filepath.Join(repoDir, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return ProposalView{}, ErrProposalNotFound
		}
		return ProposalView{}, err
	}
	p, _, _, err := parseProposalDocument(abs, rel)
	if err != nil {
		return ProposalView{}, err
	}
	p.TeamID = teamID
	p.RepoCommit = g.head(ctx, repoDir, filepath.Dir(repoDir))
	p.CreatedCommit, p.StatusCommit = g.proposalCommits(ctx, repoDir, rel)
	if includeDetails {
		g.attachTargetDetails(ctx, repoDir, &p)
	}
	return p, nil
}

func (g *TeamMemoryGit) findByIdempotency(ctx context.Context, teamID, repoDir, actorRef, idem string) (ProposalView, bool, error) {
	if strings.TrimSpace(idem) == "" {
		return ProposalView{}, false, nil
	}
	props, err := g.loadProposals(ctx, teamID, repoDir, false)
	if err != nil {
		return ProposalView{}, false, err
	}
	for _, p := range props {
		if p.AuthorRef == actorRef && p.IdempotencyKey == idem {
			return p, true, nil
		}
	}
	return ProposalView{}, false, nil
}

func (g *TeamMemoryGit) proposalCommits(ctx context.Context, repoDir, rel string) (created, status string) {
	out, err := g.runner.Run(ctx, repoDir, baseGitEnv(filepath.Dir(repoDir), "", ""), "log", "--format=%H", "--", rel)
	if err != nil {
		return "", ""
	}
	lines := strings.Fields(out)
	if len(lines) == 0 {
		return "", ""
	}
	status = lines[0]
	created = lines[len(lines)-1]
	return created, status
}

func (g *TeamMemoryGit) attachTargetDetails(ctx context.Context, repoDir string, p *ProposalView) {
	if p == nil || p.Target.SourcePath == "" {
		return
	}
	if h, err := g.blobHash(ctx, repoDir, p.Target.SourcePath); err == nil {
		p.CurrentBlobHash = h
	}
	current, _ := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath)))
	next, _ := candidateCanonicalMarkdown(repoDir, *p)
	p.DiffPreview = diffPreview(string(current), next)
}

func (g *TeamMemoryGit) blobHash(ctx context.Context, repoDir, rel string) (string, error) {
	out, err := g.runner.Run(ctx, repoDir, baseGitEnv(filepath.Dir(repoDir), "", ""), "hash-object", "--", rel)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func updateProposalReview(repoDir string, p ProposalView, status ProposalStatus, in ReviewInput) error {
	abs := filepath.Join(repoDir, filepath.FromSlash(p.proposalFilePath))
	_, fm, body, err := parseProposalDocument(abs, p.proposalFilePath)
	if err != nil {
		return err
	}
	fm.Status = string(status)
	fm.ReviewerRef = strings.TrimSpace(in.ActorRef)
	fm.ReviewAction = strings.ToLower(strings.TrimSpace(in.Action))
	fm.ReviewComment = strings.TrimSpace(in.Comment)
	fm.ReviewedAt = in.ReviewedAt.UTC().Format(time.RFC3339Nano)
	return writeProposalDocument(repoDir, fm, body)
}

func (g *TeamMemoryGit) applyPromotion(repoDir string, p ProposalView) error {
	switch p.Operation {
	case ProposalAdd:
		return writeCanonicalAdd(repoDir, p)
	case ProposalUpdate:
		if err := assertTargetCurrent(repoDir, p); err != nil {
			return err
		}
		return writeCanonicalUpdate(repoDir, p)
	case ProposalDisable:
		if err := assertTargetCurrent(repoDir, p); err != nil {
			return err
		}
		return disableCanonicalRule(repoDir, p)
	case ProposalDelete:
		if err := assertTargetCurrent(repoDir, p); err != nil {
			return err
		}
		return os.Remove(filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath)))
	default:
		return ErrUnsupportedProposalMutation
	}
}

func writeCanonicalAdd(repoDir string, p ProposalView) error {
	if p.Candidate == nil {
		return fmt.Errorf("%w: candidate is required", ErrInvalidProposal)
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
	if _, err := os.Stat(abs); err == nil {
		return ErrTargetChanged
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	content, err := renderCandidateAsCanonical(p, "", nil)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o600)
}

func writeCanonicalUpdate(repoDir string, p ProposalView) error {
	abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
	if p.TargetKind == ProposalTargetRule {
		fm, _, err := parseRule(abs)
		if err != nil {
			return err
		}
		if fm.UUID != p.Target.UUID {
			return ErrTargetChanged
		}
		content, err := renderCandidateAsCanonical(p, fm.Name, &fm.Enabled)
		if err != nil {
			return err
		}
		return os.WriteFile(abs, []byte(content), 0o600)
	}
	fm, _, err := parseEntry(abs)
	if err != nil {
		return err
	}
	if fm.UUID != p.Target.UUID {
		return ErrTargetChanged
	}
	content, err := renderCandidateAsCanonical(p, fm.Name, nil)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o600)
}

func disableCanonicalRule(repoDir string, p ProposalView) error {
	abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
	fm, body, err := parseRule(abs)
	if err != nil {
		return err
	}
	if fm.UUID != p.Target.UUID {
		return ErrTargetChanged
	}
	fm.Enabled = false
	content, err := renderRule(fm, body)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o600)
}

func assertTargetCurrent(repoDir string, p ProposalView) error {
	if err := validateTargetPath(p.TargetKind, p.Target.SourcePath); err != nil {
		return err
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return ErrTargetChanged
		}
		return err
	}
	h, err := hashFileAsGitBlob(abs)
	if err != nil {
		return err
	}
	if h != p.Target.ExpectedBlobHash {
		return ErrTargetChanged
	}
	if p.TargetKind == ProposalTargetRule {
		fm, _, err := parseRule(abs)
		if err != nil {
			return err
		}
		if fm.UUID != p.Target.UUID {
			return ErrTargetChanged
		}
		return nil
	}
	fm, _, err := parseEntry(abs)
	if err != nil {
		return err
	}
	if fm.UUID != p.Target.UUID {
		return ErrTargetChanged
	}
	return nil
}

func renderCandidateAsCanonical(p ProposalView, existingSlug string, existingEnabled *bool) (string, error) {
	if p.Candidate == nil {
		return "", fmt.Errorf("%w: candidate is required", ErrInvalidProposal)
	}
	slug := existingSlug
	if slug == "" {
		slug = p.Candidate.Slug
	}
	if slug == "" {
		return "", fmt.Errorf("%w: slug is required", ErrInvalidProposal)
	}
	if p.TargetKind == ProposalTargetRule {
		enabled := true
		if existingEnabled != nil {
			enabled = *existingEnabled
		}
		if p.Candidate.Enabled != nil {
			enabled = *p.Candidate.Enabled
		}
		applies, err := normalizeAppliesTo(p.Candidate.AppliesTo)
		if err != nil {
			return "", fmt.Errorf("%w: applies_to: %v", ErrInvalidRule, err)
		}
		return renderRule(ruleFrontmatter{
			Name:        slug,
			Title:       p.Candidate.Title,
			Description: p.Candidate.Description,
			UUID:        p.Target.UUID,
			Enabled:     enabled,
			AppliesTo:   applies,
		}, p.Candidate.Body)
	}
	return renderEntry(entryFrontmatter{
		Name:        slug,
		Title:       p.Candidate.Title,
		Description: p.Candidate.Description,
		UUID:        p.Target.UUID,
	}, p.Candidate.Body)
}

func candidateCanonicalMarkdown(repoDir string, p ProposalView) (string, error) {
	switch p.Operation {
	case ProposalDelete:
		return "", nil
	case ProposalDisable:
		abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
		fm, body, err := parseRule(abs)
		if err != nil {
			return "", err
		}
		fm.Enabled = false
		return renderRule(fm, body)
	case ProposalUpdate:
		abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
		if p.TargetKind == ProposalTargetRule {
			fm, _, err := parseRule(abs)
			if err != nil {
				return "", err
			}
			return renderCandidateAsCanonical(p, fm.Name, &fm.Enabled)
		}
		fm, _, err := parseEntry(abs)
		if err != nil {
			return "", err
		}
		return renderCandidateAsCanonical(p, fm.Name, nil)
	default:
		return renderCandidateAsCanonical(p, "", nil)
	}
}

func validateTargetPath(kind ProposalTargetKind, rel string) error {
	if strings.ContainsRune(rel, 0) {
		return fmt.Errorf("%w: target path contains NUL", ErrInvalidProposal)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean != rel || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "\\") {
		return fmt.Errorf("%w: unsafe target path", ErrInvalidProposal)
	}
	wantDir := entriesDir
	if kind == ProposalTargetRule {
		wantDir = rulesDir
	}
	if !strings.HasPrefix(clean, wantDir+"/") || !strings.HasSuffix(clean, ".md") {
		return fmt.Errorf("%w: target path must be under %s/*.md", ErrInvalidProposal, wantDir)
	}
	if strings.Count(clean, "/") != 1 {
		return fmt.Errorf("%w: nested target paths are not allowed", ErrInvalidProposal)
	}
	base := strings.TrimSuffix(filepath.Base(clean), ".md")
	return validateSegment(base)
}

func proposalPayloadHash(in ProposeInput) (string, error) {
	type payload struct {
		Operation    ProposalOperation  `json:"operation"`
		TargetKind   ProposalTargetKind `json:"target_kind"`
		Target       *ProposalTarget    `json:"target,omitempty"`
		Candidate    *ProposalCandidate `json:"candidate,omitempty"`
		Rationale    string             `json:"rationale"`
		EvidenceRefs []string           `json:"evidence_refs,omitempty"`
	}
	raw, err := json.Marshal(payload{
		Operation: in.Operation, TargetKind: in.TargetKind, Target: in.Target,
		Candidate: in.Candidate, Rationale: strings.TrimSpace(in.Rationale),
		EvidenceRefs: append([]string(nil), in.EvidenceRefs...),
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func missingWarningAcknowledgements(warnings, acknowledged []string) []string {
	ack := make(map[string]struct{}, len(acknowledged))
	for _, w := range acknowledged {
		ack[strings.TrimSpace(w)] = struct{}{}
	}
	var missing []string
	for _, w := range warnings {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if _, ok := ack[w]; !ok {
			missing = append(missing, w)
		}
	}
	sort.Strings(missing)
	return missing
}

func hashFileAsGitBlob(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf("blob %d\x00", len(raw))
	sum := sha1Bytes([]byte(header), raw)
	return hex.EncodeToString(sum), nil
}

func sha1Bytes(parts ...[]byte) []byte {
	h := sha1.New()
	for _, p := range parts {
		_, _ = h.Write(p)
	}
	return h.Sum(nil)
}

func diffPreview(current, next string) string {
	if current == next {
		return ""
	}
	const capBytes = 16 * 1024
	if len(current) > capBytes {
		current = current[:capBytes] + "\n[truncated]\n"
	}
	if len(next) > capBytes {
		next = next[:capBytes] + "\n[truncated]\n"
	}
	return "--- current\n+++ candidate\n@@\n" + next
}

func nonNilStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	return append([]string(nil), in...)
}

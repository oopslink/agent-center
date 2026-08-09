package centergit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/cognition/memory/teammemory"
	"github.com/oopslink/agent-center/internal/idgen"
)

const proposalsDir = "proposals"

var stableTeamMemoryAuthor = Author{Name: "agent-center", Email: "team-memory@agent-center.local"}

var teamMemoryReviewLocks sync.Map // map teamID+"\x00"+proposalID -> *sync.Mutex

// TeamMemoryRepository is the Git-backed Repository adapter for ADR-0057. Each
// command uses a fresh clone of the team's bare repo, and the pushed main HEAD is
// the aggregate version.
type TeamMemoryRepository struct {
	host          *Host
	runner        memory.GitRunner
	newProposalID func() string
	now           func() time.Time
	author        Author
}

// TeamMemoryRepositoryOption configures the Git repository adapter.
type TeamMemoryRepositoryOption func(*TeamMemoryRepository)

// WithProposalIDGen injects deterministic proposal ids for tests. The function
// may return either a bare id or a tmprop- prefixed id.
func WithProposalIDGen(fn func() string) TeamMemoryRepositoryOption {
	return func(r *TeamMemoryRepository) { r.newProposalID = fn }
}

// WithRepositoryClock injects deterministic timestamps.
func WithRepositoryClock(fn func() time.Time) TeamMemoryRepositoryOption {
	return func(r *TeamMemoryRepository) { r.now = fn }
}

// WithRepositoryAuthor overrides the stable system git identity.
func WithRepositoryAuthor(a Author) TeamMemoryRepositoryOption {
	return func(r *TeamMemoryRepository) { r.author = a }
}

// NewTeamMemoryRepository wires the Git-backed Team Memory repository.
func NewTeamMemoryRepository(host *Host, runner memory.GitRunner, opts ...TeamMemoryRepositoryOption) *TeamMemoryRepository {
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	r := &TeamMemoryRepository{
		host:          host,
		runner:        runner,
		newProposalID: func() string { return "tmprop-" + idgen.MustNewULID() },
		now:           func() time.Time { return time.Now().UTC() },
		author:        stableTeamMemoryAuthor,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

var _ teammemory.Repository = (*TeamMemoryRepository)(nil)

// Propose writes a pending proposal under proposals/ and pushes it. Concurrent
// unrelated proposals retry from a fresh clone so both commits survive without
// last-write-wins.
func (r *TeamMemoryRepository) Propose(ctx context.Context, teamID string, cmd teammemory.ProposeCommand) (teammemory.Result, error) {
	if err := r.require(); err != nil {
		return teammemory.Result{}, err
	}
	cmd, warnings, payloadHash, err := normalizeProposeCommand(cmd)
	if err != nil {
		return teammemory.Result{}, err
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		var result *teammemory.Result
		work, repoDir, env, err := r.cloneTeam(ctx, teamID)
		if err != nil {
			return teammemory.Result{}, err
		}
		func() {
			defer os.RemoveAll(work)
			existing, head, eErr := findByIdempotency(ctx, r.runner, repoDir, env, cmd.ActorRef, cmd.IdempotencyKey)
			if eErr != nil {
				lastErr = eErr
				return
			}
			if existing != nil {
				if existing.PayloadHash != payloadHash {
					lastErr = teammemory.ErrIdempotencyConflict
					return
				}
				lastErr = nil
				result = &teammemory.Result{
					TeamID:     teamID,
					ProposalID: existing.ProposalID,
					Status:     existing.Status,
					RepoCommit: head,
					SourcePath: existing.SourcePath,
					Warnings:   append([]string(nil), existing.Warnings...),
				}
				return
			}
			proposalID := normalizeProposalID(r.newProposalID())
			p := teammemory.Proposal{
				TeamID:         strings.TrimSpace(teamID),
				ProposalID:     proposalID,
				Operation:      cmd.Operation,
				TargetKind:     cmd.TargetKind,
				Target:         cloneTarget(cmd.Target),
				Candidate:      cloneCandidate(cmd.Candidate),
				Rationale:      strings.TrimSpace(cmd.Rationale),
				EvidenceRefs:   cleanStrings(cmd.EvidenceRefs, 20),
				AuthorRef:      strings.TrimSpace(cmd.ActorRef),
				CreatedAt:      r.now().UTC(),
				IdempotencyKey: strings.TrimSpace(cmd.IdempotencyKey),
				Status:         teammemory.StatusPending,
				Warnings:       append([]string(nil), warnings...),
				PayloadHash:    payloadHash,
			}
			if wErr := writeProposal(repoDir, p); wErr != nil {
				lastErr = wErr
				return
			}
			store := NewStore(repoDir, r.runner, WithHomeOverride(work))
			if cErr := store.Commit(ctx, r.author, fmt.Sprintf("propose team memory change %s", proposalID)); cErr != nil {
				lastErr = mapGitErr(cErr)
				return
			}
			newHead, hErr := revParseHead(ctx, r.runner, repoDir, env)
			if hErr != nil {
				lastErr = hErr
				return
			}
			if pErr := pushMain(ctx, r.runner, repoDir, env); pErr != nil {
				if errors.Is(pErr, ErrPushRetriesExhausted) || isNonFastForward(pErr.Error()) {
					lastErr = pErr
					return
				}
				lastErr = pErr
				return
			}
			lastErr = nil
			result = &teammemory.Result{
				TeamID:     teamID,
				ProposalID: proposalID,
				Status:     teammemory.StatusPending,
				RepoCommit: newHead,
				Warnings:   append([]string(nil), p.Warnings...),
			}
		}()
		if lastErr == nil {
			if result == nil {
				return teammemory.Result{}, teammemory.ErrGitUnavailable
			}
			return *result, nil
		}
		if !isRetryablePushRace(lastErr) {
			return teammemory.Result{}, lastErr
		}
	}
	return teammemory.Result{}, fmt.Errorf("%w: %v", teammemory.ErrTeamMemoryVersionConflict, lastErr)
}

// List returns proposals in the team repo. Default status is pending.
func (r *TeamMemoryRepository) List(ctx context.Context, teamID string, filter teammemory.Filter) (teammemory.ListResult, error) {
	if err := r.require(); err != nil {
		return teammemory.ListResult{}, err
	}
	work, repoDir, env, err := r.cloneTeam(ctx, teamID)
	if err != nil {
		return teammemory.ListResult{}, err
	}
	defer os.RemoveAll(work)
	head, err := revParseHead(ctx, r.runner, repoDir, env)
	if err != nil && !isUnbornHead(err) {
		return teammemory.ListResult{}, err
	}
	props, err := readAllProposals(repoDir)
	if err != nil {
		return teammemory.ListResult{}, err
	}
	statuses := filter.Status
	if len(statuses) == 0 {
		statuses = []teammemory.ProposalStatus{teammemory.StatusPending}
	}
	statusSet := map[teammemory.ProposalStatus]struct{}{}
	for _, st := range statuses {
		statusSet[st] = struct{}{}
	}
	views := make([]teammemory.ProposalView, 0, len(props))
	for _, p := range props {
		if _, ok := statusSet[p.Status]; !ok {
			continue
		}
		if filter.TargetKind != "" && p.TargetKind != filter.TargetKind {
			continue
		}
		view := proposalView(ctx, r.runner, repoDir, env, p, head)
		views = append(views, view)
	}
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i].Proposal, views[j].Proposal
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ProposalID < b.ProposalID
	})
	total := len(views)
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > len(views) {
		offset = len(views)
	}
	end := offset + limit
	if end > len(views) {
		end = len(views)
	}
	return teammemory.ListResult{
		TeamID:     teamID,
		RepoCommit: head,
		Proposals:  views[offset:end],
		Total:      total,
		HasMore:    end < total,
	}, nil
}

// Get reads one proposal by id from one team repo.
func (r *TeamMemoryRepository) Get(ctx context.Context, teamID, proposalID string) (teammemory.ProposalView, error) {
	if err := r.require(); err != nil {
		return teammemory.ProposalView{}, err
	}
	work, repoDir, env, err := r.cloneTeam(ctx, teamID)
	if err != nil {
		return teammemory.ProposalView{}, err
	}
	defer os.RemoveAll(work)
	head, err := revParseHead(ctx, r.runner, repoDir, env)
	if err != nil && !isUnbornHead(err) {
		return teammemory.ProposalView{}, err
	}
	p, err := readProposal(repoDir, normalizeProposalID(proposalID))
	if err != nil {
		return teammemory.ProposalView{}, err
	}
	return proposalView(ctx, r.runner, repoDir, env, p, head), nil
}

// Review promotes or rejects one pending proposal. Promotion updates canonical
// memory, proposal status, and MEMORY.md in one commit; push races return a
// fail-loud version conflict.
func (r *TeamMemoryRepository) Review(ctx context.Context, teamID string, cmd teammemory.ReviewCommand) (teammemory.Result, error) {
	if err := r.require(); err != nil {
		return teammemory.Result{}, err
	}
	cmd.ProposalID = normalizeProposalID(cmd.ProposalID)
	if cmd.ProposalID == "" {
		return teammemory.Result{}, teammemory.Invalid("proposal_id is required")
	}
	if cmd.ExpectedProposalStatus == "" {
		cmd.ExpectedProposalStatus = teammemory.StatusPending
	}
	if cmd.ExpectedProposalStatus != teammemory.StatusPending {
		return teammemory.Result{}, teammemory.Invalid("expected_proposal_status must be pending")
	}
	if strings.TrimSpace(cmd.ExpectedRepoCommit) == "" {
		return teammemory.Result{}, teammemory.Invalid("expected_repo_commit is required")
	}
	if strings.TrimSpace(cmd.Comment) == "" {
		return teammemory.Result{}, teammemory.Invalid("review comment is required")
	}
	switch cmd.Action {
	case teammemory.ActionPromote, teammemory.ActionReject:
	default:
		return teammemory.Result{}, teammemory.Invalid("unknown review action %q", cmd.Action)
	}
	lock := reviewLock(teamID, cmd.ProposalID)
	lock.Lock()
	defer lock.Unlock()

	work, repoDir, env, err := r.cloneTeam(ctx, teamID)
	if err != nil {
		return teammemory.Result{}, err
	}
	defer os.RemoveAll(work)

	oldHead, err := revParseHead(ctx, r.runner, repoDir, env)
	if err != nil {
		return teammemory.Result{}, err
	}
	if oldHead != strings.TrimSpace(cmd.ExpectedRepoCommit) {
		return teammemory.Result{}, teammemory.ErrTeamMemoryVersionConflict
	}
	p, err := readProposal(repoDir, cmd.ProposalID)
	if err != nil {
		return teammemory.Result{}, err
	}
	if p.Status != teammemory.StatusPending {
		return teammemory.Result{}, teammemory.ErrProposalNotPending
	}
	if !warningsAcknowledged(p.Warnings, cmd.AcknowledgeWarnings) && cmd.Action == teammemory.ActionPromote {
		return teammemory.Result{}, teammemory.ErrWarningUnacknowledged
	}

	sourcePath := p.SourcePath
	if cmd.Action == teammemory.ActionPromote {
		sourcePath, err = applyPromotion(ctx, r.runner, repoDir, env, p)
		if err != nil {
			return teammemory.Result{}, err
		}
		p.SourcePath = sourcePath
		if err := NewStore(repoDir, r.runner, WithHomeOverride(work)).RegenerateIndex(); err != nil {
			return teammemory.Result{}, err
		}
		p.Status = teammemory.StatusPromoted
	} else {
		p.Status = teammemory.StatusRejected
	}
	p.ReviewerRef = strings.TrimSpace(cmd.ActorRef)
	p.ReviewComment = strings.TrimSpace(cmd.Comment)
	p.ReviewedAt = r.now().UTC()
	if err := writeProposal(repoDir, p); err != nil {
		return teammemory.Result{}, err
	}
	store := NewStore(repoDir, r.runner, WithHomeOverride(work))
	msg := fmt.Sprintf("%s team memory proposal %s", cmd.Action, p.ProposalID)
	if err := store.Commit(ctx, r.author, msg); err != nil {
		return teammemory.Result{}, mapGitErr(err)
	}
	newHead, err := revParseHead(ctx, r.runner, repoDir, env)
	if err != nil {
		return teammemory.Result{}, err
	}
	if p.Status == teammemory.StatusPromoted {
		p.PromotionCommit = newHead
	}
	if err := pushMainLease(ctx, r.runner, repoDir, env, oldHead); err != nil {
		if isRetryablePushRace(err) {
			return teammemory.Result{}, teammemory.ErrTeamMemoryVersionConflict
		}
		return teammemory.Result{}, err
	}
	effective := ""
	if p.Status == teammemory.StatusPromoted {
		effective = teammemory.EffectiveForNewSessionsAndForks
	}
	return teammemory.Result{
		TeamID:       teamID,
		ProposalID:   p.ProposalID,
		Status:       p.Status,
		RepoCommit:   newHead,
		SourcePath:   sourcePath,
		Warnings:     append([]string(nil), p.Warnings...),
		EffectiveFor: effective,
		OldCommit:    oldHead,
		NewCommit:    newHead,
	}, nil
}

// TrustedBootstrapCommand is the non-MCP/non-Web path used only for team
// instantiate and one-time legacy migration. It writes canonical memory without
// proposals, under the stable system Git author.
type TrustedBootstrapCommand struct {
	ActorRef string
	Source   string
	Entries  []Entry
	Rules    []Rule
}

// Bootstrap applies trusted seed content to canonical memory. It exists so
// Producer/migration do not keep a separate long-term Store bypass.
func (r *TeamMemoryRepository) Bootstrap(ctx context.Context, teamID string, cmd TrustedBootstrapCommand) (int, string, error) {
	n, _, commit, err := r.BootstrapWithPaths(ctx, teamID, cmd)
	return n, commit, err
}

// BootstrapWithPaths is the detailed form used by migrations that must report
// exact rollback paths.
func (r *TeamMemoryRepository) BootstrapWithPaths(ctx context.Context, teamID string, cmd TrustedBootstrapCommand) (int, []string, string, error) {
	if err := r.require(); err != nil {
		return 0, nil, "", err
	}
	work, repoDir, env, err := r.cloneTeam(ctx, teamID)
	if err != nil {
		return 0, nil, "", err
	}
	defer os.RemoveAll(work)
	store := NewStore(repoDir, r.runner, WithHomeOverride(work))
	written := 0
	paths := make([]string, 0, len(cmd.Entries)+len(cmd.Rules))
	for _, e := range cmd.Entries {
		if strings.TrimSpace(e.Slug) == "" || strings.TrimSpace(e.Description) == "" {
			continue
		}
		if path, err := store.WriteEntry(e); err == nil {
			written++
			paths = append(paths, path)
		}
	}
	for _, rule := range cmd.Rules {
		if strings.TrimSpace(rule.Slug) == "" || strings.TrimSpace(rule.Description) == "" {
			continue
		}
		if path, err := store.WriteRule(rule); err == nil {
			written++
			paths = append(paths, path)
		}
	}
	if written == 0 {
		head, _ := revParseHead(ctx, r.runner, repoDir, env)
		return 0, nil, head, nil
	}
	source := strings.TrimSpace(cmd.Source)
	if source == "" {
		source = "trusted-bootstrap"
	}
	if err := store.SyncPush(ctx, "origin", "main", r.author,
		fmt.Sprintf("bootstrap team memory from %s (%d items)", source, written), 3); err != nil {
		return 0, nil, "", mapGitErr(err)
	}
	head, err := revParseHead(ctx, r.runner, repoDir, env)
	if err != nil {
		return written, paths, "", err
	}
	return written, paths, head, nil
}

func (r *TeamMemoryRepository) require() error {
	if r == nil || r.host == nil {
		return fmt.Errorf("%w: team memory git repository not wired", teammemory.ErrGitUnavailable)
	}
	if r.runner == nil {
		r.runner = memory.NewExecGitRunner()
	}
	if r.author.Name == "" || r.author.Email == "" {
		r.author = stableTeamMemoryAuthor
	}
	return nil
}

func (r *TeamMemoryRepository) cloneTeam(ctx context.Context, teamID string) (work, repoDir string, env []string, err error) {
	teamID = strings.TrimSpace(teamID)
	if teamID == "" {
		return "", "", nil, teammemory.Invalid("team_id is required")
	}
	ref := TeamRepo(teamID)
	if err := r.host.EnsureRepo(ctx, ref); err != nil {
		return "", "", nil, mapGitErr(err)
	}
	bareDir, err := r.host.RepoDir(ref)
	if err != nil {
		return "", "", nil, mapGitErr(err)
	}
	work, err = os.MkdirTemp("", "team-memory-repo-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: mktemp: %v", teammemory.ErrGitUnavailable, err)
	}
	env = baseGitEnv(work, r.author.Name, r.author.Email)
	repoDir = filepath.Join(work, "repo")
	if out, cErr := r.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		_ = os.RemoveAll(work)
		return "", "", nil, fmt.Errorf("%w: clone %s: %v: %s", teammemory.ErrGitUnavailable, bareDir, cErr, out)
	}
	return work, repoDir, env, nil
}

func normalizeProposeCommand(cmd teammemory.ProposeCommand) (teammemory.ProposeCommand, []string, string, error) {
	cmd.ActorRef = strings.TrimSpace(cmd.ActorRef)
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	cmd.Rationale = strings.TrimSpace(cmd.Rationale)
	cmd.EvidenceRefs = cleanStrings(cmd.EvidenceRefs, 20)
	if cmd.ActorRef == "" {
		return cmd, nil, "", teammemory.Invalid("actor_ref is required")
	}
	if cmd.IdempotencyKey == "" {
		return cmd, nil, "", teammemory.Invalid("idempotency_key is required")
	}
	if cmd.Rationale == "" {
		return cmd, nil, "", teammemory.Invalid("rationale is required")
	}
	switch cmd.Operation {
	case teammemory.OperationAdd:
		if cmd.Target != nil {
			return cmd, nil, "", teammemory.Invalid("add must not include target")
		}
		if cmd.Candidate == nil {
			return cmd, nil, "", teammemory.Invalid("add requires candidate")
		}
	case teammemory.OperationUpdate:
		if cmd.Target == nil || cmd.Candidate == nil {
			return cmd, nil, "", teammemory.Invalid("update requires target and candidate")
		}
	case teammemory.OperationDisable:
		if cmd.TargetKind != teammemory.TargetRule {
			return cmd, nil, "", teammemory.Invalid("disable is only valid for rules")
		}
		if cmd.Target == nil {
			return cmd, nil, "", teammemory.Invalid("disable requires target")
		}
	case teammemory.OperationDelete:
		if cmd.Target == nil {
			return cmd, nil, "", teammemory.Invalid("delete requires target")
		}
	default:
		return cmd, nil, "", teammemory.Invalid("unknown operation %q", cmd.Operation)
	}
	switch cmd.TargetKind {
	case teammemory.TargetEntry, teammemory.TargetRule:
	default:
		return cmd, nil, "", teammemory.Invalid("unknown target_kind %q", cmd.TargetKind)
	}
	if cmd.Target != nil {
		t := cloneTarget(cmd.Target)
		if err := validateTargetRef(cmd.TargetKind, t); err != nil {
			return cmd, nil, "", err
		}
		cmd.Target = t
	}
	if cmd.Candidate != nil {
		c := cloneCandidate(cmd.Candidate)
		if err := validateCandidate(cmd.Operation, cmd.TargetKind, c); err != nil {
			return cmd, nil, "", err
		}
		cmd.Candidate = c
	}
	if err := rejectSecretish(cmd); err != nil {
		return cmd, nil, "", err
	}
	warnings := warningsFor(cmd)
	return cmd, warnings, payloadHash(cmd), nil
}

func validateCandidate(op teammemory.Operation, kind teammemory.TargetKind, c *teammemory.Candidate) error {
	if c == nil {
		return nil
	}
	c.Slug = strings.TrimSpace(c.Slug)
	c.Title = strings.TrimSpace(c.Title)
	c.Description = strings.TrimSpace(c.Description)
	c.Type = strings.TrimSpace(c.Type)
	c.AppliesTo = cleanStrings(c.AppliesTo, 8)
	if op == teammemory.OperationAdd && c.Slug == "" {
		return teammemory.Invalid("add candidate slug is required")
	}
	if op == teammemory.OperationAdd {
		if err := validateSegment(c.Slug); err != nil {
			return fmt.Errorf("%w: slug: %v", teammemory.ErrInvalidCandidate, err)
		}
	}
	if c.Description == "" {
		return teammemory.Invalid("candidate description is required")
	}
	if strings.ContainsRune(c.Body, 0) || strings.ContainsRune(c.Description, 0) || strings.ContainsRune(c.Title, 0) {
		return teammemory.Invalid("candidate contains NUL")
	}
	if len(c.Body) > 64*1024 || len(c.Description) > 4096 || len(c.Title) > 512 {
		return teammemory.Invalid("candidate payload too large")
	}
	if kind == teammemory.TargetRule {
		if _, err := normalizeAppliesTo(c.AppliesTo); err != nil {
			return fmt.Errorf("%w: applies_to: %v", teammemory.ErrInvalidCandidate, err)
		}
	}
	return nil
}

func validateTargetRef(kind teammemory.TargetKind, t *teammemory.TargetRef) error {
	if t == nil {
		return nil
	}
	t.SourcePath = strings.TrimSpace(filepath.ToSlash(t.SourcePath))
	t.UUID = strings.TrimSpace(t.UUID)
	t.ExpectedBlobHash = strings.TrimSpace(t.ExpectedBlobHash)
	if t.SourcePath == "" || t.UUID == "" || t.ExpectedBlobHash == "" {
		return teammemory.Invalid("target source_path, uuid, expected_blob_hash are required")
	}
	dir, err := dirForKind(kind)
	if err != nil {
		return err
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(t.SourcePath)))
	if clean != t.SourcePath || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") ||
		!strings.HasPrefix(clean, dir+"/") || !strings.HasSuffix(clean, ".md") {
		return teammemory.Invalid("unsafe target source_path")
	}
	return nil
}

func dirForKind(kind teammemory.TargetKind) (string, error) {
	switch kind {
	case teammemory.TargetEntry:
		return entriesDir, nil
	case teammemory.TargetRule:
		return rulesDir, nil
	default:
		return "", teammemory.Invalid("unknown target_kind %q", kind)
	}
}

func payloadHash(cmd teammemory.ProposeCommand) string {
	type payload struct {
		Operation    teammemory.Operation  `json:"operation"`
		TargetKind   teammemory.TargetKind `json:"target_kind"`
		Target       *teammemory.TargetRef `json:"target,omitempty"`
		Candidate    *teammemory.Candidate `json:"candidate,omitempty"`
		Rationale    string                `json:"rationale"`
		EvidenceRefs []string              `json:"evidence_refs,omitempty"`
	}
	raw, _ := json.Marshal(payload{
		Operation: cmd.Operation, TargetKind: cmd.TargetKind, Target: cmd.Target,
		Candidate: cmd.Candidate, Rationale: cmd.Rationale, EvidenceRefs: cmd.EvidenceRefs,
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func rejectSecretish(cmd teammemory.ProposeCommand) error {
	var parts []string
	parts = append(parts, cmd.Rationale)
	parts = append(parts, cmd.EvidenceRefs...)
	if cmd.Candidate != nil {
		parts = append(parts, cmd.Candidate.Title, cmd.Candidate.Description, cmd.Candidate.Body)
	}
	if cmd.Target != nil {
		parts = append(parts, cmd.Target.SourcePath, cmd.Target.UUID)
	}
	text := strings.ToLower(strings.Join(parts, "\n"))
	for _, marker := range []string{
		"-----begin private key-----",
		"-----begin rsa private key-----",
		"agent_center_admin_token",
		"admin_bootstrap_token",
		"worker_token",
		"acat_",
	} {
		if strings.Contains(text, marker) {
			return teammemory.ErrSecretDetected
		}
	}
	return nil
}

func warningsFor(cmd teammemory.ProposeCommand) []string {
	var text string
	if cmd.Candidate != nil {
		text = cmd.Candidate.Body + "\n" + cmd.Candidate.Description
	}
	var warnings []string
	lo := strings.ToLower(text)
	if strings.Contains(lo, "http://") || strings.Contains(lo, "https://") || strings.Contains(lo, "git@") {
		warnings = append(warnings, "contains_external_reference")
	}
	if strings.Contains(text, "/Users/") || strings.Contains(text, "/home/") || strings.Contains(text, `C:\`) {
		warnings = append(warnings, "contains_local_path")
	}
	return warnings
}

func applyPromotion(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, p teammemory.Proposal) (string, error) {
	switch p.Operation {
	case teammemory.OperationAdd:
		return applyAdd(repoDir, p)
	case teammemory.OperationUpdate:
		return applyUpdate(ctx, runner, repoDir, env, p)
	case teammemory.OperationDisable:
		return applyDisable(ctx, runner, repoDir, env, p)
	case teammemory.OperationDelete:
		return applyDelete(ctx, runner, repoDir, env, p)
	default:
		return "", teammemory.Invalid("unknown operation %q", p.Operation)
	}
}

func applyAdd(repoDir string, p teammemory.Proposal) (string, error) {
	if p.Candidate == nil {
		return "", teammemory.Invalid("add requires candidate")
	}
	uuid := proposalUUID(p.ProposalID)
	store := NewStore(repoDir, nil, WithIDGen(func() string { return uuid }))
	switch p.TargetKind {
	case teammemory.TargetEntry:
		return store.WriteEntry(Entry{
			Slug: p.Candidate.Slug, Title: p.Candidate.Title, Description: p.Candidate.Description,
			Body: p.Candidate.Body, Type: p.Candidate.Type,
		})
	case teammemory.TargetRule:
		enabled := true
		if p.Candidate.Enabled != nil {
			enabled = *p.Candidate.Enabled
		}
		return store.WriteRule(Rule{
			Slug: p.Candidate.Slug, Title: p.Candidate.Title, Description: p.Candidate.Description,
			Body: p.Candidate.Body, Enabled: enabled, AppliesTo: p.Candidate.AppliesTo,
		})
	default:
		return "", teammemory.Invalid("unknown target_kind %q", p.TargetKind)
	}
}

func applyUpdate(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, p teammemory.Proposal) (string, error) {
	if p.Target == nil || p.Candidate == nil {
		return "", teammemory.Invalid("update requires target and candidate")
	}
	if err := verifyTarget(ctx, runner, repoDir, env, p.TargetKind, p.Target); err != nil {
		return "", err
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
	switch p.TargetKind {
	case teammemory.TargetEntry:
		fm, _, err := parseEntry(abs)
		if err != nil {
			return "", err
		}
		body, err := renderEntry(entryFrontmatter{
			Name: fm.Name, Title: p.Candidate.Title, Description: p.Candidate.Description,
			UUID: fm.UUID, Type: p.Candidate.Type,
		}, p.Candidate.Body)
		if err != nil {
			return "", err
		}
		return p.Target.SourcePath, os.WriteFile(abs, []byte(body), 0o600)
	case teammemory.TargetRule:
		fm, _, err := parseRule(abs)
		if err != nil {
			return "", err
		}
		enabled := fm.Enabled
		if p.Candidate.Enabled != nil {
			enabled = *p.Candidate.Enabled
		}
		applies := p.Candidate.AppliesTo
		if len(applies) == 0 {
			applies = fm.AppliesTo
		}
		applies, err = normalizeAppliesTo(applies)
		if err != nil {
			return "", fmt.Errorf("%w: applies_to: %v", teammemory.ErrInvalidCandidate, err)
		}
		body, err := renderRule(ruleFrontmatter{
			Name: fm.Name, Title: p.Candidate.Title, Description: p.Candidate.Description,
			UUID: fm.UUID, Enabled: enabled, AppliesTo: applies,
		}, p.Candidate.Body)
		if err != nil {
			return "", err
		}
		return p.Target.SourcePath, os.WriteFile(abs, []byte(body), 0o600)
	default:
		return "", teammemory.Invalid("unknown target_kind %q", p.TargetKind)
	}
}

func applyDisable(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, p teammemory.Proposal) (string, error) {
	if p.Target == nil || p.TargetKind != teammemory.TargetRule {
		return "", teammemory.Invalid("disable requires rule target")
	}
	if err := verifyTarget(ctx, runner, repoDir, env, p.TargetKind, p.Target); err != nil {
		return "", err
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
	fm, bodyText, err := parseRule(abs)
	if err != nil {
		return "", err
	}
	fm.Enabled = false
	rendered, err := renderRule(fm, bodyText)
	if err != nil {
		return "", err
	}
	return p.Target.SourcePath, os.WriteFile(abs, []byte(rendered), 0o600)
}

func applyDelete(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, p teammemory.Proposal) (string, error) {
	if p.Target == nil {
		return "", teammemory.Invalid("delete requires target")
	}
	if err := verifyTarget(ctx, runner, repoDir, env, p.TargetKind, p.Target); err != nil {
		return "", err
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(p.Target.SourcePath))
	return p.Target.SourcePath, os.Remove(abs)
}

func verifyTarget(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, kind teammemory.TargetKind, target *teammemory.TargetRef) error {
	if err := validateTargetRef(kind, target); err != nil {
		return err
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(target.SourcePath))
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return teammemory.ErrTargetChanged
		}
		return err
	}
	hash, err := blobHash(ctx, runner, repoDir, env, target.SourcePath)
	if err != nil {
		return err
	}
	if hash != target.ExpectedBlobHash {
		return teammemory.ErrTargetChanged
	}
	switch kind {
	case teammemory.TargetEntry:
		fm, _, err := parseEntry(abs)
		if err != nil {
			return err
		}
		if fm.UUID != target.UUID {
			return teammemory.ErrTargetChanged
		}
	case teammemory.TargetRule:
		fm, _, err := parseRule(abs)
		if err != nil {
			return err
		}
		if fm.UUID != target.UUID {
			return teammemory.ErrTargetChanged
		}
	}
	return nil
}

func blobHash(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, rel string) (string, error) {
	out, err := runner.Run(ctx, repoDir, env, "hash-object", "--", rel)
	if err != nil {
		return "", fmt.Errorf("%w: hash-object %s: %v: %s", teammemory.ErrGitUnavailable, rel, err, out)
	}
	return strings.TrimSpace(out), nil
}

func proposalView(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, p teammemory.Proposal, head string) teammemory.ProposalView {
	view := teammemory.ProposalView{Proposal: p, RepoCommit: head}
	if view.Proposal.Status == teammemory.StatusPromoted && view.Proposal.PromotionCommit == "" {
		view.Proposal.PromotionCommit = head
	}
	if p.Target != nil {
		if hash, err := blobHash(ctx, runner, repoDir, env, p.Target.SourcePath); err == nil {
			view.CurrentTargetBlobHash = hash
		}
	}
	view.DiffPreview = diffPreview(p)
	return view
}

func diffPreview(p teammemory.Proposal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", p.Operation, p.TargetKind)
	if p.Target != nil {
		fmt.Fprintf(&b, " %s", p.Target.SourcePath)
	}
	if p.Candidate != nil {
		fmt.Fprintf(&b, "\n\n%s\n\n%s", p.Candidate.Description, p.Candidate.Body)
	}
	return strings.TrimSpace(b.String())
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
	ReviewerRef      string   `yaml:"reviewer_ref,omitempty"`
	ReviewComment    string   `yaml:"review_comment,omitempty"`
	ReviewedAt       string   `yaml:"reviewed_at,omitempty"`
	PromotionCommit  string   `yaml:"promotion_commit,omitempty"`
	SourcePath       string   `yaml:"source_path,omitempty"`
	Supersedes       string   `yaml:"supersedes,omitempty"`
	PayloadHash      string   `yaml:"payload_hash"`
}

func writeProposal(repoDir string, p teammemory.Proposal) error {
	if p.ProposalID == "" {
		return teammemory.Invalid("proposal_id is required")
	}
	rel := filepath.ToSlash(filepath.Join(proposalsDir, p.ProposalID+".md"))
	abs := filepath.Join(repoDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	body, err := renderProposal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(body), 0o600)
}

func renderProposal(p teammemory.Proposal) (string, error) {
	fm := proposalFrontmatter{
		ProposalID: p.ProposalID, Operation: string(p.Operation), TargetKind: string(p.TargetKind),
		AuthorRef: p.AuthorRef, CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339Nano),
		IdempotencyKey: p.IdempotencyKey, Status: string(p.Status), Rationale: p.Rationale,
		EvidenceRefs: p.EvidenceRefs, Warnings: p.Warnings, ReviewerRef: p.ReviewerRef,
		ReviewComment: p.ReviewComment, PromotionCommit: p.PromotionCommit,
		SourcePath: p.SourcePath, Supersedes: p.Supersedes, PayloadHash: p.PayloadHash,
	}
	if p.Target != nil {
		fm.TargetSourcePath = p.Target.SourcePath
		fm.TargetUUID = p.Target.UUID
		fm.ExpectedBlobHash = p.Target.ExpectedBlobHash
	}
	if !p.ReviewedAt.IsZero() {
		fm.ReviewedAt = p.ReviewedAt.UTC().Format(time.RFC3339Nano)
	}
	y, err := yaml.Marshal(fm)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(y)
	b.WriteString("---\n\n")
	if p.Candidate != nil {
		raw, err := json.MarshalIndent(p.Candidate, "", "  ")
		if err != nil {
			return "", err
		}
		b.WriteString("```json team_memory_candidate\n")
		b.Write(raw)
		b.WriteString("\n```\n")
	}
	return b.String(), nil
}

func readProposal(repoDir, proposalID string) (teammemory.Proposal, error) {
	if proposalID == "" {
		return teammemory.Proposal{}, teammemory.ErrProposalNotFound
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(filepath.ToSlash(filepath.Join(proposalsDir, proposalID+".md"))))
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return teammemory.Proposal{}, teammemory.ErrProposalNotFound
		}
		return teammemory.Proposal{}, err
	}
	return parseProposalFile(abs)
}

func readAllProposals(repoDir string) ([]teammemory.Proposal, error) {
	dir := filepath.Join(repoDir, proposalsDir)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	props := make([]teammemory.Proposal, 0, len(ents))
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		p, err := parseProposalFile(filepath.Join(dir, de.Name()))
		if err != nil {
			return nil, fmt.Errorf("parse proposal %s: %w", de.Name(), err)
		}
		props = append(props, p)
	}
	return props, nil
}

func parseProposalFile(path string) (teammemory.Proposal, error) {
	var fm proposalFrontmatter
	body, err := parseMarkdown(path, &fm)
	if err != nil {
		return teammemory.Proposal{}, err
	}
	p := teammemory.Proposal{
		ProposalID: fm.ProposalID, Operation: teammemory.Operation(fm.Operation),
		TargetKind: teammemory.TargetKind(fm.TargetKind), AuthorRef: fm.AuthorRef,
		IdempotencyKey: fm.IdempotencyKey, Status: teammemory.ProposalStatus(fm.Status),
		Rationale: fm.Rationale, EvidenceRefs: append([]string(nil), fm.EvidenceRefs...),
		Warnings: append([]string(nil), fm.Warnings...), ReviewerRef: fm.ReviewerRef,
		ReviewComment: fm.ReviewComment, PromotionCommit: fm.PromotionCommit,
		SourcePath: fm.SourcePath, Supersedes: fm.Supersedes, PayloadHash: fm.PayloadHash,
	}
	if fm.TargetSourcePath != "" || fm.TargetUUID != "" || fm.ExpectedBlobHash != "" {
		p.Target = &teammemory.TargetRef{
			SourcePath: fm.TargetSourcePath, UUID: fm.TargetUUID, ExpectedBlobHash: fm.ExpectedBlobHash,
		}
	}
	if fm.CreatedAt != "" {
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, fm.CreatedAt)
	}
	if fm.ReviewedAt != "" {
		p.ReviewedAt, _ = time.Parse(time.RFC3339Nano, fm.ReviewedAt)
	}
	candidate, err := parseProposalCandidate(body)
	if err != nil {
		return teammemory.Proposal{}, err
	}
	p.Candidate = candidate
	return p, nil
}

func parseProposalCandidate(body string) (*teammemory.Candidate, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, nil
	}
	start := strings.Index(body, "```json team_memory_candidate")
	if start < 0 {
		return nil, nil
	}
	rest := body[start+len("```json team_memory_candidate"):]
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n```")
	if end < 0 {
		return nil, teammemory.Invalid("unterminated candidate section")
	}
	var c teammemory.Candidate
	if err := json.Unmarshal([]byte(rest[:end]), &c); err != nil {
		return nil, fmt.Errorf("%w: candidate json: %v", teammemory.ErrInvalidCandidate, err)
	}
	return &c, nil
}

func findByIdempotency(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, authorRef, key string) (*teammemory.Proposal, string, error) {
	head, err := revParseHead(ctx, runner, repoDir, env)
	if err != nil && !isUnbornHead(err) {
		return nil, "", err
	}
	props, err := readAllProposals(repoDir)
	if err != nil {
		return nil, "", err
	}
	for _, p := range props {
		if p.AuthorRef == authorRef && p.IdempotencyKey == key {
			cp := p
			return &cp, head, nil
		}
	}
	return nil, head, nil
}

func revParseHead(ctx context.Context, runner memory.GitRunner, repoDir string, env []string) (string, error) {
	out, err := runner.Run(ctx, repoDir, env, "rev-parse", "HEAD")
	if err != nil {
		if strings.Contains(out, "ambiguous argument") || strings.Contains(out, "unknown revision") ||
			strings.Contains(out, "does not have any commits yet") {
			return "", fmt.Errorf("%w: unborn HEAD", errUnbornHead)
		}
		return "", fmt.Errorf("%w: rev-parse HEAD: %v: %s", teammemory.ErrGitUnavailable, err, out)
	}
	return strings.TrimSpace(out), nil
}

var errUnbornHead = errors.New("unborn head")

func isUnbornHead(err error) bool { return errors.Is(err, errUnbornHead) }

func pushMain(ctx context.Context, runner memory.GitRunner, repoDir string, env []string) error {
	out, err := runner.Run(ctx, repoDir, env, "push", "origin", "HEAD:main")
	if err == nil {
		return nil
	}
	if isNonFastForward(out) {
		return fmt.Errorf("%w: %s", ErrPushRetriesExhausted, out)
	}
	return fmt.Errorf("%w: push: %v: %s", teammemory.ErrGitUnavailable, err, out)
}

func pushMainLease(ctx context.Context, runner memory.GitRunner, repoDir string, env []string, expectedHead string) error {
	expectedHead = strings.TrimSpace(expectedHead)
	args := []string{"push"}
	if expectedHead != "" {
		args = append(args, "--force-with-lease=refs/heads/main:"+expectedHead)
	}
	args = append(args, "origin", "HEAD:main")
	out, err := runner.Run(ctx, repoDir, env, args...)
	if err == nil {
		return nil
	}
	lo := strings.ToLower(out)
	if isNonFastForward(out) || strings.Contains(lo, "stale info") || strings.Contains(lo, "rejected") {
		return fmt.Errorf("%w: %s", ErrPushRetriesExhausted, out)
	}
	return fmt.Errorf("%w: push: %v: %s", teammemory.ErrGitUnavailable, err, out)
}

func isRetryablePushRace(err error) bool {
	return errors.Is(err, ErrPushRetriesExhausted) || strings.Contains(strings.ToLower(err.Error()), "non-fast-forward") ||
		strings.Contains(strings.ToLower(err.Error()), "fetch first") || strings.Contains(strings.ToLower(err.Error()), "updates were rejected")
}

func mapGitErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, teammemory.ErrInvalidCandidate) || errors.Is(err, teammemory.ErrTargetChanged) ||
		errors.Is(err, teammemory.ErrGitUnavailable) || errors.Is(err, ErrGitOpFailed) {
		if errors.Is(err, ErrGitOpFailed) {
			return fmt.Errorf("%w: %v", teammemory.ErrGitUnavailable, err)
		}
		return err
	}
	if errors.Is(err, ErrInvalidEntry) || errors.Is(err, ErrInvalidRule) {
		return fmt.Errorf("%w: %v", teammemory.ErrInvalidCandidate, err)
	}
	return err
}

func cleanStrings(in []string, max int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" || strings.ContainsRune(s, 0) {
			continue
		}
		if len(s) > 512 {
			s = s[:512]
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if max > 0 && len(out) == max {
			break
		}
	}
	return out
}

func cloneTarget(t *teammemory.TargetRef) *teammemory.TargetRef {
	if t == nil {
		return nil
	}
	return &teammemory.TargetRef{
		SourcePath:       strings.TrimSpace(filepath.ToSlash(t.SourcePath)),
		UUID:             strings.TrimSpace(t.UUID),
		ExpectedBlobHash: strings.TrimSpace(t.ExpectedBlobHash),
	}
}

func cloneCandidate(c *teammemory.Candidate) *teammemory.Candidate {
	if c == nil {
		return nil
	}
	out := *c
	out.Slug = strings.TrimSpace(out.Slug)
	out.Title = strings.TrimSpace(out.Title)
	out.Description = strings.TrimSpace(out.Description)
	out.Type = strings.TrimSpace(out.Type)
	out.AppliesTo = cleanStrings(out.AppliesTo, 8)
	return &out
}

func normalizeProposalID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "tmprop-") {
		return id
	}
	return "tmprop-" + id
}

func proposalUUID(proposalID string) string {
	u := strings.TrimPrefix(normalizeProposalID(proposalID), "tmprop-")
	u = strings.TrimSpace(u)
	if u == "" {
		return idgen.MustNewULID()
	}
	return u
}

func warningsAcknowledged(warnings, ack []string) bool {
	if len(warnings) == 0 {
		return true
	}
	seen := map[string]struct{}{}
	for _, a := range ack {
		seen[strings.TrimSpace(a)] = struct{}{}
	}
	for _, w := range warnings {
		if _, ok := seen[strings.TrimSpace(w)]; !ok {
			return false
		}
	}
	return true
}

func reviewLock(teamID, proposalID string) *sync.Mutex {
	key := strings.TrimSpace(teamID) + "\x00" + strings.TrimSpace(proposalID)
	v, _ := teamMemoryReviewLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

package centergit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/idgen"
)

const (
	legacyProposalsDir = "proposals"
	settingsDir        = "settings"
	settingsFile       = "settings/team-memory.yml"

	ProposalStatusPending  = "pending"
	ProposalStatusPromoted = "promoted"
	ProposalStatusRejected = "rejected"

	MemoryItemEntry    = "entry"
	MemoryItemRule     = "rule"
	MemoryItemProposal = "proposal"
	MemoryItemIndex    = "index"

	TeamMemoryEffectHint = "New sessions and fresh forks load promoted team memory from the current commit; in-flight sessions keep their snapshotted rules until restarted or forked again."
)

var (
	ErrTeamMemoryNotConfigured      = errors.New("team memory: service not configured")
	ErrTeamMemoryNotFound           = errors.New("team memory: not found")
	ErrTeamMemoryInvalidProposal    = errors.New("team memory: invalid proposal")
	ErrTeamMemoryProposalNotPending = errors.New("team memory: proposal is not pending")
	ErrTeamMemoryWarningAckRequired = errors.New("team memory: warning acknowledgement required")
	ErrTeamMemoryAgentSelfGrant     = errors.New("team memory: agents cannot self-grant curator access")
	ErrTeamMemoryInvalidSettings    = errors.New("team memory: invalid settings")
)

type TeamMemoryService struct {
	host   *Host
	runner memory.GitRunner
	newID  func() string
	now    func() time.Time
}

type TeamMemoryServiceOption func(*TeamMemoryService)

func WithTeamMemoryIDGen(fn func() string) TeamMemoryServiceOption {
	return func(s *TeamMemoryService) {
		if fn != nil {
			s.newID = fn
		}
	}
}

func WithTeamMemoryClock(fn func() time.Time) TeamMemoryServiceOption {
	return func(s *TeamMemoryService) {
		if fn != nil {
			s.now = fn
		}
	}
}

func NewTeamMemoryService(host *Host, runner memory.GitRunner, opts ...TeamMemoryServiceOption) *TeamMemoryService {
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	s := &TeamMemoryService{
		host:   host,
		runner: runner,
		newID:  idgen.MustNewULID,
		now:    func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *TeamMemoryService) Configured() bool {
	return s != nil && s.host != nil
}

type TeamMemorySnapshot struct {
	TeamID           string
	Commit           string
	Entries          []MemoryItem
	Rules            []MemoryItem
	Proposals        []MemoryProposal
	Skipped          []string
	EffectHint       string
	RefreshSemantics string
}

type MemoryItem struct {
	Kind        string
	Slug        string
	Path        string
	Title       string
	Description string
	Scope       string
	UUID        string
	Commit      string
	Enabled     bool
	AppliesTo   []string
}

type MemoryDocument struct {
	Kind        string
	Slug        string
	Path        string
	Title       string
	Frontmatter string
	Body        string
	UUID        string
	Commit      string
	Proposal    *MemoryProposal
}

type MemoryProposal struct {
	ID                  string
	UUID                string
	Status              string
	TargetKind          string
	Slug                string
	Title               string
	Description         string
	Body                string
	AuthorRef           string
	CreatedAt           string
	UpdatedAt           string
	SourcePath          string
	PromotedPath        string
	TargetUUID          string
	Commit              string
	Enabled             bool
	AppliesTo           []string
	WarningAcknowledged bool
	RejectReason        string
	Diff                string
}

type CreateMemoryProposalInput struct {
	TargetKind          string
	Slug                string
	Title               string
	Description         string
	Body                string
	Enabled             bool
	AppliesTo           []string
	WarningAcknowledged bool
	AuthorRef           string
	Author              Author
}

type PromoteMemoryProposalInput struct {
	ProposalID          string
	WarningAcknowledged bool
	ActorRef            string
	Author              Author
}

type RejectMemoryProposalInput struct {
	ProposalID string
	Reason     string
	ActorRef   string
	Author     Author
}

type TeamMemorySettings struct {
	CuratorAgents []string `json:"curator_agents"`
	Policy        string   `json:"policy"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	UpdatedBy     string   `json:"updated_by,omitempty"`
	Commit        string   `json:"commit,omitempty"`
	EffectHint    string   `json:"effect_hint,omitempty"`
}

type UpdateTeamMemorySettingsInput struct {
	CuratorAgents []string
	Policy        string
	ActorRef      string
	ActorKind     string
	Author        Author
}

type legacyProposalFrontmatter struct {
	ID                  string   `yaml:"id"`
	UUID                string   `yaml:"uuid"`
	Status              string   `yaml:"status"`
	TargetKind          string   `yaml:"target_kind"`
	Slug                string   `yaml:"slug"`
	Title               string   `yaml:"title,omitempty"`
	Description         string   `yaml:"description"`
	AuthorRef           string   `yaml:"author_ref"`
	CreatedAt           string   `yaml:"created_at"`
	UpdatedAt           string   `yaml:"updated_at,omitempty"`
	PromotedPath        string   `yaml:"promoted_path,omitempty"`
	TargetUUID          string   `yaml:"target_uuid,omitempty"`
	Enabled             bool     `yaml:"enabled,omitempty"`
	AppliesTo           []string `yaml:"applies_to,omitempty"`
	WarningAcknowledged bool     `yaml:"warning_acknowledged"`
	RejectReason        string   `yaml:"reject_reason,omitempty"`
}

type settingsFileModel struct {
	CuratorAgents []string `yaml:"curator_agents"`
	Policy        string   `yaml:"policy"`
	UpdatedAt     string   `yaml:"updated_at,omitempty"`
	UpdatedBy     string   `yaml:"updated_by,omitempty"`
}

func (s *TeamMemoryService) List(ctx context.Context, teamID string) (TeamMemorySnapshot, error) {
	snap := TeamMemorySnapshot{
		TeamID:           teamID,
		EffectHint:       TeamMemoryEffectHint,
		RefreshSemantics: RuleRefreshSemantics,
	}
	if !s.Configured() {
		return snap, ErrTeamMemoryNotConfigured
	}
	wc, err := s.cloneForRead(ctx, teamID)
	if err != nil {
		return snap, err
	}
	if wc == nil {
		return snap, nil
	}
	defer wc.cleanup()
	snap.Commit = wc.commit

	store := NewStore(wc.repoDir, s.runner, WithHomeOverride(wc.workDir))
	entries, skipped, err := store.ReadEntries()
	if err != nil {
		return snap, err
	}
	snap.Skipped = append(snap.Skipped, skipped...)
	for _, e := range entries {
		snap.Entries = append(snap.Entries, MemoryItem{
			Kind: MemoryItemEntry, Slug: e.Slug, Path: e.SourcePath, Title: e.Title,
			Description: e.Description, Scope: e.Type, UUID: e.UUID, Commit: wc.commit,
		})
	}
	rules, skippedRules, err := store.ReadRules()
	if err != nil {
		return snap, err
	}
	snap.Skipped = append(snap.Skipped, skippedRules...)
	for _, r := range rules {
		snap.Rules = append(snap.Rules, MemoryItem{
			Kind: MemoryItemRule, Slug: r.Slug, Path: r.SourcePath, Title: r.Title,
			Description: r.Description, UUID: r.UUID, Commit: wc.commit,
			Enabled: r.Enabled, AppliesTo: append([]string(nil), r.AppliesTo...),
		})
	}
	proposals, skippedProposals, err := readProposals(wc.repoDir, wc.commit)
	if err != nil {
		return snap, err
	}
	snap.Skipped = append(snap.Skipped, skippedProposals...)
	snap.Proposals = proposals
	sort.Strings(snap.Skipped)
	return snap, nil
}

func (s *TeamMemoryService) GetDocument(ctx context.Context, teamID, kind, slug string) (MemoryDocument, error) {
	if !s.Configured() {
		return MemoryDocument{}, ErrTeamMemoryNotConfigured
	}
	wc, err := s.cloneForRead(ctx, teamID)
	if err != nil {
		return MemoryDocument{}, err
	}
	if wc == nil {
		return MemoryDocument{}, ErrTeamMemoryNotFound
	}
	defer wc.cleanup()

	kind = normalizeMemoryKind(kind)
	if kind == MemoryItemProposal {
		p, err := readProposalByID(wc.repoDir, wc.commit, slug)
		if err != nil {
			return MemoryDocument{}, err
		}
		return proposalDocument(p), nil
	}
	if slug == indexFile || kind == MemoryItemIndex {
		body, err := os.ReadFile(filepath.Join(wc.repoDir, indexFile))
		if err != nil {
			if os.IsNotExist(err) {
				return MemoryDocument{}, ErrTeamMemoryNotFound
			}
			return MemoryDocument{}, err
		}
		return MemoryDocument{
			Kind: MemoryItemIndex, Slug: indexFile, Path: indexFile, Title: indexFile,
			Body: string(body), Commit: wc.commit,
		}, nil
	}

	if kind == "" || kind == MemoryItemEntry {
		if doc, ok, err := readEntryDoc(wc.repoDir, wc.commit, slug); err != nil || ok {
			return doc, err
		}
	}
	if kind == "" || kind == MemoryItemRule {
		if doc, ok, err := readRuleDoc(wc.repoDir, wc.commit, slug); err != nil || ok {
			return doc, err
		}
	}
	return MemoryDocument{}, ErrTeamMemoryNotFound
}

func (s *TeamMemoryService) CreateProposal(ctx context.Context, teamID string, in CreateMemoryProposalInput) (MemoryProposal, error) {
	if !s.Configured() {
		return MemoryProposal{}, ErrTeamMemoryNotConfigured
	}
	targetKind := normalizeMemoryKind(in.TargetKind)
	if targetKind != MemoryItemEntry && targetKind != MemoryItemRule {
		return MemoryProposal{}, fmt.Errorf("%w: target_kind must be entry or rule", ErrTeamMemoryInvalidProposal)
	}
	if !in.WarningAcknowledged {
		return MemoryProposal{}, ErrTeamMemoryWarningAckRequired
	}
	if err := validateSegment(in.Slug); err != nil {
		return MemoryProposal{}, fmt.Errorf("%w: slug: %v", ErrTeamMemoryInvalidProposal, err)
	}
	if strings.TrimSpace(in.Description) == "" {
		return MemoryProposal{}, fmt.Errorf("%w: description is required", ErrTeamMemoryInvalidProposal)
	}
	if err := requireAuthor(in.Author); err != nil {
		return MemoryProposal{}, err
	}
	wc, err := s.cloneForWrite(ctx, teamID)
	if err != nil {
		return MemoryProposal{}, err
	}
	defer wc.cleanup()

	now := s.now().UTC().Format(time.RFC3339)
	id := "proposal-" + strings.ToLower(s.newID())
	if err := validateSegment(id); err != nil {
		return MemoryProposal{}, err
	}
	fm := legacyProposalFrontmatter{
		ID:                  id,
		UUID:                s.newID(),
		Status:              ProposalStatusPending,
		TargetKind:          targetKind,
		Slug:                strings.TrimSpace(in.Slug),
		Title:               strings.TrimSpace(in.Title),
		Description:         strings.TrimSpace(in.Description),
		AuthorRef:           strings.TrimSpace(in.AuthorRef),
		CreatedAt:           now,
		UpdatedAt:           now,
		Enabled:             in.Enabled,
		AppliesTo:           append([]string(nil), in.AppliesTo...),
		WarningAcknowledged: true,
	}
	if targetKind == MemoryItemRule {
		applies, aerr := normalizeAppliesTo(fm.AppliesTo)
		if aerr != nil {
			return MemoryProposal{}, fmt.Errorf("%w: applies_to: %v", ErrTeamMemoryInvalidProposal, aerr)
		}
		fm.AppliesTo = applies
	}
	path := filepath.ToSlash(filepath.Join(legacyProposalsDir, id+".md"))
	if err := writeProposalFile(wc.repoDir, fm, in.Body); err != nil {
		return MemoryProposal{}, err
	}
	store := NewStore(wc.repoDir, s.runner, WithHomeOverride(wc.workDir))
	if err := store.SyncPush(ctx, "origin", "main", in.Author, "propose team memory "+id, 3); err != nil {
		return MemoryProposal{}, err
	}
	commit, _ := currentCommit(ctx, s.runner, wc.repoDir, wc.workDir)
	p := proposalFromFrontmatter(fm, in.Body, path, commit)
	p.Diff = proposalDiff(p)
	return p, nil
}

func (s *TeamMemoryService) PromoteProposal(ctx context.Context, teamID string, in PromoteMemoryProposalInput) (MemoryProposal, error) {
	if !s.Configured() {
		return MemoryProposal{}, ErrTeamMemoryNotConfigured
	}
	if err := requireAuthor(in.Author); err != nil {
		return MemoryProposal{}, err
	}
	wc, err := s.cloneForWrite(ctx, teamID)
	if err != nil {
		return MemoryProposal{}, err
	}
	defer wc.cleanup()
	fm, body, sourcePath, err := parseLegacyProposalFile(wc.repoDir, in.ProposalID)
	if err != nil {
		return MemoryProposal{}, err
	}
	if fm.Status != ProposalStatusPending {
		return MemoryProposal{}, ErrTeamMemoryProposalNotPending
	}
	if !fm.WarningAcknowledged && !in.WarningAcknowledged {
		return MemoryProposal{}, ErrTeamMemoryWarningAckRequired
	}
	store := NewStore(wc.repoDir, s.runner, WithHomeOverride(wc.workDir))
	var promotedPath string
	switch normalizeMemoryKind(fm.TargetKind) {
	case MemoryItemEntry:
		promotedPath, err = store.WriteEntry(Entry{
			Slug: fm.Slug, Title: fm.Title, Description: fm.Description, Body: body,
		})
	case MemoryItemRule:
		promotedPath, err = store.WriteRule(Rule{
			Slug: fm.Slug, Title: fm.Title, Description: fm.Description, Body: body,
			Enabled: fm.Enabled, AppliesTo: append([]string(nil), fm.AppliesTo...),
		})
	default:
		err = fmt.Errorf("%w: unknown target kind %q", ErrTeamMemoryInvalidProposal, fm.TargetKind)
	}
	if err != nil {
		return MemoryProposal{}, err
	}
	fm.Status = ProposalStatusPromoted
	fm.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	fm.PromotedPath = promotedPath
	fm.TargetUUID = uuidFromGeneratedPath(promotedPath, fm.Slug)
	fm.WarningAcknowledged = true
	if err := writeProposalFileWithPath(wc.repoDir, sourcePath, fm, body); err != nil {
		return MemoryProposal{}, err
	}
	if err := store.SyncPush(ctx, "origin", "main", in.Author, "promote team memory "+fm.ID, 3); err != nil {
		return MemoryProposal{}, err
	}
	commit, _ := currentCommit(ctx, s.runner, wc.repoDir, wc.workDir)
	p := proposalFromFrontmatter(fm, body, sourcePath, commit)
	p.Diff = proposalDiff(p)
	return p, nil
}

func (s *TeamMemoryService) RejectProposal(ctx context.Context, teamID string, in RejectMemoryProposalInput) (MemoryProposal, error) {
	if !s.Configured() {
		return MemoryProposal{}, ErrTeamMemoryNotConfigured
	}
	if err := requireAuthor(in.Author); err != nil {
		return MemoryProposal{}, err
	}
	wc, err := s.cloneForWrite(ctx, teamID)
	if err != nil {
		return MemoryProposal{}, err
	}
	defer wc.cleanup()
	fm, body, sourcePath, err := parseLegacyProposalFile(wc.repoDir, in.ProposalID)
	if err != nil {
		return MemoryProposal{}, err
	}
	if fm.Status != ProposalStatusPending {
		return MemoryProposal{}, ErrTeamMemoryProposalNotPending
	}
	fm.Status = ProposalStatusRejected
	fm.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	fm.RejectReason = strings.TrimSpace(in.Reason)
	if err := writeProposalFileWithPath(wc.repoDir, sourcePath, fm, body); err != nil {
		return MemoryProposal{}, err
	}
	store := NewStore(wc.repoDir, s.runner, WithHomeOverride(wc.workDir))
	if err := store.SyncPush(ctx, "origin", "main", in.Author, "reject team memory "+fm.ID, 3); err != nil {
		return MemoryProposal{}, err
	}
	commit, _ := currentCommit(ctx, s.runner, wc.repoDir, wc.workDir)
	p := proposalFromFrontmatter(fm, body, sourcePath, commit)
	p.Diff = proposalDiff(p)
	return p, nil
}

func (s *TeamMemoryService) GetSettings(ctx context.Context, teamID string) (TeamMemorySettings, error) {
	settings := defaultTeamMemorySettings()
	settings.EffectHint = TeamMemoryEffectHint
	if !s.Configured() {
		return settings, ErrTeamMemoryNotConfigured
	}
	wc, err := s.cloneForRead(ctx, teamID)
	if err != nil {
		return settings, err
	}
	if wc == nil {
		return settings, nil
	}
	defer wc.cleanup()
	settings.Commit = wc.commit
	got, err := readSettings(wc.repoDir)
	if err != nil {
		return settings, err
	}
	got.Commit = wc.commit
	got.EffectHint = TeamMemoryEffectHint
	return got, nil
}

func (s *TeamMemoryService) UpdateSettings(ctx context.Context, teamID string, in UpdateTeamMemorySettingsInput) (TeamMemorySettings, error) {
	if !s.Configured() {
		return TeamMemorySettings{}, ErrTeamMemoryNotConfigured
	}
	if in.ActorKind == "agent" {
		return TeamMemorySettings{}, ErrTeamMemoryAgentSelfGrant
	}
	if err := requireAuthor(in.Author); err != nil {
		return TeamMemorySettings{}, err
	}
	policy := strings.TrimSpace(in.Policy)
	if policy == "" {
		policy = "owner_admin_review"
	}
	switch policy {
	case "owner_admin_review", "curator_review", "read_only":
	default:
		return TeamMemorySettings{}, fmt.Errorf("%w: unknown policy %q", ErrTeamMemoryInvalidSettings, policy)
	}
	agents := normalizeRefs(in.CuratorAgents, "agent:")
	wc, err := s.cloneForWrite(ctx, teamID)
	if err != nil {
		return TeamMemorySettings{}, err
	}
	defer wc.cleanup()
	model := settingsFileModel{
		CuratorAgents: agents,
		Policy:        policy,
		UpdatedAt:     s.now().UTC().Format(time.RFC3339),
		UpdatedBy:     strings.TrimSpace(in.ActorRef),
	}
	if err := writeSettings(wc.repoDir, model); err != nil {
		return TeamMemorySettings{}, err
	}
	store := NewStore(wc.repoDir, s.runner, WithHomeOverride(wc.workDir))
	if err := store.SyncPush(ctx, "origin", "main", in.Author, "update team memory settings", 3); err != nil {
		return TeamMemorySettings{}, err
	}
	commit, _ := currentCommit(ctx, s.runner, wc.repoDir, wc.workDir)
	return TeamMemorySettings{
		CuratorAgents: model.CuratorAgents, Policy: model.Policy, UpdatedAt: model.UpdatedAt,
		UpdatedBy: model.UpdatedBy, Commit: commit, EffectHint: TeamMemoryEffectHint,
	}, nil
}

type workClone struct {
	workDir string
	repoDir string
	commit  string
	cleanup func()
}

func (s *TeamMemoryService) cloneForRead(ctx context.Context, teamID string) (*workClone, error) {
	if err := validateSegment(teamID); err != nil {
		return nil, err
	}
	ref := TeamRepo(teamID)
	exists, err := s.host.RepoExists(ref)
	if err != nil || !exists {
		return nil, err
	}
	return s.cloneExisting(ctx, ref, "team-memory-service-read-*")
}

func (s *TeamMemoryService) cloneForWrite(ctx context.Context, teamID string) (*workClone, error) {
	if err := validateSegment(teamID); err != nil {
		return nil, err
	}
	ref := TeamRepo(teamID)
	if err := s.host.EnsureRepo(ctx, ref); err != nil {
		return nil, err
	}
	return s.cloneExisting(ctx, ref, "team-memory-service-write-*")
}

func (s *TeamMemoryService) cloneExisting(ctx context.Context, ref RepoRef, pattern string) (*workClone, error) {
	bareDir, err := s.host.RepoDir(ref)
	if err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	cleanup := func() { _ = os.RemoveAll(work) }
	repoDir := filepath.Join(work, "repo")
	env := baseGitEnv(work, "", "")
	if out, cErr := s.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		cleanup()
		return nil, fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}
	commit, _ := currentCommit(ctx, s.runner, repoDir, work)
	return &workClone{workDir: work, repoDir: repoDir, commit: commit, cleanup: cleanup}, nil
}

func currentCommit(ctx context.Context, runner memory.GitRunner, repoDir, home string) (string, error) {
	out, err := runner.Run(ctx, repoDir, baseGitEnv(home, "", ""), "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func readEntryDoc(repoDir, commit, slug string) (MemoryDocument, bool, error) {
	files, err := matchingMarkdownFiles(filepath.Join(repoDir, entriesDir))
	if err != nil {
		return MemoryDocument{}, false, err
	}
	for _, name := range files {
		rel := filepath.ToSlash(filepath.Join(entriesDir, name))
		fm, body, perr := parseEntry(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if perr != nil {
			if errors.Is(perr, errMalformedEntry) {
				continue
			}
			return MemoryDocument{}, false, perr
		}
		if fm.Name != slug {
			continue
		}
		return MemoryDocument{
			Kind: MemoryItemEntry, Slug: fm.Name, Path: rel, Title: titleOrSlug(fm.Title, fm.Name),
			Frontmatter: marshalYAML(fm), Body: body, UUID: fm.UUID, Commit: commit,
		}, true, nil
	}
	return MemoryDocument{}, false, nil
}

func readRuleDoc(repoDir, commit, slug string) (MemoryDocument, bool, error) {
	files, err := matchingMarkdownFiles(filepath.Join(repoDir, rulesDir))
	if err != nil {
		return MemoryDocument{}, false, err
	}
	for _, name := range files {
		rel := filepath.ToSlash(filepath.Join(rulesDir, name))
		fm, body, perr := parseRule(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if perr != nil {
			if errors.Is(perr, errMalformedEntry) {
				continue
			}
			return MemoryDocument{}, false, perr
		}
		if fm.Name != slug {
			continue
		}
		return MemoryDocument{
			Kind: MemoryItemRule, Slug: fm.Name, Path: rel, Title: titleOrSlug(fm.Title, fm.Name),
			Frontmatter: marshalYAML(fm), Body: body, UUID: fm.UUID, Commit: commit,
		}, true, nil
	}
	return MemoryDocument{}, false, nil
}

func matchingMarkdownFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, de := range ents {
		if !de.IsDir() && strings.HasSuffix(de.Name(), ".md") {
			out = append(out, de.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func readProposals(repoDir, commit string) ([]MemoryProposal, []string, error) {
	files, err := matchingMarkdownFiles(filepath.Join(repoDir, legacyProposalsDir))
	if err != nil {
		return nil, nil, err
	}
	var out []MemoryProposal
	var skipped []string
	for _, name := range files {
		id := strings.TrimSuffix(name, ".md")
		p, err := readProposalByID(repoDir, commit, id)
		if err != nil {
			if errors.Is(err, errMalformedEntry) || errors.Is(err, ErrTeamMemoryNotFound) {
				skipped = append(skipped, name)
				continue
			}
			return nil, nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		return out[i].CreatedAt > out[j].CreatedAt
	})
	sort.Strings(skipped)
	return out, skipped, nil
}

func readProposalByID(repoDir, commit, id string) (MemoryProposal, error) {
	fm, body, sourcePath, err := parseLegacyProposalFile(repoDir, id)
	if err != nil {
		return MemoryProposal{}, err
	}
	p := proposalFromFrontmatter(fm, body, sourcePath, commit)
	p.Diff = proposalDiff(p)
	return p, nil
}

func parseLegacyProposalFile(repoDir, id string) (legacyProposalFrontmatter, string, string, error) {
	if err := validateSegment(id); err != nil {
		return legacyProposalFrontmatter{}, "", "", err
	}
	rel := filepath.ToSlash(filepath.Join(legacyProposalsDir, id+".md"))
	var fm legacyProposalFrontmatter
	body, err := parseMarkdown(filepath.Join(repoDir, filepath.FromSlash(rel)), &fm)
	if err != nil {
		if os.IsNotExist(err) {
			return fm, "", rel, ErrTeamMemoryNotFound
		}
		return fm, "", rel, err
	}
	if fm.ID == "" {
		fm.ID = id
	}
	return fm, body, rel, nil
}

func writeProposalFile(repoDir string, fm legacyProposalFrontmatter, body string) error {
	return writeProposalFileWithPath(repoDir, filepath.ToSlash(filepath.Join(legacyProposalsDir, fm.ID+".md")), fm, body)
}

func writeProposalFileWithPath(repoDir, rel string, fm legacyProposalFrontmatter, body string) error {
	if err := validateSegment(strings.TrimSuffix(filepath.Base(rel), ".md")); err != nil {
		return err
	}
	content, err := renderMarkdown(fm, body)
	if err != nil {
		return err
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(content), 0o600)
}

func proposalFromFrontmatter(fm legacyProposalFrontmatter, body, sourcePath, commit string) MemoryProposal {
	status := strings.TrimSpace(fm.Status)
	if status == "" {
		status = ProposalStatusPending
	}
	p := MemoryProposal{
		ID: fm.ID, UUID: fm.UUID, Status: status, TargetKind: normalizeMemoryKind(fm.TargetKind),
		Slug: fm.Slug, Title: titleOrSlug(fm.Title, fm.Slug), Description: fm.Description,
		Body: body, AuthorRef: fm.AuthorRef, CreatedAt: fm.CreatedAt, UpdatedAt: fm.UpdatedAt,
		SourcePath: sourcePath, PromotedPath: fm.PromotedPath, TargetUUID: fm.TargetUUID,
		Commit: commit, Enabled: fm.Enabled, AppliesTo: append([]string(nil), fm.AppliesTo...),
		WarningAcknowledged: fm.WarningAcknowledged, RejectReason: fm.RejectReason,
	}
	return p
}

func proposalDocument(p MemoryProposal) MemoryDocument {
	fm := legacyProposalFrontmatter{
		ID: p.ID, UUID: p.UUID, Status: p.Status, TargetKind: p.TargetKind, Slug: p.Slug,
		Title: p.Title, Description: p.Description, AuthorRef: p.AuthorRef, CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt, PromotedPath: p.PromotedPath, TargetUUID: p.TargetUUID,
		Enabled: p.Enabled, AppliesTo: p.AppliesTo, WarningAcknowledged: p.WarningAcknowledged,
		RejectReason: p.RejectReason,
	}
	return MemoryDocument{
		Kind: MemoryItemProposal, Slug: p.ID, Path: p.SourcePath, Title: p.Title,
		Frontmatter: marshalYAML(fm), Body: p.Body, UUID: p.UUID, Commit: p.Commit,
		Proposal: &p,
	}
}

func proposalDiff(p MemoryProposal) string {
	var b strings.Builder
	path := p.PromotedPath
	if path == "" {
		dir := entriesDir
		if p.TargetKind == MemoryItemRule {
			dir = rulesDir
		}
		path = filepath.ToSlash(filepath.Join(dir, p.Slug+"-"+p.UUID+".md"))
	}
	fmt.Fprintf(&b, "+++ %s\n", path)
	writeDiffLine := func(line string) { fmt.Fprintf(&b, "+%s\n", line) }
	if p.TargetKind == MemoryItemRule {
		fm := ruleFrontmatter{
			Name: p.Slug, Title: emptyIfSame(p.Title, p.Slug), Description: p.Description,
			UUID: firstNonEmpty(p.TargetUUID, p.UUID), Enabled: p.Enabled, AppliesTo: p.AppliesTo,
		}
		content, _ := renderRule(fm, p.Body)
		for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
			writeDiffLine(line)
		}
		return b.String()
	}
	fm := entryFrontmatter{
		Name: p.Slug, Title: emptyIfSame(p.Title, p.Slug), Description: p.Description,
		UUID: firstNonEmpty(p.TargetUUID, p.UUID),
	}
	content, _ := renderEntry(fm, p.Body)
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		writeDiffLine(line)
	}
	return b.String()
}

func readSettings(repoDir string) (TeamMemorySettings, error) {
	model := settingsFileModel{}
	raw, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(settingsFile)))
	if err != nil {
		if os.IsNotExist(err) {
			return defaultTeamMemorySettings(), nil
		}
		return TeamMemorySettings{}, err
	}
	if err := yaml.Unmarshal(raw, &model); err != nil {
		return TeamMemorySettings{}, err
	}
	if model.Policy == "" {
		model.Policy = "owner_admin_review"
	}
	return TeamMemorySettings{
		CuratorAgents: normalizeRefs(model.CuratorAgents, "agent:"),
		Policy:        model.Policy,
		UpdatedAt:     model.UpdatedAt,
		UpdatedBy:     model.UpdatedBy,
		EffectHint:    TeamMemoryEffectHint,
	}, nil
}

func writeSettings(repoDir string, model settingsFileModel) error {
	if model.Policy == "" {
		model.Policy = "owner_admin_review"
	}
	model.CuratorAgents = normalizeRefs(model.CuratorAgents, "agent:")
	y, err := yaml.Marshal(model)
	if err != nil {
		return err
	}
	abs := filepath.Join(repoDir, filepath.FromSlash(settingsFile))
	if err := os.MkdirAll(filepath.Join(repoDir, settingsDir), 0o700); err != nil {
		return err
	}
	return os.WriteFile(abs, y, 0o600)
}

func defaultTeamMemorySettings() TeamMemorySettings {
	return TeamMemorySettings{
		CuratorAgents: []string{},
		Policy:        "owner_admin_review",
		EffectHint:    TeamMemoryEffectHint,
	}
}

func normalizeRefs(in []string, prefix string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		ref := strings.TrimSpace(raw)
		if ref == "" || !strings.HasPrefix(ref, prefix) {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

func normalizeMemoryKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "entry", "entries":
		return MemoryItemEntry
	case "rule", "rules":
		return MemoryItemRule
	case "proposal", "proposals":
		return MemoryItemProposal
	case "index":
		return MemoryItemIndex
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func marshalYAML(v any) string {
	y, err := yaml.Marshal(v)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(y), "\n")
}

func titleOrSlug(title, slug string) string {
	if strings.TrimSpace(title) != "" {
		return strings.TrimSpace(title)
	}
	return slug
}

func emptyIfSame(title, slug string) string {
	if strings.TrimSpace(title) == slug {
		return ""
	}
	return strings.TrimSpace(title)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func requireAuthor(a Author) error {
	if a.Name == "" || a.Email == "" {
		return errors.New("centergit: author name + email required")
	}
	return nil
}

func uuidFromGeneratedPath(path, slug string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	prefix := slug + "-"
	if strings.HasPrefix(base, prefix) {
		return strings.TrimPrefix(base, prefix)
	}
	if i := strings.LastIndex(base, "-"); i >= 0 {
		return base[i+1:]
	}
	return ""
}

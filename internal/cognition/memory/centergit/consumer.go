package centergit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

// consumer.go is the read counterpart to producer.go: extract_from_team (design
// §6 "从活 team 抽经验草稿") snapshots a LIVE team's accumulated memory back out of
// its center-hosted repo. It mirrors the producer's throwaway-working-clone model
// (clone the bare repo, read entries/*.md through the Store) so reads use the same
// on-disk contract writes do — no separate parser to drift.

// TeamMemoryConsumer reads a team's center-hosted memory repo.
type TeamMemoryConsumer struct {
	host   *Host
	runner memory.GitRunner
}

// RuleSnapshot is the auditable result of loading team rules from a repo commit.
type RuleSnapshot struct {
	TeamID           string
	Phase            string
	Commit           string
	Rules            []Rule
	Skipped          []string
	RefreshSemantics string
}

const RuleRefreshSemantics = "rules are snapshotted at load/fork time; in-flight executors and tier-1/tier-2 recovery keep their persisted input/runner command, while a fresh fork or tier-3 reset reloads from the current team repo"

const (
	RuleDescriptionMaxBytes = 240
	RuleIndexMaxEntries     = 64
	RuleIndexMaxBytes       = 16 * 1024
)

var (
	ErrRuleSnapshotNotFound  = errors.New("rule_snapshot_not_found")
	ErrTeamRuleNotFound      = errors.New("team_rule_not_found")
	ErrTeamRuleIndexTooLarge = errors.New("team_rule_index_too_large")
)

// RuleIndexEntry is the body-free startup/runtime projection for one rule.
type RuleIndexEntry struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description"`
	AppliesTo   []string `json:"applies_to"`
	BodyBytes   int      `json:"body_bytes"`
	SourcePath  string   `json:"source_path,omitempty"`
}

// RuleIndexSnapshot records the exact commit used to build a phase-scoped index.
type RuleIndexSnapshot struct {
	TeamID           string
	Phase            string
	Commit           string
	Rules            []RuleIndexEntry
	Skipped          []string
	RefreshSemantics string
}

// RuleBodySnapshot records one commit-bound rule body read.
type RuleBodySnapshot struct {
	TeamID           string
	Phase            string
	Commit           string
	Rule             Rule
	RefreshSemantics string
}

// NewTeamMemoryConsumer wires a consumer over host. A nil runner defaults to the
// real git binary (memory.NewExecGitRunner).
func NewTeamMemoryConsumer(host *Host, runner memory.GitRunner) *TeamMemoryConsumer {
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	return &TeamMemoryConsumer{host: host, runner: runner}
}

// ReadTeam clones team teamID's bare repo into a throwaway working copy and
// returns every memory entry (frontmatter + body). A team whose repo has not been
// provisioned yet (no memory seeded) yields nil, nil, nil — an absent history is
// not an error, it is simply an empty experience set.
//
// The returned `skipped` list names any non-standard files in the repo that are
// NOT well-formed memory entries (no frontmatter, etc.): they are skipped rather
// than crashing the read, so a member's stray push cannot break extract_from_team
// (design §6). Callers surface the count for the curator.
func (c *TeamMemoryConsumer) ReadTeam(ctx context.Context, teamID string) (entries []Entry, skipped []string, err error) {
	if c == nil || c.host == nil {
		return nil, nil, fmt.Errorf("%w: consumer not wired", ErrGitOpFailed)
	}
	ref := TeamRepo(teamID)
	exists, err := c.host.RepoExists(ref)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, nil
	}
	bareDir, err := c.host.RepoDir(ref)
	if err != nil {
		return nil, nil, err
	}

	work, err := os.MkdirTemp("", "team-memory-read-*")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	defer os.RemoveAll(work)

	env := baseGitEnv(work, "", "")
	repoDir := filepath.Join(work, "repo")
	if out, cErr := c.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		return nil, nil, fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}
	store := NewStore(repoDir, c.runner, WithHomeOverride(work))
	return store.ReadEntries()
}

// ReadTeamRules clones team teamID's bare repo into a throwaway working copy and
// returns the enabled rules that apply to phase plus the exact HEAD commit used.
// A missing team repo yields an empty snapshot, not an error. Malformed rule
// files are skipped and reported, mirroring ReadTeam's defensive contract.
func (c *TeamMemoryConsumer) ReadTeamRules(ctx context.Context, teamID, phase string) (RuleSnapshot, error) {
	snap := RuleSnapshot{
		TeamID:           teamID,
		Phase:            normalizeSnapshotPhase(phase),
		RefreshSemantics: RuleRefreshSemantics,
	}
	if c == nil || c.host == nil {
		return snap, fmt.Errorf("%w: consumer not wired", ErrGitOpFailed)
	}
	ref := TeamRepo(teamID)
	exists, err := c.host.RepoExists(ref)
	if err != nil {
		return snap, err
	}
	if !exists {
		return snap, nil
	}
	bareDir, err := c.host.RepoDir(ref)
	if err != nil {
		return snap, err
	}

	work, err := os.MkdirTemp("", "team-rules-read-*")
	if err != nil {
		return snap, fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	defer os.RemoveAll(work)

	env := baseGitEnv(work, "", "")
	repoDir := filepath.Join(work, "repo")
	if out, cErr := c.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		return snap, fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}
	if out, rErr := c.runner.Run(ctx, repoDir, env, "rev-parse", "HEAD"); rErr == nil {
		snap.Commit = strings.TrimSpace(out)
	} else {
		// An unborn empty repo has no HEAD commit yet; keep Commit empty and
		// return an empty rule set.
		return snap, nil
	}
	store := NewStore(repoDir, c.runner, WithHomeOverride(work))
	rules, skipped, err := store.ReadRules()
	if err != nil {
		return snap, err
	}
	snap.Skipped = skipped
	for _, r := range rules {
		if RuleAppliesToPhase(r, snap.Phase) {
			snap.Rules = append(snap.Rules, r)
		}
	}
	return snap, nil
}

// ReadTeamRuleIndex returns a body-free, phase-scoped rule index from the team's
// current HEAD commit. A missing team repo yields an empty snapshot. Malformed
// rule files are skipped and reported.
func (c *TeamMemoryConsumer) ReadTeamRuleIndex(ctx context.Context, teamID, phase string) (RuleIndexSnapshot, error) {
	snap := RuleIndexSnapshot{
		TeamID:           teamID,
		Phase:            normalizeSnapshotPhase(phase),
		RefreshSemantics: RuleRefreshSemantics,
	}
	repoDir, work, env, err := c.cloneTeamForRead(ctx, teamID)
	if err != nil {
		return snap, err
	}
	if repoDir == "" {
		return snap, nil
	}
	defer os.RemoveAll(work)
	commit, err := revParseHead(ctx, c.runner, repoDir, env)
	if err != nil {
		if isUnbornHead(err) {
			return snap, nil
		}
		return snap, err
	}
	snap.Commit = commit
	store := NewStore(repoDir, c.runner, WithHomeOverride(work))
	rules, skipped, err := store.ReadRules()
	if err != nil {
		return snap, err
	}
	snap.Skipped = skipped
	snap.Rules, err = buildRuleIndexEntries(rules, snap.Phase)
	if err != nil {
		return snap, err
	}
	return snap, nil
}

// ReadTeamRule reads one enabled, phase-applicable rule body from the exact
// commit supplied by a prior index response. It never falls back to HEAD.
func (c *TeamMemoryConsumer) ReadTeamRule(ctx context.Context, teamID, phase, slug, commit string) (RuleBodySnapshot, error) {
	snap := RuleBodySnapshot{
		TeamID:           teamID,
		Phase:            normalizeSnapshotPhase(phase),
		Commit:           strings.TrimSpace(commit),
		RefreshSemantics: RuleRefreshSemantics,
	}
	if !looksLikeCommitSHA(snap.Commit) {
		return snap, fmt.Errorf("%w: commit is required", ErrRuleSnapshotNotFound)
	}
	wantSlug := strings.TrimSpace(slug)
	if wantSlug == "" {
		return snap, fmt.Errorf("%w: slug is required", ErrTeamRuleNotFound)
	}
	repoDir, work, env, err := c.cloneTeamForRead(ctx, teamID)
	if err != nil {
		return snap, err
	}
	if repoDir == "" {
		return snap, fmt.Errorf("%w: team repo is not provisioned", ErrRuleSnapshotNotFound)
	}
	defer os.RemoveAll(work)
	if out, cErr := c.runner.Run(ctx, repoDir, env, "cat-file", "-e", snap.Commit+"^{commit}"); cErr != nil {
		return snap, fmt.Errorf("%w: commit %s: %v: %s", ErrRuleSnapshotNotFound, snap.Commit, cErr, out)
	}
	if out, cErr := c.runner.Run(ctx, repoDir, env, "checkout", "--detach", snap.Commit); cErr != nil {
		return snap, fmt.Errorf("%w: checkout %s: %v: %s", ErrGitOpFailed, snap.Commit, cErr, out)
	}
	store := NewStore(repoDir, c.runner, WithHomeOverride(work))
	rules, _, err := store.ReadRules()
	if err != nil {
		return snap, err
	}
	if _, err := buildRuleIndexEntries(rules, snap.Phase); err != nil {
		return snap, err
	}
	for _, r := range rules {
		if r.Slug == wantSlug && RuleAppliesToPhase(r, snap.Phase) {
			snap.Rule = r
			return snap, nil
		}
	}
	return snap, fmt.Errorf("%w: %s", ErrTeamRuleNotFound, wantSlug)
}

// ReadTeamAllRules returns every rule file regardless of enabled/applies_to. It
// is used by extract/template flows; runtime context should use ReadTeamRules so
// disabled or non-matching rules do not leak into a run.
func (c *TeamMemoryConsumer) ReadTeamAllRules(ctx context.Context, teamID string) (rules []Rule, skipped []string, err error) {
	if c == nil || c.host == nil {
		return nil, nil, fmt.Errorf("%w: consumer not wired", ErrGitOpFailed)
	}
	ref := TeamRepo(teamID)
	exists, err := c.host.RepoExists(ref)
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, nil
	}
	bareDir, err := c.host.RepoDir(ref)
	if err != nil {
		return nil, nil, err
	}

	work, err := os.MkdirTemp("", "team-rules-read-all-*")
	if err != nil {
		return nil, nil, fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	defer os.RemoveAll(work)

	env := baseGitEnv(work, "", "")
	repoDir := filepath.Join(work, "repo")
	if out, cErr := c.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		return nil, nil, fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}
	store := NewStore(repoDir, c.runner, WithHomeOverride(work))
	return store.ReadRules()
}

func (c *TeamMemoryConsumer) cloneTeamForRead(ctx context.Context, teamID string) (repoDir, work string, env []string, err error) {
	if c == nil || c.host == nil {
		return "", "", nil, fmt.Errorf("%w: consumer not wired", ErrGitOpFailed)
	}
	ref := TeamRepo(teamID)
	exists, err := c.host.RepoExists(ref)
	if err != nil {
		return "", "", nil, err
	}
	if !exists {
		return "", "", nil, nil
	}
	bareDir, err := c.host.RepoDir(ref)
	if err != nil {
		return "", "", nil, err
	}
	work, err = os.MkdirTemp("", "team-rule-read-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	env = baseGitEnv(work, "", "")
	repoDir = filepath.Join(work, "repo")
	if out, cErr := c.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		_ = os.RemoveAll(work)
		return "", "", nil, fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}
	return repoDir, work, env, nil
}

func normalizeSnapshotPhase(phase string) string {
	if p := normalizeRulePhase(phase); p != "" && p != "all" {
		return p
	}
	return "execute"
}

func buildRuleIndexEntries(rules []Rule, phase string) ([]RuleIndexEntry, error) {
	phase = normalizeSnapshotPhase(phase)
	out := make([]RuleIndexEntry, 0, len(rules))
	for _, r := range rules {
		if !RuleAppliesToPhase(r, phase) {
			continue
		}
		desc := strings.TrimSpace(r.Description)
		if len([]byte(desc)) > RuleDescriptionMaxBytes {
			return nil, fmt.Errorf("%w: rule %q description is %d bytes, max %d",
				ErrTeamRuleIndexTooLarge, r.Slug, len([]byte(desc)), RuleDescriptionMaxBytes)
		}
		out = append(out, RuleIndexEntry{
			Slug:        strings.TrimSpace(r.Slug),
			Title:       strings.TrimSpace(r.Title),
			Description: desc,
			AppliesTo:   append([]string(nil), r.AppliesTo...),
			BodyBytes:   len([]byte(r.Body)),
			SourcePath:  strings.TrimSpace(r.SourcePath),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Slug != out[j].Slug {
			return out[i].Slug < out[j].Slug
		}
		return out[i].SourcePath < out[j].SourcePath
	})
	if len(out) > RuleIndexMaxEntries {
		return nil, fmt.Errorf("%w: phase %s has %d rules, max %d", ErrTeamRuleIndexTooLarge, phase, len(out), RuleIndexMaxEntries)
	}
	if size := ruleIndexPayloadBytes(out); size > RuleIndexMaxBytes {
		return nil, fmt.Errorf("%w: phase %s index is %d bytes, max %d", ErrTeamRuleIndexTooLarge, phase, size, RuleIndexMaxBytes)
	}
	return out, nil
}

func looksLikeCommitSHA(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, r := range commit {
		if r > unicode.MaxASCII || !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

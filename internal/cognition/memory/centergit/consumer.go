package centergit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

func normalizeSnapshotPhase(phase string) string {
	if p := normalizeRulePhase(phase); p != "" && p != "all" {
		return p
	}
	return "execute"
}

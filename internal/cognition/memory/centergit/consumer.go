package centergit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
// provisioned yet (no memory seeded) yields nil, nil — an absent history is not an
// error, it is simply an empty experience set.
func (c *TeamMemoryConsumer) ReadTeam(ctx context.Context, teamID string) ([]Entry, error) {
	if c == nil || c.host == nil {
		return nil, fmt.Errorf("%w: consumer not wired", ErrGitOpFailed)
	}
	ref := TeamRepo(teamID)
	exists, err := c.host.RepoExists(ref)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}
	bareDir, err := c.host.RepoDir(ref)
	if err != nil {
		return nil, err
	}

	work, err := os.MkdirTemp("", "team-memory-read-*")
	if err != nil {
		return nil, fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	defer os.RemoveAll(work)

	env := baseGitEnv(work, "", "")
	repoDir := filepath.Join(work, "repo")
	if out, cErr := c.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		return nil, fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}
	store := NewStore(repoDir, c.runner, WithHomeOverride(work))
	return store.ReadEntries()
}

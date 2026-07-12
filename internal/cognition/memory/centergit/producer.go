package centergit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

// producer.go implements the team-scoped memory producer (design §4.3/§6/§9):
// at team instantiation the center seeds the team's shared memory repo with the
// template's portable experiences, so a freshly instantiated team already
// carries its generalizable skills/rules before any agent runs.
//
// The center hosts the team repo as a bare repo (Host.EnsureRepo). Seeding is a
// client-side operation over a throwaway working clone: clone the bare repo,
// write one file per experience through the Store (每条经验一文件 — §9), then
// SyncPush back with the pull-rebase-retry that absorbs concurrent team writers.
// This reuses the exact write path a runtime uses, so the seed is indistinguish-
// able from an agent-authored memory.

// defaultSeedAuthor is the git identity the center commits seed memory under.
var defaultSeedAuthor = Author{Name: "agent-center", Email: "team-memory@agent-center.local"}

// TeamMemoryProducer seeds a team's center-hosted memory repo from a set of
// portable experiences. It owns no state beyond the Host it provisions against
// and the git runner it drives.
type TeamMemoryProducer struct {
	host   *Host
	runner memory.GitRunner
	author Author
}

// ProducerOption configures a TeamMemoryProducer.
type ProducerOption func(*TeamMemoryProducer)

// WithSeedAuthor overrides the git author the seed commit is attributed to.
func WithSeedAuthor(a Author) ProducerOption {
	return func(p *TeamMemoryProducer) { p.author = a }
}

// NewTeamMemoryProducer wires a producer over host. A nil runner defaults to the
// real git binary (memory.NewExecGitRunner).
func NewTeamMemoryProducer(host *Host, runner memory.GitRunner, opts ...ProducerOption) *TeamMemoryProducer {
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	p := &TeamMemoryProducer{host: host, runner: runner, author: defaultSeedAuthor}
	for _, o := range opts {
		o(p)
	}
	return p
}

// SeedTeam provisions (idempotently) team teamID's bare repo and writes each
// entry into it as one file, pushing a single seed commit. Entries that fail
// per-entry validation (empty slug/description) are skipped — seeding is
// best-effort over a human-curated template. Returns the number of entries
// actually written. A nil/zero entry set is a no-op (repo still provisioned).
func (p *TeamMemoryProducer) SeedTeam(ctx context.Context, teamID string, entries []Entry) (int, error) {
	if p == nil || p.host == nil {
		return 0, fmt.Errorf("%w: producer not wired", ErrGitOpFailed)
	}
	ref := TeamRepo(teamID)
	if err := p.host.EnsureRepo(ctx, ref); err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, nil
	}
	bareDir, err := p.host.RepoDir(ref)
	if err != nil {
		return 0, err
	}

	work, err := os.MkdirTemp("", "team-memory-seed-*")
	if err != nil {
		return 0, fmt.Errorf("%w: mktemp: %v", ErrGitOpFailed, err)
	}
	defer os.RemoveAll(work)

	env := baseGitEnv("", p.author.Name, p.author.Email)
	// Clone the bare repo into work/repo. An unborn (empty) bare repo clones to a
	// working tree with an unborn HEAD on main, which is exactly what we want to
	// commit the first seed onto.
	repoDir := filepath.Join(work, "repo")
	if out, cErr := p.runner.Run(ctx, work, env, "clone", bareDir, repoDir); cErr != nil {
		return 0, fmt.Errorf("%w: clone %s: %v: %s", ErrGitOpFailed, bareDir, cErr, out)
	}

	store := NewStore(repoDir, p.runner, WithHomeOverride(work))
	written := 0
	for _, e := range entries {
		if strings.TrimSpace(e.Slug) == "" || strings.TrimSpace(e.Description) == "" {
			continue // skip un-curated / partial experiences
		}
		if _, wErr := store.WriteEntry(e); wErr != nil {
			continue // best-effort: a single bad entry never fails the whole seed
		}
		written++
	}
	if written == 0 {
		return 0, nil
	}
	if pErr := store.SyncPush(ctx, "origin", "main", p.author,
		fmt.Sprintf("seed team %s memory (%d experiences)", teamID, written), 3); pErr != nil {
		return 0, pErr
	}
	return written, nil
}

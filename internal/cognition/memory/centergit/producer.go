package centergit

import (
	"context"
	"fmt"
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
// entry/rule into it as one file, pushing a single seed commit. Entries/rules
// that fail per-item validation are skipped — seeding is best-effort over a
// human-curated template. Returns the number of items actually written. A
// nil/zero item set is a no-op (repo still provisioned). The variadic rules
// parameter preserves the original entries-only call shape.
func (p *TeamMemoryProducer) SeedTeam(ctx context.Context, teamID string, entries []Entry, ruleSets ...[]Rule) (int, error) {
	if p == nil || p.host == nil {
		return 0, fmt.Errorf("%w: producer not wired", ErrGitOpFailed)
	}
	rules := flattenRules(ruleSets)
	filteredEntries := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if strings.TrimSpace(e.Slug) != "" && strings.TrimSpace(e.Description) != "" {
			filteredEntries = append(filteredEntries, e)
		}
	}
	filteredRules := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if strings.TrimSpace(r.Slug) != "" && strings.TrimSpace(r.Description) != "" {
			filteredRules = append(filteredRules, r)
		}
	}
	repo := NewTeamMemoryRepository(p.host, p.runner, WithRepositoryAuthor(p.author))
	written, _, err := repo.Bootstrap(ctx, teamID, TrustedBootstrapCommand{
		ActorRef: "system:bootstrap",
		Source:   "team-memory-producer",
		Entries:  filteredEntries,
		Rules:    filteredRules,
	})
	return written, err
}

func flattenRules(ruleSets [][]Rule) []Rule {
	var total int
	for _, rs := range ruleSets {
		total += len(rs)
	}
	if total == 0 {
		return nil
	}
	out := make([]Rule, 0, total)
	for _, rs := range ruleSets {
		out = append(out, rs...)
	}
	return out
}

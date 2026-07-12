package centergit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTeamMemoryProducer_SeedTeam proves the team-scoped memory producer
// provisions a team's bare repo and pushes one file per experience plus the
// derived MEMORY.md index — the design §4.3/§9 seed path a fresh team carries.
func TestTeamMemoryProducer_SeedTeam(t *testing.T) {
	host := NewHost(t.TempDir(), nil)
	prod := NewTeamMemoryProducer(host, nil)
	ctx := context.Background()

	entries := []Entry{
		{Slug: "prefer-tdd", Description: "write tests first", Body: "always TDD", Type: "team"},
		{Slug: "review-checklist", Description: "what to check in review", Body: "look for X", Type: "team"},
		{Slug: "", Description: "missing slug — skipped", Body: "x"}, // invalid → skipped
	}
	n, err := prod.SeedTeam(ctx, "team-42", entries)
	if err != nil {
		t.Fatalf("SeedTeam: %v", err)
	}
	if n != 2 {
		t.Fatalf("seeded=%d want 2 (invalid entry skipped)", n)
	}

	// The bare repo must now exist and hold the seeded entries + a derived index.
	if ok, _ := host.RepoExists(TeamRepo("team-42")); !ok {
		t.Fatalf("team repo not provisioned")
	}
	bareDir, _ := host.RepoDir(TeamRepo("team-42"))
	work := t.TempDir()
	runner := host.runner
	if out, cerr := runner.Run(ctx, work, baseGitEnv("", "", ""), "clone", bareDir, filepath.Join(work, "co")); cerr != nil {
		t.Fatalf("clone seeded repo: %v: %s", cerr, out)
	}
	co := filepath.Join(work, "co")

	idx, err := os.ReadFile(filepath.Join(co, indexFile))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	for _, want := range []string{"write tests first", "what to check in review"} {
		if !strings.Contains(string(idx), want) {
			t.Errorf("index missing %q; index=\n%s", want, idx)
		}
	}
	files, _ := os.ReadDir(filepath.Join(co, entriesDir))
	if len(files) != 2 {
		t.Fatalf("entries dir has %d files, want 2", len(files))
	}
}

// TestTeamMemoryProducer_EmptySeedProvisions proves an empty seed still
// provisions the (readable) team repo — instantiation always yields a repo even
// when the template carried no portable experience.
func TestTeamMemoryProducer_EmptySeedProvisions(t *testing.T) {
	host := NewHost(t.TempDir(), nil)
	prod := NewTeamMemoryProducer(host, nil)
	n, err := prod.SeedTeam(context.Background(), "team-empty", nil)
	if err != nil {
		t.Fatalf("SeedTeam empty: %v", err)
	}
	if n != 0 {
		t.Fatalf("seeded=%d want 0", n)
	}
	if ok, _ := host.RepoExists(TeamRepo("team-empty")); !ok {
		t.Fatalf("empty-seed team repo not provisioned")
	}
}

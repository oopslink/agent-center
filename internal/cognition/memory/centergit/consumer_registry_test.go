package centergit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeamRuleIndexAndBodyReadAreCommitBound(t *testing.T) {
	ctx := context.Background()
	host := NewHost(t.TempDir(), nil)
	prod := NewTeamMemoryProducer(host, nil)
	if _, err := prod.SeedTeam(ctx, "team-index", nil, []Rule{
		{Slug: "execute-rule", Description: "read before changing code", Body: "old body", Enabled: true, AppliesTo: []string{"execute"}},
		{Slug: "review-rule", Description: "read during review", Body: "review body", Enabled: true, AppliesTo: []string{"review"}},
		{Slug: "disabled-rule", Description: "do not read", Body: "off", Enabled: false, AppliesTo: []string{"execute"}},
	}); err != nil {
		t.Fatalf("SeedTeam: %v", err)
	}
	consumer := NewTeamMemoryConsumer(host, nil)
	idx, err := consumer.ReadTeamRuleIndex(ctx, "team-index", "execute")
	if err != nil {
		t.Fatalf("ReadTeamRuleIndex: %v", err)
	}
	if idx.Commit == "" || len(idx.Rules) != 1 || idx.Rules[0].Slug != "execute-rule" || idx.Rules[0].BodyBytes != len("old body") {
		t.Fatalf("index = %+v, want one execute rule with body bytes", idx)
	}

	updateRuleBodyAtHEAD(t, host, "team-index", idx.Rules[0].SourcePath, "old body", "new body")
	idx2, err := consumer.ReadTeamRuleIndex(ctx, "team-index", "execute")
	if err != nil {
		t.Fatalf("ReadTeamRuleIndex after update: %v", err)
	}
	if idx2.Commit == "" || idx2.Commit == idx.Commit {
		t.Fatalf("updated HEAD commit did not move: before=%s after=%s", idx.Commit, idx2.Commit)
	}

	oldSnap, err := consumer.ReadTeamRule(ctx, "team-index", "execute", "execute-rule", idx.Commit)
	if err != nil {
		t.Fatalf("ReadTeamRule old commit: %v", err)
	}
	if oldSnap.Rule.Body != "old body" || oldSnap.Commit != idx.Commit {
		t.Fatalf("old commit body = %+v, want old body at %s", oldSnap, idx.Commit)
	}
	newSnap, err := consumer.ReadTeamRule(ctx, "team-index", "execute", "execute-rule", idx2.Commit)
	if err != nil {
		t.Fatalf("ReadTeamRule new commit: %v", err)
	}
	if newSnap.Rule.Body != "new body" || newSnap.Commit != idx2.Commit {
		t.Fatalf("new commit body = %+v, want new body at %s", newSnap, idx2.Commit)
	}
	if _, err := consumer.ReadTeamRule(ctx, "team-index", "review", "execute-rule", idx.Commit); !errors.Is(err, ErrTeamRuleNotFound) {
		t.Fatalf("wrong phase error = %v, want ErrTeamRuleNotFound", err)
	}
	if _, err := consumer.ReadTeamRule(ctx, "team-index", "execute", "execute-rule", strings.Repeat("f", 40)); !errors.Is(err, ErrRuleSnapshotNotFound) {
		t.Fatalf("missing commit error = %v, want ErrRuleSnapshotNotFound", err)
	}
}

func TestTeamRuleIndexBudgetsEnforcedOnWriteAndRead(t *testing.T) {
	s := NewStore(t.TempDir(), nil)
	if _, err := s.WriteRule(Rule{
		Slug:        "too-long",
		Description: strings.Repeat("x", RuleDescriptionMaxBytes+1),
		Enabled:     true,
		AppliesTo:   []string{"execute"},
	}); !errors.Is(err, ErrTeamRuleIndexTooLarge) {
		t.Fatalf("long description error = %v, want ErrTeamRuleIndexTooLarge", err)
	}

	dir := t.TempDir()
	store := NewStore(dir, nil)
	store.newID = mustDeterministicIDs(ruleIDs(65)...)
	for i := 0; i < RuleIndexMaxEntries+1; i++ {
		if _, err := store.WriteRule(Rule{
			Slug:        fmt.Sprintf("rule-%02d", i),
			Description: "read when needed",
			Body:        "body",
			Enabled:     true,
			AppliesTo:   []string{"execute"},
		}); err != nil {
			t.Fatalf("WriteRule %d: %v", i, err)
		}
	}
	if err := store.ValidateRuleIndexBudgets(); !errors.Is(err, ErrTeamRuleIndexTooLarge) {
		t.Fatalf("ValidateRuleIndexBudgets error = %v, want ErrTeamRuleIndexTooLarge", err)
	}

	host := NewHost(t.TempDir(), nil)
	if err := host.EnsureRepo(context.Background(), TeamRepo("team-budget")); err != nil {
		t.Fatal(err)
	}
	commitOversizedRulesWithoutValidation(t, host, "team-budget", RuleIndexMaxEntries+1)
	if _, err := NewTeamMemoryConsumer(host, nil).ReadTeamRuleIndex(context.Background(), "team-budget", "execute"); !errors.Is(err, ErrTeamRuleIndexTooLarge) {
		t.Fatalf("ReadTeamRuleIndex oversize error = %v, want ErrTeamRuleIndexTooLarge", err)
	}
}

func updateRuleBodyAtHEAD(t *testing.T, host *Host, teamID, sourcePath, oldBody, newBody string) {
	t.Helper()
	home := t.TempDir()
	bareDir, err := host.RepoDir(TeamRepo(teamID))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	runGit(t, home, work, "clone", bareDir, "wc")
	wc := filepath.Join(work, "wc")
	abs := filepath.Join(wc, filepath.FromSlash(sourcePath))
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(string(raw), oldBody, newBody, 1)
	if next == string(raw) {
		t.Fatalf("old body %q not found in %s", oldBody, sourcePath)
	}
	if err := os.WriteFile(abs, []byte(next), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(wc, nil, WithHomeOverride(home))
	if err := store.SyncPush(context.Background(), "origin", "main", testAuthor(), "update team rule body", 0); err != nil {
		t.Fatalf("SyncPush update: %v", err)
	}
}

func commitOversizedRulesWithoutValidation(t *testing.T, host *Host, teamID string, n int) {
	t.Helper()
	home := t.TempDir()
	bareDir, err := host.RepoDir(TeamRepo(teamID))
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	runGit(t, home, work, "clone", bareDir, "wc")
	wc := filepath.Join(work, "wc")
	if err := os.MkdirAll(filepath.Join(wc, rulesDir), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		fm := ruleFrontmatter{
			Name:        fmt.Sprintf("rule-%02d", i),
			Description: "read when needed",
			UUID:        fmt.Sprintf("id-%02d", i),
			Enabled:     true,
			AppliesTo:   []string{"execute"},
		}
		content, err := renderRule(fm, "body")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(wc, rulesDir, fmt.Sprintf("rule-%02d-id-%02d.md", i, i))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, home, wc, "add", "-A")
	runGit(t, home, wc, "-c", "commit.gpgsign=false", "commit", "-m", "oversized rules")
	runGit(t, home, wc, "push", "origin", "HEAD:main")
}

func ruleIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("id-%02d", i)
	}
	return out
}

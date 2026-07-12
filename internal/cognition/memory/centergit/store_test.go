package centergit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testAuthor() Author { return Author{Name: "Tester", Email: "tester@agent-center.local"} }

func TestWriteEntryNamingAndFrontmatter(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, nil, WithIDGen(mustDeterministicIDs("uuid1")))
	rel, err := s.WriteEntry(Entry{
		Slug:        "prefer-table-driven-tests",
		Title:       "Prefer table-driven tests",
		Description: "Use table-driven tests for matrix cases",
		Body:        "Body here.",
		Type:        "feedback",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "entries/prefer-table-driven-tests-uuid1.md"
	if rel != want {
		t.Fatalf("rel=%q want %q", rel, want)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	content := string(got)
	for _, must := range []string{
		"name: prefer-table-driven-tests",
		"description: Use table-driven tests for matrix cases",
		"uuid: uuid1",
		"type: feedback",
		"Body here.",
	} {
		if !strings.Contains(content, must) {
			t.Errorf("entry missing %q in:\n%s", must, content)
		}
	}
	if !strings.HasPrefix(content, "---\n") {
		t.Errorf("entry must start with frontmatter fence")
	}
}

func TestWriteEntryValidation(t *testing.T) {
	s := NewStore(t.TempDir(), nil)
	if _, err := s.WriteEntry(Entry{Slug: "", Description: "x"}); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("empty slug should be ErrInvalidEntry, got %v", err)
	}
	if _, err := s.WriteEntry(Entry{Slug: "../evil", Description: "x"}); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("traversal slug should be ErrInvalidEntry, got %v", err)
	}
	if _, err := s.WriteEntry(Entry{Slug: "ok", Description: "  "}); !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("blank description should be ErrInvalidEntry, got %v", err)
	}
}

func TestRegenerateIndexDeterministicAndDerived(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, nil, WithIDGen(mustDeterministicIDs("b1", "a1")))
	// Write out of alphabetical order; index must be sorted by name.
	if _, err := s.WriteEntry(Entry{Slug: "zeta", Description: "last one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteEntry(Entry{Slug: "alpha", Description: "first one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RegenerateIndex(); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		t.Fatal(err)
	}
	body := string(idx)
	if !strings.Contains(body, "do not edit by hand") {
		t.Errorf("index missing generated banner")
	}
	ai := strings.Index(body, "alpha")
	zi := strings.Index(body, "zeta")
	if ai < 0 || zi < 0 || ai > zi {
		t.Errorf("index not sorted by name (alpha before zeta):\n%s", body)
	}
	if !strings.Contains(body, "[alpha](entries/alpha-a1.md) — first one") {
		t.Errorf("index row not derived from entry frontmatter:\n%s", body)
	}

	// Regenerating again must be byte-identical (determinism).
	if err := s.RegenerateIndex(); err != nil {
		t.Fatal(err)
	}
	idx2, _ := os.ReadFile(filepath.Join(dir, indexFile))
	if string(idx2) != body {
		t.Errorf("RegenerateIndex not deterministic")
	}
}

func TestRegenerateIndexEmpty(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, nil)
	if err := s.RegenerateIndex(); err != nil {
		t.Fatal(err)
	}
	idx, _ := os.ReadFile(filepath.Join(dir, indexFile))
	if !strings.Contains(string(idx), "No entries yet") {
		t.Errorf("empty index should note no entries:\n%s", idx)
	}
}

func TestCommitRequiresAuthor(t *testing.T) {
	s := NewStore(t.TempDir(), nil)
	if err := s.Commit(context.Background(), Author{}, "msg"); err == nil {
		t.Fatal("expected author-required error")
	}
}

func TestSyncPushSingleWriter(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	root := t.TempDir()
	host := NewHost(root, nil)
	ref := TeamRepo("team-alpha")
	if err := host.EnsureRepo(ctx, ref); err != nil {
		t.Fatal(err)
	}
	bareDir, _ := host.RepoDir(ref)
	seedBare(t, home, bareDir)

	tmp := t.TempDir()
	runGit(t, home, tmp, "clone", bareDir, "wc")
	wc := filepath.Join(tmp, "wc")
	s := NewStore(wc, nil, WithHomeOverride(home), WithIDGen(mustDeterministicIDs("id1")))
	if _, err := s.WriteEntry(Entry{Slug: "lesson-a", Description: "lesson A"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncPush(ctx, "origin", "main", testAuthor(), "add lesson-a", 3); err != nil {
		t.Fatalf("SyncPush: %v", err)
	}

	// Fresh clone sees the entry + a derived index listing it.
	verify := t.TempDir()
	runGit(t, home, verify, "clone", bareDir, "check")
	check := filepath.Join(verify, "check")
	if _, err := os.Stat(filepath.Join(check, "entries", "lesson-a-id1.md")); err != nil {
		t.Fatalf("pushed entry missing: %v", err)
	}
	idx, _ := os.ReadFile(filepath.Join(check, indexFile))
	if !strings.Contains(string(idx), "lesson-a") {
		t.Fatalf("index not pushed/derived:\n%s", idx)
	}
}

// TestSyncPushConcurrentRetry drives two writers racing on one team repo: the
// second push is a non-fast-forward, so SyncPush must pull --rebase and retry,
// converging with BOTH entries and a derived index listing both (§9 并发写).
func TestSyncPushConcurrentRetry(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	root := t.TempDir()
	host := NewHost(root, nil)
	ref := TeamRepo("team-alpha")
	if err := host.EnsureRepo(ctx, ref); err != nil {
		t.Fatal(err)
	}
	bareDir, _ := host.RepoDir(ref)
	seedBare(t, home, bareDir)

	// Two independent clones sharing the seed base.
	base := t.TempDir()
	runGit(t, home, base, "clone", bareDir, "wc1")
	runGit(t, home, base, "clone", bareDir, "wc2")
	wc1 := filepath.Join(base, "wc1")
	wc2 := filepath.Join(base, "wc2")

	s1 := NewStore(wc1, nil, WithHomeOverride(home), WithIDGen(mustDeterministicIDs("id-a")))
	s2 := NewStore(wc2, nil, WithHomeOverride(home), WithIDGen(mustDeterministicIDs("id-b")))

	if _, err := s1.WriteEntry(Entry{Slug: "lesson-a", Description: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.WriteEntry(Entry{Slug: "lesson-b", Description: "B"}); err != nil {
		t.Fatal(err)
	}
	// Writer 1 lands first.
	if err := s1.SyncPush(ctx, "origin", "main", testAuthor(), "add A", 3); err != nil {
		t.Fatalf("s1 SyncPush: %v", err)
	}
	// Writer 2 is now behind → must rebase+retry and still succeed.
	if err := s2.SyncPush(ctx, "origin", "main", testAuthor(), "add B", 3); err != nil {
		t.Fatalf("s2 SyncPush (retry path): %v", err)
	}

	verify := t.TempDir()
	runGit(t, home, verify, "clone", bareDir, "check")
	check := filepath.Join(verify, "check")
	for _, f := range []string{"entries/lesson-a-id-a.md", "entries/lesson-b-id-b.md"} {
		if _, err := os.Stat(filepath.Join(check, filepath.FromSlash(f))); err != nil {
			t.Fatalf("converged repo missing %s: %v", f, err)
		}
	}
	idx, _ := os.ReadFile(filepath.Join(check, indexFile))
	body := string(idx)
	if !strings.Contains(body, "lesson-a") || !strings.Contains(body, "lesson-b") {
		t.Fatalf("derived index must list both entries after concurrent write:\n%s", body)
	}
}

func TestSyncPushRetriesExhausted(t *testing.T) {
	// maxRetries=0 with a guaranteed non-FF must surface ErrPushRetriesExhausted.
	ctx := context.Background()
	home := t.TempDir()
	root := t.TempDir()
	host := NewHost(root, nil)
	ref := TeamRepo("team-x")
	if err := host.EnsureRepo(ctx, ref); err != nil {
		t.Fatal(err)
	}
	bareDir, _ := host.RepoDir(ref)
	seedBare(t, home, bareDir)

	base := t.TempDir()
	runGit(t, home, base, "clone", bareDir, "wc1")
	runGit(t, home, base, "clone", bareDir, "wc2")
	wc1 := filepath.Join(base, "wc1")
	wc2 := filepath.Join(base, "wc2")
	s1 := NewStore(wc1, nil, WithHomeOverride(home), WithIDGen(mustDeterministicIDs("id-a")))
	s2 := NewStore(wc2, nil, WithHomeOverride(home), WithIDGen(mustDeterministicIDs("id-b")))
	if _, err := s1.WriteEntry(Entry{Slug: "a", Description: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.WriteEntry(Entry{Slug: "b", Description: "B"}); err != nil {
		t.Fatal(err)
	}
	if err := s1.SyncPush(ctx, "origin", "main", testAuthor(), "A", 3); err != nil {
		t.Fatal(err)
	}
	err := s2.SyncPush(ctx, "origin", "main", testAuthor(), "B", 0)
	if !errors.Is(err, ErrPushRetriesExhausted) {
		t.Fatalf("want ErrPushRetriesExhausted, got %v", err)
	}
}

package centergit

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAuthErrorBranches(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{ErrUnauthenticated, 401},
		{ErrForbidden, 403},
		{errors.New("boom"), 500},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeAuthError(rec, c.err)
		if rec.Code != c.want {
			t.Errorf("writeAuthError(%v) → %d want %d", c.err, rec.Code, c.want)
		}
	}
}

func TestParseFrontmatterErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	if _, err := parseFrontmatter(filepath.Join(dir, "nope.md")); err == nil {
		t.Error("expected read error for missing file")
	}
	if _, err := parseFrontmatter(write("nofm.md", "no frontmatter here")); err == nil {
		t.Error("expected error for missing frontmatter")
	}
	if _, err := parseFrontmatter(write("open.md", "---\nname: x\n")); err == nil {
		t.Error("expected error for unterminated frontmatter")
	}
	if _, err := parseFrontmatter(write("bad.md", "---\n: : broken\n---\n")); err == nil {
		t.Error("expected yaml error")
	}
	// Valid one parses.
	fm, err := parseFrontmatter(write("ok.md", "---\nname: good\ndescription: d\n---\nbody\n"))
	if err != nil || fm.Name != "good" {
		t.Fatalf("valid frontmatter failed: fm=%+v err=%v", fm, err)
	}
}

func TestListEntriesPropagatesParseError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, entriesDir), 0o700); err != nil {
		t.Fatal(err)
	}
	// A malformed entry file makes ListEntries (and thus RegenerateIndex) fail.
	if err := os.WriteFile(filepath.Join(dir, entriesDir, "bad.md"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir, nil)
	if _, err := s.ListEntries(); err == nil {
		t.Fatal("expected ListEntries error on malformed entry")
	}
	if err := s.RegenerateIndex(); err == nil {
		t.Fatal("expected RegenerateIndex error on malformed entry")
	}
}

func TestListEntriesIgnoresNonMarkdownAndDirs(t *testing.T) {
	dir := t.TempDir()
	ed := filepath.Join(dir, entriesDir)
	if err := os.MkdirAll(filepath.Join(ed, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ed, "notes.txt"), []byte("ignore me"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir, nil)
	rows, err := s.ListEntries()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected non-.md and dirs ignored, got %d rows", len(rows))
	}
}

func TestCommitErrorOnNonRepo(t *testing.T) {
	s := NewStore(t.TempDir(), nil) // not a git repo
	err := s.Commit(context.Background(), testAuthor(), "msg")
	if err == nil || !errors.Is(err, ErrGitOpFailed) {
		t.Fatalf("want ErrGitOpFailed on non-repo, got %v", err)
	}
}

func TestSyncPushSurfacesNonRetryableError(t *testing.T) {
	// A push to a non-existent remote fails with a non-"non-fast-forward" error,
	// which SyncPush must surface directly (not loop).
	ctx := context.Background()
	home := t.TempDir()
	root := t.TempDir()
	host := NewHost(root, nil)
	ref := TeamRepo("team-z")
	if err := host.EnsureRepo(ctx, ref); err != nil {
		t.Fatal(err)
	}
	bareDir, _ := host.RepoDir(ref)
	seedBare(t, home, bareDir)

	tmp := t.TempDir()
	runGit(t, home, tmp, "clone", bareDir, "wc")
	wc := filepath.Join(tmp, "wc")
	s := NewStore(wc, nil, WithHomeOverride(home), WithIDGen(mustDeterministicIDs("id1")))
	if _, err := s.WriteEntry(Entry{Slug: "x", Description: "X"}); err != nil {
		t.Fatal(err)
	}
	err := s.SyncPush(ctx, "no-such-remote", "main", testAuthor(), "x", 3)
	if err == nil || errors.Is(err, ErrPushRetriesExhausted) {
		t.Fatalf("want a raw git error, got %v", err)
	}
	if !errors.Is(err, ErrGitOpFailed) {
		t.Fatalf("want ErrGitOpFailed, got %v", err)
	}
}

func TestSyncPushRejectsBadAuthorAndNegativeRetries(t *testing.T) {
	s := NewStore(t.TempDir(), nil)
	if err := s.SyncPush(context.Background(), "origin", "main", Author{}, "m", -5); err == nil {
		t.Fatal("expected author-required error")
	}
}

func TestIsNonFastForward(t *testing.T) {
	yes := []string{
		"! [rejected] main -> main (non-fast-forward)",
		"Updates were rejected because the tip of your current branch is behind",
		"hint: fetch first",
		"error: failed to push some refs; [rejected] (would-be push)",
	}
	for _, s := range yes {
		if !isNonFastForward(s) {
			t.Errorf("expected non-FF true for %q", s)
		}
	}
	if isNonFastForward("fatal: repository not found") {
		t.Error("unrelated error must not be treated as non-FF")
	}
}

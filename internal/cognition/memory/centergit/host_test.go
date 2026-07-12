package centergit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestHostEnsureRepoIdempotentAndBare(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	host := NewHost(root, nil)

	refs := []RepoRef{AgentRepo("agent-1"), TeamRepo("team-alpha"), GlobalRepo()}
	for _, ref := range refs {
		if exists, err := host.RepoExists(ref); err != nil || exists {
			t.Fatalf("%s: expected not-exists before provision (exists=%v err=%v)", ref, exists, err)
		}
		if err := host.EnsureRepo(ctx, ref); err != nil {
			t.Fatalf("%s: EnsureRepo: %v", ref, err)
		}
		// idempotent second call
		if err := host.EnsureRepo(ctx, ref); err != nil {
			t.Fatalf("%s: EnsureRepo (again): %v", ref, err)
		}
		exists, err := host.RepoExists(ref)
		if err != nil || !exists {
			t.Fatalf("%s: expected exists after provision (exists=%v err=%v)", ref, exists, err)
		}
		dir, err := host.RepoDir(ref)
		if err != nil {
			t.Fatalf("RepoDir: %v", err)
		}
		// It is a bare repo: HEAD file present, no working tree (.git absent).
		if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
			t.Fatalf("%s: bare HEAD missing: %v", ref, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
			t.Fatalf("%s: expected bare (no .git), stat err=%v", ref, err)
		}
		// receivepack enabled so push over http works
		out := runGit(t, root, dir, "config", "--get", "http.receivepack")
		if out == "" {
			t.Fatalf("%s: http.receivepack not set", ref)
		}
	}
}

func TestHostRepoDirValidation(t *testing.T) {
	host := NewHost(t.TempDir(), nil)
	if _, err := host.RepoDir(AgentRepo("../evil")); err == nil {
		t.Fatal("expected validation error for traversal id")
	}
	empty := NewHost("", nil)
	if _, err := empty.RepoDir(AgentRepo("a")); err != ErrHostRootEmpty {
		t.Fatalf("want ErrHostRootEmpty, got %v", err)
	}
}

func TestHostRepoExistsInvalid(t *testing.T) {
	host := NewHost(t.TempDir(), nil)
	if _, err := host.RepoExists(RepoRef{Kind: "bogus", ID: "x"}); err == nil {
		t.Fatal("expected error for invalid ref")
	}
}

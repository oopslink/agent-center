package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeManualDelivery_PushedAheadOfBase(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	repo := filepath.Join(root, "repo")
	mustGit(t, ctx, root, "init", "--bare", remote)
	mustGit(t, ctx, root, "init", repo)
	mustGit(t, ctx, repo, "config", "user.email", "agent@example.test")
	mustGit(t, ctx, repo, "config", "user.name", "Agent Center Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, repo, "add", "README.md")
	mustGit(t, ctx, repo, "commit", "-m", "base")
	mustGit(t, ctx, repo, "branch", "-M", "main")
	mustGit(t, ctx, repo, "remote", "add", "origin", remote)
	mustGit(t, ctx, repo, "push", "-u", "origin", "main")
	mustGit(t, ctx, repo, "checkout", "-b", "feat/recover")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\nchange\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, repo, "add", "README.md")
	mustGit(t, ctx, repo, "commit", "-m", "change")
	head := mustGit(t, ctx, repo, "rev-parse", "HEAD")
	mustGit(t, ctx, repo, "push", "origin", "HEAD:refs/heads/feat/recover")

	got, err := probeManualDelivery(ctx, repo, "feat/recover", trimNL(head), "origin/main", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if got["pushed"] != true || got["base_known"] != true || got["ahead_of_base"] != 1 {
		t.Fatalf("delivery probe = %+v", got)
	}
	if got["head_sha"] != trimNL(head) || got["branch"] != "feat/recover" {
		t.Fatalf("delivery identity = %+v", got)
	}
}

func mustGit(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	out, err := gitOutput(ctx, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

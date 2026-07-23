package reporepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func cacheTestRemote(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	remote := filepath.Join(root, "remote.git")
	runGitTest(t, root, "init", "--bare", remote)
	runGitTest(t, root, "init", "-b", "main", work)
	runGitTest(t, work, "config", "user.email", "test@example.invalid")
	runGitTest(t, work, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, work, "add", "README.md")
	runGitTest(t, work, "commit", "-m", "initial")
	runGitTest(t, work, "remote", "add", "origin", remote)
	runGitTest(t, work, "push", "-u", "origin", "main")
	runGitTest(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	return remote
}

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestRepoCacheManager_ReusesMirrorAndTracksWorktreeLifecycle(t *testing.T) {
	ctx := context.Background()
	remote := cacheTestRemote(t)
	m, err := NewRepoCacheManager(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	target := RepoTarget{RepoID: "repo-1", URL: remote, DefaultBranch: "main"}
	coldStarted := time.Now()
	first, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	coldDuration := time.Since(coldStarted)
	warmStarted := time.Now()
	second, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	warmDuration := time.Since(warmStarted)
	t.Logf("materialization timing: cold_mirror=%s warm_reuse=%s", coldDuration, warmDuration)
	if first.Path != second.Path || filepath.Base(first.Path) != "mirror.git" {
		t.Fatalf("mirror was not reused: first=%q second=%q", first.Path, second.Path)
	}

	health, err := m.Health(second.RepoKey)
	if err != nil || health.MirrorPath != second.Path || health.Stale {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	wt, err := m.CreateWorktree(ctx, second, WorktreeRequest{
		ExecutorID: "exec-1",
		TaskID:     "task-1",
		BranchName: "ac-exec/task-1/exec-1",
		BaseRef:    "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(m.runtimeRoot, "worktrees", "exec-1")
	if wt.WorkspacePath != wantPath {
		t.Fatalf("workspace=%q want=%q", wt.WorkspacePath, wantPath)
	}
	rec, err := m.readWorktreeRecord("exec-1")
	if err != nil || rec.Status != "active" || rec.Ref != wt.BaseRef {
		t.Fatalf("active registry record=%+v err=%v", rec, err)
	}
	if err := m.RemoveWorktree(ctx, wt); err != nil {
		t.Fatal(err)
	}
	rec, err = m.readWorktreeRecord("exec-1")
	if err != nil || rec.Status != "cleaned" || rec.CleanedAt.IsZero() {
		t.Fatalf("cleaned registry record=%+v err=%v", rec, err)
	}
	records, err := m.Worktrees()
	if err != nil || len(records) != 1 || records[0].Status != "cleaned" {
		t.Fatalf("worktree audit=%+v err=%v", records, err)
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}

func TestRepoCacheManager_ConcurrentEnsureUsesOneValidMirror(t *testing.T) {
	ctx := context.Background()
	remote := cacheTestRemote(t)
	root := t.TempDir()
	const workers = 8
	errs := make(chan error, workers)
	paths := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := NewRepoCacheManager(root, nil, nil)
			if err != nil {
				errs <- err
				return
			}
			src, err := m.EnsureSource(ctx, RepoTarget{URL: remote, DefaultBranch: "main"})
			errs <- err
			paths <- src.Path
		}()
	}
	wg.Wait()
	close(errs)
	close(paths)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var mirror string
	for path := range paths {
		if mirror == "" {
			mirror = path
		}
		if path != mirror {
			t.Fatalf("different mirror paths: %q != %q", path, mirror)
		}
	}
	if got := runGitTest(t, mirror, "rev-parse", "--is-bare-repository"); got != "true\n" {
		t.Fatalf("mirror is not bare: %q", got)
	}
}

func TestRepoCacheManager_OfflineUsesCachedRefAndRejectsMissingRef(t *testing.T) {
	ctx := context.Background()
	remote := cacheTestRemote(t)
	m, err := NewRepoCacheManager(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.fetchTTL = time.Nanosecond
	target := RepoTarget{URL: remote, DefaultBranch: "main"}
	if _, err := m.EnsureSource(ctx, target); err != nil {
		t.Fatal(err)
	}
	offline := remote + ".offline"
	if err := os.Rename(remote, offline); err != nil {
		t.Fatal(err)
	}
	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("cached main should remain usable: %v", err)
	}
	if !src.Stale {
		t.Fatal("offline cached source was not marked stale")
	}
	_, err = m.EnsureSource(ctx, RepoTarget{URL: remote, DefaultBranch: "missing"})
	if !errors.Is(err, ErrCacheRefUnavailable) {
		t.Fatalf("missing cached ref error=%v want ErrCacheRefUnavailable", err)
	}
}

func TestRepoCacheManager_ReconcileRemovesOnlyOrphans(t *testing.T) {
	ctx := context.Background()
	remote := cacheTestRemote(t)
	m, err := NewRepoCacheManager(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	src, err := m.EnsureSource(ctx, RepoTarget{URL: remote, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"live", "orphan"} {
		if _, err := m.PrepareWorktree(ctx, src, WorktreeRequest{
			ExecutorID: id,
			TaskID:     id,
			BranchName: "ac-exec/" + id + "/" + id,
			BaseRef:    "main",
		}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := m.ReapOrphanWorktrees(ctx, func(id string) bool { return id == "live" })
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reaped=%d want=1", n)
	}
	if _, err := os.Stat(filepath.Join(m.worktreeRoot, "live")); err != nil {
		t.Fatalf("live worktree removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.worktreeRoot, "orphan")); !os.IsNotExist(err) {
		t.Fatalf("orphan worktree remains: %v", err)
	}
}

func TestRepoCacheManager_ReconcileDoesNotRemoveAnotherOwnerWorktree(t *testing.T) {
	ctx := context.Background()
	remote := cacheTestRemote(t)
	root := t.TempDir()
	ownerA, err := NewRepoCacheManager(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerA.SetOwner("agent-a")
	ownerB, err := NewRepoCacheManager(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerB.SetOwner("agent-b")
	src, err := ownerB.EnsureSource(ctx, RepoTarget{URL: remote, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ownerB.CreateWorktree(ctx, src, WorktreeRequest{
		ExecutorID: "exec-b",
		TaskID:     "task-b",
		BranchName: "ac-exec/task-b/exec-b",
		BaseRef:    "main",
	}); err != nil {
		t.Fatal(err)
	}
	n, err := ownerA.ReapOrphanWorktrees(ctx, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("owner A reaped %d owner B worktrees", n)
	}
	if _, err := os.Stat(filepath.Join(root, "worktrees", "exec-b")); err != nil {
		t.Fatalf("owner B worktree was removed: %v", err)
	}
}

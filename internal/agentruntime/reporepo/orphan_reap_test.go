package reporepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/clock"
)

// TestOrphanExecID_DoubleParseFailSafe pins the hard fail-safe (v2.31.1): an executor
// id is returned ONLY when it parses from BOTH the workspace path AND the branch and the
// two AGREE; any mismatch / missing side → "" so the caller KEEPS the worktree (never
// reap a worktree we can't positively tie to one dead executor).
func TestOrphanExecID_DoubleParseFailSafe(t *testing.T) {
	cases := []struct {
		name, path, branch, want string
	}{
		{"agree", "/home/a/executors/exec-123/workspace", "ac-exec/task-9/exec-123", "exec-123"},
		{"path-only (detached, no branch)", "/home/a/executors/exec-123/workspace", "", ""},
		{"branch-only (foreign path)", "/tmp/random/workspace", "ac-exec/task-9/exec-123", ""},
		{"mismatch", "/home/a/executors/exec-AAA/workspace", "ac-exec/task-9/exec-BBB", ""},
		{"not an ac-exec branch", "/home/a/executors/exec-1/workspace", "feature/x", ""},
		{"not an executors path", "/home/a/other/exec-1/workspace", "ac-exec/t/exec-1", ""},
		{"not a workspace leaf", "/home/a/executors/exec-1/other", "ac-exec/t/exec-1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := orphanExecID(tc.path, tc.branch); got != tc.want {
				t.Fatalf("orphanExecID(%q,%q) = %q, want %q", tc.path, tc.branch, got, tc.want)
			}
		})
	}
}

// TestParseWorktreeList pins the porcelain parser (path + short branch; detached → no branch).
func TestParseWorktreeList(t *testing.T) {
	out := "worktree /r/source\nHEAD abc\nbranch refs/heads/main\n\n" +
		"worktree /r/executors/exec-1/workspace\nHEAD def\nbranch refs/heads/ac-exec/t1/exec-1\n\n" +
		"worktree /r/detached\nHEAD 999\ndetached\n"
	got := parseWorktreeList(out)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %+v", len(got), got)
	}
	if got[0].path != "/r/source" || got[0].branch != "main" {
		t.Fatalf("entry0 = %+v", got[0])
	}
	if got[1].branch != "ac-exec/t1/exec-1" {
		t.Fatalf("entry1 branch = %q", got[1].branch)
	}
	if got[2].branch != "" {
		t.Fatalf("detached entry must have empty branch, got %q", got[2].branch)
	}
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestPruneOrphanWorktrees_RealGit is the end-to-end reap with the real git binary: a
// dead executor's worktree + stale branch are removed while a LIVE executor's worktree
// and the canonical source survive (hard constraints ①②).
func TestPruneOrphanWorktrees_RealGit(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	root := t.TempDir()
	key := "repo-key-1"
	sourcePath := filepath.Join(root, key, "source")
	if err := os.MkdirAll(sourcePath, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real source repo with one commit on master so worktrees can branch off it.
	runGit(t, sourcePath, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(sourcePath, "MARKER.txt"), []byte("canonical"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, sourcePath, "add", "-A")
	runGit(t, sourcePath, "commit", "-q", "-m", "init")

	m, err := NewLocalGitMaterializer(root, executor.NewExecGitRunner(), clock.SystemClock{})
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	src := SourceRepo{RepoKey: key, Path: sourcePath, BaseRef: "master"}
	ctx := context.Background()

	// Two per-executor worktrees: exec-dead (will be orphaned) + exec-live (kept).
	execRoot := filepath.Join(root, "agent")
	mkWT := func(id string) PreparedWorktree {
		wt, werr := m.PrepareWorktree(ctx, src, WorktreeRequest{
			ExecutorID:    id,
			TaskID:        "task-" + id,
			BranchName:    "ac-exec/task-" + id + "/" + id,
			WorkspacePath: filepath.Join(execRoot, "executors", id, "workspace"),
			BaseRef:       "master",
		})
		if werr != nil {
			t.Fatalf("PrepareWorktree(%s): %v", id, werr)
		}
		return wt
	}
	dead := mkWT("exec-dead")
	live := mkWT("exec-live")

	// Both worktrees + branches exist now.
	if list := runGit(t, sourcePath, "worktree", "list", "--porcelain"); !strings.Contains(list, dead.WorkspacePath) || !strings.Contains(list, live.WorkspacePath) {
		t.Fatalf("pre-prune: both worktrees must be listed:\n%s", list)
	}

	// isLive: only exec-live is live → exec-dead is the orphan.
	pruned, perr := m.PruneOrphanWorktrees(ctx, src, func(id string) bool { return id == "exec-live" })
	if perr != nil {
		t.Fatalf("PruneOrphanWorktrees: %v", perr)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1 (only exec-dead)", pruned)
	}

	list := runGit(t, sourcePath, "worktree", "list", "--porcelain")
	if strings.Contains(list, dead.WorkspacePath) {
		t.Fatalf("dead worktree must be reaped, still listed:\n%s", list)
	}
	if !strings.Contains(list, live.WorkspacePath) {
		t.Fatalf("LIVE worktree must be KEPT (fail-safe), missing:\n%s", list)
	}
	if !strings.Contains(list, filepath.Clean(sourcePath)) {
		t.Fatalf("canonical source MAIN worktree must survive:\n%s", list)
	}
	// The dead executor's stale branch is gone; the live one's remains.
	branches := runGit(t, sourcePath, "branch", "--list")
	if strings.Contains(branches, "ac-exec/task-exec-dead/exec-dead") {
		t.Fatalf("dead stale branch must be deleted:\n%s", branches)
	}
	if !strings.Contains(branches, "ac-exec/task-exec-live/exec-live") {
		t.Fatalf("live branch must remain:\n%s", branches)
	}
	// Canonical source content is untouched (hard constraint ①).
	if b, rerr := os.ReadFile(filepath.Join(sourcePath, "MARKER.txt")); rerr != nil || string(b) != "canonical" {
		t.Fatalf("canonical source MARKER must survive intact: %q err=%v", string(b), rerr)
	}
	// The dead worktree dir is gone; the live one's dir remains.
	if _, serr := os.Stat(dead.WorkspacePath); !os.IsNotExist(serr) {
		t.Fatalf("dead worktree dir must be removed, stat err=%v", serr)
	}
	if _, serr := os.Stat(live.WorkspacePath); serr != nil {
		t.Fatalf("live worktree dir must remain: %v", serr)
	}
}

// TestReapOrphanWorktrees_RealGit_MultiRepo pins the boot sweep across EVERY materialized
// source under the repos root: each repo's orphan is reaped, each canonical source survives.
func TestReapOrphanWorktrees_RealGit_MultiRepo(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	root := t.TempDir()
	m, err := NewLocalGitMaterializer(root, executor.NewExecGitRunner(), clock.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	execRoot := filepath.Join(root, "agent")
	orphans := map[string]string{} // key → orphan workspace path
	for _, key := range []string{"repo-A", "repo-B"} {
		sp := filepath.Join(root, key, "source")
		if err := os.MkdirAll(sp, 0o755); err != nil {
			t.Fatal(err)
		}
		runGit(t, sp, "init", "-q", "-b", "master")
		if err := os.WriteFile(filepath.Join(sp, "F"), []byte(key), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, sp, "add", "-A")
		runGit(t, sp, "commit", "-q", "-m", "init")
		id := "exec-" + key
		ws := filepath.Join(execRoot, "executors", id, "workspace")
		if _, werr := m.PrepareWorktree(context.Background(), SourceRepo{RepoKey: key, Path: sp, BaseRef: "master"},
			WorktreeRequest{ExecutorID: id, TaskID: "t", BranchName: "ac-exec/t/" + id, WorkspacePath: ws, BaseRef: "master"}); werr != nil {
			t.Fatalf("prepare %s: %v", key, werr)
		}
		orphans[key] = ws
	}

	// Nothing is live → both orphans reaped.
	n, err := m.ReapOrphanWorktrees(context.Background(), func(string) bool { return false })
	if err != nil {
		t.Fatalf("ReapOrphanWorktrees: %v", err)
	}
	if n != 2 {
		t.Fatalf("reaped = %d, want 2 (one per repo)", n)
	}
	for key, ws := range orphans {
		if _, serr := os.Stat(ws); !os.IsNotExist(serr) {
			t.Fatalf("%s orphan worktree must be reaped, stat err=%v", key, serr)
		}
		if _, serr := os.Stat(filepath.Join(root, key, "source", "F")); serr != nil {
			t.Fatalf("%s canonical source must survive: %v", key, serr)
		}
	}
}

// TestPruneOrphanWorktrees_NilIsLiveReapsNothing pins the nil-oracle fail-safe.
func TestPruneOrphanWorktrees_NilIsLiveReapsNothing(t *testing.T) {
	m := &LocalGitMaterializer{}
	n, err := m.PruneOrphanWorktrees(context.Background(), SourceRepo{RepoKey: "k", Path: "/x"}, nil)
	if err != nil || n != 0 {
		t.Fatalf("nil isLive must reap nothing without error, got n=%d err=%v", n, err)
	}
}

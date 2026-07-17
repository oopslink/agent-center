package reporepo

// base_advance_test.go — I112 regression lock: after a fetch, a NEW worktree must be cut
// from origin's CURRENT tip, and its recorded BaseRef must be the real SHA it was cut from.
//
// These run against REAL git, on the REAL EnsureSource → PrepareWorktree path, deliberately:
// the bug was never in a pure function. resolvedBaseRef() was correct — it returned "main",
// exactly as designed — but "main" is a LOCAL branch name, and in a canonical source that is
// only ever `fetch --prune`ed, the local branch is frozen at the first clone's tip forever
// while origin/main runs away from it. The defect lives entirely in the gap between the two,
// so a test that stubs git, or that asserts on resolvedBaseRef()'s return, reproduces
// nothing: the pre-fix code passes it. Only real refs that really diverge can fail these.
// (Same shape as I107, where taskCancelEvidence was correct but was never called.)

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// advanceOrigin commits a new file on the remote's main and returns the new tip sha.
func advanceOrigin(t *testing.T, remote, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(remote, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	runGit(t, remote, "add", "-A")
	runGit(t, remote, "commit", "-q", "-m", "advance "+name)
	return strings.TrimSpace(runGit(t, remote, "rev-parse", "HEAD"))
}

// TestPrepareWorktree_RealGit_BaseFollowsOriginAfterFetch is THE lock on the reported bug,
// walked exactly as prod does: clone at A, origin moves to B, EnsureSource (fetch), spawn.
//
// Pre-fix this fails at the last assert with base=A: the fetch advanced refs/remotes/origin/
// main to B, nothing ever moved refs/heads/main off A, and PrepareWorktree branched off the
// name "main". In prod that gap was 4 commits and growing without bound — every executor was
// handed the tip of whenever the worker first cloned the repo.
func TestPrepareWorktree_RealGit_BaseFollowsOriginAfterFetch(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	m, _ := realGitMat(t)
	remote := makeRealRemote(t)
	shaA := strings.TrimSpace(runGit(t, remote, "rev-parse", "HEAD"))

	// First materialization: the canonical source is cloned with main at A.
	target := RepoTarget{URL: remote, DefaultBranch: "main"}
	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("EnsureSource (clone): %v", err)
	}

	// Origin moves on — someone lands work while this source sits on disk.
	shaB := advanceOrigin(t, remote, "b.txt")
	if shaA == shaB {
		t.Fatal("origin did not actually advance — test is not exercising the bug")
	}

	// Second materialization: this is the fetch path.
	src, err = m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("EnsureSource (fetch): %v", err)
	}

	// The source's LOCAL main is still at A and this fix does not touch it (moving it would
	// desync the shared source's own checked-out worktree). Asserting it stays stale pins
	// that the fix works by READING THROUGH origin/*, not by racing to correct local refs.
	if localMain := strings.TrimSpace(runGit(t, src.Path, "rev-parse", "refs/heads/main")); localMain != shaA {
		t.Fatalf("source local main = %s, want it left untouched at %s", localMain, shaA)
	}

	ws := filepath.Join(t.TempDir(), "executors", "exec-1", "workspace")
	wt, err := m.PrepareWorktree(ctx, src, WorktreeRequest{
		ExecutorID:    "exec-1",
		TaskID:        "task-1",
		BranchName:    "ac-exec/task-1/exec-1",
		WorkspacePath: ws,
	})
	if err != nil {
		t.Fatalf("PrepareWorktree: %v", err)
	}

	// The lock: the executor's worktree starts at origin's CURRENT tip, not the clone's.
	head := strings.TrimSpace(runGit(t, ws, "rev-parse", "HEAD"))
	if head != shaB {
		t.Fatalf("worktree HEAD = %s, want origin tip %s (got clone-time tip %s ⇒ base never advanced)",
			head, shaB, shaA)
	}
	// And it must carry origin's commit, not merely point at the right sha.
	if _, err := os.Stat(filepath.Join(ws, "b.txt")); err != nil {
		t.Fatalf("worktree missing b.txt from origin tip: %v", err)
	}
	// BaseRef is the REAL sha cut from — never the name "main", never a moving ref.
	if wt.BaseRef != shaB {
		t.Fatalf("BaseRef = %q, want the pinned cut-time sha %s", wt.BaseRef, shaB)
	}
}

// TestPrepareWorktree_RealGit_BaseRefCountsOnlyThisExecutorsCommits pins commandment 3: the
// recorded BaseRef feeds `rev-list --count <base>..HEAD`, whose value gates the eager-push
// (BaseKnown && ahead<=0 && !Dirty ⇒ silent skip ⇒ committed work dies with the reaped
// worktree — issue-f30b7e7b). So the count must be exactly this executor's own commits.
//
// HONEST RED NOTE — this one does NOT fail on pre-fix main, and that is the point of it.
// Pre-fix the two halves were consistently stale (cut from local main=A, base name resolving
// to that same A), so the count came out right by accident. It fails on the HALF-FIX — cut
// from origin's tip but still record the NAME "main" — which is the tempting, obvious repair
// and is measurably worse than the bug: verified, it reports ahead=2 for one real commit,
// because base "main" still resolves to the frozen local branch while HEAD now starts at
// origin's tip. That is the "报告写 ahead 5 实际 1" shape. So this test exists to stop the
// fix from being half-applied — the sha pinning is not decoration on the origin/* read, it is
// what makes the origin/* read safe.
//
// The other direction the pinning guards, which no test can provoke without a racing remote:
// storing the moving ref "origin/main" would look equally fresh, but the base would then
// SLIDE FORWARD under a running executor as others land commits, driving ahead toward 0 —
// and 0 is precisely the value that SILENTLY SKIPS the push and loses the delivery. A sha
// cannot move; that is why one is recorded.
func TestPrepareWorktree_RealGit_BaseRefCountsOnlyThisExecutorsCommits(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	m, _ := realGitMat(t)
	remote := makeRealRemote(t)
	target := RepoTarget{URL: remote, DefaultBranch: "main"}

	if _, err := m.EnsureSource(ctx, target); err != nil {
		t.Fatalf("EnsureSource (clone): %v", err)
	}
	advanceOrigin(t, remote, "b.txt") // origin moves before this executor spawns
	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("EnsureSource (fetch): %v", err)
	}

	ws := filepath.Join(t.TempDir(), "executors", "exec-1", "workspace")
	wt, err := m.PrepareWorktree(ctx, src, WorktreeRequest{
		ExecutorID:    "exec-1",
		TaskID:        "task-1",
		BranchName:    "ac-exec/task-1/exec-1",
		WorkspacePath: ws,
	})
	if err != nil {
		t.Fatalf("PrepareWorktree: %v", err)
	}

	// This executor does its work: exactly ONE commit.
	if err := os.WriteFile(filepath.Join(ws, "mine.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, ws, "add", "-A")
	runGit(t, ws, "commit", "-q", "-m", "my one commit")

	// ...while the rest of the world lands two more commits on origin and the source refetches
	// (another executor spawning for a different task is enough to trigger this).
	advanceOrigin(t, remote, "c.txt")
	advanceOrigin(t, remote, "d.txt")
	if _, err := m.EnsureSource(ctx, target); err != nil {
		t.Fatalf("EnsureSource (refetch): %v", err)
	}

	// Measure exactly as probeGitStatus does, from inside the worktree, using the durable
	// BaseRef — the real consumer path, not a re-derivation.
	out := strings.TrimSpace(runGit(t, ws, "rev-list", "--count", wt.BaseRef+"..HEAD"))
	if out != "1" {
		t.Fatalf("ahead_of_base = %s, want exactly 1 (this executor's own commit); "+
			">1 ⇒ crediting others' commits, 0 ⇒ eager-push silently skips and the delivery is lost", out)
	}
}

// TestPrepareWorktree_RealGit_AheadIsExactWhenExecutorRebasesOntoOrigin reproduces the
// "ahead 5 实际 1" report from the field, and is red on pre-fix main.
//
// It models what an executor handed a stale base actually DOES: notice it is behind, and
// rebase onto origin/main to get onto the real base. Pre-fix that produced HEAD = origin tip
// + its own 1 commit, while the recorded base "main" still pointed at the frozen clone-time
// tip — so ahead counted everyone else's commits too. The delivery report was arithmetically
// truthful and completely meaningless.
//
// Post-fix the rebase is a NO-OP (the worktree already starts at origin's tip) and the pinned
// base makes the count exact. That is the real repair: executors stop having to hand-correct
// their own base, which is the maneuver that broke the count in the first place.
func TestPrepareWorktree_RealGit_AheadIsExactWhenExecutorRebasesOntoOrigin(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	m, _ := realGitMat(t)
	remote := makeRealRemote(t)
	target := RepoTarget{URL: remote, DefaultBranch: "main"}

	if _, err := m.EnsureSource(ctx, target); err != nil {
		t.Fatalf("EnsureSource (clone): %v", err)
	}
	// Origin gains two commits between the clone and this executor's spawn.
	advanceOrigin(t, remote, "b.txt")
	advanceOrigin(t, remote, "c.txt")
	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("EnsureSource (fetch): %v", err)
	}

	ws := filepath.Join(t.TempDir(), "executors", "exec-1", "workspace")
	wt, err := m.PrepareWorktree(ctx, src, WorktreeRequest{
		ExecutorID:    "exec-1",
		TaskID:        "task-1",
		BranchName:    "ac-exec/task-1/exec-1",
		WorkspacePath: ws,
	})
	if err != nil {
		t.Fatalf("PrepareWorktree: %v", err)
	}

	// The executor does its one commit, then rebases onto origin/main to make sure it is
	// building on the real base (post-fix: already true, so this is a no-op).
	if err := os.WriteFile(filepath.Join(ws, "mine.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, ws, "add", "-A")
	runGit(t, ws, "commit", "-q", "-m", "my one commit")
	runGit(t, ws, "rebase", "-q", "refs/remotes/origin/main")

	out := strings.TrimSpace(runGit(t, ws, "rev-list", "--count", wt.BaseRef+"..HEAD"))
	if out != "1" {
		t.Fatalf("ahead_of_base = %s, want exactly 1 — the executor made ONE commit; "+
			"a higher count is other people's work billed to it (the reported \"ahead 5, actually 1\")", out)
	}
}

// TestPrepareWorktree_RealGit_ExplicitShaBaseStillWorks guards the fallback half of the
// resolution order: a task-pinned SHA has no refs/remotes/origin/<sha> counterpart, so it
// must resolve through the bare-ref candidate untouched. A fix that only ever consulted
// origin/* would break every explicit-base task.
func TestPrepareWorktree_RealGit_ExplicitShaBaseStillWorks(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	m, _ := realGitMat(t)
	remote := makeRealRemote(t)
	shaA := strings.TrimSpace(runGit(t, remote, "rev-parse", "HEAD"))
	target := RepoTarget{URL: remote, DefaultBranch: "main"}

	if _, err := m.EnsureSource(ctx, target); err != nil {
		t.Fatalf("EnsureSource (clone): %v", err)
	}
	advanceOrigin(t, remote, "b.txt")
	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("EnsureSource (fetch): %v", err)
	}

	// Task explicitly pins the OLD commit — that must be honoured verbatim, not "helpfully"
	// advanced to origin's tip.
	ws := filepath.Join(t.TempDir(), "executors", "exec-1", "workspace")
	wt, err := m.PrepareWorktree(ctx, src, WorktreeRequest{
		ExecutorID:    "exec-1",
		TaskID:        "task-1",
		BranchName:    "ac-exec/task-1/exec-1",
		WorkspacePath: ws,
		BaseRef:       shaA,
	})
	if err != nil {
		t.Fatalf("PrepareWorktree with explicit sha base: %v", err)
	}
	if wt.BaseRef != shaA {
		t.Fatalf("BaseRef = %q, want the explicitly requested %s", wt.BaseRef, shaA)
	}
	if head := strings.TrimSpace(runGit(t, ws, "rev-parse", "HEAD")); head != shaA {
		t.Fatalf("worktree HEAD = %s, want the explicitly requested base %s", head, shaA)
	}
}

// TestPrepareWorktree_RealGit_UnresolvableBaseFailsClosed pins the fail-closed rule: a base
// that names nothing must refuse to spawn. Branching off some silently-substituted commit
// (or off whatever HEAD happens to be) would hand the executor a wrong tree and make its
// ahead-count meaningless — a worse outcome than not spawning.
func TestPrepareWorktree_RealGit_UnresolvableBaseFailsClosed(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	ctx := context.Background()
	m, _ := realGitMat(t)
	remote := makeRealRemote(t)
	src, err := m.EnsureSource(ctx, RepoTarget{URL: remote, DefaultBranch: "main"})
	if err != nil {
		t.Fatalf("EnsureSource: %v", err)
	}

	ws := filepath.Join(t.TempDir(), "executors", "exec-1", "workspace")
	_, err = m.PrepareWorktree(ctx, src, WorktreeRequest{
		ExecutorID:    "exec-1",
		TaskID:        "task-1",
		BranchName:    "ac-exec/task-1/exec-1",
		WorkspacePath: ws,
		BaseRef:       "no/such/branch",
	})
	if !errors.Is(err, ErrBaseRefUnresolved) {
		t.Fatalf("err = %v, want ErrBaseRefUnresolved (fail-closed)", err)
	}
	if _, statErr := os.Stat(ws); statErr == nil {
		t.Fatal("no worktree may be created for an unresolvable base")
	}
}

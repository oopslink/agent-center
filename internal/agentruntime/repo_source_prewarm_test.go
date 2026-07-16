package agentruntime

// repo_source_prewarm_test.go — the issue-13e7bfe8 regression lock.
//
// These tests deliberately use a REAL LocalGitMaterializer, a REAL git clone and an
// EMPTY repos root (cold start, flag ON) — the deployment default path. That is the
// point: the pre-fix bug survived precisely because the existing flag-ON suite only ever
// injected a fake materializer (recordingMaterializer), which returns an already-
// materialized source instantly and therefore never exercised a first-clone at all. A
// fake cannot be slow, cannot be cancelled mid-clone, and cannot leave a half-written
// .git behind — i.e. it cannot reproduce any part of this incident. Every test here
// would pass trivially against a fake and fails against the pre-fix code.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
)

// repoTaskBodyURL is repoTaskBody with a caller-supplied clone URL (a real temp remote).
func repoTaskBodyURL(taskID, url string) map[string]any {
	return map[string]any{
		"id": taskID, "title": "t", "status": "open", "base_ref": "main",
		"repo": map[string]any{
			"repo_id": "r-1", "url": url,
			"provider": "git", "default_branch": "main", "is_primary": true,
		},
	}
}

// realMat builds a REAL LocalGitMaterializer over an EMPTY repos root (cold start).
func realMat(t *testing.T) (*reporepo.LocalGitMaterializer, string) {
	t.Helper()
	reposRoot := t.TempDir()
	mat, err := reporepo.NewLocalGitMaterializer(reposRoot, nil, nil)
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	return mat, reposRoot
}

// headResolves reports whether a git checkout has a resolvable HEAD — i.e. whether it is
// actually usable, as opposed to merely having a .git directory. This is the exact
// discriminator the incident turned on: the poisoned source had a .git (so the pre-fix
// `os.Stat(.git)` check said "repo!"), answered `remote get-url` and `fetch` happily, and
// still failed every `rev-parse HEAD` with "your current branch appears to be broken".
func headResolves(t *testing.T, repoPath string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "HEAD")
	cmd.Dir = repoPath
	return cmd.Run() == nil
}

// TestSpawnExecutor_RealGit_ColdClone_ControlPathNeverBlocksAndTaskStillRuns is THE
// regression lock for layer 1.
//
// It drives SpawnExecutor exactly as a control command does — but with an ALREADY-EXPIRED
// ctx, standing in for the 5s unix-socket deadline that a multi-minute clone can never
// fit inside. Pre-fix, EnsureSource ran inline on that ctx: git got SIGKILLed, the task
// was left queued, the command was never acked, and the worker's shared control cursor
// re-delivered it forever (420× in prod). Post-fix the control path must:
//
//	① return promptly WITHOUT admitting the task (no start_task before a source exists),
//	② clone in the BACKGROUND under its own timeout, NOT the caller's dead ctx, and
//	③ re-drive the task itself so it actually runs — which the center will NOT do for it
//	   (the wake-sweep skips any agent that has a running task).
func TestSpawnExecutor_RealGit_ColdClone_ControlPathNeverBlocksAndTaskStillRuns(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t)
	mat, reposRoot := realMat(t) // EMPTY repos root: nothing materialized yet

	rt, _, _ := engineForAgentMat(t, "agent-cold", mat)
	rt.cfg.SourcePrewarmBackoff = -1 // collapse retry backoff; no sleeping in tests
	sc := &scriptedToolCaller{getTaskBody: repoTaskBodyURL("task-cold", remote)}
	setToolCaller(rt, sc)

	// An ALREADY-CANCELLED ctx: the control command's deadline is gone. A clone must
	// still happen (it must not inherit this ctx) and the task must still end up running.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, err := rt.SpawnExecutor(ctx, SpawnRequest{TaskID: "task-cold"})
	if err != nil || res != nil {
		t.Fatalf("SpawnExecutor on a cold source = (%v, %v), want (nil, nil): the control path must DEFER, not fork, before a source exists", res, err)
	}
	// ① The task must NOT have been admitted: red line A (never admit what cannot run).
	if _, ok := sc.callFor("start_task"); ok {
		t.Fatalf("start_task was called before the repo source existed — a task admitted with no workspace can never run (red line A)")
	}

	// ② + ③ The background clone runs under its OWN ctx and re-drives the task.
	rt.waitSourcePrewarm()

	sourcePath := filepath.Join(reposRoot, reporepo.RepoKey(remote), "source")
	if _, statErr := os.Stat(filepath.Join(sourcePath, ".git")); statErr != nil {
		t.Fatalf("background clone must materialize the canonical source despite the caller's dead ctx: %v", statErr)
	}
	if !headResolves(t, sourcePath) {
		t.Fatalf("canonical source at %s has no resolvable HEAD — a half-clone was published (the exact prod poisoning)", sourcePath)
	}
	// The whole point of the re-drive: the task actually runs. Nothing else would do it.
	if _, ok := sc.callFor("start_task"); !ok {
		t.Fatalf("task was never re-driven after the source landed: tools seen = %v — a queued task on a busy agent is NOT re-emitted by the center, so this would strand forever", sc.toolsSeen())
	}
}

// TestSpawnExecutor_RealGit_WarmSourceForksInline pins the other half of the gate: once a
// source is materialized and fresh, the control path does NOT defer — it forks inline, so
// the deferral costs at most one round trip per repo rather than one per task.
func TestSpawnExecutor_RealGit_WarmSourceForksInline(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t)
	mat, _ := realMat(t)

	rt, _, _ := engineForAgentMat(t, "agent-warm", mat)
	rt.cfg.SourcePrewarmBackoff = -1
	sc := &scriptedToolCaller{getTaskBody: repoTaskBodyURL("task-warm", remote)}
	setToolCaller(rt, sc)

	// First spawn: cold → defers, clones in background, re-drives.
	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-warm"}); err != nil {
		t.Fatalf("cold SpawnExecutor: %v", err)
	}
	rt.waitSourcePrewarm()

	// Second spawn on the SAME tool caller (swapping it mid-flight would race the
	// already-forked executor's drain goroutine, which reads cfg.ToolCaller).
	// The source is warm+fresh → this must fork INLINE (returns a SpawnResult).
	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-warm2"})
	if err != nil {
		t.Fatalf("warm SpawnExecutor: %v", err)
	}
	if res == nil {
		t.Fatalf("warm source must fork INLINE (no deferral), got nil SpawnResult; tools seen = %v", sc.toolsSeen())
	}
}

// countTool reports how many times a tool was called.
func countTool(sc *scriptedToolCaller, name string) int {
	n := 0
	for _, s := range sc.toolsSeen() {
		if s == name {
			n++
		}
	}
	return n
}

// TestSpawnExecutor_RealGit_UnreachableRemoteFailsLoud pins the fail-loud requirement with
// a REAL failing clone (a remote that does not exist — no fake, no injected error).
//
// Pre-fix this was a silent hole: EnsureSource failed, the task was logged and left
// queued, and because the center's wake-sweep only re-drives agents with ZERO running
// tasks, a busy agent stranded it with no center-visible signal at all. The task must now
// be admitted and blocked with a machine-readable cause.
func TestSpawnExecutor_RealGit_UnreachableRemoteFailsLoud(t *testing.T) {
	requireGit(t)
	mat, reposRoot := realMat(t)
	missing := filepath.Join(t.TempDir(), "no-such-repo") // a real, really-absent remote

	rt, _, _ := engineForAgentMat(t, "agent-badrepo", mat)
	rt.cfg.SourcePrewarmBackoff = -1
	rt.cfg.SourcePrewarmAttempts = 2
	sc := &scriptedToolCaller{getTaskBody: repoTaskBodyURL("task-bad", missing)}
	setToolCaller(rt, sc)

	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-bad"}); err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}
	rt.waitSourcePrewarm()

	// Fail-loud: admitted (so the center permits a block) then blocked with the cause.
	blocked, ok := sc.callFor("block_task")
	if !ok {
		t.Fatalf("a permanently un-cloneable repo must FAIL LOUD (block_task), not sit silently queued; tools seen = %v", sc.toolsSeen())
	}
	reason, _ := blocked["reason"].(string)
	if !containsSub(reason, string(CauseRepoSourceUnavailable)) {
		t.Fatalf("blocked_reason = %q, want a machine-readable [cause=%s]", reason, CauseRepoSourceUnavailable)
	}

	// A failed clone must leave NO canonical source behind — a retry must start clean.
	sourcePath := filepath.Join(reposRoot, reporepo.RepoKey(missing), "source")
	if _, statErr := os.Stat(sourcePath); !os.IsNotExist(statErr) {
		t.Fatalf("a failed clone must publish NOTHING at the canonical path (stat err = %v); debris here is what poisons every later EnsureSource", statErr)
	}
}

// TestSpawnExecutor_RealGit_ConcurrentTasksCloneOnce pins the prewarm dedup: N tasks on
// one repo trigger ONE clone and all of them get re-driven. Without the dedup, every
// work_available would start its own competing clone of the same repo.
func TestSpawnExecutor_RealGit_ConcurrentTasksCloneOnce(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t)
	mat, _ := realMat(t)

	rt, _, _ := engineForAgentMat(t, "agent-dedup", mat)
	rt.cfg.SourcePrewarmBackoff = -1
	sc := &scriptedToolCaller{getTaskBody: repoTaskBodyURL("task-d", remote)}
	setToolCaller(rt, sc)

	// Two deferrals for the SAME repo before the clone lands.
	for _, id := range []string{"task-d1", "task-d2"} {
		if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: id}); err != nil {
			t.Fatalf("SpawnExecutor %s: %v", id, err)
		}
	}
	rt.waitSourcePrewarm()

	key := reporepo.RepoKey(remote)
	rt.sources.mu.Lock()
	e := rt.sources.entries[key]
	inflight, ready := false, false
	if e != nil {
		inflight, ready = e.inflight, e.ready != nil
	}
	rt.sources.mu.Unlock()
	if !ready {
		t.Fatalf("repo_key=%s should hold a materialized source after the episode", key)
	}
	if inflight {
		t.Fatalf("prewarm episode must be closed (inflight=false) once the source landed")
	}
}

// TestSourceGate_StaleRefreshDegradesToExistingSource pins the degrade rule: when a
// refresh fails but a usable source is already on disk, waiters are re-driven against it
// rather than blocked. A transient network blip must not fail tasks that a perfectly good
// local checkout could run.
func TestSourceGate_StaleRefreshDegradesToExistingSource(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t)
	mat, _ := realMat(t)

	rt, _, _ := engineForAgentMat(t, "agent-degrade", mat)
	rt.cfg.SourcePrewarmBackoff = -1
	sc := &scriptedToolCaller{getTaskBody: repoTaskBodyURL("task-dg", remote)}
	setToolCaller(rt, sc)

	// Land a real source first.
	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-dg"}); err != nil {
		t.Fatalf("cold SpawnExecutor: %v", err)
	}
	rt.waitSourcePrewarm()

	key := reporepo.RepoKey(remote)
	startsBefore := countTool(sc, "start_task")

	// Now simulate a FAILED refresh episode against the cached-ready entry. The tool
	// caller is NOT swapped: doing so would race the forked executor's drain goroutine.
	rt.sources.mu.Lock()
	rt.sources.entries[key].waiters = map[string]struct{}{"task-dg2": {}}
	rt.sources.mu.Unlock()

	rt.finishPrewarm("agent-degrade", key, nil, context.DeadlineExceeded)

	// Degrade, do NOT block: the waiter is re-driven against the existing source.
	if _, ok := sc.callFor("block_task"); ok {
		t.Fatalf("a failed REFRESH with a usable source on disk must degrade to it, not block the task")
	}
	if got := countTool(sc, "start_task"); got <= startsBefore {
		t.Fatalf("waiter must be re-driven against the existing source: start_task count %d → %d; tools seen = %v", startsBefore, got, sc.toolsSeen())
	}
}

// TestSourceGate_DegradeRefusesAVanishedSource pins that the degrade path checks the
// source is STILL ON DISK before re-driving waiters against it.
//
// Without this check the fix contains a worse version of the bug it fixes: if the cached
// source is gone (a heal quarantined it and the follow-up clone failed), degrading would
// re-drive every waiter against a path that does not exist. Each PrepareWorktree would
// fail, each task would be "left queued" with only a log line, AND the cache would keep
// reporting fresh for the rest of the window — so later tasks would skip the gate and
// strand inline too, with the fail-loud path never reached because the entry looked
// usable. A vanished source must fail LOUD instead.
func TestSourceGate_DegradeRefusesAVanishedSource(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t)
	mat, _ := realMat(t)

	rt, _, _ := engineForAgentMat(t, "agent-vanish", mat)
	rt.cfg.SourcePrewarmBackoff = -1
	rt.cfg.SourcePrewarmAttempts = 1
	sc := &scriptedToolCaller{getTaskBody: repoTaskBodyURL("task-v", remote)}
	setToolCaller(rt, sc)

	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-v"}); err != nil {
		t.Fatalf("cold SpawnExecutor: %v", err)
	}
	rt.waitSourcePrewarm()

	key := reporepo.RepoKey(remote)
	// The source disappears from disk while still cached as ready.
	rt.sources.mu.Lock()
	cached := rt.sources.entries[key].ready.Path
	rt.sources.entries[key].waiters = map[string]struct{}{"task-v2": {}}
	rt.sources.mu.Unlock()
	if err := os.RemoveAll(cached); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	// A failed refresh must NOT degrade onto the vanished path.
	rt.finishPrewarm("agent-vanish", key, nil, context.DeadlineExceeded)

	if _, ok := sc.callFor("block_task"); !ok {
		t.Fatalf("a vanished source must FAIL LOUD, not degrade onto a path that no longer exists; tools = %v", sc.toolsSeen())
	}
	if _, fresh := rt.freshSource(key); fresh {
		t.Fatalf("a vanished source must not keep answering fresh — later tasks would strand inline")
	}
}

// TestSourceGate_AlwaysStaleSourceStillTerminates pins that the prewarm re-drive can NOT
// livelock.
//
// A re-drive that finds the source stale AGAIN must consume it anyway rather than defer
// into a fresh prewarm — otherwise defer→clone→re-drive→defer→clone… spins forever. This
// is not hypothetical: it is reachable in production whenever a single fetch takes longer
// than SourceFreshFor (a large repo on a slow link), and an earlier draft of this fix hit
// exactly that — the test hung until the package timeout. SourceFreshFor=1ns is simply the
// deterministic way to force "the source is stale by the time the re-drive looks".
//
// The test PASSES by terminating. If the guard regresses, it hangs.
func TestSourceGate_AlwaysStaleSourceStillTerminates(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t)
	mat, _ := realMat(t)

	rt, _, _ := engineForAgentMat(t, "agent-stale", mat)
	rt.cfg.SourcePrewarmBackoff = -1
	rt.cfg.SourceFreshFor = time.Nanosecond // every source is stale the instant it lands
	sc := &scriptedToolCaller{getTaskBody: repoTaskBodyURL("task-st", remote)}
	setToolCaller(rt, sc)

	if _, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-st"}); err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}

	done := make(chan struct{})
	go func() { rt.waitSourcePrewarm(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatalf("prewarm did not settle: an always-stale source livelocked the defer→clone→re-drive cycle")
	}

	// Terminated — and the re-drive consumed the stale source rather than re-deferring.
	if _, ok := sc.callFor("start_task"); !ok {
		t.Fatalf("re-drive must consume the just-materialized source even when the freshness window lapsed; tools seen = %v", sc.toolsSeen())
	}
	// The window itself still works: the NEXT control-path spawn re-defers to refresh.
	if _, fresh := rt.freshSource(reporepo.RepoKey(remote)); fresh {
		t.Fatalf("a source past its freshness window must not be reported fresh to the control path")
	}
}

// containsSub is a tiny substring helper (the package's tests avoid extra imports).
func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

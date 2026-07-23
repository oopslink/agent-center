package agentruntime

// repo_workspace_test.go — flag-ON (AC_EXECUTOR_GIT_WORKTREE) integration coverage for
// the P3/P4/P5 repo-workspace track: SpawnExecutor materializes the canonical source +
// a per-executor worktree BEFORE start_task (red line A), tears it down on every
// failure path (red line B) AND on finalize/recovery, and NEVER deletes the canonical
// source (red line C). Flag-OFF behavior is proven byte-for-byte by the pre-existing
// executor_runtime_test.go suite (unchanged).

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
)

// requireGit skips the test when the git binary is unavailable.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

// makeGitRemote creates a temp git repo with one commit on branch main and returns its
// path (usable as a clone URL). Deterministic identity so no host gitconfig is needed.
func makeGitRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.x",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir
}

// callSeq is a shared ordered event log so a test can assert cross-collaborator order
// (materializer EnsureSource/PrepareWorktree vs the tool caller's start_task).
type callSeq struct {
	mu sync.Mutex
	ev []string
}

func (s *callSeq) add(e string) {
	s.mu.Lock()
	s.ev = append(s.ev, e)
	s.mu.Unlock()
}

func (s *callSeq) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ev...)
}

// indexOf returns the first index of e in seq, or -1.
func indexOf(seq []string, e string) int {
	for i, v := range seq {
		if v == e {
			return i
		}
	}
	return -1
}

type removeArgs struct{ repoKey, sourcePath, workspacePath string }

// recordingMaterializer is a fake reporepo.RepoMaterializer that records call order +
// args into a shared callSeq, injects per-step errors, and (on PrepareWorktree)
// simulates a real worktree by creating the workspace dir + a stand-in canonical
// source dir. RemoveWorktree removes ONLY the workspace dir (never sourcePath) so a
// test can assert the canonical source survives (red line C).
type recordingMaterializer struct {
	seq *callSeq

	ensureErr  error
	prepareErr error

	repoKey    string
	sourcePath string
	baseRef    string

	mu         sync.Mutex
	prepared   []reporepo.WorktreeRequest
	cloned     []reporepo.CloneRequest
	removed    []removeArgs
	pruneCalls []pruneCall
	lastTarget reporepo.RepoTarget
}

var _ reporepo.RepoMaterializer = (*recordingMaterializer)(nil)

func (m *recordingMaterializer) EnsureSource(_ context.Context, target reporepo.RepoTarget) (reporepo.SourceRepo, error) {
	m.seq.add("EnsureSource")
	m.mu.Lock()
	m.lastTarget = target
	m.mu.Unlock()
	if m.ensureErr != nil {
		return reporepo.SourceRepo{}, m.ensureErr
	}
	base := m.baseRef
	if base == "" {
		base = target.BaseRef
	}
	// Materialize the stand-in canonical source dir (must SURVIVE cleanup).
	_ = os.MkdirAll(m.sourcePath, 0o755)
	return reporepo.SourceRepo{RepoKey: m.repoKey, Path: m.sourcePath, URL: target.URL, BaseRef: base}, nil
}

func (m *recordingMaterializer) PrepareWorktree(_ context.Context, source reporepo.SourceRepo, req reporepo.WorktreeRequest) (reporepo.PreparedWorktree, error) {
	m.seq.add("PrepareWorktree")
	m.mu.Lock()
	m.prepared = append(m.prepared, req)
	m.mu.Unlock()
	if m.prepareErr != nil {
		return reporepo.PreparedWorktree{}, m.prepareErr
	}
	_ = os.MkdirAll(req.WorkspacePath, 0o755) // simulate the worktree checkout
	return reporepo.PreparedWorktree{
		ExecutorID:    req.ExecutorID,
		RepoKey:       source.RepoKey,
		SourcePath:    source.Path,
		WorkspacePath: req.WorkspacePath,
		Branch:        req.BranchName,
		BaseRef:       req.BaseRef,
	}, nil
}

func (m *recordingMaterializer) PrepareClone(_ context.Context, target reporepo.RepoTarget, req reporepo.CloneRequest) (reporepo.PreparedClone, error) {
	m.seq.add("PrepareClone")
	m.mu.Lock()
	m.lastTarget = target
	m.cloned = append(m.cloned, req)
	m.mu.Unlock()
	if m.prepareErr != nil {
		return reporepo.PreparedClone{}, m.prepareErr
	}
	_ = os.MkdirAll(req.WorkspacePath, 0o755)
	base := req.BaseRef
	if base == "" {
		base = target.BaseRef
	}
	return reporepo.PreparedClone{
		ExecutorID:    req.ExecutorID,
		RepoKey:       reporepo.RepoKey(target.URL),
		WorkspacePath: req.WorkspacePath,
		Branch:        req.BranchName,
		BaseRef:       base,
	}, nil
}

func (m *recordingMaterializer) RemoveWorktree(_ context.Context, wt reporepo.PreparedWorktree) error {
	m.seq.add("RemoveWorktree")
	m.mu.Lock()
	m.removed = append(m.removed, removeArgs{wt.RepoKey, wt.SourcePath, wt.WorkspacePath})
	m.mu.Unlock()
	// Tear down ONLY the worktree — never the canonical source (design §10).
	_ = os.RemoveAll(wt.WorkspacePath)
	return nil
}

// pruneCall records one PruneOrphanWorktrees invocation (the spawn-hook) + the ids the
// runtime's isLive predicate reported live at that moment.
type pruneCall struct {
	repoKey string
	live    map[string]bool
}

func (m *recordingMaterializer) PruneOrphanWorktrees(_ context.Context, source reporepo.SourceRepo, isLive func(string) bool) (int, error) {
	m.seq.add("PruneOrphanWorktrees")
	// Snapshot which of the previously-prepared executor ids the runtime considers live,
	// so a test can assert the fail-safe (a live executor's worktree is never reaped).
	live := map[string]bool{}
	m.mu.Lock()
	for _, req := range m.prepared {
		if isLive != nil && isLive(req.ExecutorID) {
			live[req.ExecutorID] = true
		}
	}
	m.pruneCalls = append(m.pruneCalls, pruneCall{repoKey: source.RepoKey, live: live})
	m.mu.Unlock()
	return 0, nil
}

func (m *recordingMaterializer) ReapOrphanWorktrees(_ context.Context, _ func(string) bool) (int, error) {
	m.seq.add("ReapOrphanWorktrees")
	return 0, nil
}

func (m *recordingMaterializer) pruneHookCalls() []pruneCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]pruneCall(nil), m.pruneCalls...)
}

func (m *recordingMaterializer) removeCalls() []removeArgs {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]removeArgs(nil), m.removed...)
}

func (m *recordingMaterializer) preparedReqs() []reporepo.WorktreeRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]reporepo.WorktreeRequest(nil), m.prepared...)
}

func (m *recordingMaterializer) cloneReqs() []reporepo.CloneRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]reporepo.CloneRequest(nil), m.cloned...)
}

// engineForAgentMat builds a flag-ON runtime (materializer wired) + attached engine.
func engineForAgentMat(t *testing.T, agentID string, mat reporepo.RepoMaterializer) (*LocalRuntime, *ExecutorEngine, string) {
	t.Helper()
	trueBin := lookTrue(t)
	base := t.TempDir()
	rt := newExecRuntime(t, base, agentID, trueBin)
	rt.cfg.Materializer = mat        // AC_EXECUTOR_GIT_WORKTREE ON
	rt.cfg.SourcePrewarmBackoff = -1 // collapse the retry backoff: no sleeping in tests
	home, _, _, err := rt.agentPaths(agentID)
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID:              agentID,
		MaxConcurrentTasks:   2,
		DefaultExecutorModel: "claude-default",
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)
	return rt, ee, home
}

func engineForAgentClone(t *testing.T, agentID string, mat *recordingMaterializer) (*LocalRuntime, *ExecutorEngine, string) {
	t.Helper()
	trueBin := lookTrue(t)
	base := t.TempDir()
	rt := newExecRuntime(t, base, agentID, trueBin)
	rt.cfg.CloneMaterializer = mat // AC_EXECUTOR_GIT_WORKTREE OFF
	home, _, _, err := rt.agentPaths(agentID)
	if err != nil {
		t.Fatalf("agentPaths: %v", err)
	}
	ee, err := rt.BuildExecutorEngine(home, ExecutorConfig{
		AgentID:              agentID,
		MaxConcurrentTasks:   2,
		DefaultExecutorModel: "claude-default",
	})
	if err != nil {
		t.Fatalf("BuildExecutorEngine: %v", err)
	}
	attach(rt, ee)
	return rt, ee, home
}

// spawnSettled drives SpawnExecutor for a COLD repo source and waits for the whole
// episode to settle.
//
// The first spawn for a repo deliberately does NOT fork (issue-13e7bfe8): materializing a
// source is a `git clone`, and SpawnExecutor may be running inside a control-command
// handler with a 5s transport deadline, so the source is materialized on a BACKGROUND
// goroutine and the task is re-driven from there. These tests therefore assert on the
// settled end state rather than on the first call's return value — which is exactly the
// invariant that used to be violated: an inline clone here wedged the worker's shared
// control cursor.
func spawnSettled(t *testing.T, rt *LocalRuntime, taskID string) {
	t.Helper()
	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: taskID})
	if err != nil {
		t.Fatalf("SpawnExecutor(%s) = %v, want nil error", taskID, err)
	}
	if res != nil {
		t.Fatalf("SpawnExecutor(%s) forked INLINE on a cold source — the control path must DEFER to the background prewarm", taskID)
	}
	rt.waitSourcePrewarm() // the background materialize + its re-drive
}

// repoTaskBody is a get_task projection carrying a primary repo hint (P1).
func repoTaskBody(taskID string) map[string]any {
	return map[string]any{
		"id": taskID, "title": "t", "status": "open", "base_ref": "main",
		"repo": map[string]any{
			"repo_id": "r-1", "url": "git@example.com:o/repo.git",
			"provider": "github", "default_branch": "main", "is_primary": true,
		},
	}
}

// TestSpawnExecutor_Repo_PrepareBeforeStartTask proves red line A (order:
// EnsureSource+PrepareWorktree BEFORE start_task), that the prepared worktree path is
// the launched executor's ACTUAL workspace (the pre-minted id threads pool→worktree),
// and that the worktree teardown handle (RepoKey/SourcePath) is persisted into the
// recovery Record so finalize/recovery can tear it down later.
// TestSpawnExecutor_Repo_PruneHookRunsBeforePrepare pins the v2.31.1 spawn-hook: the
// orphan-worktree reap runs AFTER EnsureSource and BEFORE PrepareWorktree — so the new
// executor's own worktree does not exist yet and can never be reaped, while any prior
// orphan under the source is swept before we add ours.
func TestSpawnExecutor_Repo_PruneHookRunsBeforePrepare(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-abc", sourcePath: t.TempDir() + "/repos/key-abc/source", baseRef: "main"}
	rt, _, _ := engineForAgentMat(t, "agent-repo", mat)
	setToolCaller(rt, &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-9")})

	spawnSettled(t, rt, "task-9")

	order := seq.snapshot()
	iEnsure, iPrune, iPrep := indexOf(order, "EnsureSource"), indexOf(order, "PruneOrphanWorktrees"), indexOf(order, "PrepareWorktree")
	if iEnsure < 0 || iPrune < 0 || iPrep < 0 {
		t.Fatalf("missing prune-hook in order %v", order)
	}
	if !(iEnsure < iPrune && iPrune < iPrep) {
		t.Fatalf("order violation: want EnsureSource<PruneOrphanWorktrees<PrepareWorktree, got %v", order)
	}
	// The reap ran before this executor's worktree existed → its id was NOT reported live
	// (the fail-safe live set is built from already-prepared executors; there are none yet).
	if calls := mat.pruneHookCalls(); len(calls) != 1 || len(calls[0].live) != 0 {
		t.Fatalf("prune hook: want 1 call with empty live set (new executor not yet a worktree), got %+v", calls)
	}
}

func TestSpawnExecutor_Repo_WorktreeSwitchOffUsesIndependentClone(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-off", sourcePath: t.TempDir() + "/src", baseRef: "main"}
	rt, _, home := engineForAgentClone(t, "agent-repo-off", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-off")}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-off"})
	if err != nil {
		t.Fatalf("SpawnExecutor: %v", err)
	}
	if res == nil {
		t.Fatalf("OFF path must fork inline after independent clone, got nil")
	}

	order := seq.snapshot()
	if indexOf(order, "EnsureSource") >= 0 || indexOf(order, "PrepareWorktree") >= 0 || indexOf(order, "PruneOrphanWorktrees") >= 0 {
		t.Fatalf("OFF path must not use canonical source/worktree/prune, order=%v", order)
	}
	iClone, iStart := indexOf(order, "PrepareClone"), indexOf(order, "start_task")
	if iClone < 0 || iStart < 0 || !(iClone < iStart) {
		t.Fatalf("order violation: want PrepareClone<start_task, got %v", order)
	}
	reqs := mat.cloneReqs()
	if len(reqs) != 1 {
		t.Fatalf("PrepareClone calls = %d, want 1", len(reqs))
	}
	execID := reqs[0].ExecutorID
	wantWS, _ := rt.execEngine().fx.Layout().WorkspaceDir(execID)
	if reqs[0].WorkspacePath != wantWS {
		t.Fatalf("clone path %q != executor workspace %q", reqs[0].WorkspacePath, wantWS)
	}
	_, tr := seedExchange(t, home)
	rec, err := tr.Read(execID)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if rec.RepoKey != "" || rec.SourcePath != "" {
		t.Fatalf("OFF clone must not persist worktree cleanup handles, got repo_key=%q source_path=%q", rec.RepoKey, rec.SourcePath)
	}
}

func TestSpawnExecutor_Repo_WorktreeSwitchOffCloneFailureFailsLoud(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-off", sourcePath: t.TempDir() + "/src", prepareErr: context.Canceled}
	rt, _, home := engineForAgentClone(t, "agent-repo-off-fail", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-off-fail")}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-off-fail"})
	if err != nil || res != nil {
		t.Fatalf("SpawnExecutor = (%v, %v), want (nil, nil)", res, err)
	}
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("failed clone must NOT fork, got %+v", probs)
	}
	blocked, ok := sc.callFor("block_task")
	if !ok {
		t.Fatalf("failed clone must FAIL LOUD (block_task), tools=%v", sc.toolsSeen())
	}
	if reason, _ := blocked["reason"].(string); !containsSub(reason, string(CauseRepoSourceUnavailable)) {
		t.Fatalf("blocked_reason = %q, want [cause=%s]", reason, CauseRepoSourceUnavailable)
	}
}

func TestSpawnExecutor_Repo_WorktreeSwitchOffStartTaskDeclinedCleansClone(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-off", sourcePath: t.TempDir() + "/src", baseRef: "main"}
	rt, _, _ := engineForAgentClone(t, "agent-repo-off-decl", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-off-decl"), startErr: context.DeadlineExceeded}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-off-decl"})
	if err != nil || res != nil {
		t.Fatalf("SpawnExecutor = (%v, %v), want (nil, nil)", res, err)
	}
	reqs := mat.cloneReqs()
	if len(reqs) == 0 {
		t.Fatal("PrepareClone was not called")
	}
	for _, req := range reqs {
		if _, statErr := os.Stat(req.WorkspacePath); !os.IsNotExist(statErr) {
			t.Fatalf("declined admission must remove prepared clone %s, stat err=%v", req.WorkspacePath, statErr)
		}
	}
}

func TestRepo_RealGitWorktreeSwitchOffIndependentLocalClone(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t)
	reposRoot := t.TempDir()
	mat, err := reporepo.NewLocalGitMaterializer(reposRoot, nil, nil)
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	ws := filepath.Join(t.TempDir(), "executors", "exec-off", "workspace")
	clone, err := mat.PrepareClone(context.Background(), reporepo.RepoTarget{
		URL: remote, Provider: "git", DefaultBranch: "main", BaseRef: "main",
	}, reporepo.CloneRequest{
		ExecutorID: "exec-off", TaskID: "task-off",
		BranchName: "ac-exec/task-off/exec-off", WorkspacePath: ws, BaseRef: "main",
	})
	if err != nil {
		t.Fatalf("PrepareClone: %v", err)
	}
	if clone.WorkspacePath != ws || clone.BaseRef == "" {
		t.Fatalf("clone result = %+v, want workspace + pinned base", clone)
	}
	if _, statErr := os.Stat(filepath.Join(ws, ".git")); statErr != nil {
		t.Fatalf("independent clone missing .git: %v", statErr)
	}
	sourcePath := filepath.Join(reposRoot, reporepo.RepoKey(remote), "source")
	if _, statErr := os.Stat(sourcePath); !os.IsNotExist(statErr) {
		t.Fatalf("OFF clone must not create canonical source %s, stat err=%v", sourcePath, statErr)
	}
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = remote
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list: %v: %s", err, out)
	}
	if containsSub(string(out), ws) {
		t.Fatalf("OFF clone must not register as a linked worktree of the local source:\n%s", out)
	}
}

func TestRepo_RealGitWorktreeSwitchOffRejectsLocalRemoteMismatch(t *testing.T) {
	requireGit(t)
	local := makeGitRemote(t)
	cmd := exec.Command("git", "remote", "add", "origin", "https://example.invalid/not-this-repo.git")
	cmd.Dir = local
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v: %s", err, out)
	}
	mat, err := reporepo.NewLocalGitMaterializer(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	ws := filepath.Join(t.TempDir(), "executors", "exec-mismatch", "workspace")
	_, err = mat.PrepareClone(context.Background(), reporepo.RepoTarget{
		URL: local, Provider: "git", DefaultBranch: "main", BaseRef: "main",
	}, reporepo.CloneRequest{
		ExecutorID: "exec-mismatch", TaskID: "task-mismatch",
		BranchName: "ac-exec/task-mismatch/exec-mismatch", WorkspacePath: ws, BaseRef: "main",
	})
	if !errors.Is(err, reporepo.ErrRemoteMismatch) {
		t.Fatalf("PrepareClone err = %v, want ErrRemoteMismatch", err)
	}
	if _, statErr := os.Stat(ws); !os.IsNotExist(statErr) {
		t.Fatalf("remote mismatch must not create workspace, stat err=%v", statErr)
	}
}

func TestSpawnExecutor_Repo_PrepareBeforeStartTask(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-abc", sourcePath: t.TempDir() + "/repos/key-abc/source", baseRef: "main"}
	rt, _, home := engineForAgentMat(t, "agent-repo", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-9")}
	setToolCaller(rt, sc)

	spawnSettled(t, rt, "task-9")

	// The ordering invariant is unchanged by the prewarm — EnsureSource simply runs on
	// the background goroutine and the rest follows on its re-drive.
	order := seq.snapshot()
	iEnsure, iPrep, iStart := indexOf(order, "EnsureSource"), indexOf(order, "PrepareWorktree"), indexOf(order, "start_task")
	if iEnsure < 0 || iPrep < 0 || iStart < 0 {
		t.Fatalf("missing steps in order %v", order)
	}
	if !(iEnsure < iPrep && iPrep < iStart) {
		t.Fatalf("order violation: want EnsureSource<PrepareWorktree<start_task, got %v", order)
	}

	// The prepared worktree path must be the launched executor's actual workspace. The
	// executor id comes from the prepare request (the re-drive's SpawnResult is consumed
	// inside the prewarm, not returned to this caller).
	reqs := mat.preparedReqs()
	if len(reqs) != 1 {
		t.Fatalf("PrepareWorktree calls = %d, want 1", len(reqs))
	}
	execID := reqs[0].ExecutorID
	wantWS, _ := rt.execEngine().fx.Layout().WorkspaceDir(execID)
	if reqs[0].WorkspacePath != wantWS {
		t.Fatalf("worktree path %q != executor workspace %q", reqs[0].WorkspacePath, wantWS)
	}
	if reqs[0].BranchName != "ac-exec/task-9/"+execID {
		t.Fatalf("branch = %q, want ac-exec/task-9/%s", reqs[0].BranchName, execID)
	}

	// P5: the pool persisted the worktree teardown handle into the recovery Record.
	_, tr := seedExchange(t, home)
	rec, err := tr.Read(execID)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if rec.RepoKey != "key-abc" || rec.SourcePath != mat.sourcePath {
		t.Fatalf("record teardown handle = {%q,%q}, want {key-abc,%s}", rec.RepoKey, rec.SourcePath, mat.sourcePath)
	}
}

// TestSpawnExecutor_Repo_EnsureSourceFailNoForkFailsLoud proves red line A's failure half:
// an EnsureSource failure means NO fork and no worktree teardown (nothing was prepared).
//
// The tail of this contract CHANGED with issue-13e7bfe8. It used to assert "start_task is
// NEVER called" — i.e. the task was left silently queued. That silence was a hole: the
// center's wake-sweep only re-drives a queued task while the agent has zero running tasks,
// so on a busy agent the task simply vanished, with a worker log line as its only trace.
// A permanently un-materializable repo is now surfaced instead: the runtime admits the
// task solely so the center will accept a block (Task.Block requires status=running) and
// blocks it with a machine-readable cause. start_task here is the fail-loud mechanism, NOT
// an admission for execution — no executor is ever forked.
func TestSpawnExecutor_Repo_EnsureSourceFailNoForkFailsLoud(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "k", sourcePath: t.TempDir() + "/src", ensureErr: context.Canceled}
	rt, _, home := engineForAgentMat(t, "agent-esf", mat)
	rt.cfg.SourcePrewarmAttempts = 1
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-1")}
	setToolCaller(rt, sc)

	spawnSettled(t, rt, "task-1")

	// Never forked, and nothing to tear down (no worktree was ever prepared).
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("an un-materializable source must NOT fork, got %+v", probs)
	}
	if n := len(mat.removeCalls()); n != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0 (nothing prepared)", n)
	}
	if n := len(mat.preparedReqs()); n != 0 {
		t.Fatalf("PrepareWorktree calls = %d, want 0 (no source)", n)
	}
	// Fail-loud: blocked with the machine-readable cause rather than left queued.
	blocked, ok := sc.callFor("block_task")
	if !ok {
		t.Fatalf("an un-materializable source must FAIL LOUD (block_task), got tools %v", sc.toolsSeen())
	}
	if reason, _ := blocked["reason"].(string); !containsSub(reason, string(CauseRepoSourceUnavailable)) {
		t.Fatalf("blocked_reason = %q, want [cause=%s]", reason, CauseRepoSourceUnavailable)
	}
}

// TestSpawnExecutor_Repo_PrepareFailNoStartTask: a PrepareWorktree failure likewise
// blocks start_task and needs no cleanup (the worktree was never created).
func TestSpawnExecutor_Repo_PrepareFailNoStartTask(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "k", sourcePath: t.TempDir() + "/src", baseRef: "main", prepareErr: context.Canceled}
	rt, _, _ := engineForAgentMat(t, "agent-pwf", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-2")}
	setToolCaller(rt, sc)

	spawnSettled(t, rt, "task-2")

	// The source materialized, so the task WAS re-driven — but the worktree failed, so
	// admission must never happen (red line A) and there is nothing to tear down.
	if _, ok := sc.callFor("start_task"); ok {
		t.Fatalf("start_task must NOT run when PrepareWorktree failed: tools = %v", sc.toolsSeen())
	}
	if n := len(mat.removeCalls()); n != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0", n)
	}
}

// TestSpawnExecutor_Repo_StartTaskDeclinedCleansWorktree proves red line B: start_task
// declined AFTER the worktree is prepared → the worktree is torn down (no leak) and no
// fork happens.
func TestSpawnExecutor_Repo_StartTaskDeclinedCleansWorktree(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-decl", sourcePath: t.TempDir() + "/src", baseRef: "main"}
	rt, _, home := engineForAgentMat(t, "agent-decl", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-3"), startErr: context.DeadlineExceeded}
	setToolCaller(rt, sc)

	spawnSettled(t, rt, "task-3")

	if _, ok := sc.callFor("start_task"); !ok {
		t.Fatalf("admission must still be attempted: tool calls = %v", sc.toolsSeen())
	}
	// Red line B is a NO-LEAK invariant, not a call count: every worktree this spawn
	// prepared must be torn down. (The prewarm re-drives a bounded number of times, so a
	// permanently-declining start_task legitimately yields several prepare→cleanup
	// cycles; what must never happen is a prepare without its remove.)
	assertNoWorktreeLeak(t, mat, "key-decl")
	if probs := loadRouting(t, home); len(probs) != 0 {
		t.Fatalf("declined admission must NOT fork, got %+v", probs)
	}
}

// TestSpawnExecutor_Repo_ForkFailsCleansWorktree proves red line B's second path: the
// center admits (start_task ok) but the local pool is saturated, so the fork fails —
// the already-prepared worktree must be torn down.
func TestSpawnExecutor_Repo_ForkFailsCleansWorktree(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-skew", sourcePath: t.TempDir() + "/src", baseRef: "main"}
	rt, ee, _ := engineForAgentMat(t, "agent-skew2", mat) // pool max 2
	for i := 0; i < 2; i++ {
		if _, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
			TaskRef: "sat-" + string(rune('a'+i)), Goal: executor.Goal{Title: "g"},
		}); err != nil {
			t.Fatalf("saturate %d: %v", i, err)
		}
	}
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-6")}
	setToolCaller(rt, sc)

	spawnSettled(t, rt, "task-6")

	if _, ok := sc.callFor("start_task"); !ok {
		t.Fatalf("admission still runs: tool calls = %v", sc.toolsSeen())
	}
	assertNoWorktreeLeak(t, mat, "key-skew")
}

// assertNoWorktreeLeak pins red line B: every worktree the runtime prepared was torn down
// again (matched by workspace path), and every teardown named the expected repo_key. It
// asserts the INVARIANT rather than a call count, so it stays valid regardless of how many
// times the prewarm re-drives a task whose fork keeps failing.
func assertNoWorktreeLeak(t *testing.T, mat *recordingMaterializer, wantKey string) {
	t.Helper()
	prepared := mat.preparedReqs()
	removed := mat.removeCalls()
	if len(prepared) == 0 {
		t.Fatalf("precondition: expected at least one PrepareWorktree")
	}
	removedPaths := map[string]bool{}
	for _, r := range removed {
		if r.repoKey != wantKey {
			t.Fatalf("RemoveWorktree repo_key = %q, want %q", r.repoKey, wantKey)
		}
		removedPaths[r.workspacePath] = true
	}
	for _, p := range prepared {
		if !removedPaths[p.WorkspacePath] {
			t.Fatalf("worktree LEAK: %s was prepared but never removed (prepared=%d removed=%d)",
				p.WorkspacePath, len(prepared), len(removed))
		}
	}
}

// TestRecover_Repo_CleansWorktreeSourceSurvives proves P5 + red line C: a TERMINAL
// executor whose Record carries a RepoKey has its worktree torn down at recovery, while
// the canonical source dir survives.
func TestRecover_Repo_CleansWorktreeSourceSurvives(t *testing.T) {
	seq := &callSeq{}
	sourceDir := t.TempDir() + "/repos/key-rec/source"
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-rec", sourcePath: sourceDir}
	rt, ee, home := engineForAgentMat(t, "agent-rec", mat)

	// Plant a TERMINAL orphan: a succeeded output + a Record with the worktree handle.
	fx, tr := seedExchange(t, home)
	execID := "exec-recovered1"
	if _, err := fx.Provision(execID); err != nil {
		t.Fatalf("provision: %v", err)
	}
	ws, _ := fx.Layout().WorkspaceDir(execID)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	if err := fx.WriteInput(executor.Input{
		ExecutorID: execID, ProblemID: "p1", Goal: executor.Goal{Title: "g"},
		Model: "m", CLI: "claude-code", Source: executor.SourceRefs{TaskRef: "task-r"},
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := fx.WriteOutput(executor.Output{ExecutorID: execID, Success: true, Result: "done", FinishedAt: time.Now()}); err != nil {
		t.Fatalf("write output: %v", err)
	}
	if err := tr.Write(executor.Record{
		ExecutorID: execID, PID: deadPID(t), SpawnedAt: time.Now(),
		RepoKey: "key-rec", SourcePath: sourceDir,
	}); err != nil {
		t.Fatalf("write record: %v", err)
	}

	if err := rt.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	// Delayed teardown (issue-f30b7e7b): recovery retains + marks the terminal executor;
	// the worktree cleaner runs on the reaper pass, not inline at recovery-finalize.
	if _, err := ee.monitor.ReapFinalized(context.Background(), 0, 0); err != nil {
		t.Fatalf("ReapFinalized: %v", err)
	}
	rm := mat.removeCalls()
	if len(rm) != 1 || rm[0].repoKey != "key-rec" || rm[0].sourcePath != sourceDir {
		t.Fatalf("recovery+reap RemoveWorktree = %+v, want one for key-rec/%s", rm, sourceDir)
	}
	if _, err := os.Stat(sourceDir); err != nil {
		t.Fatalf("canonical source must survive recovery cleanup: %v", err)
	}
}

// TestRepo_RealGitEndToEndCanonicalSourceSurvives exercises a REAL LocalGitMaterializer
// over a temp git repo end-to-end: real clone (EnsureSource) → real `git worktree add`
// (PrepareWorktree) → terminal finalize via recovery → real `git worktree remove`
// (through the executor.WorktreeCleaner adapter wired into the Monitor). It asserts the
// per-executor worktree is torn down but repos/<key>/source SURVIVES (red line C,
// end-to-end). Finalize is driven through the deterministic recovery path (no live-drain
// race).
func TestRepo_RealGitEndToEndCanonicalSourceSurvives(t *testing.T) {
	requireGit(t)
	remote := makeGitRemote(t) // a source with one commit on main
	reposRoot := t.TempDir()
	mat, err := reporepo.NewLocalGitMaterializer(reposRoot, nil, nil)
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	rt, ee, home := engineForAgentMat(t, "agent-realgit", mat)
	ctx := context.Background()

	// Real clone + real worktree add (the exact calls SpawnExecutor makes).
	execID := ee.engine.NewExecutorID()
	source, err := mat.EnsureSource(ctx, reporepo.RepoTarget{URL: remote, Provider: "git", DefaultBranch: "main", BaseRef: "main"})
	if err != nil {
		t.Fatalf("EnsureSource: %v", err)
	}
	fx, tr := seedExchange(t, home)
	if _, err := fx.Provision(execID); err != nil {
		t.Fatalf("provision: %v", err)
	}
	ws, _ := fx.Layout().WorkspaceDir(execID)
	wt, err := mat.PrepareWorktree(ctx, source, reporepo.WorktreeRequest{
		ExecutorID: execID, TaskID: "task-rg",
		BranchName: "ac-exec/task-rg/" + execID, WorkspacePath: ws, BaseRef: source.BaseRef,
	})
	if err != nil {
		t.Fatalf("PrepareWorktree: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(ws, ".git")); statErr != nil {
		t.Fatalf("real worktree should exist after PrepareWorktree: %v", statErr)
	}
	sourcePath := filepath.Join(reposRoot, reporepo.RepoKey(remote), "source")
	if _, statErr := os.Stat(filepath.Join(sourcePath, ".git")); statErr != nil {
		t.Fatalf("canonical source should exist after clone: %v", statErr)
	}

	// Plant a TERMINAL executor (succeeded) + a Record carrying the worktree handle, then
	// drive finalize via recovery → real `git worktree remove`.
	if err := fx.WriteInput(executor.Input{
		ExecutorID: execID, ProblemID: "p", Goal: executor.Goal{Title: "g"}, Model: "m",
		CLI: "claude-code", Source: executor.SourceRefs{TaskRef: "task-rg"}, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := fx.WriteOutput(executor.Output{ExecutorID: execID, Success: true, Result: "done", FinishedAt: time.Now()}); err != nil {
		t.Fatalf("write output: %v", err)
	}
	if err := tr.Write(executor.Record{
		ExecutorID: execID, PID: deadPID(t), SpawnedAt: time.Now(),
		RepoKey: wt.RepoKey, SourcePath: wt.SourcePath,
	}); err != nil {
		t.Fatalf("write record: %v", err)
	}

	if err := rt.Recover(ctx); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	if _, statErr := os.Stat(ws); !os.IsNotExist(statErr) {
		t.Fatalf("worktree must be removed by finalize, stat err = %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(sourcePath, ".git")); statErr != nil {
		t.Fatalf("canonical source must survive real worktree removal: %v", statErr)
	}
}

package agentruntime

// repo_workspace_test.go — flag-ON (AC_EXECUTOR_GIT_WORKTREE) integration coverage for
// the P3/P4/P5 repo-workspace track: SpawnExecutor materializes the canonical source +
// a per-executor worktree BEFORE start_task (red line A), tears it down on every
// failure path (red line B) AND on finalize/recovery, and NEVER deletes the canonical
// source (red line C). Flag-OFF behavior is proven byte-for-byte by the pre-existing
// executor_runtime_test.go suite (unchanged).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
	"github.com/oopslink/agent-center/internal/workerdaemon/reporepo"
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
	removed    []removeArgs
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

func (m *recordingMaterializer) RemoveWorktree(_ context.Context, wt reporepo.PreparedWorktree) error {
	m.seq.add("RemoveWorktree")
	m.mu.Lock()
	m.removed = append(m.removed, removeArgs{wt.RepoKey, wt.SourcePath, wt.WorkspacePath})
	m.mu.Unlock()
	// Tear down ONLY the worktree — never the canonical source (design §10).
	_ = os.RemoveAll(wt.WorkspacePath)
	return nil
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

// engineForAgentMat builds a flag-ON runtime (materializer wired) + attached engine.
func engineForAgentMat(t *testing.T, agentID string, mat reporepo.RepoMaterializer) (*LocalRuntime, *ExecutorEngine, string) {
	t.Helper()
	trueBin := lookTrue(t)
	base := t.TempDir()
	rt := newExecRuntime(t, base, agentID, trueBin)
	rt.cfg.Materializer = mat // AC_EXECUTOR_GIT_WORKTREE ON
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
func TestSpawnExecutor_Repo_PrepareBeforeStartTask(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "key-abc", sourcePath: t.TempDir() + "/repos/key-abc/source", baseRef: "main"}
	rt, _, home := engineForAgentMat(t, "agent-repo", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-9")}
	setToolCaller(rt, sc)

	res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-9"})
	if err != nil || res == nil {
		t.Fatalf("SpawnExecutor = (%v, %v), want a SpawnResult", res, err)
	}

	order := seq.snapshot()
	iEnsure, iPrep, iStart := indexOf(order, "EnsureSource"), indexOf(order, "PrepareWorktree"), indexOf(order, "start_task")
	if iEnsure < 0 || iPrep < 0 || iStart < 0 {
		t.Fatalf("missing steps in order %v", order)
	}
	if !(iEnsure < iPrep && iPrep < iStart) {
		t.Fatalf("order violation: want EnsureSource<PrepareWorktree<start_task, got %v", order)
	}

	// The prepared worktree path must be the launched executor's actual workspace.
	reqs := mat.preparedReqs()
	if len(reqs) != 1 {
		t.Fatalf("PrepareWorktree calls = %d, want 1", len(reqs))
	}
	wantWS, _ := rt.execEngine().fx.Layout().WorkspaceDir(res.ExecutorID)
	if reqs[0].WorkspacePath != wantWS {
		t.Fatalf("worktree path %q != executor workspace %q", reqs[0].WorkspacePath, wantWS)
	}
	if reqs[0].ExecutorID != res.ExecutorID {
		t.Fatalf("worktree executor id %q != launched %q", reqs[0].ExecutorID, res.ExecutorID)
	}
	if reqs[0].BranchName != "ac-exec/task-9/"+res.ExecutorID {
		t.Fatalf("branch = %q, want ac-exec/task-9/%s", reqs[0].BranchName, res.ExecutorID)
	}

	// P5: the pool persisted the worktree teardown handle into the recovery Record.
	_, tr := seedExchange(t, home)
	rec, err := tr.Read(res.ExecutorID)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if rec.RepoKey != "key-abc" || rec.SourcePath != mat.sourcePath {
		t.Fatalf("record teardown handle = {%q,%q}, want {key-abc,%s}", rec.RepoKey, rec.SourcePath, mat.sourcePath)
	}
}

// TestSpawnExecutor_Repo_EnsureSourceFailNoStartTask proves red line A's failure half:
// an EnsureSource failure means start_task is NEVER called and nothing is torn down.
func TestSpawnExecutor_Repo_EnsureSourceFailNoStartTask(t *testing.T) {
	seq := &callSeq{}
	mat := &recordingMaterializer{seq: seq, repoKey: "k", sourcePath: t.TempDir() + "/src", ensureErr: context.Canceled}
	rt, _, _ := engineForAgentMat(t, "agent-esf", mat)
	sc := &scriptedToolCaller{seq: seq, getTaskBody: repoTaskBody("task-1")}
	setToolCaller(rt, sc)

	if res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-1"}); res != nil || err != nil {
		t.Fatalf("SpawnExecutor = (%v, %v), want (nil, nil)", res, err)
	}
	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want [get_task] only (start_task NEVER called)", seen)
	}
	if n := len(mat.removeCalls()); n != 0 {
		t.Fatalf("RemoveWorktree calls = %d, want 0 (nothing prepared)", n)
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

	if res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-2"}); res != nil || err != nil {
		t.Fatalf("SpawnExecutor = (%v, %v), want (nil, nil)", res, err)
	}
	if seen := sc.toolsSeen(); len(seen) != 1 || seen[0] != "get_task" {
		t.Fatalf("tool calls = %v, want [get_task] only", seen)
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

	if res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-3"}); res != nil || err != nil {
		t.Fatalf("SpawnExecutor = (%v, %v), want (nil, nil)", res, err)
	}
	if seen := sc.toolsSeen(); len(seen) != 2 || seen[1] != "start_task" {
		t.Fatalf("tool calls = %v, want get_task then start_task", seen)
	}
	rm := mat.removeCalls()
	if len(rm) != 1 || rm[0].repoKey != "key-decl" {
		t.Fatalf("RemoveWorktree calls = %+v, want exactly one for key-decl (cleanup)", rm)
	}
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

	if res, err := rt.SpawnExecutor(context.Background(), SpawnRequest{TaskID: "task-6"}); res != nil || err != nil {
		t.Fatalf("SpawnExecutor = (%v, %v), want (nil, nil)", res, err)
	}
	if seen := sc.toolsSeen(); len(seen) != 2 || seen[1] != "start_task" {
		t.Fatalf("admission still runs: tool calls = %v", seen)
	}
	rm := mat.removeCalls()
	if len(rm) != 1 || rm[0].repoKey != "key-skew" {
		t.Fatalf("RemoveWorktree calls = %+v, want one for key-skew (fork-fail cleanup)", rm)
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
	_ = ee

	rm := mat.removeCalls()
	if len(rm) != 1 || rm[0].repoKey != "key-rec" || rm[0].sourcePath != sourceDir {
		t.Fatalf("recovery RemoveWorktree = %+v, want one for key-rec/%s", rm, sourceDir)
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

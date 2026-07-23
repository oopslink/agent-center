package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// fakeRunner records git invocations and returns canned output/errors keyed by a
// substring of the joined args. Mirrors mergecheck's fake.
type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, _ string, _ []string, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	return "", f.err
}

func (f *fakeRunner) lastCall() string {
	if len(f.calls) == 0 {
		return ""
	}
	return strings.Join(f.calls[len(f.calls)-1], " ")
}

func TestNewWorktreeProvisioner_Errors(t *testing.T) {
	if _, err := NewWorktreeProvisioner("  ", nil); err == nil {
		t.Fatal("expected error for empty repo dir")
	}
	p, err := NewWorktreeProvisioner("/repo", nil) // nil runner → real git default
	if err != nil {
		t.Fatalf("NewWorktreeProvisioner: %v", err)
	}
	if p.runner == nil {
		t.Fatal("nil runner should default to exec git runner")
	}
}

func TestExecGitRunner_ContextCancelKillsGitProcessGroup(t *testing.T) {
	dir := t.TempDir()
	sshPath := filepath.Join(dir, "fake-ssh.sh")
	pidPath := filepath.Join(dir, "ssh.pid")
	script := "#!/bin/sh\n" +
		"echo $$ > " + strconv.Quote(pidPath) + "\n" +
		"printf 'fake ssh started\\n' >&2\n" +
		"sleep 30\n"
	if err := os.WriteFile(sshPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	_, err := NewExecGitRunner().Run(ctx, dir, append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND="+sshPath,
	), "ls-remote", "ssh://example.invalid/repo.git")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("Run unexpectedly succeeded")
	}
	if elapsed > 4*time.Second {
		t.Fatalf("Run took %s after ctx cancellation; a child process likely kept git pipes open", elapsed)
	}

	pidBytes, readErr := os.ReadFile(pidPath)
	if readErr != nil {
		t.Fatalf("fake ssh was not invoked: %v", readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if parseErr != nil {
		t.Fatalf("parse fake ssh pid: %v", parseErr)
	}
	for i := 0; i < 10; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("fake ssh pid %d survived git context cancellation", pid)
}

func TestWorktree_Add_Args(t *testing.T) {
	fr := &fakeRunner{}
	p, _ := NewWorktreeProvisioner("/repo", fr)
	if err := p.Add(context.Background(), "/wt", "main"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := fr.lastCall(); got != "worktree add /wt main" {
		t.Errorf("Add args=%q", got)
	}
}

func TestWorktree_AddNewBranch_Args(t *testing.T) {
	fr := &fakeRunner{}
	p, _ := NewWorktreeProvisioner("/repo", fr)
	if err := p.AddNewBranch(context.Background(), "/wt", "feat/x", "dev"); err != nil {
		t.Fatalf("AddNewBranch: %v", err)
	}
	if got := fr.lastCall(); got != "worktree add -b feat/x /wt dev" {
		t.Errorf("AddNewBranch args=%q", got)
	}
}

func TestWorktree_Remove_Args(t *testing.T) {
	fr := &fakeRunner{}
	p, _ := NewWorktreeProvisioner("/repo", fr)
	if err := p.Remove(context.Background(), "/wt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(fr.calls) != 2 {
		t.Fatalf("Remove should run remove + prune, got %d calls", len(fr.calls))
	}
	if strings.Join(fr.calls[0], " ") != "worktree remove --force /wt" {
		t.Errorf("remove args=%q", fr.calls[0])
	}
	if strings.Join(fr.calls[1], " ") != "worktree prune" {
		t.Errorf("prune args=%q", fr.calls[1])
	}
}

func TestWorktree_Validation(t *testing.T) {
	fr := &fakeRunner{}
	p, _ := NewWorktreeProvisioner("/repo", fr)
	ctx := context.Background()
	cases := []struct {
		name string
		fn   func() error
	}{
		{"Add empty path", func() error { return p.Add(ctx, "", "ref") }},
		{"Add empty ref", func() error { return p.Add(ctx, "/wt", "") }},
		{"AddNewBranch empty path", func() error { return p.AddNewBranch(ctx, "", "b", "base") }},
		{"AddNewBranch empty branch", func() error { return p.AddNewBranch(ctx, "/wt", "", "base") }},
		{"AddNewBranch empty base", func() error { return p.AddNewBranch(ctx, "/wt", "b", "") }},
		{"Remove empty path", func() error { return p.Remove(ctx, "") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.fn(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
	if len(fr.calls) != 0 {
		t.Errorf("validation failures should not invoke git, got %v", fr.calls)
	}
}

func TestWorktree_RunnerError(t *testing.T) {
	fr := &fakeRunner{err: errors.New("git boom")}
	p, _ := NewWorktreeProvisioner("/repo", fr)
	if err := p.Add(context.Background(), "/wt", "ref"); err == nil || !strings.Contains(err.Error(), "git boom") {
		t.Errorf("Add should surface runner error, got %v", err)
	}
	if err := p.AddNewBranch(context.Background(), "/wt", "b", "base"); err == nil {
		t.Error("AddNewBranch should surface runner error")
	}
	if err := p.Remove(context.Background(), "/wt"); err == nil {
		t.Error("Remove should surface runner error")
	}
}

// TestWorktree_IsolatedFromLiveBranchWorktree is the issue-37015227 ① regression lock:
// a task targeting an ALREADY-EXISTING branch X, where a live agent already holds a
// worktree checked out on X, must get its OWN independent worktree at a DIFFERENT path on
// a FRESH branch — never the live worktree, never sharing branch X. It also pins the
// isolation guard that refuses to provision over an existing worktree or onto a live branch.
func TestWorktree_IsolatedFromLiveBranchWorktree(t *testing.T) {
	gitBin, repo := newSourceRepo(t) // source repo on branch main, one commit
	// An existing feature branch X off main (the branch the task targets).
	runGitIn(t, gitBin, repo, "branch", "feat/x", "main")

	prov, err := NewWorktreeProvisioner(repo, NewExecGitRunner())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Simulate the LIVE AGENT: a worktree checked out directly on feat/x, with a working
	// file the agent is mid-edit on (uncommitted).
	liveWt := filepath.Join(t.TempDir(), "live-agent-wt")
	if err := prov.Add(ctx, liveWt, "feat/x"); err != nil {
		t.Fatalf("provision live-agent worktree: %v", err)
	}
	liveMarker := filepath.Join(liveWt, "live-agent-wip.txt")
	if err := os.WriteFile(liveMarker, []byte("owned by live agent"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Dispatch a SAME-BRANCH-X executor task: it must land in its own id-keyed worktree on
	// a fresh branch OFF feat/x's tip — never the live worktree.
	agentRoot := t.TempDir()
	layout, _ := NewLayout(agentRoot)
	execID := "exec-samebranch"
	execWs, _ := layout.WorkspaceDir(execID)
	if err := prov.AddNewBranch(ctx, execWs, "executor/"+execID, "feat/x"); err != nil {
		t.Fatalf("provision same-branch executor worktree: %v", err)
	}

	// (1) A DIFFERENT path from the live worktree.
	if cleanPath(execWs) == cleanPath(liveWt) {
		t.Fatalf("executor worktree path %q collides with the live worktree %q", execWs, liveWt)
	}
	// (2) A real, independent checkout: has the base content...
	if _, err := os.Stat(filepath.Join(execWs, "README.md")); err != nil {
		t.Errorf("executor worktree missing base content: %v", err)
	}
	// ...on its OWN fresh branch, not the live branch feat/x.
	br := gitOut(t, gitBin, execWs, "rev-parse", "--abbrev-ref", "HEAD")
	if br != "executor/"+execID {
		t.Errorf("executor worktree on branch %q, want its own fresh branch executor/%s (never the live branch)", br, execID)
	}
	// (3) The live agent's uncommitted work is NOT visible in the executor worktree, and the
	// live worktree is left untouched.
	if _, err := os.Stat(filepath.Join(execWs, "live-agent-wip.txt")); !os.IsNotExist(err) {
		t.Errorf("live agent's file leaked into the executor worktree (err=%v) — not isolated", err)
	}
	if _, err := os.Stat(liveMarker); err != nil {
		t.Errorf("live agent's worktree was disturbed: %v", err)
	}

	// (4) Guard: refuse to provision OVER the existing live worktree path.
	if err := prov.AddNewBranch(ctx, liveWt, "executor/other", "main"); !errors.Is(err, ErrWorktreePathInUse) {
		t.Errorf("provisioning over an existing worktree must fail ErrWorktreePathInUse, got %v", err)
	}
	// (5) Guard: refuse to OCCUPY the live branch feat/x in a second worktree (never share a
	// live agent's branch), via either AddNewBranch or a direct Add.
	freshPath := filepath.Join(t.TempDir(), "would-share-feat-x")
	if err := prov.AddNewBranch(ctx, freshPath, "feat/x", "main"); !errors.Is(err, ErrBranchCheckedOutElsewhere) {
		t.Errorf("occupying a live branch must fail ErrBranchCheckedOutElsewhere, got %v", err)
	}
	if err := prov.Add(ctx, freshPath, "feat/x"); !errors.Is(err, ErrBranchCheckedOutElsewhere) {
		t.Errorf("direct-checkout of a live branch must fail ErrBranchCheckedOutElsewhere, got %v", err)
	}
}

// gitOut runs git in dir and returns trimmed stdout, failing on error.
func gitOut(t *testing.T, gitBin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(gitBin, args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestTwoConcurrentExecutorsIsolated is the F2 acceptance test with REAL git: two
// executors each get their own worktree off a shared source repo; work written in
// one worktree is invisible to the other, and the full file-exchange chain runs
// for both. git is a deterministic local binary; the test skips if it is absent.
func TestTwoConcurrentExecutorsIsolated(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}

	// Build a source repo with one commit on branch dev.
	repo := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "dev")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "-A")
	runGit("commit", "-q", "-m", "base")

	agentRoot := t.TempDir()
	layout, _ := NewLayout(agentRoot)
	fx, _ := NewFileExchange(layout, clock.NewFakeClock(testNow))
	prov, err := NewWorktreeProvisioner(repo, NewExecGitRunner())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	type exrec struct {
		id     string
		branch string
		marker string
	}
	execs := []exrec{
		{"exec-aaa111", "executor/aaa", "from-aaa"},
		{"exec-bbb222", "executor/bbb", "from-bbb"},
	}

	for _, e := range execs {
		if _, err := fx.Provision(e.id); err != nil {
			t.Fatalf("Provision %s: %v", e.id, err)
		}
		in := validInput()
		in.ExecutorID = e.id
		if err := fx.WriteInput(in); err != nil {
			t.Fatalf("WriteInput %s: %v", e.id, err)
		}
		ws, _ := layout.WorkspaceDir(e.id)
		if err := prov.AddNewBranch(ctx, ws, e.branch, "dev"); err != nil {
			t.Fatalf("AddNewBranch %s: %v", e.id, err)
		}
		// Executor writes a distinct file into ITS worktree, via the containment guard.
		dest, err := fx.ContainedPath(e.id, e.marker+".txt", false)
		if err != nil {
			t.Fatalf("ContainedPath %s: %v", e.id, err)
		}
		if err := os.WriteFile(dest, []byte(e.marker), 0o600); err != nil {
			t.Fatalf("write marker %s: %v", e.id, err)
		}
		if err := fx.WriteOutput(Output{ExecutorID: e.id, Success: true, Result: e.marker}); err != nil {
			t.Fatalf("WriteOutput %s: %v", e.id, err)
		}
		if err := fx.WriteStatus(Status{ExecutorID: e.id, State: StateDone, Model: in.Model}); err != nil {
			t.Fatalf("WriteStatus %s: %v", e.id, err)
		}
	}

	// Isolation: each worktree sees ONLY its own marker, not the other's, and the
	// shared README is present in both (each is a real checkout of dev).
	for _, e := range execs {
		ws, _ := layout.WorkspaceDir(e.id)
		if _, err := os.Stat(filepath.Join(ws, e.marker+".txt")); err != nil {
			t.Errorf("%s missing own marker: %v", e.id, err)
		}
		if _, err := os.Stat(filepath.Join(ws, "README.md")); err != nil {
			t.Errorf("%s missing shared README: %v", e.id, err)
		}
		for _, other := range execs {
			if other.id == e.id {
				continue
			}
			if _, err := os.Stat(filepath.Join(ws, other.marker+".txt")); !os.IsNotExist(err) {
				t.Errorf("%s leaked %s's marker (err=%v) — worktrees not isolated", e.id, other.id, err)
			}
		}
	}

	// Orchestrator reads each output back independently.
	for _, e := range execs {
		out, err := fx.ReadOutput(e.id)
		if err != nil {
			t.Fatalf("ReadOutput %s: %v", e.id, err)
		}
		if out.Result != e.marker {
			t.Errorf("%s output=%q want %q", e.id, out.Result, e.marker)
		}
	}

	// Scan sees both executors.
	snaps, err := fx.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("Scan = %d executors, want 2", len(snaps))
	}

	// Cleanup: remove one worktree; the other remains intact.
	wsA, _ := layout.WorkspaceDir(execs[0].id)
	if err := prov.Remove(ctx, wsA); err != nil {
		t.Fatalf("Remove worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wsA, "README.md")); !os.IsNotExist(err) {
		t.Errorf("worktree A still present after Remove: %v", err)
	}
	wsB, _ := layout.WorkspaceDir(execs[1].id)
	if _, err := os.Stat(filepath.Join(wsB, execs[1].marker+".txt")); err != nil {
		t.Errorf("worktree B disturbed by A's removal: %v", err)
	}
}

package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Isolation-invariant sentinels (issue-37015227 ①). A fork executor must ALWAYS get its
// own independent worktree; it must never be provisioned OVER an existing worktree, nor
// occupy a branch that is already checked out in another (live) worktree. These make that
// invariant a loud, machine-readable failure instead of a silent collision / cryptic git
// error (the "共用 worktree 撞车" hole). Match with errors.Is.
var (
	// ErrWorktreePathInUse is returned when the intended worktree path is already a
	// registered git worktree — refusing to reuse it (would clobber a live checkout).
	ErrWorktreePathInUse = errors.New("executor: worktree path already in use (isolation)")
	// ErrBranchCheckedOutElsewhere is returned when the branch the new worktree would
	// OCCUPY is already checked out in another worktree — refusing to share a live agent's
	// branch worktree (fork executors branch OFF a ref, never onto a live branch).
	ErrBranchCheckedOutElsewhere = errors.New("executor: branch already checked out in another worktree (isolation)")
)

// GitRunner is the port the worktree provisioner uses to invoke git. Extracted so
// tests can swap a fake (recording args + returning canned output/exit errors);
// real git is reached via execGitRunner. Mirrors cognition.memory's GitRunner.
type GitRunner interface {
	// Run executes "git" with args under workdir + env, returning combined
	// stdout/stderr and the underlying exec error (an *exec.ExitError on non-zero).
	Run(ctx context.Context, workdir string, env []string, args ...string) (string, error)
}

type execGitRunner struct{}

func (execGitRunner) Run(ctx context.Context, workdir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return out.String(), err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return out.String(), err
	case <-ctx.Done():
		signalProcessGroup(cmd.Process.Pid, syscall.SIGTERM)
		var err error
		select {
		case err = <-done:
		case <-time.After(2 * time.Second):
			signalProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
			err = <-done
		}
		if err == nil {
			err = ctx.Err()
		}
		return out.String(), err
	}
}

func signalProcessGroup(pid int, sig syscall.Signal) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, sig)
}

// NewExecGitRunner returns the real git-binary GitRunner (production default).
func NewExecGitRunner() GitRunner { return execGitRunner{} }

// WorktreeProvisioner materialises and tears down an executor's isolated git
// worktree (design §6.D) under a source repo. Each executor gets its OWN worktree
// path, so two concurrent executors operate on independent checkouts and never
// step on each other — the F2 acceptance criterion. Safe to share (stateless
// beyond repoDir + runner).
type WorktreeProvisioner struct {
	repoDir string
	runner  GitRunner
}

// NewWorktreeProvisioner builds a provisioner rooted at repoDir (the source git
// repository the worktrees branch off). A nil runner defaults to the real git
// binary.
func NewWorktreeProvisioner(repoDir string, runner GitRunner) (*WorktreeProvisioner, error) {
	if strings.TrimSpace(repoDir) == "" {
		return nil, errors.New("executor: repo_dir required")
	}
	if runner == nil {
		runner = NewExecGitRunner()
	}
	return &WorktreeProvisioner{repoDir: repoDir, runner: runner}, nil
}

// Add creates a worktree at worktreePath checked out at ref (a branch / commit /
// tag that must already exist). Runs `git worktree add <path> <ref>` from the
// source repo. Use AddNewBranch when the executor needs a fresh branch.
func (p *WorktreeProvisioner) Add(ctx context.Context, worktreePath, ref string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return errors.New("executor: worktree_path required")
	}
	if strings.TrimSpace(ref) == "" {
		return errors.New("executor: ref required")
	}
	// Isolation precheck: Add checks out ref DIRECTLY, so ref is the branch this worktree
	// would occupy — refuse if that path is taken or ref is live in another worktree.
	if err := p.assertIsolated(ctx, worktreePath, ref); err != nil {
		return err
	}
	if out, err := p.run(ctx, "worktree", "add", worktreePath, ref); err != nil {
		return fmt.Errorf("executor: worktree add %q@%s: %w: %s", worktreePath, ref, err, out)
	}
	return nil
}

// AddNewBranch creates a worktree at worktreePath on a NEW branch newBranch based
// at baseRef. Runs `git worktree add -b <newBranch> <path> <baseRef>`. This is the
// common case: the orchestrator gives each executor its own branch so their work
// is isolated end-to-end.
func (p *WorktreeProvisioner) AddNewBranch(ctx context.Context, worktreePath, newBranch, baseRef string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return errors.New("executor: worktree_path required")
	}
	if strings.TrimSpace(newBranch) == "" {
		return errors.New("executor: new_branch required")
	}
	if strings.TrimSpace(baseRef) == "" {
		return errors.New("executor: base_ref required")
	}
	// Isolation precheck: AddNewBranch creates newBranch OFF baseRef (baseRef is only the
	// start point, never occupied), so the branch this worktree occupies is newBranch —
	// refuse if the path is taken or newBranch is somehow already checked out elsewhere.
	// This is the treatment for a task on an existing branch X: baseRef=X gives an
	// INDEPENDENT worktree on a fresh branch, and the guard proves we never land on X's
	// own (live) worktree.
	if err := p.assertIsolated(ctx, worktreePath, newBranch); err != nil {
		return err
	}
	if out, err := p.run(ctx, "worktree", "add", "-b", newBranch, worktreePath, baseRef); err != nil {
		return fmt.Errorf("executor: worktree add -b %s %q@%s: %w: %s", newBranch, worktreePath, baseRef, err, out)
	}
	return nil
}

// assertIsolated is the fork-executor isolation guard (issue-37015227 ①): before adding a
// worktree it lists the repo's existing worktrees and refuses when (a) worktreePath is
// already a registered worktree (ErrWorktreePathInUse) or (b) occupiesBranch — the branch
// the new worktree will check out — is already checked out in another worktree
// (ErrBranchCheckedOutElsewhere). It turns "共用 worktree 撞车" from a silent collision /
// cryptic git error into a loud, machine-readable failure. FAIL-CLOSED: if the worktree
// list itself cannot be read, provisioning is refused (we will not add a worktree we
// cannot prove is isolated) rather than risk a collision.
func (p *WorktreeProvisioner) assertIsolated(ctx context.Context, worktreePath, occupiesBranch string) error {
	entries, err := p.listWorktrees(ctx)
	if err != nil {
		return fmt.Errorf("executor: worktree isolation precheck for %q: %w", worktreePath, err)
	}
	target := cleanPath(worktreePath)
	branch := strings.TrimSpace(occupiesBranch)
	for _, e := range entries {
		if cleanPath(e.path) == target {
			return fmt.Errorf("%w: %q is already a registered worktree", ErrWorktreePathInUse, worktreePath)
		}
		if branch != "" && e.branch == branch {
			return fmt.Errorf("%w: branch %q is checked out at %q", ErrBranchCheckedOutElsewhere, branch, e.path)
		}
	}
	return nil
}

// worktreeRef is one `git worktree list --porcelain` record: the worktree path and its
// checked-out branch (short name; "" when detached).
type worktreeRef struct {
	path   string
	branch string
}

// listWorktrees returns the repo's registered worktrees (parsed from `git worktree list
// --porcelain`). Surfaces the git error so assertIsolated can fail-closed.
func (p *WorktreeProvisioner) listWorktrees(ctx context.Context) ([]worktreeRef, error) {
	out, err := p.run(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("worktree list: %w: %s", err, out)
	}
	var refs []worktreeRef
	var cur worktreeRef
	flush := func() {
		if cur.path != "" {
			refs = append(refs, cur)
		}
		cur = worktreeRef{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			cur.branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "":
			flush()
		}
	}
	flush()
	return refs, nil
}

// cleanPath normalizes a filesystem path for worktree-identity comparison: EvalSymlinks so
// a symlinked temp root (e.g. macOS /var→/private/var) matches git's resolved path, falling
// back to filepath.Clean when the path does not exist yet.
func cleanPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// Remove tears down the worktree at worktreePath (design §7 step h cleanup).
// `git worktree remove --force` drops it even with a dirty/locked working tree;
// the orchestrator has already harvested output.json before cleanup. A follow-up
// `worktree prune` is best-effort to clear any stale administrative entry.
func (p *WorktreeProvisioner) Remove(ctx context.Context, worktreePath string) error {
	if strings.TrimSpace(worktreePath) == "" {
		return errors.New("executor: worktree_path required")
	}
	if out, err := p.run(ctx, "worktree", "remove", "--force", worktreePath); err != nil {
		return fmt.Errorf("executor: worktree remove %q: %w: %s", worktreePath, err, out)
	}
	if out, err := p.run(ctx, "worktree", "prune"); err != nil {
		return fmt.Errorf("executor: worktree prune after remove %q: %w: %s", worktreePath, err, out)
	}
	return nil
}

// run invokes git under repoDir with a NEUTRALIZED environment so host gitconfig /
// prompts / signing can never interfere with worktree management (mirrors
// cognition.memory's GitRunner).
func (p *WorktreeProvisioner) run(ctx context.Context, args ...string) (string, error) {
	env := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME=" + p.repoDir,
		"PATH=" + safeDefaultPath(),
	}
	return p.runner.Run(ctx, p.repoDir, env, args...)
}

// safeDefaultPath returns a deterministic minimal PATH that finds git on common
// dev / CI images (mirrors cognition.memory's GitRunner).
func safeDefaultPath() string {
	return "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
}

package executor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// GitRunner is the port the worktree provisioner uses to invoke git. Extracted so
// tests can swap a fake (recording args + returning canned output/exit errors);
// real git is reached via execGitRunner. Mirrors mergecheck / cognition.memory's
// GitRunner exactly.
type GitRunner interface {
	// Run executes "git" with args under workdir + env, returning combined
	// stdout/stderr and the underlying exec error (an *exec.ExitError on non-zero).
	Run(ctx context.Context, workdir string, env []string, args ...string) (string, error)
}

type execGitRunner struct{}

func (execGitRunner) Run(ctx context.Context, workdir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
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
	if out, err := p.run(ctx, "worktree", "add", "-b", newBranch, worktreePath, baseRef); err != nil {
		return fmt.Errorf("executor: worktree add -b %s %q@%s: %w: %s", newBranch, worktreePath, baseRef, err, out)
	}
	return nil
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
// mergecheck.run).
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
// dev / CI images (matches mergecheck / cognition.memory's helper).
func safeDefaultPath() string {
	return "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
}

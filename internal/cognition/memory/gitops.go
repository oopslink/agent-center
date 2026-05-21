package memory

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Sentinel errors. Use errors.Is to test.
var (
	ErrMemoryDirNotInitialized = errors.New("memory: directory is not a git repository")
	ErrMemoryFileExists        = errors.New("memory: skeleton file already exists")
	ErrMemoryGitOpFailed       = errors.New("memory: git operation failed")
	ErrMemoryDirEmpty          = errors.New("memory: memoryDir is empty")
)

// GitRunner is the port the gitops service uses to invoke git. It is
// extracted so tests can swap a fake implementation; real git is reached
// via execGitRunner.
type GitRunner interface {
	// Run executes "git" with args under workdir + env. Returns combined
	// stdout/stderr and the underlying exec error (if any).
	Run(ctx context.Context, workdir string, env []string, args ...string) (string, error)
}

// execGitRunner shells out to the system git binary. Production default.
type execGitRunner struct{}

// Run invokes git via os/exec.
func (execGitRunner) Run(ctx context.Context, workdir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// NewExecGitRunner returns the real git-binary GitRunner.
func NewExecGitRunner() GitRunner { return execGitRunner{} }

// GitOps implements the gitops service backing MemoryGitOpsService
// (cognition/02-memory § 5.1).
type GitOps struct {
	memoryDir string
	runner    GitRunner
	// authorName / authorEmail set as GIT_AUTHOR_* / GIT_COMMITTER_*. HOME
	// is overridden so the user's ~/.gitconfig (gpgsign, hooks) cannot
	// pollute test runs.
	homeOverride string
}

// NewGitOps wires a GitOps against memoryDir + GitRunner. homeOverride
// MAY be empty; when set, all git invocations get HOME=<value> +
// XDG_CONFIG_HOME=<value> + GIT_CONFIG_GLOBAL=/dev/null env so they
// never inherit dev-machine settings (cognition/02 § 3.2).
func NewGitOps(memoryDir string, runner GitRunner, homeOverride string) *GitOps {
	if runner == nil {
		runner = NewExecGitRunner()
	}
	return &GitOps{memoryDir: memoryDir, runner: runner, homeOverride: homeOverride}
}

// IsGitRepo reports whether memoryDir is an initialised git repository.
func (g *GitOps) IsGitRepo(ctx context.Context) (bool, error) {
	if g.memoryDir == "" {
		return false, ErrMemoryDirEmpty
	}
	out, err := g.run(ctx, "system:bootstrap", "system:bootstrap@agent-center.local",
		"rev-parse", "--git-dir")
	if err != nil {
		// "not a git repository" is a normal not-initialized state, not
		// an error to surface.
		if strings.Contains(out, "not a git repository") {
			return false, nil
		}
		return false, fmt.Errorf("%w: rev-parse: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	return true, nil
}

// Init runs `git init` + sets the initial branch to main + ensures
// commits work without a global config. Idempotent.
func (g *GitOps) Init(ctx context.Context) error {
	if g.memoryDir == "" {
		return ErrMemoryDirEmpty
	}
	out, err := g.run(ctx, "system:bootstrap", "system:bootstrap@agent-center.local",
		"init", "--initial-branch=main")
	if err != nil {
		return fmt.Errorf("%w: init: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	return nil
}

// CommitFile stages and commits a single file under the supplied author.
// If git status shows no changes (already committed), returns nil
// (idempotent).
func (g *GitOps) CommitFile(ctx context.Context, relPath, authorName, authorEmail, message string) error {
	if g.memoryDir == "" {
		return ErrMemoryDirEmpty
	}
	if authorName == "" || authorEmail == "" {
		return errors.New("memory: author name + email required")
	}
	// stage
	out, err := g.run(ctx, authorName, authorEmail, "add", "--", relPath)
	if err != nil {
		return fmt.Errorf("%w: add: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	// commit only if there's something staged for this file.
	out, err = g.run(ctx, authorName, authorEmail, "diff", "--cached", "--quiet", "--", relPath)
	// `git diff --quiet` exits 1 when there are differences; we want to
	// commit only in that case. exit 0 = nothing to commit.
	if err == nil {
		return nil
	}
	out, err = g.run(ctx, authorName, authorEmail,
		"commit", "-m", message, "--", relPath)
	if err != nil {
		return fmt.Errorf("%w: commit: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	return nil
}

// AutoCommitDirty stages everything and commits if the working tree is
// dirty. Returns nil when nothing changed (clean tree, idempotent).
func (g *GitOps) AutoCommitDirty(ctx context.Context, authorName, authorEmail, message string) error {
	if g.memoryDir == "" {
		return ErrMemoryDirEmpty
	}
	if authorName == "" || authorEmail == "" {
		return errors.New("memory: author name + email required")
	}
	out, err := g.run(ctx, authorName, authorEmail, "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("%w: status: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	if strings.TrimSpace(out) == "" {
		return nil // clean
	}
	out, err = g.run(ctx, authorName, authorEmail, "add", "-A")
	if err != nil {
		return fmt.Errorf("%w: add -A: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	out, err = g.run(ctx, authorName, authorEmail, "commit", "-m", message)
	if err != nil {
		return fmt.Errorf("%w: commit: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	return nil
}

// LogOneline runs `git log --oneline` and returns the output (used by
// tests and `inspect`).
func (g *GitOps) LogOneline(ctx context.Context) (string, error) {
	if g.memoryDir == "" {
		return "", ErrMemoryDirEmpty
	}
	out, err := g.run(ctx, "system:read-only", "system:read-only@agent-center.local",
		"log", "--oneline", "--all")
	if err != nil {
		// Empty repo case (no commits yet) — git log exits 128 with
		// "does not have any commits yet". Treat as empty log.
		if strings.Contains(out, "does not have any commits yet") {
			return "", nil
		}
		return out, fmt.Errorf("%w: log: %v: %s", ErrMemoryGitOpFailed, err, out)
	}
	return out, nil
}

// run is the central invocation wrapper. Each call:
//   - normalises env (HOME / XDG_CONFIG_HOME override when homeOverride set)
//   - injects GIT_AUTHOR_* / GIT_COMMITTER_*
//   - sets GIT_CONFIG_GLOBAL=/dev/null (disable dev machine config)
//   - sets GIT_OPTIONAL_LOCKS=0 to reduce CI flake
//   - disables prompt + gpg signing on every invocation
func (g *GitOps) run(ctx context.Context, authorName, authorEmail string, args ...string) (string, error) {
	env := []string{
		"GIT_AUTHOR_NAME=" + authorName,
		"GIT_AUTHOR_EMAIL=" + authorEmail,
		"GIT_COMMITTER_NAME=" + authorName,
		"GIT_COMMITTER_EMAIL=" + authorEmail,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"PATH=" + safeDefaultPath(),
	}
	if g.homeOverride != "" {
		env = append(env,
			"HOME="+g.homeOverride,
			"XDG_CONFIG_HOME="+g.homeOverride,
		)
	} else {
		env = append(env, "HOME="+g.memoryDir)
	}
	// Always disable gpg signing for memory commits — supervisor commits
	// would never have a key configured.
	args = append([]string{"-c", "commit.gpgsign=false"}, args...)
	return g.runner.Run(ctx, g.memoryDir, env, args...)
}

// safeDefaultPath returns a deterministic minimal PATH that finds git on
// common dev / CI images. Tests can rely on this without needing to clone
// the caller's env (which we deliberately discard).
func safeDefaultPath() string {
	// Include the common locations on macOS / linux CI images.
	return "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
}

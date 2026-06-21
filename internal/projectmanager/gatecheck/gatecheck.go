// Package gatecheck is the concrete adapter behind the B3 decision-automation
// DecisionGate port (v2.13.0 I18/B3 — docs/design/v2.13.0/control-flow-engine-spec.md
// §2.3). It answers ONE question for the pm service: did the §-1 gate
// (build/lint/tsc/test) PASS on a feature branch?
//
// It keeps a per-repoURL working clone under cacheDir, ALWAYS fetches origin first
// (only-trust-origin, mirroring mergecheck), checks out the branch detached, and runs
// a configured gate command in the worktree. The verdict is read from the gate
// command's EXIT CODE: 0 ⇒ green, a non-zero process exit ⇒ red, anything else
// (clone/fetch/checkout/infra failure) ⇒ unknown + error (the caller defers to a
// human — it never auto-passes or auto-rejects on a gate it could not actually run).
//
// NOTE (cost): running the real gate is HEAVY (a full checkout + build/test). The pm
// service calls GateStatus synchronously from complete_task, so wiring this adapter
// is an explicit operator opt-in (see SetDecisionGate / the composition root) — when
// unwired, B3 defers every decision to a human. Commands are reached through a
// CommandRunner port so tests swap a fake and never touch git / the toolchain.
package gatecheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// CommandRunner is the port the gate checker uses to invoke external commands (git
// and the gate command). Extracted so tests swap a fake (recording args + returning
// canned output/exit errors); production shells out via execCommandRunner. The error
// for a non-zero exit MUST be an *exec.ExitError (so the caller can read the code).
type CommandRunner interface {
	Run(ctx context.Context, workdir string, env []string, name string, args ...string) (string, error)
}

// execCommandRunner shells out to the system binary. Production default.
type execCommandRunner struct{}

// Run invokes name+args via os/exec, returning combined stdout/stderr + exec error.
func (execCommandRunner) Run(ctx context.Context, workdir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workdir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// NewExecCommandRunner returns the real os/exec CommandRunner.
func NewExecCommandRunner() CommandRunner { return execCommandRunner{} }

// GitGateChecker implements the pm service's DecisionGate over a per-repoURL working
// clone cache. Safe for concurrent use (a mutex serializes the shared per-repo
// checkout so concurrent decisions never race on the working tree).
type GitGateChecker struct {
	cacheDir string
	gateCmd  []string // e.g. ["make","gate"]; first elem is the program, rest its args
	runner   CommandRunner
	mu       sync.Mutex
}

// New constructs a GitGateChecker that keeps working clones under cacheDir and runs
// gateCmd (program + args) as the §-1 gate. A nil runner defaults to the real exec
// runner. An empty gateCmd defaults to ["make","gate"].
func New(cacheDir string, gateCmd []string, runner CommandRunner) *GitGateChecker {
	if runner == nil {
		runner = NewExecCommandRunner()
	}
	if len(gateCmd) == 0 {
		gateCmd = []string{"make", "gate"}
	}
	return &GitGateChecker{cacheDir: cacheDir, gateCmd: gateCmd, runner: runner}
}

// GateStatus checks out <branch> from origin in repoURL's working clone and runs the
// gate command. The base arg is accepted for signature parity with the port (the
// gate runs on the branch itself) and is currently unused. Returns:
//
//	(GateGreen, nil)         gate command exited 0
//	(GateRed, nil)           gate command exited non-zero (a real failure)
//	(GateUnknown, err)       could not clone/fetch/checkout, or a non-exit run error
func (c *GitGateChecker) GateStatus(ctx context.Context, repoURL, branch, _ string) (pm.GateVerdict, error) {
	if strings.TrimSpace(repoURL) == "" {
		return pm.GateUnknown, errors.New("gatecheck: repoURL required")
	}
	if strings.TrimSpace(branch) == "" {
		return pm.GateUnknown, errors.New("gatecheck: branch required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	dir, err := c.syncClone(ctx, repoURL)
	if err != nil {
		return pm.GateUnknown, err
	}
	// Check out the branch as origin sees it (detached, no local-branch drift).
	if out, cerr := c.git(ctx, dir, "checkout", "--detach", "origin/"+branch); cerr != nil {
		return pm.GateUnknown, fmt.Errorf("gatecheck: checkout origin/%s in %s: %v: %s", branch, dir, cerr, out)
	}
	// Run the gate. Exit 0 = green; a process exit (non-zero) = red; anything else
	// (could not even start the command) = unknown.
	out, gerr := c.runner.Run(ctx, dir, gateEnv(), c.gateCmd[0], c.gateCmd[1:]...)
	if gerr == nil {
		return pm.GateGreen, nil
	}
	if _, ok := exitCode(gerr); ok {
		return pm.GateRed, nil // the gate ran and failed → red (definitive, no error)
	}
	return pm.GateUnknown, fmt.Errorf("gatecheck: gate command %v in %s could not run: %v: %s", c.gateCmd, dir, gerr, out)
}

// syncClone ensures a working clone of repoURL exists under cacheDir and is fresh:
// clone on first use, else fetch --prune origin. Always fetches so the checkout only
// reflects origin. Returns the clone dir.
func (c *GitGateChecker) syncClone(ctx context.Context, repoURL string) (string, error) {
	dir := filepath.Join(c.cacheDir, cloneSubdir(repoURL))
	if _, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("gatecheck: stat clone %s: %w", dir, err)
		}
		if mkErr := os.MkdirAll(c.cacheDir, 0o755); mkErr != nil {
			return "", fmt.Errorf("gatecheck: mkdir cache %s: %w", c.cacheDir, mkErr)
		}
		if out, cerr := c.git(ctx, c.cacheDir, "clone", repoURL, dir); cerr != nil {
			return "", fmt.Errorf("gatecheck: clone %s: %v: %s", repoURL, cerr, out)
		}
		return dir, nil
	}
	if out, ferr := c.git(ctx, dir, "fetch", "--prune", "origin"); ferr != nil {
		return "", fmt.Errorf("gatecheck: fetch --prune origin in %s: %v: %s", dir, ferr, out)
	}
	return dir, nil
}

// git runs a git subcommand under workdir with a neutralized environment (host
// gitconfig / prompts / signing can never change the answer; mirrors mergecheck).
func (c *GitGateChecker) git(ctx context.Context, workdir string, args ...string) (string, error) {
	env := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME=" + c.cacheDir,
		"PATH=" + safeDefaultPath(),
	}
	return c.runner.Run(ctx, workdir, env, "git", args...)
}

// gateEnv is the environment the gate command runs under: a real, host-like PATH so
// the toolchain (make/go/pnpm/…) resolves, but no inherited git prompt/lock noise.
func gateEnv() []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"PATH=" + safeDefaultPath(),
		"HOME=" + os.Getenv("HOME"),
	}
}

// cloneSubdir derives a stable, filesystem-safe subdir name from repoURL.
func cloneSubdir(repoURL string) string {
	sum := sha256.Sum256([]byte(repoURL))
	return hex.EncodeToString(sum[:])
}

// exitCode extracts a process exit code from an exec error. ok=false when err is not
// an *exec.ExitError (e.g. command not found / context canceled).
func exitCode(err error) (int, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

// safeDefaultPath returns a deterministic PATH that finds git + common toolchains on
// dev / CI images (matches mergecheck's helper, plus the Homebrew bin already there).
func safeDefaultPath() string {
	return "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
}

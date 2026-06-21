// Package mergecheck is the concrete git adapter behind the F3 Integrate-complete
// merge guardrail (v2.13.0 I18 — docs/design/v2.13.0/cycle-node-graph-spec.md §5).
// It answers ONE question for the pm service's MergeChecker port: has a feature
// branch actually merged back into the integration trunk ON ORIGIN?
//
// "Only trust origin" is the core discipline: the adapter NEVER trusts a local
// checkout's stale refs. It keeps a per-repoURL BARE MIRROR cache and ALWAYS
// fetches before answering, then resolves branch+base to SHAs from the mirror's
// refs and asks `git merge-base --is-ancestor`. The git binary is reached through
// a GitRunner port (mirroring internal/cognition/memory/gitops.go) so tests swap a
// fake and never hit the network.
package mergecheck

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
)

// GitRunner is the port the merge checker uses to invoke git. Extracted so tests
// can swap a fake (recording args + returning canned output/exit errors); real git
// is reached via execGitRunner. Mirrors cognition/memory's GitRunner exactly.
type GitRunner interface {
	// Run executes "git" with args under workdir + env. Returns combined
	// stdout/stderr and the underlying exec error (if any) — including an
	// *exec.ExitError whose code the caller inspects (is-ancestor exit 0/1).
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

// GitMergeChecker implements the pm service's MergeChecker over a per-repoURL bare
// mirror cache. It is safe to share (stateless beyond the cacheDir + runner).
type GitMergeChecker struct {
	cacheDir string
	runner   GitRunner
}

// New constructs a GitMergeChecker that keeps its bare mirrors under cacheDir. A
// nil runner defaults to the real git binary.
func New(cacheDir string, runner GitRunner) *GitMergeChecker {
	if runner == nil {
		runner = NewExecGitRunner()
	}
	return &GitMergeChecker{cacheDir: cacheDir, runner: runner}
}

// BranchMergedToOrigin reports whether <branch>'s HEAD is an ancestor of
// origin/<base> in the repo at repoURL. It (1) clones-or-fetches the repo's bare
// mirror (always fetching, so the answer reflects origin, not a stale local), (2)
// resolves refs/heads/<branch> and refs/heads/<base> to SHAs, then (3) runs
// `git merge-base --is-ancestor` — exit 0 ⇒ merged (true), exit 1 ⇒ not merged
// (false, nil), any other exit ⇒ error. A missing branch/base ref or a
// clone/fetch failure is returned as an error (the pm guard fails closed on it).
func (c *GitMergeChecker) BranchMergedToOrigin(ctx context.Context, repoURL, branch, base string) (bool, error) {
	if strings.TrimSpace(repoURL) == "" {
		return false, errors.New("mergecheck: repoURL required")
	}
	if strings.TrimSpace(branch) == "" || strings.TrimSpace(base) == "" {
		return false, errors.New("mergecheck: branch and base required")
	}
	dir, err := c.syncMirror(ctx, repoURL)
	if err != nil {
		return false, err
	}
	// Confirm both refs exist on origin (resolve to SHAs). A missing ref means the
	// branch/base doesn't exist on origin — surfaced as an error so the guard can
	// say "couldn't verify" rather than silently pass.
	branchRef := "refs/heads/" + branch
	baseRef := "refs/heads/" + base
	if _, err := c.run(ctx, dir, "rev-parse", "--verify", "--quiet", branchRef); err != nil {
		return false, fmt.Errorf("mergecheck: branch %q not found on origin %s: %w", branch, repoURL, err)
	}
	if _, err := c.run(ctx, dir, "rev-parse", "--verify", "--quiet", baseRef); err != nil {
		return false, fmt.Errorf("mergecheck: base %q not found on origin %s: %w", base, repoURL, err)
	}
	// Ancestry: is the branch HEAD contained in base? exit 0 = yes, exit 1 = no.
	out, err := c.run(ctx, dir, "merge-base", "--is-ancestor", branchRef, baseRef)
	if err == nil {
		return true, nil // exit 0 ⇒ branch is an ancestor of base ⇒ merged
	}
	if code, ok := exitCode(err); ok && code == 1 {
		return false, nil // exit 1 ⇒ NOT an ancestor ⇒ not merged (definitive, no error)
	}
	return false, fmt.Errorf("mergecheck: is-ancestor %s..%s failed in %s: %v: %s", branch, base, repoURL, err, out)
}

// syncMirror ensures a bare mirror of repoURL exists under cacheDir and is fresh:
// clone --mirror on first use, else fetch --prune origin (a --mirror's origin IS
// repoURL). It ALWAYS fetches an existing mirror so the answer only trusts origin.
// Returns the mirror dir.
func (c *GitMergeChecker) syncMirror(ctx context.Context, repoURL string) (string, error) {
	dir := filepath.Join(c.cacheDir, mirrorSubdir(repoURL))
	if _, err := os.Stat(dir); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("mergecheck: stat mirror %s: %w", dir, err)
		}
		if mkErr := os.MkdirAll(c.cacheDir, 0o755); mkErr != nil {
			return "", fmt.Errorf("mergecheck: mkdir cache %s: %w", c.cacheDir, mkErr)
		}
		// `git clone --mirror <repoURL> <dir>` runs from the cache root (no repo dir yet).
		if out, cerr := c.run(ctx, c.cacheDir, "clone", "--mirror", repoURL, dir); cerr != nil {
			return "", fmt.Errorf("mergecheck: clone --mirror %s: %v: %s", repoURL, cerr, out)
		}
		return dir, nil
	}
	// Existing mirror: always refresh from origin (only-trust-origin).
	if out, ferr := c.run(ctx, dir, "fetch", "--prune", "origin"); ferr != nil {
		return "", fmt.Errorf("mergecheck: fetch --prune origin in %s: %v: %s", dir, ferr, out)
	}
	return dir, nil
}

// run invokes git under workdir with a NEUTRALIZED environment so host gitconfig /
// prompts / signing can never change the answer (mirrors cognition/memory's run).
func (c *GitMergeChecker) run(ctx context.Context, workdir string, args ...string) (string, error) {
	env := []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME=" + c.cacheDir,
		"PATH=" + safeDefaultPath(),
	}
	return c.runner.Run(ctx, workdir, env, args...)
}

// mirrorSubdir derives a stable, filesystem-safe subdir name from repoURL via a
// sha256 hex digest, so distinct URLs never collide and the same URL always maps
// to the same mirror (cache hit across runs).
func mirrorSubdir(repoURL string) string {
	sum := sha256.Sum256([]byte(repoURL))
	return hex.EncodeToString(sum[:]) + ".git"
}

// exitCode extracts a process exit code from an exec error. ok=false when err is
// not an *exec.ExitError (e.g. git not found / context canceled).
func exitCode(err error) (int, bool) {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), true
	}
	return 0, false
}

// safeDefaultPath returns a deterministic minimal PATH that finds git on common
// dev / CI images (matches cognition/memory's helper).
func safeDefaultPath() string {
	return "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
}

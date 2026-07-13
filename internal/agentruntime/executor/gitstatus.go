package executor

// gitstatus.go — structured git-state capture for the delayed-teardown audit trail
// (issue-0186f85e ②, root-causing T947). When Finalize retains a TERMINAL executor's
// worktree it stamps the `finalized` marker with this machine-readable git status, so a
// later delivery-detection / audit pass can decide — WITHOUT the worktree still being
// present — whether the executor actually produced work and whether that work was pushed.
//
// The gap it closes: teardown used to remove the worktree BEFORE any audit, so an
// executor that DID commit real work but never got it pushed was indistinguishable from
// one that produced nothing ("真做了丢了" vs "虚报完成"). Recording branch / HEAD sha /
// dirty / pushed at finalize makes that judgement mechanical.
//
// Every probe is BEST-EFFORT: a non-git workspace (a plain-dir executor) or any git
// failure yields Probed=false and leaves the fields zero — Finalize must never fail
// because git status could not be read.

import (
	"context"
	"strings"
)

// FinalizedGitStatus is the structured git state of a terminal executor's worktree,
// captured at finalize and persisted in the `finalized` marker (the issue's
// "finalized.json"). It is the audit/delivery-detection contract: read it after the
// executor is gone to judge whether it truly delivered.
type FinalizedGitStatus struct {
	// Branch is the worktree's checked-out branch (abbrev ref), "" when detached/unknown.
	Branch string `json:"branch,omitempty"`
	// HeadSHA is the worktree HEAD commit, "" when unresolvable.
	HeadSHA string `json:"head_sha,omitempty"`
	// Dirty is true when the worktree has uncommitted changes (staged or unstaged) — the
	// executor left work it never committed.
	Dirty bool `json:"dirty"`
	// Pushed is true when HEAD is already present on a remote-tracking branch — the work
	// is durably delivered off this machine. false ⇒ HEAD lives only in this (about-to-be
	// -reaped) worktree, so reaping it without an eager-push would lose the commits.
	Pushed bool `json:"pushed"`
	// Probed is true iff the git status was successfully read. false ⇒ the workspace was
	// not a git repo (plain-dir executor) or git errored, so the other fields are unknown
	// (zero) rather than meaningfully false.
	Probed bool `json:"probed"`
}

// probeGitStatus reads the structured git state of the worktree at dir via runner
// (best-effort). A nil runner, empty dir, or a `rev-parse HEAD` failure (not a git repo /
// no commit) returns the zero status with Probed=false — the caller records "git state
// unknown" rather than a misleading all-false. Runs git under a neutralized env so host
// gitconfig / prompts / signing can never interfere (mirrors WorktreeProvisioner.run).
func probeGitStatus(ctx context.Context, runner GitRunner, dir string) FinalizedGitStatus {
	var gs FinalizedGitStatus
	if runner == nil || strings.TrimSpace(dir) == "" {
		return gs
	}
	env := gitProbeEnv(dir)
	// HEAD sha doubles as the "is this a git worktree" probe: if it fails, dir is not a
	// repo (or has no commit) — leave Probed=false so unknown ≠ clean.
	head, err := runner.Run(ctx, dir, env, "rev-parse", "HEAD")
	if err != nil {
		return gs
	}
	gs.HeadSHA = strings.TrimSpace(head)
	gs.Probed = true
	if br, berr := runner.Run(ctx, dir, env, "rev-parse", "--abbrev-ref", "HEAD"); berr == nil {
		if b := strings.TrimSpace(br); b != "HEAD" { // "HEAD" ⇒ detached, no branch name
			gs.Branch = b
		}
	}
	if st, serr := runner.Run(ctx, dir, env, "status", "--porcelain"); serr == nil {
		gs.Dirty = strings.TrimSpace(st) != ""
	}
	// HEAD present on ANY remote-tracking branch ⇒ pushed. Empty/err ⇒ not pushed (best
	// -effort: an un-fetched remote reads as not-pushed, the safe side for delivery audit).
	if rc, rerr := runner.Run(ctx, dir, env, "branch", "-r", "--contains", "HEAD"); rerr == nil {
		gs.Pushed = strings.TrimSpace(rc) != ""
	}
	return gs
}

// gitProbeEnv is the neutralized environment the git-status probe runs under — the same
// isolation WorktreeProvisioner.run uses, but HOME rooted at the worktree dir being read.
func gitProbeEnv(dir string) []string {
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME=" + dir,
		"PATH=" + safeDefaultPath(),
	}
}

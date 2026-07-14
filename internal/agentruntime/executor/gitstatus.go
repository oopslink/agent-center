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
	"strconv"
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
	// BaseRef is the ref the executor's worktree branched off (the spawn-time base, from the
	// recovery Record). "" when the caller had no base to compare against (plain-dir / pool
	// default / unknown) — then BaseKnown is false and AheadOfBase is meaningless.
	BaseRef string `json:"base_ref,omitempty"`
	// BaseKnown is true iff a non-empty BaseRef was supplied AND it resolved in the worktree
	// so AheadOfBase is a real count. false ⇒ "couldn't tell how far HEAD moved" — the
	// delivery gate must treat that as UNKNOWN (fail-safe trust), never as zero-delivery.
	BaseKnown bool `json:"base_known,omitempty"`
	// AheadOfBase is the number of commits HEAD is ahead of BaseRef (0 = HEAD never advanced
	// past base). Only meaningful when BaseKnown. It is the mechanical "did the executor
	// commit anything" signal the non-delivery gate reads.
	AheadOfBase int `json:"ahead_of_base,omitempty"`
}

// HasDelivery reports whether the worktree shows ANY real git side effect: a commit
// beyond base, uncommitted changes, or an already-pushed HEAD. Delivery detection
// (issue-37015227 ②): a self-reported success that has NO delivery is not trustworthy.
func (g FinalizedGitStatus) HasDelivery() bool {
	return g.Dirty || g.Pushed || g.AheadOfBase > 0
}

// ZeroDelivery reports an UNAMBIGUOUS non-delivery: git WAS probed (a real worktree),
// the base IS known (so AheadOfBase is real), and there is provably no side effect —
// HEAD did not advance past base, the tree is clean, and nothing was pushed. It is the
// strict gate condition: it distinguishes "proven nothing produced" from "couldn't tell"
// (base unknown / plain-dir), so a task the tool cannot mechanically judge is never
// falsely blocked (fail-safe: only a positive zero-delivery downgrades a success).
func (g FinalizedGitStatus) ZeroDelivery() bool {
	return g.Probed && g.BaseKnown && !g.HasDelivery()
}

// probeGitStatus reads the structured git state of the worktree at dir via runner
// (best-effort). A nil runner, empty dir, or a `rev-parse HEAD` failure (not a git repo /
// no commit) returns the zero status with Probed=false — the caller records "git state
// unknown" rather than a misleading all-false. Runs git under a neutralized env so host
// gitconfig / prompts / signing can never interfere (mirrors WorktreeProvisioner.run).
//
// baseRef is the ref the worktree branched off (spawn-time base); when non-empty and it
// resolves, AheadOfBase records how many commits HEAD is past it (BaseKnown=true). An
// empty or unresolvable base leaves BaseKnown=false — the caller treats that as UNKNOWN,
// never as zero-delivery (issue-37015227 ②: only a POSITIVE zero-delivery gates a success).
func probeGitStatus(ctx context.Context, runner GitRunner, dir, baseRef string) FinalizedGitStatus {
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
	sha := strings.TrimSpace(head)
	if sha == "" {
		// No resolvable HEAD sha (empty output, no error) — not a judgeable git worktree.
		// A real repo returns a sha or errors; treat empty as "can't judge" (Probed=false)
		// so the delivery gate trusts rather than downgrading a non-git run to non-delivery.
		return gs
	}
	gs.HeadSHA = sha
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
	// Commits HEAD is ahead of the spawn-time base — the mechanical "did it commit anything"
	// signal. `rev-list --count <base>..HEAD` counts commits reachable from HEAD but not base;
	// 0 ⇒ HEAD never advanced. An unresolvable base (deleted / never fetched) errors → leave
	// BaseKnown=false so an un-judgeable run is not mistaken for zero-delivery.
	if b := strings.TrimSpace(baseRef); b != "" {
		gs.BaseRef = b
		if rl, rerr := runner.Run(ctx, dir, env, "rev-list", "--count", b+"..HEAD"); rerr == nil {
			if n, perr := strconv.Atoi(strings.TrimSpace(rl)); perr == nil && n >= 0 {
				gs.BaseKnown = true
				gs.AheadOfBase = n
			}
		}
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

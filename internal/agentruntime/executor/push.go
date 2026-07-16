package executor

// push.go — eager supervisor-push (issue-f30b7e7b PRIMARY fix). A review-only Dev node's
// forked executor COMMITS its work onto its provisioned worktree branch but never PUSHES
// it; when Finalize retains-then-reaps the worktree the commit dies with it ("committed ≠
// delivered", zero reviewable delivery). The executor is isolated (F1: no center, no
// credentials) and cannot push itself — so the agent-runtime (the "supervisor" half of the
// same organism, which HOLDS the git credentials the materializer clones/fetches with)
// pushes the executor's branch to origin at finalize, BEFORE the delivery gate and the
// worktree teardown. This makes a committed feat branch durably deliverable and lets the
// gate see Pushed=true (genuine success) instead of downgrading it to a retry loop.

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// gitNetworkEnv is the AUTHENTICATED environment an eager-push runs under. It inherits the
// process environment (host SSH agent / deploy key / gitconfig url-rewrites — the same v1
// auth model reporepo clone/fetch already use successfully) so the push can reach origin,
// and only disables interactive prompts so a missing credential fails CLOSED (error) rather
// than hanging. It is DELIBERATELY NOT gitProbeEnv: the probe neutralizes HOME + gitconfig
// for a hermetic READ, which would strip the very SSH/credential config a push (a WRITE)
// needs. Read = hermetic, write = authenticated.
func gitNetworkEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
}

// expectedExecutorBranch reconstructs the executor's provisioned worktree branch —
// "ac-exec/<task_ref>/<executor_id>" (materializer WorktreeRequest.BranchName in
// executor_runtime.go) — the ONLY branch an eager-push is ever allowed to push. It is
// deterministic from taskRef + executorID, so the guardrail needs no extra persisted state.
// "" when the task ref is unknown (then there is no legitimate push target and push is
// refused).
func (m *Monitor) expectedExecutorBranch(executorID string) string {
	taskRef := strings.TrimSpace(m.taskRef(executorID))
	if taskRef == "" {
		return ""
	}
	return "ac-exec/" + taskRef + "/" + executorID
}

// eagerSupervisorPush pushes a TERMINAL executor's committed worktree branch to origin.
//
// It is GUARDED hard on the branch name (the most dangerous corner): it pushes ONLY the
// executor's own provisioned ac-exec/<task>/<exec> branch. HEAD on main/master, detached,
// or any unexpected branch is REFUSED with an error and NOT pushed — pushing local commits
// to origin/main is exactly the "dead-code-onto-main" class of accident this must never
// cause. The caller routes a refusal to the non-delivery path (never force-push, never
// clobber).
//
// Returns pushed=true only when origin actually accepted the ref. On any error (guardrail
// refusal, auth/write-permission failure, non-fast-forward, network) it returns
// (false, err); the caller must NOT set Pushed and must route the run to non-delivery with
// the error surfaced + the worktree retained for retry/inspection.
func (m *Monitor) eagerSupervisorPush(ctx context.Context, c Completion) (bool, error) {
	gs := c.Git
	if gs == nil || !gs.Probed {
		return false, nil // non-git / unjudgeable workspace — nothing to push
	}
	if gs.Pushed {
		return true, nil // already durable off-machine
	}
	// Only treat "no commit past base + clean tree" as nothing-to-deliver when the base is
	// KNOWN. When BaseKnown is false the ahead count is meaningless ("couldn't tell" —
	// gitstatus.go's invariant), so we must NOT skip on it: fall through to the guardrail +
	// push (a HEAD on the expected ac-exec branch IS pushed; the guardrail still blocks
	// main/detached). This is the exact invariant ZeroDelivery() already honours — the push
	// gate must too (issue-f30b7e7b P0: base-unknown skipped the push and lost the commit).
	if gs.BaseKnown && gs.AheadOfBase <= 0 && !gs.Dirty {
		return false, nil // base known AND no commit past it AND clean → genuinely nothing to deliver
	}
	// Branch guardrail: only the provisioned executor branch may be pushed.
	want := m.expectedExecutorBranch(c.ExecutorID)
	branch := strings.TrimSpace(gs.Branch)
	if want == "" || branch == "" || branch != want {
		return false, fmt.Errorf("eager-push refused: HEAD on %q, expected executor branch %q (main/detached/unexpected — not pushing to origin)", gs.Branch, want)
	}
	if m.git == nil {
		return false, fmt.Errorf("eager-push: no git runner wired")
	}
	ws, err := m.fx.Layout().WorkspaceDir(c.ExecutorID)
	if err != nil {
		return false, fmt.Errorf("eager-push: resolve workspace dir: %w", err)
	}
	// Push under the authenticated network env. --force is NEVER used: the ac-exec branch is
	// unique per executor, so a non-fast-forward means an unexpected race → surface as error,
	// do not clobber the remote.
	if out, perr := m.git.Run(ctx, ws, gitNetworkEnv(), "push", "origin", branch); perr != nil {
		return false, fmt.Errorf("eager-push %s → origin failed: %w: %s", branch, perr, strings.TrimSpace(out))
	}
	return true, nil
}

// eagerPushBeforeGate is the Finalize step (issue-f30b7e7b) that runs BETWEEN the git-status
// probe and the non-delivery gate. For a would-be success carrying committed-but-unpushed
// work on the executor's own branch it pushes to origin; on success it flips Pushed=true so
// the downstream gate treats the run as a genuine, durable delivery (positive path: NOT
// retryable, NOT reopened). On a push refusal/failure it leaves Pushed=false and records the
// error on c.Git.PushError, so the gate downgrades the run to a retryable non_delivery that
// carries WHY it was not delivered (surfaced to the supervisor judgment + escalation), with
// the worktree retained for retry/manual push.
func (m *Monitor) eagerPushBeforeGate(ctx context.Context, c Completion) Completion {
	if c.Kind != OutcomeSucceeded {
		return c // only a would-be success is pushed; failures go to judgment as-is
	}
	// Each of the three not-applicable branches logs DISTINCTLY: a zero EAGER-PUSH log count
	// must never be ambiguous between "unreachable" (no worktree), "reachable but had no work
	// to do" (executor pushed itself), and "no such run happened at all". The c.Git == nil
	// branch below is the one that kept this fix inert for a whole cycle while looking exactly
	// like "never ran" — it is silent no longer.
	if c.Git == nil {
		m.log("EAGER-PUSH n/a executor=%s task=%s: no git worktree on this run (flag off / unmanaged workspace) — eager-push UNREACHABLE here, nothing to push",
			c.ExecutorID, m.taskRef(c.ExecutorID))
		return c
	}
	if !c.Git.Probed {
		m.log("EAGER-PUSH n/a executor=%s task=%s: git-status probe did not run — workspace unjudgeable, nothing to push",
			c.ExecutorID, m.taskRef(c.ExecutorID))
		return c
	}
	if c.Git.Pushed {
		m.log("EAGER-PUSH n/a executor=%s task=%s branch=%s head=%s: executor already pushed its own branch — legitimate success path, supervisor push not needed",
			c.ExecutorID, m.taskRef(c.ExecutorID), c.Git.Branch, c.Git.HeadSHA)
		return c
	}
	// Skip the push ONLY when we can PROVE there is nothing to deliver: base KNOWN, HEAD not
	// ahead, clean tree. A base-unknown run is "couldn't tell" and MUST fall through to the
	// (guardrail-gated) push rather than be silently dropped — the P0 that lost review-only
	// commits on the real materializer spawn path (issue-f30b7e7b). Every path out of this
	// function — skip, n/a, failure, success — is logged fail-loud: a SILENT skip (zero log) is
	// exactly what hid this bug for a whole cycle.
	if c.Git.BaseKnown && c.Git.AheadOfBase <= 0 && !c.Git.Dirty {
		m.log("EAGER-PUSH skip executor=%s task=%s branch=%s: base known, HEAD not ahead (%d) + clean tree — nothing to deliver",
			c.ExecutorID, m.taskRef(c.ExecutorID), c.Git.Branch, c.Git.AheadOfBase)
		return c
	}
	if !c.Git.BaseKnown {
		m.log("EAGER-PUSH executor=%s task=%s branch=%s: base UNKNOWN (cannot measure ahead) — NOT treating as zero-delivery, attempting guarded push",
			c.ExecutorID, m.taskRef(c.ExecutorID), c.Git.Branch)
	}
	pushed, err := m.eagerSupervisorPush(ctx, c)
	if err != nil {
		m.log("EAGER-PUSH FAILED executor=%s task=%s branch=%s: %v — routing to non_delivery "+
			"(worktree retained for retry/inspection; NOT pushed, NOT force-pushed)",
			c.ExecutorID, m.taskRef(c.ExecutorID), c.Git.Branch, err)
		c.Git.PushError = err.Error()
		return c
	}
	if pushed {
		c.Git.Pushed = true // durable off-machine now → the gate passes → genuine success
		m.log("EAGER-PUSH ok executor=%s task=%s branch=%s head=%s — durably delivered to origin",
			c.ExecutorID, m.taskRef(c.ExecutorID), c.Git.Branch, c.Git.HeadSHA)
	}
	return c
}

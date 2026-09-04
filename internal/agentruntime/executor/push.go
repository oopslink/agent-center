package executor

// push.go — eager supervisor-push (issue-f30b7e7b PRIMARY fix). A review-only Dev node's
// forked executor may COMMIT its work without reliably PUSHING it; when Finalize later
// reaps the worktree the commit dies with it ("committed ≠ delivered", zero reviewable
// delivery). The agent-runtime (the "supervisor" half of the same organism, which owns the
// materializer's authenticated git path) therefore verifies an executor self-push or pushes
// the executor's guarded branch at finalize, BEFORE the delivery gate and worktree teardown.
// This makes a committed feat branch durably deliverable and lets the gate see Pushed=true
// (genuine success) instead of downgrading it to a retry loop.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const defaultOriginVerificationTimeout = 30 * time.Second

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

// expectedExecutorBranch reconstructs the system-owned executor delivery ref —
// "ac-exec/<task_ref>/<executor_id>". Eager-push always pushes HEAD explicitly to this
// safe remote ref, independent of the checked-out local branch name. "" when the task ref
// is unknown (then there is no legitimate push target and push is refused).
func (m *Monitor) expectedExecutorBranch(executorID string) string {
	taskRef := strings.TrimSpace(m.taskRef(executorID))
	if taskRef == "" {
		return ""
	}
	return "ac-exec/" + taskRef + "/" + executorID
}

// normalizedBranchName converts the branch-shaped refs carried by input/records to the
// short name returned by `rev-parse --abbrev-ref HEAD`. Commit SHAs and tags deliberately
// remain unchanged, so they can never accidentally alias a branch.
func normalizedBranchName(ref string) string {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{"refs/remotes/origin/", "remotes/origin/", "refs/heads/", "origin/"} {
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

// deliveryBranchAllowed is the read/write policy shared by self-push discovery and
// supervisor push. A durable ref on origin is evidence, not permission: delivery from the
// task's base/protected branch must never be accepted merely because an executor managed to
// push it with inherited host credentials.
func (m *Monitor) deliveryBranchAllowed(executorID, branch string) error {
	branch = normalizedBranchName(branch)
	if branch == "" {
		return errors.New("delivery branch is detached or unknown")
	}
	if branch == "main" || branch == "master" {
		return fmt.Errorf("delivery branch %q is protected", branch)
	}
	protected := []string{m.recordBaseRef(executorID)}
	if in, err := m.fx.ReadInput(executorID); err == nil && in.Repo != nil {
		protected = append(protected, in.Repo.BaseRef, in.Repo.DefaultBranch)
	}
	for _, ref := range protected {
		if base := normalizedBranchName(ref); base != "" && branch == base {
			return fmt.Errorf("delivery branch %q is the task base branch", branch)
		}
	}
	return nil
}

// verifyExistingOriginDelivery independently binds the actual checked-out branch to this
// exact HEAD on origin. It applies the same protected-branch policy as runtime push, so an
// executor self-push can supply evidence but can never widen the delivery boundary.
func (m *Monitor) verifyExistingOriginDelivery(ctx context.Context, c Completion) (bool, error) {
	gs := c.Git
	if gs == nil || !gs.Probed {
		return false, nil
	}
	if err := m.deliveryBranchAllowed(c.ExecutorID, gs.Branch); err != nil {
		return false, fmt.Errorf("origin delivery refused: %w", err)
	}
	if m.git == nil {
		return false, fmt.Errorf("origin verification: no git runner wired")
	}
	ws, err := m.executorWorkspacePath(c.ExecutorID)
	if err != nil {
		return false, fmt.Errorf("origin verification: resolve actual workspace: %w", err)
	}
	return m.originHeadMatches(ctx, ws, gs.Branch, gs.HeadSHA)
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
// Returns pushed=true only when origin is independently confirmed to hold the actual branch
// at this exact HEAD (either it was already there or origin accepted this call's push). On
// any error (guardrail refusal, auth/write-permission failure, non-fast-forward, network) it returns
// (false, err); the caller must NOT set Pushed and must route the run to non-delivery with
// the error surfaced + the worktree retained for retry/inspection.
func (m *Monitor) eagerSupervisorPush(ctx context.Context, c Completion) (bool, error) {
	gs := c.Git
	if gs == nil || !gs.Probed {
		return false, nil // non-git / unjudgeable workspace — nothing to push
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
	// A shared `git clone --mirror` cache maps origin heads into refs/heads/*, so the
	// local `branch -r --contains HEAD` fast probe is empty even after the executor has
	// successfully pushed. Independently verify the ACTUAL branch against origin before
	// applying the runtime-push branch guard. This only observes an already-durable ref;
	// it never grants permission to push an unexpected branch.
	if policyErr := m.deliveryBranchAllowed(c.ExecutorID, gs.Branch); policyErr != nil {
		return false, fmt.Errorf("eager-push refused: %w", policyErr)
	}
	want := m.expectedExecutorBranch(c.ExecutorID)
	var verifyErr error
	if want != "" {
		var verified bool
		verified, verifyErr = m.originHeadMatchesForCompletion(ctx, c, want)
		if verified {
			gs.Branch = want
			return true, nil
		}
	} else {
		verified, err := m.verifyExistingOriginDelivery(ctx, c)
		if verified {
			return true, nil
		}
		verifyErr = err
	}
	// Ref guardrail: only the system-generated executor ref may receive HEAD.
	if want == "" {
		if verifyErr != nil {
			return false, fmt.Errorf("eager-push refused: no expected executor ref; origin verification failed: %v", verifyErr)
		}
		return false, fmt.Errorf("eager-push refused: no expected executor ref for HEAD %s", gs.HeadSHA)
	}
	if m.git == nil {
		return false, fmt.Errorf("eager-push: no git runner wired")
	}
	ws, err := m.executorWorkspacePath(c.ExecutorID)
	if err != nil {
		return false, fmt.Errorf("eager-push: resolve actual workspace: %w", err)
	}
	// Push under the authenticated network env. --force is NEVER used: the ac-exec branch is
	// unique per executor, so a non-fast-forward means an unexpected race → surface as error,
	// do not clobber the remote. RepoCacheManager's canonical source is a --mirror clone, whose
	// remote.origin.mirror=true would otherwise reject every explicit refspec with
	// "--mirror can't be combined with refspecs". Override that inherited transport mode for
	// this one command and name both sides exactly; this remains a normal non-force push of one
	// guarded branch, never a mirror push.
	pushCtx, cancel := context.WithTimeout(ctx, defaultOriginVerificationTimeout)
	defer cancel()
	ref := "refs/heads/" + want
	if out, perr := m.git.Run(pushCtx, ws, gitNetworkEnv(),
		"-c", "remote.origin.mirror=false", "push", "origin", "HEAD:"+ref); perr != nil {
		return false, fmt.Errorf("eager-push %s → origin failed: %w: %s", want, perr, strings.TrimSpace(out))
	}
	verified, err := m.originHeadMatches(ctx, ws, want, gs.HeadSHA)
	if err != nil {
		return false, fmt.Errorf("eager-push %s reached origin but exact ref/SHA verification failed: %w", want, err)
	}
	if !verified {
		return false, fmt.Errorf("eager-push %s returned success but origin ref does not point at HEAD %s", want, gs.HeadSHA)
	}
	gs.Branch = want
	return true, nil
}

func (m *Monitor) originHeadMatchesForCompletion(ctx context.Context, c Completion, branch string) (bool, error) {
	if m.git == nil {
		return false, fmt.Errorf("origin verification: no git runner wired")
	}
	ws, err := m.executorWorkspacePath(c.ExecutorID)
	if err != nil {
		return false, fmt.Errorf("origin verification: resolve actual workspace: %w", err)
	}
	return m.originHeadMatches(ctx, ws, branch, c.Git.HeadSHA)
}

// originHeadMatches proves that refs/heads/<branch> exists on origin at exactly headSHA.
// It is the durable-delivery check for mirror-backed worktrees, where remote-tracking refs
// do not exist locally. No match (including a same-name ref at another SHA) is false, nil;
// transport/auth failures are returned so the non-delivery reason remains observable.
func (m *Monitor) originHeadMatches(ctx context.Context, workspace, branch, headSHA string) (bool, error) {
	branch = strings.TrimSpace(branch)
	headSHA = strings.TrimSpace(headSHA)
	if m.git == nil || strings.TrimSpace(workspace) == "" || branch == "" || headSHA == "" {
		return false, nil
	}
	ref := "refs/heads/" + branch
	verifyCtx, cancel := context.WithTimeout(ctx, defaultOriginVerificationTimeout)
	defer cancel()
	out, err := m.git.Run(verifyCtx, workspace, gitNetworkEnv(), "ls-remote", "--heads", "origin", ref)
	if err != nil {
		return false, fmt.Errorf("git ls-remote %s: %w: %s", ref, err, strings.TrimSpace(out))
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == headSHA && fields[1] == ref {
			return true, nil
		}
	}
	return false, nil
}

// eagerPushBeforeGate is the Finalize step (issue-f30b7e7b) that runs BETWEEN the git-status
// probe and the non-delivery gate. Every terminal outcome replaces the local remote-tracking
// hint with exact origin ref/SHA proof, so failed runs retain trustworthy partial-delivery
// evidence too. Only a would-be success may actively push, and only on its provisioned
// branch. On a push refusal/failure Pushed remains false and PushError carries the reason;
// the success gate then downgrades it to retryable non_delivery while preserving the
// worktree for retry/manual inspection.
func (m *Monitor) eagerPushBeforeGate(ctx context.Context, c Completion) Completion {
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
	localPushedHint := c.Git.Pushed
	if localPushedHint {
		// branch -r is only a local cache hint. Clear it until ls-remote binds the actual
		// origin ref to this exact HEAD; a deleted/force-moved remote must not pass the
		// durable-delivery gate from stale local metadata.
		m.log("EAGER-PUSH verify executor=%s task=%s branch=%s head=%s: remote-tracking hint present — requiring exact origin ref/SHA proof",
			c.ExecutorID, m.taskRef(c.ExecutorID), c.Git.Branch, c.Git.HeadSHA)
		c.Git.Pushed = false
	}
	// Failed/crashed runs are never actively pushed, but their partial delivery is still
	// valuable supervisor evidence. Verify every terminal outcome so mirror-backed self-
	// pushes are reported and stale local remote-tracking hints are never trusted.
	if c.Kind != OutcomeSucceeded {
		verified, err := m.verifyExistingOriginDelivery(ctx, c)
		switch {
		case verified:
			c.Git.Pushed = true
			m.log("ORIGIN-VERIFY ok executor=%s task=%s outcome=%s branch=%s head=%s — partial delivery is independently durable",
				c.ExecutorID, m.taskRef(c.ExecutorID), c.Kind, c.Git.Branch, c.Git.HeadSHA)
		case err != nil:
			c.Git.PushError = err.Error()
			m.log("ORIGIN-VERIFY FAILED executor=%s task=%s outcome=%s branch=%s: %v",
				c.ExecutorID, m.taskRef(c.ExecutorID), c.Kind, c.Git.Branch, err)
		case localPushedHint:
			c.Git.PushError = fmt.Sprintf("stale remote-tracking hint: origin ref does not point at HEAD %s", c.Git.HeadSHA)
			m.log("ORIGIN-VERIFY stale executor=%s task=%s outcome=%s branch=%s head=%s — local hint rejected",
				c.ExecutorID, m.taskRef(c.ExecutorID), c.Kind, c.Git.Branch, c.Git.HeadSHA)
		}
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
		if want := m.expectedExecutorBranch(c.ExecutorID); want != "" {
			c.Git.Branch = want
		}
		m.log("EAGER-PUSH ok executor=%s task=%s branch=%s head=%s — durably delivered to origin",
			c.ExecutorID, m.taskRef(c.ExecutorID), c.Git.Branch, c.Git.HeadSHA)
	}
	return c
}

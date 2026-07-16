package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRefExists reports whether ref resolves in the git dir (a pushed branch on a bare remote
// resolves; an unpushed / refused branch does not).
func gitRefExists(gitBin, dir, ref string) bool {
	cmd := exec.Command(gitBin, "rev-parse", "--verify", "--quiet", ref)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	return cmd.Run() == nil
}

// setupAcExecPushCase provisions an executor on the given branch off main, wires an origin
// remote (originURL), writes a TaskRef input + a committed change, and returns the workspace
// dir. taskRef drives the expected ac-exec/<task>/<exec> branch the guardrail enforces.
func setupAcExecPushCase(t *testing.T, f *finalizeGateFixture, id, taskRef, branch, originURL string) string {
	return setupAcExecPushCaseBase(t, f, id, taskRef, branch, originURL, "main")
}

// setupAcExecPushCaseBase is setupAcExecPushCase with an explicit Record base ref, so a test
// can reproduce the production condition where the Record carries NO base (recordBase="" →
// BaseKnown=false / AheadOfBase=0 — the issue-f30b7e7b P0 that skipped the push).
func setupAcExecPushCaseBase(t *testing.T, f *finalizeGateFixture, id, taskRef, branch, originURL, recordBase string) string {
	t.Helper()
	if originURL != "" {
		runGitIn(t, f.git, f.repo, "remote", "add", "origin", originURL)
	}
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatalf("provision %s: %v", id, err)
	}
	ws, _ := f.fx.Layout().WorkspaceDir(id)
	if err := f.prov.AddNewBranch(context.Background(), ws, branch, "main"); err != nil {
		t.Fatalf("AddNewBranch %s@%s: %v", id, branch, err)
	}
	must(t, f.tr.Write(Record{ExecutorID: id, PID: 1234, SpawnedAt: testNow, BaseRef: recordBase}))
	mustWriteInput(t, f.fx, inputWithTaskRef(id, taskRef))
	must(t, f.fx.WriteOutput(*okOutput(id)))
	must(t, f.fx.WriteStatus(*doneStatus(id)))
	// The executor committed real work onto its branch (but never pushed — that is D1's job).
	if err := os.WriteFile(filepath.Join(ws, "work.txt"), []byte("delivered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, f.git, ws, "add", "-A")
	runGitIn(t, f.git, ws, "commit", "-q", "-m", "committed work")
	return ws
}

// TestMonitor_Finalize_EagerPush_BaseUnknown_StillPushes is the issue-f30b7e7b P0
// regression lock (the RR-caught bug): when the Record carries NO base ref — the exact
// production condition where recordBaseRef()="" → BaseKnown=false and AheadOfBase reads a
// false-precise 0 — the eager-push MUST STILL FIRE (base-unknown is "couldn't tell", NOT
// "nothing to deliver"). The guardrail still confines it to the ac-exec branch. Before the
// fix the push was silently skipped and the committed review-only work was lost.
func TestMonitor_Finalize_EagerPush_BaseUnknown_StillPushes(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id := "exec-baseunknown"
	taskRef := "T-D1"
	branch := "ac-exec/" + taskRef + "/" + id
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")

	// recordBase="" → the probe cannot resolve a base → BaseKnown=false, AheadOfBase=0 (the
	// production P0 shape), even though the executor really committed onto its ac-exec branch.
	setupAcExecPushCaseBase(t, f, id, taskRef, branch, bare, "")

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	reps := f.wb.reports
	if len(reps) != 1 || reps[0].Kind != OutcomeSucceeded {
		t.Fatalf("base-unknown committed delivery must be eager-pushed and stay succeeded, kinds=%v", f.wb.kinds())
	}
	if reps[0].Git == nil || !reps[0].Git.Pushed {
		t.Errorf("base-unknown run must still be pushed (couldn't-tell ≠ nothing-to-deliver), got %+v", reps[0].Git)
	}
	if reps[0].Git != nil && reps[0].Git.BaseKnown {
		t.Errorf("test setup invalid: BaseKnown should be false (no record base), got %+v", reps[0].Git)
	}
	// The branch REALLY reached origin — the P0 was that it silently did not.
	if !gitRefExists(f.git, bare, "refs/heads/"+branch) {
		t.Errorf("branch %q must exist on origin after eager-push (P0: base-unknown skipped the push)", branch)
	}
	if !f.loggedContains("base UNKNOWN") {
		t.Errorf("expected a fail-loud base-UNKNOWN log (never a silent skip), logs=%v", f.logs)
	}
}

// TestMonitor_Finalize_EagerPush_PushedStaysSucceeded is the issue-f30b7e7b PRIMARY-fix
// positive lock: a committed-but-unpushed success on the executor's own ac-exec branch is
// eager-pushed to origin by the agent-runtime, so it becomes a DURABLE delivery — the gate
// sees Pushed=true and leaves it OutcomeSucceeded, NOT retryable / NOT reopened (the
// steady-state "successful delivery does not respawn" assertion), and the branch really
// lands on the remote.
func TestMonitor_Finalize_EagerPush_PushedStaysSucceeded(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id := "exec-push-ok"
	taskRef := "T-D1"
	branch := "ac-exec/" + taskRef + "/" + id
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")

	setupAcExecPushCase(t, f, id, taskRef, branch, bare)

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	reps := f.wb.reports
	if len(reps) != 1 || reps[0].Kind != OutcomeSucceeded {
		t.Fatalf("eager-pushed delivery must stay succeeded, kinds=%v", f.wb.kinds())
	}
	if reps[0].Retryable {
		t.Error("a successfully eager-pushed delivery must NOT be retryable/reopened (positive steady-state)")
	}
	if reps[0].Git == nil || !reps[0].Git.Pushed {
		t.Errorf("after eager-push Completion.Git.Pushed must be true, got %+v", reps[0].Git)
	}
	if reps[0].Git != nil && reps[0].Git.PushError != "" {
		t.Errorf("a successful push must leave PushError empty, got %q", reps[0].Git.PushError)
	}
	if f.loggedContains("NON-DELIVERY") {
		t.Errorf("an eager-pushed delivery must NOT be logged non-delivery, logs=%v", f.logs)
	}
	if !f.loggedContains("EAGER-PUSH ok") {
		t.Errorf("expected EAGER-PUSH ok log, logs=%v", f.logs)
	}
	// The branch is REALLY on the remote (durable off-machine).
	if !gitRefExists(f.git, bare, "refs/heads/"+branch) {
		t.Errorf("branch %q must exist on the origin remote after eager-push", branch)
	}
}

// TestMonitor_Finalize_EagerPush_MainBranchRefused is the branch-guardrail lock (the most
// dangerous corner): an executor whose HEAD is NOT its provisioned ac-exec branch (here a
// stray executor/<id> branch — stands in for main/detached/unexpected) must NEVER be pushed,
// so a stray local commit can never reach origin/main. The run is refused and downgraded to
// non_delivery carrying the refusal, and the branch does NOT appear on the remote.
func TestMonitor_Finalize_EagerPush_MainBranchRefused(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id := "exec-wrongbranch"
	taskRef := "T-D1"
	wrongBranch := "executor/" + id // != ac-exec/<task>/<exec> → guardrail must refuse
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")

	setupAcExecPushCase(t, f, id, taskRef, wrongBranch, bare)

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	reps := f.wb.reports
	if len(reps) != 1 {
		t.Fatalf("want 1 report, got %d", len(reps))
	}
	got := reps[0]
	if got.Kind != OutcomeCrashed || !got.Retryable || got.Error == nil || got.Error.Kind != "non_delivery" {
		t.Errorf("refused push must downgrade to retryable non_delivery, got kind=%q retryable=%v err=%+v", got.Kind, got.Retryable, got.Error)
	}
	if got.Git == nil || got.Git.Pushed || got.Git.PushError == "" || !strings.Contains(got.Git.PushError, "refused") {
		t.Errorf("expected a 'refused' PushError with Pushed=false, got %+v", got.Git)
	}
	// CRITICAL: the stray branch must NOT have been pushed to the remote.
	if gitRefExists(f.git, bare, "refs/heads/"+wrongBranch) {
		t.Errorf("guardrail breach: stray branch %q was pushed to origin — must never happen", wrongBranch)
	}
	if !f.loggedContains("EAGER-PUSH FAILED") {
		t.Errorf("expected EAGER-PUSH FAILED (refused) log, logs=%v", f.logs)
	}
}

// TestMonitor_Finalize_EagerPush_PushFailureDowngrades is the failure-path lock (P0-B / #3):
// when the eager-push to origin fails (here an unreachable remote — stands in for auth /
// write-permission / non-ff / network failure), Pushed stays false, the run is downgraded to
// a retryable non_delivery carrying the push error, and the worktree is RETAINED (so the
// commit survives for retry / manual push) — never silently dropped, never force-pushed.
func TestMonitor_Finalize_EagerPush_PushFailureDowngrades(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id := "exec-pushfail"
	taskRef := "T-D1"
	branch := "ac-exec/" + taskRef + "/" + id
	badRemote := filepath.Join(t.TempDir(), "no-such-remote") // does not exist → push errors

	setupAcExecPushCase(t, f, id, taskRef, branch, badRemote)

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	reps := f.wb.reports
	if len(reps) != 1 {
		t.Fatalf("want 1 report, got %d", len(reps))
	}
	got := reps[0]
	if got.Kind != OutcomeCrashed || !got.Retryable || got.Error == nil || got.Error.Kind != "non_delivery" {
		t.Errorf("push failure must downgrade to retryable non_delivery, got kind=%q retryable=%v err=%+v", got.Kind, got.Retryable, got.Error)
	}
	if got.Git == nil || got.Git.Pushed || got.Git.PushError == "" {
		t.Errorf("failed push must leave Pushed=false with a PushError set, got %+v", got.Git)
	}
	// The non_delivery reason surfaces the push failure to the supervisor/audit.
	if got.Error != nil && !strings.Contains(got.Error.Message, "eager-push failed") {
		t.Errorf("non_delivery reason must surface the eager-push failure, got %q", got.Error.Message)
	}
	// Worktree RETAINED for retry / manual push.
	d, _ := f.fx.Layout().Dir(id)
	if _, err := os.Stat(d); err != nil {
		t.Errorf("push-failed run must RETAIN the executor dir for retry, stat: %v", err)
	}
	if !f.loggedContains("EAGER-PUSH FAILED") {
		t.Errorf("expected EAGER-PUSH FAILED log, logs=%v", f.logs)
	}
}

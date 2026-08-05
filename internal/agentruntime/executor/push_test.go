package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type deadlineCheckingGitRunner struct{ sawDeadline bool }

func (r *deadlineCheckingGitRunner) Run(ctx context.Context, _ string, _ []string, _ ...string) (string, error) {
	_, r.sawDeadline = ctx.Deadline()
	return "", context.DeadlineExceeded
}

func TestMonitor_DeliveryHelpersFailClosedOnUnknownInputs(t *testing.T) {
	f := newFinalizeGateFixture(t)
	ctx := context.Background()

	if pushed, err := f.mon.eagerSupervisorPush(ctx, Completion{}); pushed || err != nil {
		t.Fatalf("nil git status = (%v, %v), want (false, nil)", pushed, err)
	}
	zero := &FinalizedGitStatus{Probed: true, BaseKnown: true}
	if pushed, err := f.mon.eagerSupervisorPush(ctx, Completion{Git: zero}); pushed || err != nil {
		t.Fatalf("proven zero delivery = (%v, %v), want (false, nil)", pushed, err)
	}

	savedGit := f.mon.git
	nilRunnerID := "exec-nil-git-runner"
	if _, err := f.fx.Provision(nilRunnerID); err != nil {
		t.Fatal(err)
	}
	must(t, f.tr.Write(Record{ExecutorID: nilRunnerID, PID: 1234, SpawnedAt: testNow, BaseRef: "main"}))
	mustWriteInput(t, f.fx, inputWithTaskRef(nilRunnerID, "task-nil-git-runner"))
	f.mon.git = nil
	if _, err := f.mon.eagerSupervisorPush(ctx, Completion{ExecutorID: nilRunnerID, Git: &FinalizedGitStatus{
		Probed: true, Dirty: true, Branch: "ac-exec/task-nil-git-runner/" + nilRunnerID, HeadSHA: "abc",
	}}); err == nil || !strings.Contains(err.Error(), "no git runner") {
		t.Fatalf("nil git runner error=%v", err)
	}
	f.mon.git = savedGit

	id := "exec-corrupt-workspace-record"
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	recordPath, err := f.tr.path(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.mon.executorWorkspacePath(id); err == nil || !strings.Contains(err.Error(), "workspace record") {
		t.Fatalf("corrupt workspace record error=%v", err)
	}
	if gs := f.mon.finalizeGitStatus(ctx, id); gs != nil || !f.loggedContains("GIT-STATUS FAILED") {
		t.Fatalf("corrupt record must fail-loud without a false git status: gs=%+v logs=%v", gs, f.logs)
	}
	mustWriteInput(t, f.fx, inputWithTaskRef(id, "task-corrupt-workspace"))
	if _, err := f.mon.eagerSupervisorPush(ctx, Completion{ExecutorID: id, Git: &FinalizedGitStatus{
		Probed: true, Dirty: true, Branch: "ac-exec/task-corrupt-workspace/" + id, HeadSHA: "abc",
	}}); err == nil || !strings.Contains(err.Error(), "actual workspace") {
		t.Fatalf("corrupt workspace must block eager push, err=%v", err)
	}

	if ok, err := f.mon.originHeadMatches(ctx, "", "branch", "sha"); ok || err != nil {
		t.Fatalf("blank workspace = (%v, %v), want (false, nil)", ok, err)
	}
	f.mon.git = scriptedGitRunner{err: map[string]error{
		"ls-remote --heads origin refs/heads/branch": errors.New("network down"),
	}}
	if ok, err := f.mon.originHeadMatches(ctx, t.TempDir(), "branch", "sha"); ok || err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("origin verification failure = (%v, %v)", ok, err)
	}
	deadlineRunner := &deadlineCheckingGitRunner{}
	f.mon.git = deadlineRunner
	if ok, err := f.mon.originHeadMatches(context.Background(), t.TempDir(), "branch", "sha"); ok || err == nil {
		t.Fatalf("deadline-check origin verification = (%v, %v)", ok, err)
	}
	if !deadlineRunner.sawDeadline {
		t.Fatal("origin verification must impose a bounded network deadline")
	}

	policyID := "exec-protected-policy"
	if _, err := f.fx.Provision(policyID); err != nil {
		t.Fatal(err)
	}
	must(t, f.tr.Write(Record{ExecutorID: policyID, PID: 1234, SpawnedAt: testNow, BaseRef: "release/v2"}))
	policyInput := inputWithTaskRef(policyID, "task-protected-policy")
	policyInput.Repo = &RepoRef{URL: "origin", BaseRef: "release/v2", BaseSHA: "abc", DefaultBranch: "develop"}
	mustWriteInput(t, f.fx, policyInput)
	for _, branch := range []string{"main", "master", "release/v2", "origin/develop"} {
		if err := f.mon.deliveryBranchAllowed(policyID, branch); err == nil {
			t.Errorf("protected branch %q was allowed", branch)
		}
	}
	if err := f.mon.deliveryBranchAllowed(policyID, "hotfix/safe"); err != nil {
		t.Errorf("custom delivery branch was refused: %v", err)
	}
}

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

// setupRecordedWorkspacePushCase reproduces the RepoCacheManager production topology:
// the executor exchange directory exists under Layout, while git runs in a distinct
// runtime-managed worktree persisted in Record.WorkspacePath.
func setupRecordedWorkspacePushCase(t *testing.T, f *finalizeGateFixture, id, taskRef, branch, originURL, recordBase string) string {
	t.Helper()
	if originURL != "" {
		runGitIn(t, f.git, f.repo, "remote", "add", "origin", originURL)
	}
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatalf("provision %s: %v", id, err)
	}
	ws := filepath.Join(t.TempDir(), "runtime-worktrees", id)
	if err := f.prov.AddNewBranch(context.Background(), ws, branch, "main"); err != nil {
		t.Fatalf("AddNewBranch %s@%s: %v", id, branch, err)
	}
	must(t, f.tr.Write(Record{
		ExecutorID: id, PID: 1234, SpawnedAt: testNow, BaseRef: recordBase,
		WorkspacePath: ws,
	}))
	mustWriteInput(t, f.fx, inputWithTaskRef(id, taskRef))
	must(t, f.fx.WriteOutput(*okOutput(id)))
	must(t, f.fx.WriteStatus(*doneStatus(id)))
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

func TestMonitor_Finalize_RecordedWorkspace_EagerPushesExpectedBranch(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, taskRef := "exec-recorded-push", "task-recorded-push"
	branch := "ac-exec/" + taskRef + "/" + id
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	ws := setupRecordedWorkspacePushCase(t, f, id, taskRef, branch, bare, "main")

	layoutWS, _ := f.fx.Layout().WorkspaceDir(id)
	if layoutWS == ws {
		t.Fatal("test setup invalid: recorded workspace must differ from exchange layout workspace")
	}
	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	got := f.wb.reports[0]
	if got.Kind != OutcomeSucceeded || got.Git == nil || !got.Git.Pushed || got.Git.Branch != branch {
		t.Fatalf("recorded workspace delivery=%+v", got)
	}
	if !gitRefExists(f.git, bare, "refs/heads/"+branch) {
		t.Fatalf("expected branch %q was not pushed from recorded workspace", branch)
	}
}

// Regression for exec-a9ae5c15: the executor used a RepoCacheManager worktree, switched
// to a task-specific branch, and pushed it itself. Mirror caches have no refs/remotes/*,
// so branch -r cannot prove delivery; Finalize must resolve Record.WorkspacePath and bind
// the actual origin ref to the exact local HEAD SHA before accepting it.
func TestMonitor_Finalize_RecordedWorkspace_DiscoversActualPushedBranch(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, taskRef := "exec-recorded-discovery", "task-recorded-discovery"
	branch := "hotfix/deployed-binary-smoke-version-assertions"
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	ws := setupRecordedWorkspacePushCase(t, f, id, taskRef, branch, bare, "main")
	runGitIn(t, f.git, ws, "push", "-q", "origin", branch)
	// Ensure the local fast-path is unavailable, matching a --mirror refspec checkout.
	runGitIn(t, f.git, ws, "update-ref", "-d", "refs/remotes/origin/"+branch)
	if got := strings.TrimSpace(evidenceGitOut(t, f.git, ws, "branch", "-r", "--contains", "HEAD")); got != "" {
		t.Fatalf("test setup invalid: unexpected remote-tracking proof %q", got)
	}
	wantSHA := strings.TrimSpace(evidenceGitOut(t, f.git, ws, "rev-parse", "HEAD"))

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	got := f.wb.reports[0]
	if got.Kind != OutcomeSucceeded || got.Git == nil || !got.Git.Pushed {
		t.Fatalf("independently verified origin delivery must stay succeeded: %+v", got)
	}
	if got.Git.Branch != branch || got.Git.HeadSHA != wantSHA {
		t.Fatalf("delivery identity branch=%q sha=%q, want branch=%q sha=%q", got.Git.Branch, got.Git.HeadSHA, branch, wantSHA)
	}
	if !f.loggedContains("EAGER-PUSH ok") {
		t.Fatalf("remote discovery must be observable as durable delivery, logs=%v", f.logs)
	}
}

func TestMonitor_Finalize_FailedRunDiscoversPartialOriginDelivery(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, taskRef := "exec-failed-partial", "task-failed-partial"
	branch := "hotfix/partial-delivery"
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	ws := setupRecordedWorkspacePushCase(t, f, id, taskRef, branch, bare, "main")
	runGitIn(t, f.git, ws, "push", "-q", "origin", branch)
	runGitIn(t, f.git, ws, "update-ref", "-d", "refs/remotes/origin/"+branch)

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeFailed,
		Error: &ErrorDetail{Kind: "tests_failed", Message: "implementation is partial"},
	}))

	got := f.wb.reports[0]
	if got.Kind != OutcomeFailed || got.Git == nil || !got.Git.Pushed || got.Git.Branch != branch {
		t.Fatalf("failed run must retain independently verified partial delivery: %+v", got)
	}
	if !f.loggedContains("ORIGIN-VERIFY ok") {
		t.Fatalf("partial delivery verification must be observable, logs=%v", f.logs)
	}
}

func TestMonitor_Finalize_FailedRunNeverCreatesOriginDelivery(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, taskRef := "exec-failed-unpushed", "task-failed-unpushed"
	branch := "ac-exec/" + taskRef + "/" + id
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	setupRecordedWorkspacePushCase(t, f, id, taskRef, branch, bare, "main")

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeFailed,
		Error: &ErrorDetail{Kind: "tests_failed", Message: "do not publish a failed run"},
	}))

	got := f.wb.reports[0]
	if got.Kind != OutcomeFailed || got.Git == nil || got.Git.Pushed {
		t.Fatalf("failed unpushed run must remain failed and unpushed: %+v", got)
	}
	if gitRefExists(f.git, bare, "refs/heads/"+branch) {
		t.Fatalf("failed run must never create origin ref %q", branch)
	}
}

func TestMonitor_Finalize_FailedRunRejectsStaleRemoteTrackingHint(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, taskRef := "exec-failed-stale", "task-failed-stale"
	branch := "hotfix/stale-partial"
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	ws := setupRecordedWorkspacePushCase(t, f, id, taskRef, branch, bare, "main")
	runGitIn(t, f.git, ws, "push", "-q", "origin", branch)
	if err := os.WriteFile(filepath.Join(ws, "second.txt"), []byte("local only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, f.git, ws, "add", "-A")
	runGitIn(t, f.git, ws, "commit", "-q", "-m", "local partial")
	// Plant the exact stale-local shape: the local hint contains HEAD, while origin remains
	// on the prior commit. It must never survive as durable Pushed evidence.
	runGitIn(t, f.git, ws, "update-ref", "refs/remotes/origin/"+branch, "HEAD")

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeFailed,
		Error: &ErrorDetail{Kind: "tests_failed", Message: "implementation is partial"},
	}))

	got := f.wb.reports[0]
	if got.Git == nil || got.Git.Pushed || !strings.Contains(got.Git.PushError, "stale remote-tracking hint") {
		t.Fatalf("stale local hint must be rejected for failed runs: %+v", got.Git)
	}
}

func TestMonitor_Finalize_ExactOriginMainStillRefused(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, taskRef := "exec-protected-main", "task-protected-main"
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	ws := filepath.Join(root, "work")
	runGitIn(t, f.git, root, "clone", "-q", "--bare", f.repo, bare)
	runGitIn(t, f.git, root, "clone", "-q", bare, ws)
	baseSHA := strings.TrimSpace(evidenceGitOut(t, f.git, ws, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(ws, "protected.txt"), []byte("must not count\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, f.git, ws, "add", "-A")
	runGitIn(t, f.git, ws, "commit", "-q", "-m", "executor wrote main")
	runGitIn(t, f.git, ws, "push", "-q", "origin", "main")

	if _, err := f.fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	must(t, f.tr.Write(Record{
		ExecutorID: id, PID: 1234, SpawnedAt: testNow, BaseRef: baseSHA, WorkspacePath: ws,
	}))
	in := inputWithTaskRef(id, taskRef)
	in.Repo = &RepoRef{URL: bare, DefaultBranch: "main", BaseRef: "main", BaseSHA: baseSHA}
	mustWriteInput(t, f.fx, in)

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	got := f.wb.reports[0]
	if got.Kind != OutcomeCrashed || got.Git == nil || got.Git.Pushed ||
		!strings.Contains(got.Git.PushError, "protected") {
		t.Fatalf("an exact origin/main ref must still be refused as delivery: %+v", got)
	}
}

func TestMonitor_Finalize_RecordedWorkspace_RejectsOriginAtDifferentSHA(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id, taskRef := "exec-recorded-mismatch", "task-recorded-mismatch"
	branch := "hotfix/remote-behind"
	bare := t.TempDir()
	runGitIn(t, f.git, bare, "init", "-q", "--bare")
	ws := setupRecordedWorkspacePushCase(t, f, id, taskRef, branch, bare, "main")
	runGitIn(t, f.git, ws, "push", "-q", "origin", branch)
	remoteSHA := strings.TrimSpace(evidenceGitOut(t, f.git, bare, "rev-parse", "refs/heads/"+branch))
	runGitIn(t, f.git, ws, "update-ref", "-d", "refs/remotes/origin/"+branch)
	if err := os.WriteFile(filepath.Join(ws, "second.txt"), []byte("not pushed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, f.git, ws, "add", "-A")
	runGitIn(t, f.git, ws, "commit", "-q", "-m", "unpublished second commit")

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	got := f.wb.reports[0]
	if got.Kind != OutcomeCrashed || !got.Retryable || got.Error == nil || got.Error.Kind != "non_delivery" {
		t.Fatalf("origin SHA mismatch must fail closed: %+v", got)
	}
	if got.Git == nil || got.Git.Pushed || !strings.Contains(got.Git.PushError, "does not point at HEAD") {
		t.Fatalf("origin SHA mismatch reason missing: %+v", got.Git)
	}
	after := strings.TrimSpace(evidenceGitOut(t, f.git, bare, "rev-parse", "refs/heads/"+branch))
	if after != remoteSHA {
		t.Fatalf("guardrail pushed unexpected branch: origin moved from %s to %s", remoteSHA, after)
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

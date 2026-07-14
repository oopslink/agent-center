package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/clock"
)

// gitEnv is the neutralized env for the deterministic local-git test helpers (mirrors the
// probe's own env, plus a fixed identity so commits are reproducible).
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
}

// runGitIn runs git in dir, failing the test on error. Deterministic local binary.
func runGitIn(t *testing.T, gitBin, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(gitBin, args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %v: %v\n%s", dir, args, err, out)
	}
}

// newSourceRepo builds a source repo with one commit on branch main and returns its path
// (skips the test if git is unavailable).
func newSourceRepo(t *testing.T) (gitBin, repo string) {
	t.Helper()
	gb, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	repo = t.TempDir()
	runGitIn(t, gb, repo, "init", "-q", "-b", "main")
	if werr := os.WriteFile(filepath.Join(repo, "README.md"), []byte("base\n"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	runGitIn(t, gb, repo, "add", "-A")
	runGitIn(t, gb, repo, "commit", "-q", "-m", "base")
	return gb, repo
}

// TestProbeGitStatus_DeliverySignals pins the mechanical delivery signals the non-delivery
// gate reads: a fresh worktree off base with no work is a proven ZERO-delivery; a commit
// past base OR a dirty tree is a delivery; an empty/unresolvable base is UNKNOWN (never
// mistaken for zero-delivery).
func TestProbeGitStatus_DeliverySignals(t *testing.T) {
	gitBin, repo := newSourceRepo(t)
	prov, err := NewWorktreeProvisioner(repo, NewExecGitRunner())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runner := NewExecGitRunner()

	// (a) fresh worktree off main, no work → ahead=0, clean, not pushed → ZeroDelivery.
	wsClean := filepath.Join(t.TempDir(), "clean")
	if err := prov.AddNewBranch(ctx, wsClean, "executor/clean", "main"); err != nil {
		t.Fatalf("AddNewBranch clean: %v", err)
	}
	gs := probeGitStatus(ctx, runner, wsClean, "main")
	if !gs.Probed || !gs.BaseKnown || gs.AheadOfBase != 0 || gs.Dirty || gs.Pushed {
		t.Fatalf("clean worktree: %+v", gs)
	}
	if gs.HasDelivery() {
		t.Error("clean worktree must not report delivery")
	}
	if !gs.ZeroDelivery() {
		t.Error("clean worktree off base must be a proven zero-delivery")
	}

	// (b) a commit past base → ahead=1 → delivery.
	wsCommit := filepath.Join(t.TempDir(), "commit")
	if err := prov.AddNewBranch(ctx, wsCommit, "executor/commit", "main"); err != nil {
		t.Fatalf("AddNewBranch commit: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsCommit, "new.txt"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, gitBin, wsCommit, "add", "-A")
	runGitIn(t, gitBin, wsCommit, "commit", "-q", "-m", "real work")
	gs = probeGitStatus(ctx, runner, wsCommit, "main")
	if gs.AheadOfBase != 1 || !gs.HasDelivery() || gs.ZeroDelivery() {
		t.Errorf("committed worktree: %+v (want ahead=1, delivery, not zero)", gs)
	}

	// (c) a dirty tree (uncommitted change) → delivery even with ahead=0.
	wsDirty := filepath.Join(t.TempDir(), "dirty")
	if err := prov.AddNewBranch(ctx, wsDirty, "executor/dirty", "main"); err != nil {
		t.Fatalf("AddNewBranch dirty: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDirty, "wip.txt"), []byte("wip\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gs = probeGitStatus(ctx, runner, wsDirty, "main")
	if gs.AheadOfBase != 0 || !gs.Dirty || !gs.HasDelivery() || gs.ZeroDelivery() {
		t.Errorf("dirty worktree: %+v (want ahead=0, dirty, delivery, not zero)", gs)
	}

	// (d) an unresolvable base → BaseKnown=false → UNKNOWN, never zero-delivery.
	gs = probeGitStatus(ctx, runner, wsClean, "no/such/ref")
	if !gs.Probed || gs.BaseKnown || gs.ZeroDelivery() {
		t.Errorf("unresolvable base: %+v (want probed, base-unknown, not zero-delivery)", gs)
	}

	// (e) an empty base → BaseKnown=false (no base to compare) → never zero-delivery.
	gs = probeGitStatus(ctx, runner, wsClean, "")
	if !gs.Probed || gs.BaseKnown || gs.ZeroDelivery() {
		t.Errorf("empty base: %+v (want probed, base-unknown, not zero-delivery)", gs)
	}

	// (f) a non-git dir → Probed=false → never zero-delivery.
	gs = probeGitStatus(ctx, runner, t.TempDir(), "main")
	if gs.Probed || gs.ZeroDelivery() {
		t.Errorf("non-git dir: %+v (want not probed, not zero-delivery)", gs)
	}
}

// finalizeGateFixture wires a Monitor with REAL git (so the delivery gate actually probes
// a worktree), a Tracker (for the spawn-time base ref), and a recording writeback.
type finalizeGateFixture struct {
	repo string
	git  string
	fx   *FileExchange
	tr   *Tracker
	prov *WorktreeProvisioner
	wb   *fakeWriteback
	mon  *Monitor
	logs []string
}

func newFinalizeGateFixture(t *testing.T) *finalizeGateFixture {
	t.Helper()
	gitBin, repo := newSourceRepo(t)
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatal(err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(testNow))
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := NewTracker(layout)
	prov, err := NewWorktreeProvisioner(repo, NewExecGitRunner())
	if err != nil {
		t.Fatal(err)
	}
	wb := &fakeWriteback{}
	f := &finalizeGateFixture{repo: repo, git: gitBin, fx: fx, tr: tr, prov: prov, wb: wb}
	mon, err := NewMonitor(MonitorConfig{
		Exchange: fx, Worktrees: prov, Tracker: tr, Writeback: wb,
		GitRunner: NewExecGitRunner(), Clock: clock.NewFakeClock(testNow),
		Log: func(format string, a ...any) { f.logs = append(f.logs, fmt.Sprintf(format, a...)) },
	})
	if err != nil {
		t.Fatal(err)
	}
	f.mon = mon
	return f
}

func (f *finalizeGateFixture) loggedContains(sub string) bool {
	for _, l := range f.logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// setupExecutorWorktree provisions an executor dir + a real worktree off main (base=main)
// and records its BaseRef, then writes a self-reported SUCCESS output/status.
func (f *finalizeGateFixture) setupExecutorWorktree(t *testing.T, id string) string {
	t.Helper()
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatalf("provision %s: %v", id, err)
	}
	ws, _ := f.fx.Layout().WorkspaceDir(id)
	if err := f.prov.AddNewBranch(context.Background(), ws, "executor/"+id, "main"); err != nil {
		t.Fatalf("AddNewBranch %s: %v", id, err)
	}
	must(t, f.tr.Write(Record{ExecutorID: id, PID: 1234, SpawnedAt: testNow, BaseRef: "main"}))
	must(t, f.fx.WriteOutput(*okOutput(id)))
	must(t, f.fx.WriteStatus(*doneStatus(id)))
	return ws
}

// TestMonitor_Finalize_NonDeliveryGate is the issue-37015227 ② regression lock: an executor
// that self-reports SUCCESS but produced NO git side effect (HEAD never advanced past base,
// clean tree, nothing pushed) must NOT be completed — it is downgraded to a retryable
// non_delivery, fail-loud, and its dir/worktree is RETAINED (not torn down).
func TestMonitor_Finalize_NonDeliveryGate(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id := "exec-nodeliver"
	f.setupExecutorWorktree(t, id)

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	reps := f.wb.reports
	if len(reps) != 1 {
		t.Fatalf("writeback got %d reports, want 1", len(reps))
	}
	got := reps[0]
	if got.Kind != OutcomeCrashed {
		t.Errorf("zero-delivery success must be downgraded, Kind=%q want crashed", got.Kind)
	}
	if !got.Retryable {
		t.Error("non-delivery downgrade must be retryable")
	}
	if got.Error == nil || got.Error.Kind != "non_delivery" {
		t.Errorf("expected non_delivery error, got %+v", got.Error)
	}
	if !f.loggedContains("NON-DELIVERY") {
		t.Errorf("expected fail-loud NON-DELIVERY log, logs=%v", f.logs)
	}
	// Retryable ⇒ dir/worktree RETAINED for re-launch + inspection (never torn down as if
	// it had delivered).
	d, _ := f.fx.Layout().Dir(id)
	if _, err := os.Stat(d); err != nil {
		t.Errorf("non-delivery (retryable) must RETAIN the executor dir, stat: %v", err)
	}
}

// TestMonitor_Finalize_PushedDeliveryStaysSucceeded is the inverse lock: a success whose
// work is DURABLY delivered — committed AND pushed (HEAD present on a remote-tracking
// branch) — is left as OutcomeSucceeded. The durable-delivery criterion (issue-f30b7e7b N3)
// requires the push, not just a commit.
func TestMonitor_Finalize_PushedDeliveryStaysSucceeded(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id := "exec-delivered"
	ws := f.setupExecutorWorktree(t, id)
	// The executor committed real work AND pushed it: HEAD lands on a remote-tracking branch.
	if err := os.WriteFile(filepath.Join(ws, "delivered.txt"), []byte("real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, f.git, ws, "add", "-A")
	runGitIn(t, f.git, ws, "commit", "-q", "-m", "delivered")
	// Simulate a push: put HEAD on a remote-tracking ref so `branch -r --contains HEAD`
	// reports it (Pushed=true) without needing a real remote.
	runGitIn(t, f.git, ws, "update-ref", "refs/remotes/origin/executor/"+id, "HEAD")

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	reps := f.wb.reports
	if len(reps) != 1 || reps[0].Kind != OutcomeSucceeded {
		t.Fatalf("durable (pushed) delivery must stay succeeded, reports=%v", f.wb.kinds())
	}
	if f.loggedContains("NON-DELIVERY") {
		t.Errorf("a durable delivery must not be logged as non-delivery, logs=%v", f.logs)
	}
}

// TestMonitor_Finalize_CommittedButUnpushed_Downgraded is the issue-f30b7e7b N3 blind-spot
// regression lock: an executor that COMMITTED real work (HEAD past base) but never PUSHED it
// self-reports success — but the commit dies with the reaped worktree, so it delivered
// NOTHING durable. The prior gate (HasDelivery = ahead>0) TRUSTED this (the review-only
// false-success); the durable criterion (Probed && !Pushed) must downgrade it to
// non_delivery + RETAIN the worktree.
func TestMonitor_Finalize_CommittedButUnpushed_Downgraded(t *testing.T) {
	f := newFinalizeGateFixture(t)
	id := "exec-unpushed"
	ws := f.setupExecutorWorktree(t, id)
	// Committed, but NOT pushed (no remote-tracking ref).
	if err := os.WriteFile(filepath.Join(ws, "work.txt"), []byte("committed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, f.git, ws, "add", "-A")
	runGitIn(t, f.git, ws, "commit", "-q", "-m", "committed-not-pushed")

	must(t, f.mon.Finalize(context.Background(), Completion{
		ExecutorID: id, Kind: OutcomeSucceeded, Output: okOutput(id), Status: doneStatus(id),
	}))

	reps := f.wb.reports
	if len(reps) != 1 {
		t.Fatalf("writeback got %d reports, want 1", len(reps))
	}
	got := reps[0]
	if got.Kind != OutcomeCrashed || !got.Retryable {
		t.Errorf("committed-but-unpushed must be downgraded to retryable non-delivery, Kind=%q retryable=%v", got.Kind, got.Retryable)
	}
	if got.Error == nil || got.Error.Kind != "non_delivery" {
		t.Errorf("expected non_delivery error, got %+v", got.Error)
	}
	// The committed work IS carried to the center (Completion.Git) so the writeback/center
	// can see it was committed-but-unpushed (ahead>0, pushed=false).
	if got.Git == nil || got.Git.Pushed || got.Git.AheadOfBase == 0 {
		t.Errorf("Completion.Git must show committed-but-unpushed (ahead>0, pushed=false), got %+v", got.Git)
	}
	if !f.loggedContains("NON-DELIVERY") {
		t.Errorf("expected fail-loud NON-DELIVERY log, logs=%v", f.logs)
	}
	// Retryable ⇒ worktree RETAINED (so the supervisor can still push the commit).
	d, _ := f.fx.Layout().Dir(id)
	if _, err := os.Stat(d); err != nil {
		t.Errorf("downgraded run must RETAIN the executor dir, stat: %v", err)
	}
}

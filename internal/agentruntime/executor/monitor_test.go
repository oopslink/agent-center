package executor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// fakeWriteback records every Completion reported to the sole-writer sink.
type fakeWriteback struct {
	mu      sync.Mutex
	reports []Completion
	err     error
}

func (f *fakeWriteback) Report(_ context.Context, c Completion) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.reports = append(f.reports, c)
	return nil
}

func (f *fakeWriteback) kinds() []OutcomeKind {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]OutcomeKind, len(f.reports))
	for i, r := range f.reports {
		out[i] = r.Kind
	}
	return out
}

type monitorFixture struct {
	root string
	fx   *FileExchange
	tr   *Tracker
	pool *Pool
	wb   *fakeWriteback
	mon  *Monitor
	clk  *clock.FakeClock
	sigs *recordingSignaler
}

func newMonitorFixture(t *testing.T, max int) *monitorFixture {
	t.Helper()
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	clk := clock.NewFakeClock(time.Unix(1700000000, 0))
	fx, err := NewFileExchange(layout, clk)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	tr, _ := NewTracker(layout)
	wt, _ := NewWorktreeProvisioner(root, &fakeGitRunner{})
	sigs := &recordingSignaler{}
	var pidSeq int
	var pmu sync.Mutex
	sp := &Spawner{
		start: func(cmd *exec.Cmd) error {
			pmu.Lock()
			pidSeq++
			cmd.Process = &os.Process{Pid: 5000 + pidSeq}
			pmu.Unlock()
			return nil
		},
		signal: sigs.signal,
	}
	pool, err := NewPool(PoolConfig{Exchange: fx, Worktrees: wt, Spawner: sp, AgentRoot: root, BaseRef: "main", Max: max})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	wd := NewWatchdog(WatchdogConfig{StallTimeout: time.Minute, Clock: clk, Sleep: func(time.Duration) {}})
	rc, _ := NewReconciler(fx, tr, fakeLiveness{})
	wb := &fakeWriteback{}
	mon, err := NewMonitor(MonitorConfig{Exchange: fx, Worktrees: wt, Pool: pool, Watchdog: wd, Reconciler: rc, Writeback: wb, Clock: clk})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	return &monitorFixture{root: root, fx: fx, tr: tr, pool: pool, wb: wb, mon: mon, clk: clk, sigs: sigs}
}

func dirExists(t *testing.T, fx *FileExchange, id string) bool {
	t.Helper()
	d, err := fx.Layout().Dir(id)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	_, statErr := os.Stat(d)
	return statErr == nil
}

// assertDelayedTeardown asserts a terminal executor is RETAINED after Finalize
// (delayed teardown, issue-f30b7e7b) and then removed by the reaper (ttl<=0 reaps
// all finalized). Use for single-terminal-executor tests.
func assertDelayedTeardown(t *testing.T, mon *Monitor, fx *FileExchange, id string) {
	t.Helper()
	if !dirExists(t, fx, id) {
		t.Errorf("terminal executor %s must be RETAINED until reaped (delayed teardown)", id)
	}
	if n, err := mon.ReapFinalized(context.Background(), 0, 0); err != nil {
		t.Fatalf("ReapFinalized: %v", err)
	} else if n < 1 {
		t.Errorf("ReapFinalized reaped %d, want >=1", n)
	}
	if dirExists(t, fx, id) {
		t.Errorf("reap must remove the retained terminal executor dir %s", id)
	}
}

// startRealProc starts a tiny real process that exits with code, so Handle.Wait
// reaps a genuine child (the live completion path needs a reapable process).
func startRealProc(t *testing.T, id string, code int, sig groupSignaler) *Handle {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit "+itoa(code))
	if err := cmd.Start(); err != nil {
		t.Fatalf("start proc: %v", err)
	}
	return &Handle{ExecutorID: id, PID: cmd.Process.Pid, cmd: cmd, signal: sig}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func TestMonitor_AwaitCompletion_Success(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-ok"
	mustProvision(t, f.fx, id)
	mustWriteStatus(t, f.fx, *doneStatus(id))
	mustWriteOutput(t, f.fx, *okOutput(id))
	if err := f.pool.Adopt(id); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	h := startRealProc(t, id, 0, f.sigs.signal)

	c, err := f.mon.AwaitCompletion(context.Background(), h)
	if err != nil {
		t.Fatalf("AwaitCompletion: %v", err)
	}
	if c.Kind != OutcomeSucceeded {
		t.Fatalf("Kind = %q, want succeeded", c.Kind)
	}
	if k := f.wb.kinds(); len(k) != 1 || k[0] != OutcomeSucceeded {
		t.Errorf("writeback kinds = %v, want [succeeded]", k)
	}
	if f.pool.Active() != 0 {
		t.Errorf("slot must be released, Active = %d", f.pool.Active())
	}
	assertDelayedTeardown(t, f.mon, f.fx, id)
}

func TestMonitor_AwaitCompletion_Failure(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-fail"
	mustProvision(t, f.fx, id)
	mustWriteStatus(t, f.fx, *failedStatus(id))
	h := startRealProc(t, id, 3, f.sigs.signal)

	c, err := f.mon.AwaitCompletion(context.Background(), h)
	if err != nil {
		t.Fatalf("AwaitCompletion: %v", err)
	}
	if c.Kind != OutcomeFailed {
		t.Fatalf("Kind = %q, want failed", c.Kind)
	}
	if c.Error == nil || c.Error.Kind != "stk" {
		t.Errorf("failure detail should come from status.error, got %+v", c.Error)
	}
	assertDelayedTeardown(t, f.mon, f.fx, id)
}

// ReapFinalized removes retained terminal executors by TTL (age) and, independently,
// by a count cap (oldest beyond the cap reaped even within the TTL). Delayed teardown.
func TestMonitor_ReapFinalized_TTLAndCap(t *testing.T) {
	f := newMonitorFixture(t, 10)
	// Finalize 4 terminals at t, t+1m, t+2m, t+3m (advance the clock between each).
	for _, id := range []string{"e0", "e1", "e2", "e3"} {
		mustProvision(t, f.fx, id)
		if err := f.mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeSucceeded}); err != nil {
			t.Fatalf("Finalize %s: %v", id, err)
		}
		f.clk.Advance(time.Minute)
	}
	// All 4 retained + marked (now = t+4m; ages e0=4m e1=3m e2=2m e3=1m).
	if refs, _ := f.fx.ListFinalized(); len(refs) != 4 {
		t.Fatalf("ListFinalized = %d, want 4", len(refs))
	}
	// TTL = 2m30s → reap e0(4m) + e1(3m); keep e2(2m) + e3(1m).
	if n, err := f.mon.ReapFinalized(context.Background(), 150*time.Second, 0); err != nil || n != 2 {
		t.Fatalf("TTL reap = %d,%v, want 2,nil", n, err)
	}
	if dirExists(t, f.fx, "e0") || dirExists(t, f.fx, "e1") {
		t.Error("e0/e1 (over TTL) must be reaped")
	}
	if !dirExists(t, f.fx, "e2") || !dirExists(t, f.fx, "e3") {
		t.Error("e2/e3 (within TTL) must be retained")
	}
	// cap=1 with a huge TTL: keep the NEWEST (e3), reap the oldest over cap (e2).
	if n, err := f.mon.ReapFinalized(context.Background(), time.Hour, 1); err != nil || n != 1 {
		t.Fatalf("cap reap = %d,%v, want 1,nil", n, err)
	}
	if dirExists(t, f.fx, "e2") {
		t.Error("e2 (oldest, over cap) must be reaped")
	}
	if !dirExists(t, f.fx, "e3") {
		t.Error("e3 (newest, within cap) must be retained")
	}
}

func TestMonitor_AwaitCompletion_CrashRetainsDir(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-crash"
	mustProvision(t, f.fx, id)
	mustWriteStatus(t, f.fx, runningStatusAt(id, f.clk.Now())) // exits 0 but never wrote output
	if err := f.pool.Adopt(id); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	h := startRealProc(t, id, 0, f.sigs.signal)

	c, err := f.mon.AwaitCompletion(context.Background(), h)
	if err != nil {
		t.Fatalf("AwaitCompletion: %v", err)
	}
	if c.Kind != OutcomeCrashed || !c.Retryable {
		t.Fatalf("Kind = %q retryable=%v, want crashed/true", c.Kind, c.Retryable)
	}
	if f.pool.Active() != 0 {
		t.Errorf("slot must be released even on crash, Active = %d", f.pool.Active())
	}
	if !dirExists(t, f.fx, id) {
		t.Error("a retryable crash must RETAIN the dir for re-launch")
	}
}

func TestMonitor_AwaitCompletion_NilHandle(t *testing.T) {
	f := newMonitorFixture(t, 1)
	if _, err := f.mon.AwaitCompletion(context.Background(), nil); err == nil {
		t.Error("nil handle must error")
	}
}

func TestMonitor_Sweep_KillsOnlyStalled(t *testing.T) {
	f := newMonitorFixture(t, 3)
	base := time.Unix(1700000000, 0)
	// Launch three executors into the pool (fake spawn → real Handles with signal).
	for _, id := range []string{"stalled", "fresh", "nostatus"} {
		if _, err := f.pool.Launch(context.Background(), LaunchSpec{Input: validPoolInput(id), RunnerCmd: []string{"x"}}); err != nil {
			t.Fatalf("launch %s: %v", id, err)
		}
	}
	mustWriteStatus(t, f.fx, runningStatusAt("stalled", base))                  // last progress at base
	mustWriteStatus(t, f.fx, runningStatusAt("fresh", base.Add(9*time.Minute))) // recent
	// "nostatus" has no status file → skipped.
	f.clk.Set(base.Add(10 * time.Minute)) // now: stalled idle=10m (>1m), fresh idle=1m (==threshold→not stalled)

	killed, err := f.mon.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(killed) != 1 || killed[0] != "stalled" {
		t.Fatalf("killed = %v, want [stalled]", killed)
	}
	// The stalled executor's group got SIGTERM then SIGKILL.
	f.sigs.mu.Lock()
	defer f.sigs.mu.Unlock()
	if len(f.sigs.sigs) != 2 || f.sigs.sigs[0] != syscall.SIGTERM || f.sigs.sigs[1] != syscall.SIGKILL {
		t.Errorf("signal sequence = %v, want [SIGTERM SIGKILL]", f.sigs.sigs)
	}
}

func TestMonitor_Sweep_NoPoolOrWatchdog(t *testing.T) {
	mon, err := NewMonitor(MonitorConfig{Exchange: mustExchange(t)})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	killed, err := mon.Sweep(context.Background())
	if err != nil || killed != nil {
		t.Errorf("Sweep with no pool/watchdog = (%v, %v), want (nil, nil)", killed, err)
	}
}

func TestMonitor_Recover_FinalizesAndAdopts(t *testing.T) {
	f := newMonitorFixture(t, 3)
	base := time.Unix(1700000000, 0)
	// alive → re-adopt; done → succeeded+finalize; crash → crashed+finalize(retain).
	mustProvision(t, f.fx, "e-alive")
	mustWriteStatus(t, f.fx, runningStatusAt("e-alive", base))
	mustWriteRecord(t, f.tr, "e-alive", 1001)
	mustProvision(t, f.fx, "e-done")
	mustWriteStatus(t, f.fx, *doneStatus("e-done"))
	mustWriteOutput(t, f.fx, *okOutput("e-done"))
	mustWriteRecord(t, f.tr, "e-done", 1002)
	mustProvision(t, f.fx, "e-crash")
	mustWriteStatus(t, f.fx, runningStatusAt("e-crash", base))
	mustWriteRecord(t, f.tr, "e-crash", 1003)

	// Rebuild the reconciler with a liveness probe that only 1001 is alive.
	rc, _ := NewReconciler(f.fx, f.tr, fakeLiveness{alive: map[int]bool{1001: true}})
	mon, _ := NewMonitor(MonitorConfig{Exchange: f.fx, Worktrees: mustWorktrees(t, f.root), Pool: f.pool, Watchdog: NewWatchdog(WatchdogConfig{Sleep: func(time.Duration) {}}), Reconciler: rc, Writeback: f.wb, Clock: f.clk})

	items, err := mon.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("recovered %d, want 3 (no loss)", len(items))
	}
	// Two finalized (done, crash) → two writeback reports.
	gotKinds := map[OutcomeKind]int{}
	for _, k := range f.wb.kinds() {
		gotKinds[k]++
	}
	if gotKinds[OutcomeSucceeded] != 1 || gotKinds[OutcomeCrashed] != 1 {
		t.Errorf("writeback kinds = %v, want 1 succeeded + 1 crashed", f.wb.kinds())
	}
	// Alive one re-adopted into the pool (slot accounting restored).
	if !poolHas(f.pool, "e-alive") {
		t.Error("alive orphan must be re-adopted into the pool")
	}
	// Delayed teardown: terminal success (e-done) is RETAINED + marked finalized until
	// reaped; the retryable crash (e-crash) is retained with NO marker (for re-launch).
	if !dirExists(t, f.fx, "e-done") {
		t.Error("e-done (succeeded) dir should be retained until reaped")
	}
	if !dirExists(t, f.fx, "e-crash") {
		t.Error("e-crash (retryable) dir should be retained")
	}
	// The reaper removes ONLY the finalized terminal (e-done), NEVER the retryable
	// crash (e-crash has no marker → it is left for the re-launch).
	if n, err := f.mon.ReapFinalized(context.Background(), 0, 0); err != nil || n != 1 {
		t.Fatalf("ReapFinalized = %d,%v, want 1,nil (only e-done)", n, err)
	}
	if dirExists(t, f.fx, "e-done") {
		t.Error("reap must remove the finalized e-done dir")
	}
	if !dirExists(t, f.fx, "e-crash") {
		t.Error("reap must NOT remove the retryable-crash e-crash dir (no finalized marker)")
	}
}

func TestMonitor_Recover_NoReconciler(t *testing.T) {
	mon, _ := NewMonitor(MonitorConfig{Exchange: mustExchange(t)})
	if _, err := mon.Recover(context.Background()); err == nil {
		t.Error("Recover without a reconciler must error")
	}
}

func TestMonitor_Finalize_RunningIsNoop(t *testing.T) {
	f := newMonitorFixture(t, 1)
	if err := f.mon.Finalize(context.Background(), Completion{ExecutorID: "x", Kind: OutcomeRunning}); err != nil {
		t.Fatalf("Finalize running: %v", err)
	}
	if len(f.wb.kinds()) != 0 {
		t.Error("running must not be reported to writeback")
	}
}

func TestMonitor_Finalize_WritebackErrorRetainsDir(t *testing.T) {
	f := newMonitorFixture(t, 1)
	id := "e-keep"
	mustProvision(t, f.fx, id)
	if err := f.pool.Adopt(id); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	f.wb.err = errors.New("center down")
	c := Completion{ExecutorID: id, Kind: OutcomeSucceeded}
	if err := f.mon.Finalize(context.Background(), c); err == nil {
		t.Fatal("writeback failure must surface")
	}
	if !dirExists(t, f.fx, id) {
		t.Error("a failed writeback must leave the dir intact (no silent loss)")
	}
	if f.pool.Active() != 1 {
		t.Errorf("slot must NOT be released when writeback failed, Active = %d", f.pool.Active())
	}
}

func TestMonitor_Finalize_NoWritebackStillTearsDown(t *testing.T) {
	// A Monitor without a Writeback (dry run) still releases + cleans up.
	root := t.TempDir()
	layout, _ := NewLayout(root)
	fx, _ := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	wt, _ := NewWorktreeProvisioner(root, &fakeGitRunner{})
	mon, _ := NewMonitor(MonitorConfig{Exchange: fx, Worktrees: wt})
	id := "e1"
	mustProvision(t, fx, id)
	if err := mon.Finalize(context.Background(), Completion{ExecutorID: id, Kind: OutcomeFailed, Error: &ErrorDetail{Kind: "k", Message: "m"}}); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	// Delayed teardown applies even with no writeback: retained + marked, then reaped.
	assertDelayedTeardown(t, mon, fx, id)
}

func TestNewMonitor_Validation(t *testing.T) {
	if _, err := NewMonitor(MonitorConfig{}); err == nil {
		t.Error("nil exchange must error")
	}
	mon, err := NewMonitor(MonitorConfig{Exchange: mustExchange(t)})
	if err != nil || mon == nil {
		t.Fatalf("valid minimal config: %v", err)
	}
}

// ---- monitor test helpers ----

func mustExchange(t *testing.T) *FileExchange {
	t.Helper()
	layout, _ := NewLayout(t.TempDir())
	fx, err := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	return fx
}

func mustWorktrees(t *testing.T, root string) *WorktreeProvisioner {
	t.Helper()
	wt, err := NewWorktreeProvisioner(root, &fakeGitRunner{})
	if err != nil {
		t.Fatalf("NewWorktreeProvisioner: %v", err)
	}
	return wt
}

func poolHas(p *Pool, id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.active[id]
	return ok
}

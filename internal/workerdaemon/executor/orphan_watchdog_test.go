package executor

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// orphanFixture builds a Monitor whose orphan liveness probe and watchdog-kill
// signaler are injected, so CheckOrphan's stall→kill→finalize path is exercised
// without a real process or a real signal.
type orphanFixture struct {
	fx   *FileExchange
	mon  *Monitor
	wb   *fakeWriteback
	clk  *clock.FakeClock
	sigs *recordingSignaler
	live *mutableLiveness
	base time.Time
}

// mutableLiveness is a fakeLiveness whose verdict can be flipped per pid mid-test.
type mutableLiveness struct{ alive map[int]bool }

func (m *mutableLiveness) Alive(pid int) bool { return m.alive[pid] }

func newOrphanFixture(t *testing.T, stall time.Duration) *orphanFixture {
	t.Helper()
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	base := time.Unix(1700000000, 0)
	clk := clock.NewFakeClock(base)
	fx, err := NewFileExchange(layout, clk)
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	wd := NewWatchdog(WatchdogConfig{StallTimeout: stall, Clock: clk, Sleep: func(time.Duration) {}})
	wb := &fakeWriteback{}
	sigs := &recordingSignaler{}
	live := &mutableLiveness{alive: map[int]bool{}}
	mon, err := NewMonitor(MonitorConfig{
		Exchange: fx, Watchdog: wd, Writeback: wb, Clock: clk, Liveness: live, signal: sigs.signal,
	})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	return &orphanFixture{fx: fx, mon: mon, wb: wb, clk: clk, sigs: sigs, live: live, base: base}
}

func (f *orphanFixture) provision(t *testing.T, id string) {
	t.Helper()
	if _, err := f.fx.Provision(id); err != nil {
		t.Fatalf("Provision %s: %v", id, err)
	}
}

// An alive orphan with fresh progress is simply still running: no kill, no
// finalize, not done.
func TestCheckOrphan_AliveFresh_Running(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-aaa111", 4242
	f.provision(t, id)
	f.live.alive[pid] = true
	if err := f.fx.WriteStatus(Status{ExecutorID: id, State: StateRunning, Model: "m", StartedAt: f.base, LastProgressAt: f.base}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	f.clk.Set(f.base.Add(30 * time.Second)) // within the 1-min stall window

	c, done, err := f.mon.CheckOrphan(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphan: %v", err)
	}
	if done || c.Kind != OutcomeRunning {
		t.Errorf("got (kind=%s done=%v), want (running, false)", c.Kind, done)
	}
	if len(f.sigs.pids) != 0 {
		t.Errorf("a fresh orphan must not be signalled; got %v", f.sigs.sigs)
	}
	if len(f.wb.reports) != 0 {
		t.Errorf("a running orphan must not be finalized/reported; got %v", f.wb.kinds())
	}
}

// An alive orphan whose progress has stalled is graceful-killed (SIGTERM then
// SIGKILL) and finalized as a definite, NON-retryable failure (design §9), with
// its dir torn down.
func TestCheckOrphan_AliveStalled_KilledAndFailed(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-bbb222", 5252
	f.provision(t, id)
	f.live.alive[pid] = true
	if err := f.fx.WriteStatus(Status{ExecutorID: id, State: StateRunning, Model: "m", StartedAt: f.base, LastProgressAt: f.base}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	f.clk.Set(f.base.Add(2 * time.Minute)) // past the 1-min stall window

	c, done, err := f.mon.CheckOrphan(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphan: %v", err)
	}
	if !done || c.Kind != OutcomeFailed {
		t.Fatalf("got (kind=%s done=%v), want (failed, true)", c.Kind, done)
	}
	if c.Retryable {
		t.Error("a stalled-killed executor must be non-retryable (no auto re-queue of a hang)")
	}
	if c.Error == nil || c.Error.Kind != "stalled" {
		t.Errorf("want stalled error detail, got %+v", c.Error)
	}
	// SIGTERM then SIGKILL to the orphan's process group.
	if len(f.sigs.sigs) != 2 || f.sigs.sigs[0] != syscall.SIGTERM || f.sigs.sigs[1] != syscall.SIGKILL {
		t.Errorf("kill sequence = %v, want [SIGTERM SIGKILL]", f.sigs.sigs)
	}
	if got := f.wb.kinds(); len(got) != 1 || got[0] != OutcomeFailed {
		t.Errorf("writeback = %v, want [failed]", got)
	}
	if dirExists(t, f.fx, id) {
		t.Error("terminal failure must tear down the executor dir")
	}
}

// A gone orphan that left a success output.json is finalized as Succeeded (it
// finished during our downtime) — recovered, not lost, dir torn down.
func TestCheckOrphan_GoneWithSuccess_Succeeded(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-ccc333", 6363
	f.provision(t, id)
	f.live.alive[pid] = false // process already gone
	if err := f.fx.WriteOutput(Output{ExecutorID: id, Success: true, Result: "done", FinishedAt: f.base}); err != nil {
		t.Fatalf("WriteOutput: %v", err)
	}
	if err := f.fx.WriteStatus(Status{ExecutorID: id, State: StateDone, Model: "m", StartedAt: f.base, LastProgressAt: f.base}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	c, done, err := f.mon.CheckOrphan(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphan: %v", err)
	}
	if !done || c.Kind != OutcomeSucceeded {
		t.Fatalf("got (kind=%s done=%v), want (succeeded, true)", c.Kind, done)
	}
	if len(f.sigs.sigs) != 0 {
		t.Errorf("a gone orphan must not be signalled; got %v", f.sigs.sigs)
	}
	if got := f.wb.kinds(); len(got) != 1 || got[0] != OutcomeSucceeded {
		t.Errorf("writeback = %v, want [succeeded]", got)
	}
	if dirExists(t, f.fx, id) {
		t.Error("succeeded orphan dir must be torn down")
	}
}

// A gone orphan whose status was still "running" and left no output is the core §9
// case 3: a crash the orchestrator MAY retry → dir RETAINED for re-launch.
func TestCheckOrphan_GoneWhileRunning_CrashedRetained(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-ddd444", 7474
	f.provision(t, id)
	f.live.alive[pid] = false
	if err := f.fx.WriteStatus(Status{ExecutorID: id, State: StateRunning, Model: "m", StartedAt: f.base, LastProgressAt: f.base}); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	c, done, err := f.mon.CheckOrphan(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphan: %v", err)
	}
	if !done || c.Kind != OutcomeCrashed || !c.Retryable {
		t.Fatalf("got (kind=%s retryable=%v done=%v), want (crashed, true, true)", c.Kind, c.Retryable, done)
	}
	if !dirExists(t, f.fx, id) {
		t.Error("a retryable crash must RETAIN the dir for re-launch")
	}
}

func TestCheckOrphan_InvalidPID(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	if _, _, err := f.mon.CheckOrphan(context.Background(), "exec-eee555", 0); err == nil {
		t.Fatal("expected error for non-positive pid")
	}
}

// An alive orphan with no status file yet (just launched before the crash) is not
// judged stalled — we cannot prove staleness without a timestamp.
func TestCheckOrphan_AliveNoStatus_Running(t *testing.T) {
	f := newOrphanFixture(t, time.Minute)
	id, pid := "exec-fff666", 8585
	f.provision(t, id)
	f.live.alive[pid] = true
	f.clk.Set(f.base.Add(time.Hour))

	c, done, err := f.mon.CheckOrphan(context.Background(), id, pid)
	if err != nil {
		t.Fatalf("CheckOrphan: %v", err)
	}
	if done || c.Kind != OutcomeRunning {
		t.Errorf("got (kind=%s done=%v), want (running, false) — no status means not provably stalled", c.Kind, done)
	}
	if len(f.sigs.sigs) != 0 {
		t.Errorf("must not kill an orphan with no status yet; got %v", f.sigs.sigs)
	}
}

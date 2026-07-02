package executor

import (
	"context"
	"sync"
	"testing"
	"time"
)

// T758: the Monitor is the single emit point for executor stop + progress activity
// (via the ActivityObserver port). These tests drive each terminal path through the
// real Monitor and assert the emitted StopEvent — one per finalize — carries the
// right classification + the executor_id/task_ref prefix, plus that progress is
// change-only throttled.

// recordingObserver captures every stop/progress observation.
type recordingObserver struct {
	mu       sync.Mutex
	stops    []StopEvent
	progress []ProgressEvent
}

func (o *recordingObserver) ExecutorStopped(ev StopEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stops = append(o.stops, ev)
}

func (o *recordingObserver) ExecutorProgress(ev ProgressEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.progress = append(o.progress, ev)
}

func (o *recordingObserver) lastStop(t *testing.T) StopEvent {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.stops) != 1 {
		t.Fatalf("want exactly 1 stop event, got %d: %+v", len(o.stops), o.stops)
	}
	return o.stops[0]
}

func inputWithTaskRef(id, taskRef string) Input {
	return Input{
		ExecutorID: id,
		Goal:       Goal{Title: "t"},
		Model:      "m",
		CreatedAt:  time.Unix(1700000000, 0),
		Source:     SourceRefs{TaskRef: taskRef},
	}
}

// attachObserver installs a recordingObserver on the fixture's Monitor.
func attachObserver(f *monitorFixture) *recordingObserver {
	obs := &recordingObserver{}
	f.mon.obs = obs
	return obs
}

func TestMonitor_Emit_Stop_Succeeded(t *testing.T) {
	f := newMonitorFixture(t, 3)
	obs := attachObserver(f)
	id := "e-ok"
	mustWriteInput(t, f.fx, inputWithTaskRef(id, "T758"))
	mustWriteStatus(t, f.fx, *doneStatus(id))
	mustWriteOutput(t, f.fx, *okOutput(id))
	if err := f.pool.Adopt(id); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	h := startRealProc(t, id, 0, f.sigs.signal)

	if _, err := f.mon.AwaitCompletion(context.Background(), h); err != nil {
		t.Fatalf("AwaitCompletion: %v", err)
	}
	ev := obs.lastStop(t)
	if ev.Outcome != OutcomeSucceeded || ev.Reason != "" || ev.Recovered {
		t.Fatalf("stop = %+v, want succeeded/no-reason/not-recovered", ev)
	}
	if ev.ExecutorID != id || ev.TaskRef != "T758" {
		t.Fatalf("stop prefix = executor=%q task=%q, want %q/T758", ev.ExecutorID, ev.TaskRef, id)
	}
}

func TestMonitor_Emit_Stop_FailedNonzeroExit(t *testing.T) {
	f := newMonitorFixture(t, 3)
	obs := attachObserver(f)
	id := "e-fail"
	mustWriteInput(t, f.fx, inputWithTaskRef(id, "T758"))
	mustWriteStatus(t, f.fx, runningStatusAt(id, f.clk.Now())) // still "running", no error, no output
	h := startRealProc(t, id, 3, f.sigs.signal)

	if _, err := f.mon.AwaitCompletion(context.Background(), h); err != nil {
		t.Fatalf("AwaitCompletion: %v", err)
	}
	ev := obs.lastStop(t)
	if ev.Outcome != OutcomeFailed || ev.Reason != "nonzero_exit" || ev.Recovered {
		t.Fatalf("stop = %+v, want failed/nonzero_exit/not-recovered", ev)
	}
	if ev.TaskRef != "T758" {
		t.Fatalf("task_ref = %q, want T758", ev.TaskRef)
	}
}

// Sweep must mark a stall-killed this-process executor so its eventual stop is
// labeled "stalled" (the reaped kill otherwise classifies as nonzero_exit).
func TestMonitor_Sweep_MarksStalled(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-stall"
	h, err := f.pool.Launch(context.Background(), LaunchSpec{Input: inputWithTaskRef(id, "T758")})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	_ = h
	// A running status whose progress is now well past the 1m stall timeout.
	mustWriteStatus(t, f.fx, runningStatusAt(id, f.clk.Now()))
	f.clk.Advance(2 * time.Minute)

	killed, err := f.mon.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(killed) != 1 || killed[0] != id {
		t.Fatalf("killed = %v, want [%s]", killed, id)
	}
	if !f.mon.takeStalled(id) {
		t.Fatalf("Sweep must mark %s as stall-killed", id)
	}
}

// Regression (T760): Sweep must mark the stall BEFORE GracefulKill, so a drain
// goroutine that reaps the executor DURING the SIGTERM grace window (a well-behaved
// executor flushing status=failed then exiting) still classifies the stop as
// "stalled", not a generic nonzero_exit. This test injects the reap's classification
// at the exact race point — the watchdog's grace-window sleep, which runs mid-kill.
// It FAILS on the pre-fix ordering (mark after GracefulKill → mark absent mid-window)
// and PASSES on the fix (mark before GracefulKill).
func TestMonitor_Sweep_MarksStalledBeforeGracefulKill(t *testing.T) {
	f := newMonitorFixture(t, 3)
	id := "e-race"
	if _, err := f.pool.Launch(context.Background(), LaunchSpec{Input: inputWithTaskRef(id, "T758")}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	mustWriteStatus(t, f.fx, runningStatusAt(id, f.clk.Now()))
	f.clk.Advance(2 * time.Minute) // past the 1m stall timeout

	// A watchdog whose grace-window sleep runs the SAME classification the reap's
	// Finalize→emitStop would (takeStalled): this models the drain goroutine winning
	// the race and finalizing the executor while GracefulKill is still mid-kill.
	var reapReason string
	f.mon.watchdog = NewWatchdog(WatchdogConfig{
		StallTimeout: time.Minute,
		Clock:        f.clk,
		Sleep: func(time.Duration) {
			if f.mon.takeStalled(id) {
				reapReason = "stalled"
			} else {
				reapReason = "nonzero_exit"
			}
		},
	})

	killed, err := f.mon.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(killed) != 1 || killed[0] != id {
		t.Fatalf("killed = %v, want [%s]", killed, id)
	}
	if reapReason != "stalled" {
		t.Fatalf("a reap racing the grace window classified %q, want stalled — "+
			"Sweep must markStalled BEFORE GracefulKill", reapReason)
	}
}

// A stall mark overrides the reaped completion's reason to "stalled" at Finalize.
func TestMonitor_Emit_Stop_StalledOverride(t *testing.T) {
	f := newMonitorFixture(t, 3)
	obs := attachObserver(f)
	id := "e-stall2"
	mustWriteInput(t, f.fx, inputWithTaskRef(id, "T758"))
	mustWriteStatus(t, f.fx, runningStatusAt(id, f.clk.Now()))
	f.mon.markStalled(id) // as Sweep would have, before the process is reaped
	h := startRealProc(t, id, 3, f.sigs.signal)

	if _, err := f.mon.AwaitCompletion(context.Background(), h); err != nil {
		t.Fatalf("AwaitCompletion: %v", err)
	}
	ev := obs.lastStop(t)
	if ev.Outcome != OutcomeFailed || ev.Reason != "stalled" {
		t.Fatalf("stop = %+v, want failed/stalled", ev)
	}
}

// Orphan cleanup (process gone) finalizes from durable files and is flagged Recovered.
func TestMonitor_Emit_Stop_OrphanRecovered(t *testing.T) {
	f := newMonitorFixture(t, 3)
	obs := attachObserver(f)
	f.mon.live = fakeLiveness{alive: map[int]bool{}} // pid not alive → gone
	id := "e-orphan"
	mustWriteInput(t, f.fx, inputWithTaskRef(id, "T758"))
	mustWriteStatus(t, f.fx, *doneStatus(id))
	mustWriteOutput(t, f.fx, *okOutput(id))

	_, done, err := f.mon.CheckOrphan(context.Background(), id, 42)
	if err != nil || !done {
		t.Fatalf("CheckOrphan done=%v err=%v, want done/nil", done, err)
	}
	ev := obs.lastStop(t)
	if !ev.Recovered {
		t.Fatalf("orphan stop must be Recovered: %+v", ev)
	}
	if ev.Outcome != OutcomeSucceeded || ev.TaskRef != "T758" {
		t.Fatalf("stop = %+v, want succeeded/T758", ev)
	}
}

// An adopted-orphan stall → watchdog kill by pid, finalized as failed/stalled/recovered.
func TestMonitor_Emit_Stop_OrphanStalled(t *testing.T) {
	f := newMonitorFixture(t, 3)
	obs := attachObserver(f)
	f.mon.live = fakeLiveness{alive: map[int]bool{42: true}} // still alive
	f.mon.killSig = f.sigs.signal                            // record the kill, don't signal a real pid
	id := "e-orphan-stall"
	mustWriteInput(t, f.fx, inputWithTaskRef(id, "T758"))
	mustWriteStatus(t, f.fx, runningStatusAt(id, f.clk.Now()))
	f.clk.Advance(2 * time.Minute) // past the 1m stall timeout

	_, done, err := f.mon.CheckOrphan(context.Background(), id, 42)
	if err != nil || !done {
		t.Fatalf("CheckOrphan done=%v err=%v, want done/nil", done, err)
	}
	ev := obs.lastStop(t)
	if ev.Outcome != OutcomeFailed || ev.Reason != "stalled" || !ev.Recovered {
		t.Fatalf("stop = %+v, want failed/stalled/recovered", ev)
	}
}

// Progress is emitted once per advance of status.last_progress_at (change-only).
func TestMonitor_SampleProgress_ChangeOnly(t *testing.T) {
	f := newMonitorFixture(t, 3)
	obs := attachObserver(f)
	id := "e-prog"
	if _, err := f.pool.Launch(context.Background(), LaunchSpec{Input: inputWithTaskRef(id, "T758")}); err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t1 := f.clk.Now()
	mustWriteStatus(t, f.fx, runningStatusAt(id, t1))

	f.mon.SampleProgress() // first sample → emit
	f.mon.SampleProgress() // no advance → no emit

	t2 := t1.Add(time.Minute)
	st := runningStatusAt(id, t2)
	st.Summary = "phase 2"
	mustWriteStatus(t, f.fx, st)
	f.mon.SampleProgress() // advanced → emit

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.progress) != 2 {
		t.Fatalf("want 2 progress events (one per advance), got %d: %+v", len(obs.progress), obs.progress)
	}
	if obs.progress[0].TaskRef != "T758" || obs.progress[0].State != "running" {
		t.Fatalf("progress[0] = %+v, want task=T758 state=running", obs.progress[0])
	}
	if obs.progress[1].Summary != "phase 2" {
		t.Fatalf("progress[1] summary = %q, want 'phase 2'", obs.progress[1].Summary)
	}
}

package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

func TestWatchdog_GracefulKill_NilContext(t *testing.T) {
	rs := &recordingSignaler{}
	w := NewWatchdog(WatchdogConfig{Sleep: func(time.Duration) {}})
	h := newSignalHandle("e1", 9, rs.signal)
	if err := w.GracefulKill(context.TODO(), h); err != nil { //nolint:staticcheck // exercise nil-ctx grace path
		t.Fatalf("GracefulKill: %v", err)
	}
	// Directly exercise the ctx==nil branch of sleepCtx.
	w.sleepCtx(nil)
}

func TestTracker_BadIDPaths(t *testing.T) {
	_, tr := newRecoveryFixture(t)
	if _, err := tr.Read("bad/id"); err == nil {
		t.Error("Read with a traversal id must error")
	}
	if err := tr.Write(Record{ExecutorID: "bad/id", PID: 1, SpawnedAt: time.Unix(1700000000, 0)}); err == nil {
		t.Error("Write with a traversal id must error")
	}
}

// TestMonitor_Recover_AdoptWithNilPool ensures re-adoption degrades gracefully
// when the Monitor has no pool (adopt is best-effort, never fatal to recovery).
func TestMonitor_Recover_AdoptWithNilPool(t *testing.T) {
	fx, tr := newRecoveryFixture(t)
	mustProvision(t, fx, "e-alive")
	mustWriteStatus(t, fx, runningStatusAt("e-alive", time.Unix(1700000000, 0)))
	mustWriteRecord(t, tr, "e-alive", 1001)
	rc, _ := NewReconciler(fx, tr, fakeLiveness{alive: map[int]bool{1001: true}})
	mon, _ := NewMonitor(MonitorConfig{Exchange: fx, Reconciler: rc}) // no pool
	items, err := mon.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(items) != 1 || items[0].Completion.Kind != OutcomeRunning {
		t.Fatalf("want 1 running item, got %+v", items)
	}
}

// TestMonitor_Recover_FinalizeErrorSurfaces ensures a writeback failure during
// recovery aborts with the error (the orphan is not silently lost).
func TestMonitor_Recover_FinalizeErrorSurfaces(t *testing.T) {
	fx, tr := newRecoveryFixture(t)
	mustProvision(t, fx, "e-done")
	mustWriteStatus(t, fx, *doneStatus("e-done"))
	mustWriteOutput(t, fx, *okOutput("e-done"))
	mustWriteRecord(t, tr, "e-done", 1002)
	rc, _ := NewReconciler(fx, tr, fakeLiveness{}) // 1002 not alive → terminal
	wb := &fakeWriteback{err: errors.New("center down")}
	mon, _ := NewMonitor(MonitorConfig{Exchange: fx, Reconciler: rc, Writeback: wb, Clock: clock.NewFakeClock(time.Unix(1700000000, 0))})
	if _, err := mon.Recover(context.Background()); err == nil {
		t.Fatal("a writeback failure during recovery must surface")
	}
}

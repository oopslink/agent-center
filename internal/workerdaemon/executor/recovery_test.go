package executor

import (
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// fakeLiveness reports liveness from a fixed set of "alive" pids.
type fakeLiveness struct{ alive map[int]bool }

func (f fakeLiveness) Alive(pid int) bool { return f.alive[pid] }

func newRecoveryFixture(t *testing.T) (*FileExchange, *Tracker) {
	t.Helper()
	root := t.TempDir()
	layout, err := NewLayout(root)
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	fx, err := NewFileExchange(layout, clock.NewFakeClock(time.Unix(1700000000, 0)))
	if err != nil {
		t.Fatalf("NewFileExchange: %v", err)
	}
	tr, err := NewTracker(layout)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return fx, tr
}

func TestRecord_Validate(t *testing.T) {
	now := time.Unix(1700000000, 0)
	cases := []struct {
		name string
		rec  Record
		ok   bool
	}{
		{"valid", Record{ExecutorID: "e1", PID: 10, SpawnedAt: now}, true},
		{"bad id", Record{ExecutorID: "a/b", PID: 10, SpawnedAt: now}, false},
		{"no pid", Record{ExecutorID: "e1", PID: 0, SpawnedAt: now}, false},
		{"no spawned_at", Record{ExecutorID: "e1", PID: 10}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rec.Validate()
			if tc.ok && err != nil {
				t.Errorf("want valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("want invalid")
			}
		})
	}
}

func TestTracker_WriteReadRoundTrip(t *testing.T) {
	_, tr := newRecoveryFixture(t)
	rec := Record{ExecutorID: "e1", PID: 555, SpawnedAt: time.Unix(1700000000, 0).UTC(), BaseRef: "dev", RunnerCmd: []string{"claude", "-p"}}
	if err := tr.Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := tr.Read("e1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.PID != 555 || got.BaseRef != "dev" || len(got.RunnerCmd) != 2 {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestTracker_WriteInvalidRejected(t *testing.T) {
	_, tr := newRecoveryFixture(t)
	if err := tr.Write(Record{ExecutorID: "e1"}); err == nil {
		t.Error("invalid record must be rejected")
	}
}

func TestTracker_ReadMissingIsNotExist(t *testing.T) {
	_, tr := newRecoveryFixture(t)
	_, err := tr.Read("nope")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing record err = %v, want os.ErrNotExist", err)
	}
}

func TestTracker_ReadCorruptSurfaces(t *testing.T) {
	fx, tr := newRecoveryFixture(t)
	if _, err := fx.Provision("e1"); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	p, _ := tr.path("e1")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if _, err := tr.Read("e1"); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("corrupt record should surface a non-NotExist error, got %v", err)
	}
}

func TestSignalLiveness_DeadAndSelf(t *testing.T) {
	l := SignalLiveness{}
	if l.Alive(0) || l.Alive(-1) {
		t.Error("non-positive pid must be dead")
	}
	if !l.Alive(os.Getpid()) {
		t.Error("the test process itself must read as alive")
	}
	// An almost-certainly-unused high pid should be dead (ESRCH).
	if l.Alive(2147480000) {
		t.Error("an unused high pid should read as dead")
	}
}

// TestReconciler_RebuildsEveryDirOnce is the §12 acceptance: scan rebuilds each
// executor exactly once (no loss), classified by durable files + liveness, with
// no side effects (no duplication).
func TestReconciler_RebuildsEveryDirOnce(t *testing.T) {
	fx, tr := newRecoveryFixture(t)

	// e-alive: tracked + still running → Running (re-adopt).
	mustProvision(t, fx, "e-alive")
	mustWriteStatus(t, fx, runningStatusAt("e-alive", time.Unix(1700000000, 0)))
	mustWriteRecord(t, tr, "e-alive", 1001)

	// e-done: finished successfully while we were down → Succeeded.
	mustProvision(t, fx, "e-done")
	mustWriteStatus(t, fx, *doneStatus("e-done"))
	mustWriteOutput(t, fx, *okOutput("e-done"))
	mustWriteRecord(t, tr, "e-done", 1002) // pid not alive

	// e-crash: tracked, status running, process gone → Crashed (the §9 core case).
	mustProvision(t, fx, "e-crash")
	mustWriteStatus(t, fx, runningStatusAt("e-crash", time.Unix(1700000000, 0)))
	mustWriteRecord(t, tr, "e-crash", 1003) // pid not alive

	// e-untracked: dir + status running but NO orchestrator record (we never knew
	// its pid) → treated as gone → Crashed. Surfaces, never dropped.
	mustProvision(t, fx, "e-untracked")
	mustWriteStatus(t, fx, runningStatusAt("e-untracked", time.Unix(1700000000, 0)))

	live := fakeLiveness{alive: map[int]bool{1001: true}}
	rc, err := NewReconciler(fx, tr, live)
	if err != nil {
		t.Fatalf("NewReconciler: %v", err)
	}
	items, err := rc.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got := map[string]OutcomeKind{}
	for _, it := range items {
		if _, dup := got[it.ExecutorID]; dup {
			t.Fatalf("executor %s reconciled twice (duplication)", it.ExecutorID)
		}
		got[it.ExecutorID] = it.Completion.Kind
	}
	want := map[string]OutcomeKind{
		"e-alive":     OutcomeRunning,
		"e-done":      OutcomeSucceeded,
		"e-crash":     OutcomeCrashed,
		"e-untracked": OutcomeCrashed,
	}
	if len(got) != len(want) {
		ids := make([]string, 0, len(got))
		for id := range got {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		t.Fatalf("rebuilt %d executors %v, want %d (no loss)", len(got), ids, len(want))
	}
	for id, wantKind := range want {
		if got[id] != wantKind {
			t.Errorf("%s classified %q, want %q", id, got[id], wantKind)
		}
	}

	// The alive one must carry its Record (pid) so the orchestrator can re-adopt.
	for _, it := range items {
		if it.ExecutorID == "e-alive" {
			if it.Record == nil || it.Record.PID != 1001 {
				t.Errorf("e-alive must carry its tracking record, got %+v", it.Record)
			}
		}
		if it.ExecutorID == "e-untracked" && it.Record != nil {
			t.Errorf("e-untracked must have nil record, got %+v", it.Record)
		}
	}
}

func TestReconciler_EmptyDir(t *testing.T) {
	fx, tr := newRecoveryFixture(t)
	rc, _ := NewReconciler(fx, tr, fakeLiveness{})
	items, err := rc.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("empty executors dir → 0 items, got %d", len(items))
	}
}

func TestReconciler_Validation(t *testing.T) {
	fx, tr := newRecoveryFixture(t)
	if _, err := NewReconciler(nil, tr, nil); err == nil {
		t.Error("nil exchange must error")
	}
	if _, err := NewReconciler(fx, nil, nil); err == nil {
		t.Error("nil tracker must error")
	}
	if rc, err := NewReconciler(fx, tr, nil); err != nil || rc == nil {
		t.Errorf("nil probe should default, got %v", err)
	}
}

func TestNewTracker_NilLayout(t *testing.T) {
	if _, err := NewTracker(nil); err == nil {
		t.Error("nil layout must error")
	}
}

// ---- recovery test helpers ----

func runningStatusAt(id string, t time.Time) Status {
	return Status{ExecutorID: id, State: StateRunning, Model: "m", StartedAt: t, LastProgressAt: t}
}

func mustProvision(t *testing.T, fx *FileExchange, id string) {
	t.Helper()
	if _, err := fx.Provision(id); err != nil {
		t.Fatalf("Provision %s: %v", id, err)
	}
}

func mustWriteStatus(t *testing.T, fx *FileExchange, st Status) {
	t.Helper()
	if err := fx.WriteStatus(st); err != nil {
		t.Fatalf("WriteStatus %s: %v", st.ExecutorID, err)
	}
}

func mustWriteOutput(t *testing.T, fx *FileExchange, out Output) {
	t.Helper()
	if err := fx.WriteOutput(out); err != nil {
		t.Fatalf("WriteOutput %s: %v", out.ExecutorID, err)
	}
}

func mustWriteRecord(t *testing.T, tr *Tracker, id string, pid int) {
	t.Helper()
	if err := tr.Write(Record{ExecutorID: id, PID: pid, SpawnedAt: time.Unix(1700000000, 0)}); err != nil {
		t.Fatalf("Write record %s: %v", id, err)
	}
}

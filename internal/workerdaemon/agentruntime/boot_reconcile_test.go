package agentruntime

import (
	"testing"

	"github.com/oopslink/agent-center/internal/supervisormanager"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

func TestClassifyExecutorReconcile(t *testing.T) {
	inflight := map[string]bool{"task-1": true}
	cases := []struct {
		name    string
		taskRef string
		alive   bool
		want    reconcileAction
	}{
		{"inflight+alive → adopt", "task-1", true, reconcileAdopt},
		{"inflight+dead → recover", "task-1", false, reconcileRecover},
		{"not-inflight → cancel", "task-9", true, reconcileCancel},
		{"not-inflight+dead → cancel", "task-9", false, reconcileCancel},
		{"no task ref → cancel", "", true, reconcileCancel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyExecutorReconcile(tc.taskRef, tc.alive, inflight); got != tc.want {
				t.Errorf("classify(%q, %v) = %v, want %v", tc.taskRef, tc.alive, got, tc.want)
			}
		})
	}
}

// mkItem builds a Reconciled with a task ref, liveness, and (optionally) a Record.
func mkItem(id, taskRef string, alive bool, rec *executor.Record) executor.Reconciled {
	kind := executor.OutcomeCrashed
	if alive {
		kind = executor.OutcomeRunning
	}
	return executor.Reconciled{
		ExecutorID: id,
		Snapshot:   executor.Snapshot{Input: &executor.Input{Source: executor.SourceRefs{TaskRef: taskRef}}},
		Record:     rec,
		Completion: executor.Completion{Kind: kind},
	}
}

func TestPlanExecutorReconcile_Branches(t *testing.T) {
	planner, err := orchestrator.NewRecoveryPlanner(mustLayout(t), nil)
	if err != nil {
		t.Fatalf("NewRecoveryPlanner: %v", err)
	}
	inflight := map[string]bool{"task-adopt": true, "task-recover": true}
	items := []executor.Reconciled{
		mkItem("e-adopt", "task-adopt", true, &executor.Record{ExecutorID: "e-adopt", PID: 4242}),
		mkItem("e-recover", "task-recover", false, &executor.Record{ExecutorID: "e-recover", PID: 10, RunnerCmd: []string{"claude", "-p"}}),
		mkItem("e-cancel", "task-gone", true, &executor.Record{ExecutorID: "e-cancel", PID: 77}),
	}

	got := planExecutorReconcile(items, inflight, true, planner)
	if len(got) != 3 {
		t.Fatalf("want 3 decisions, got %d", len(got))
	}
	byID := map[string]execReconcileDecision{}
	for _, d := range got {
		byID[d.ExecutorID] = d
	}
	if d := byID["e-adopt"]; d.Action != reconcileAdopt || d.PID != 4242 {
		t.Errorf("adopt decision wrong: %+v", d)
	}
	if d := byID["e-recover"]; d.Action != reconcileRecover || d.Plan.Action == 0 {
		t.Errorf("recover decision should carry a ladder plan: %+v", d)
	}
	if d := byID["e-cancel"]; d.Action != reconcileCancel {
		t.Errorf("cancel decision wrong: %+v", d)
	}
}

func TestPlanExecutorReconcile_DegradedNeverCancels(t *testing.T) {
	// haveInflight=false (center query failed): the alive executor is adopted and the
	// dead one is left alone — NOTHING is cancelled, even though no inflight set backs it.
	items := []executor.Reconciled{
		mkItem("e-alive", "task-x", true, &executor.Record{ExecutorID: "e-alive", PID: 9}),
		mkItem("e-dead", "task-y", false, &executor.Record{ExecutorID: "e-dead", PID: 8}),
	}
	got := planExecutorReconcile(items, nil, false, nil)
	for _, d := range got {
		if d.Action == reconcileCancel {
			t.Errorf("degraded reconcile must never cancel: %+v", d)
		}
	}
	if got[0].Action != reconcileAdopt {
		t.Errorf("degraded: alive executor should still be adopted, got %v", got[0].Action)
	}
}

func TestTaskRefOf_NilInput(t *testing.T) {
	if ref := taskRefOf(executor.Snapshot{}); ref != "" {
		t.Errorf("nil input should yield empty task ref, got %q", ref)
	}
}

func TestDecideBootAction(t *testing.T) {
	running := &centerRecord{DesiredLifecycle: "running"}
	runningActive := &centerRecord{DesiredLifecycle: "running", HasActive: true}
	stopped := &centerRecord{DesiredLifecycle: "stopped"}

	cases := []struct {
		name      string
		probe     supervisormanager.ProbeState
		rec       *centerRecord
		wantKind  bootActionKind
		wantNudge bool
	}{
		{"reattachable+running → reattach", supervisormanager.Reattachable, running, bootReattach, false},
		{"reattachable+stopped → stop+reap", supervisormanager.Reattachable, stopped, bootStopReap, false},
		{"reattachable+orphan → stop+reap", supervisormanager.Reattachable, nil, bootStopReap, false},
		{"unavailable+running idle → reap+relaunch no nudge", supervisormanager.Unavailable, running, bootReapRelaunch, false},
		{"unavailable+running active → reap+relaunch nudge", supervisormanager.Unavailable, runningActive, bootReapRelaunch, true},
		{"unavailable+stopped → reap only", supervisormanager.Unavailable, stopped, bootReapOnly, false},
		{"unavailable+orphan → reap only", supervisormanager.Unavailable, nil, bootReapOnly, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideBootAction(tc.probe, tc.rec)
			if got.Kind != tc.wantKind || got.Nudge != tc.wantNudge {
				t.Errorf("decideBootAction = %+v, want kind=%v nudge=%v", got, tc.wantKind, tc.wantNudge)
			}
		})
	}
}

func TestRecoveredOnceGuard(t *testing.T) {
	r := NewLocalRuntime(LocalRuntimeConfig{AgentID: "a"}, nil)
	if !r.markRecoveredOnce() {
		t.Error("first markRecoveredOnce must return true")
	}
	if r.markRecoveredOnce() {
		t.Error("second markRecoveredOnce must return false (guard held)")
	}
}

func TestExecConfigCache(t *testing.T) {
	r := NewLocalRuntime(LocalRuntimeConfig{AgentID: "a"}, nil)
	if _, ok := r.cachedExecConfig(); ok {
		t.Error("no config cached yet → ok must be false")
	}
	r.cacheExecConfig(ExecutorConfig{AgentID: "a", DisplayName: "Agent A", MaxConcurrentTasks: 3})
	got, ok := r.cachedExecConfig()
	if !ok || got.MaxConcurrentTasks != 3 || got.DisplayName != "Agent A" {
		t.Errorf("cached config round trip wrong: %+v ok=%v", got, ok)
	}
}

func TestSelfReconcile_NoEngineIsNoop(t *testing.T) {
	r := NewLocalRuntime(LocalRuntimeConfig{AgentID: "a"}, nil)
	if err := r.selfReconcile(nil); err != nil {
		t.Errorf("selfReconcile with no executor engine should be a no-op, got %v", err)
	}
}

// mustLayout builds an executor Layout over a temp dir for the planner.
func mustLayout(t *testing.T) *executor.Layout {
	t.Helper()
	l, err := executor.NewLayout(t.TempDir())
	if err != nil {
		t.Fatalf("NewLayout: %v", err)
	}
	return l
}

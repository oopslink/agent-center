package agentruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/supervisormanager"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

var errBoom = errors.New("boom")

func TestClassifyExecutor(t *testing.T) {
	inflight := map[string]bool{"task-1": true}
	f := func(kind executor.OutcomeKind, validVerdict bool, taskRef string, alive bool) execReconcileFacts {
		return execReconcileFacts{Kind: kind, HasValidVerdict: validVerdict, TaskRef: taskRef, Alive: alive, Inflight: inflight, HaveInflight: true}
	}
	cases := []struct {
		name  string
		facts execReconcileFacts
		want  reconcileAction
	}{
		{"inflight+alive → adopt", f(executor.OutcomeRunning, false, "task-1", true), reconcileAdopt},
		// DEATH (crash / failed-no-verdict) + should-continue → tier recover (the P0-2 fix).
		{"inflight+dead(crash) → recover", f(executor.OutcomeCrashed, false, "task-1", false), reconcileRecover},
		{"inflight+dead(failed,no verdict) → recover", f(executor.OutcomeFailed, false, "task-1", false), reconcileRecover},
		// LEGIT failure (valid verdict) + should-continue → finalize, NOT re-run (§9).
		{"inflight+dead(failed,valid verdict) → finalize", f(executor.OutcomeFailed, true, "task-1", false), reconcileFinalize},
		// Succeeded → finalize regardless of inflight (work is done).
		{"succeeded → finalize", f(executor.OutcomeSucceeded, true, "task-1", false), reconcileFinalize},
		// Absent from inflight → verify-cancel candidate (get_task-verified).
		{"not-inflight+alive → verify-cancel", f(executor.OutcomeRunning, false, "task-9", true), reconcileVerifyCancel},
		{"not-inflight+dead → verify-cancel", f(executor.OutcomeCrashed, false, "task-9", false), reconcileVerifyCancel},
		// Ownerless: alive → leave (running), dead → finalize (report + teardown).
		{"ownerless alive → leave", f(executor.OutcomeRunning, false, "", true), reconcileLeave},
		{"ownerless dead → finalize", f(executor.OutcomeCrashed, false, "", false), reconcileFinalize},
		// Degraded (inflight query failed): adopt alive, finalize dead (never cancel).
		{"degraded alive → adopt", execReconcileFacts{Kind: executor.OutcomeRunning, Alive: true, HaveInflight: false}, reconcileAdopt},
		{"degraded dead → finalize", execReconcileFacts{Kind: executor.OutcomeCrashed, Alive: false, HaveInflight: false}, reconcileFinalize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyExecutor(tc.facts); got != tc.want {
				t.Errorf("classifyExecutor(%+v) = %v, want %v", tc.facts, got, tc.want)
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
	if d := byID["e-cancel"]; d.Action != reconcileVerifyCancel || d.TaskRef != "task-gone" {
		t.Errorf("absent task should be a verify-cancel candidate: %+v", d)
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
		if d.Action == reconcileVerifyCancel {
			t.Errorf("degraded reconcile must never even consider cancel: %+v", d)
		}
	}
	if got[0].Action != reconcileAdopt {
		t.Errorf("degraded: alive executor should still be adopted, got %v", got[0].Action)
	}
	// Degraded dead → finalize (report its result); the pre-D6 behavior, now explicit
	// since Scan no longer finalizes for us.
	if got[1].Action != reconcileFinalize {
		t.Errorf("degraded: dead executor should be finalized, got %v", got[1].Action)
	}
}

func TestTaskRefOf_NilInput(t *testing.T) {
	if ref := taskRefOf(executor.Snapshot{}); ref != "" {
		t.Errorf("nil input should yield empty task ref, got %q", ref)
	}
}

func TestTaskCancelEvidence(t *testing.T) {
	me := "agent-a"
	cases := []struct {
		name     string
		status   string
		assignee string
		want     bool
	}{
		{"completed → cancel", "completed", "agent:agent-a", true},
		{"discarded → cancel", "discarded", "agent:agent-a", true},
		{"cancelled → cancel", "cancelled", "", true},
		{"reassigned → cancel", "running", "agent:agent-b", true},
		{"running+mine (agent: form) → keep", "running", "agent:agent-a", false},
		{"running+mine (bare form) → keep", "running", "agent-a", false},
		{"running+unknown assignee → keep", "open", "", false},
		{"empty status+mine → keep", "", "agent:agent-a", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := taskCancelEvidence(&centerTaskDetail{Status: tc.status, Assignee: tc.assignee}, me)
			if got != tc.want {
				t.Errorf("taskCancelEvidence(status=%q assignee=%q) = %v, want %v", tc.status, tc.assignee, got, tc.want)
			}
		})
	}
	if taskCancelEvidence(nil, me) {
		t.Error("nil detail must not be cancel evidence")
	}
}

// TestTaskCancelEvidence_IdentityNamespace_T872 locks the T872 root cause: the
// should-continue check must compare a task's assignee against the agent's CENTER
// identity-member ref (the "agent:<member>" namespace), NOT the runtime ULID AgentID.
// With the ULID, a still-mine task's "agent:agent-20d5e05c" assignee reads as
// "reassigned" → the crashed executor is finalized instead of tier-1 resumed. The
// original TestTaskCancelEvidence used a same-namespace id (me="agent-a" vs
// "agent:agent-a") so it never caught this — the exact "stub passes, real claude fails"
// masking. This test uses DIFFERENT namespaces (ULID runtime id vs center ref assignee).
// TestWithRerunSessionID_T877 locks bug2: a RecoverRerun relaunch must swap claude's
// --session-id for a FRESH id (the prior hard-killed claude's session lock survives
// SIGKILL, so reusing it collides "Session ID already in use"). A non-claude argv is
// left unchanged, and the input slice is not mutated (Resume must keep its session).
func TestWithRerunSessionID_T877(t *testing.T) {
	orig := claudestream.SessionUUID("exec-x", 0)
	argv := []string{"claude", "-p", "task", "--session-id", orig, "--verbose"}
	got := withRerunSessionID(argv, "exec-x")
	var gotSession string
	for i := 0; i+1 < len(got); i++ {
		if got[i] == "--session-id" {
			gotSession = got[i+1]
		}
	}
	if gotSession == "" || gotSession == orig {
		t.Errorf("rerun session-id not freshened: got %q (orig %q)", gotSession, orig)
	}
	if argv[4] != orig {
		t.Errorf("input argv was mutated (must copy): %v", argv)
	}
	if plain := withRerunSessionID([]string{"codex", "run"}, "exec-x"); len(plain) != 2 || plain[0] != "codex" {
		t.Errorf("non-claude argv (no --session-id) must be unchanged: %v", plain)
	}
}

func TestTaskCancelEvidence_IdentityNamespace_T872(t *testing.T) {
	const ulid = "01KTVBCSTHCBN1ZFGZQB6XTNW7" // runtime AgentID (r.cfg.AgentID)
	const ref = "agent-20d5e05c"              // center identity-member ref (r.identityRef())
	stillMine := &centerTaskDetail{Status: "running", Assignee: "agent:agent-20d5e05c"}

	// Hazard (the bug): comparing against the ULID misjudges the still-mine task as
	// reassigned. This asserts WHY the old code finalized instead of recovering.
	if !taskCancelEvidence(stillMine, ulid) {
		t.Fatal("precondition: with the ULID a still-mine task reads as reassigned (the T872 bug)")
	}
	// Fix: comparing against the center ref correctly keeps it (not cancel evidence) →
	// the executor is recovered/tier-1 resumed.
	if taskCancelEvidence(stillMine, ref) {
		t.Error("with the center agent-ref, a still-mine task must NOT be cancel evidence (T872 fix)")
	}
	// A genuinely reassigned task is still detected with the ref.
	reassigned := &centerTaskDetail{Status: "running", Assignee: "agent:agent-someone-else"}
	if !taskCancelEvidence(reassigned, ref) {
		t.Error("a task reassigned to another agent must remain cancel evidence")
	}
}

// TestIdentityRef_T872 locks the call-site half: identityRef returns the seeded center
// ref (so the should-continue check keys on the assignee namespace), and falls back to
// the ULID when the ref was never seeded (old center without the agent_ref projection).
func TestIdentityRef_T872(t *testing.T) {
	const ulid = "01KTVBCSTHCBN1ZFGZQB6XTNW7"
	r := NewLocalRuntime(LocalRuntimeConfig{AgentID: ulid}, &SessionState{})
	if got := r.identityRef(); got != ulid {
		t.Errorf("unseeded identityRef = %q, want ULID fallback %q", got, ulid)
	}
	r.SetAgentRef("agent-20d5e05c")
	if got := r.identityRef(); got != "agent-20d5e05c" {
		t.Errorf("seeded identityRef = %q, want the center ref", got)
	}
	r.SetAgentRef("   ") // a blank ref must not clear a good value
	if got := r.identityRef(); got != "agent-20d5e05c" {
		t.Errorf("blank SetAgentRef cleared the ref → %q", got)
	}
}

// TestVerifyThenCancel_IncompleteInflightDoesNotKill is the PD-required guard: when
// the inflight set omits a task that get_task shows is STILL MINE and running, the
// live executor is KEPT (adopted), never cancelled — an incomplete inflight set must
// not kill live work.
func TestVerifyThenCancel_IncompleteInflightDoesNotKill(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-a")
	attach(rt, ee)
	setToolCaller(rt, &scriptedToolCaller{
		getTaskRaw: []byte(`{"id":"task-1","status":"running","assignee":"agent:agent-a"}`),
	})
	d := execReconcileDecision{ExecutorID: "e-keep", TaskRef: "task-1", Alive: true, PID: 424242}

	if cancelled := rt.verifyThenCancel(context.Background(), ee, d); cancelled {
		t.Fatal("still-mine running task must NOT be cancelled (incomplete inflight set)")
	}
	if _, ok := ee.snapshotOrphans()["e-keep"]; !ok {
		t.Error("a kept live executor must be adopted as an orphan")
	}
}

// TestVerifyThenCancel_CrossNamespace_T872 locks the boot self-reconcile CALL SITE with
// production-shaped ids: the runtime AgentID is a ULID, but the still-mine task's
// assignee is the center ref "agent:<member>". verifyThenCancel must KEEP it (recover),
// not cancel — because it now compares via r.identityRef() (the seeded ref), not the
// ULID. The sibling _IncompleteInflightDoesNotKill test used same-namespace ids and so
// masked the T872 bug; this one reproduces the real namespace split.
func TestVerifyThenCancel_CrossNamespace_T872(t *testing.T) {
	const ulid = "01KTVBCSTHCBN1ZFGZQB6XTNW7"
	rt, ee, _ := engineForAgent(t, ulid)
	attach(rt, ee)
	rt.SetAgentRef("agent-20d5e05c") // seed the center identity ref, as Boot does
	setToolCaller(rt, &scriptedToolCaller{
		getTaskRaw: []byte(`{"id":"task-1","status":"running","assignee":"agent:agent-20d5e05c"}`),
	})
	d := execReconcileDecision{ExecutorID: "e-keep", TaskRef: "task-1", Alive: true, PID: 424242}
	if cancelled := rt.verifyThenCancel(context.Background(), ee, d); cancelled {
		t.Fatal("still-mine task (center-ref assignee, ULID runtime id) must NOT be cancelled with the seeded ref (T872)")
	}
	if _, ok := ee.snapshotOrphans()["e-keep"]; !ok {
		t.Error("a kept live executor must be adopted as an orphan")
	}
}

func TestVerifyThenCancel_ReassignedIsCancelled(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-a")
	attach(rt, ee)
	setToolCaller(rt, &scriptedToolCaller{
		getTaskRaw: []byte(`{"id":"task-1","status":"running","assignee":"agent:agent-b"}`),
	})
	// Alive=false so enactCancel does no process kill in the test — only cleanup.
	d := execReconcileDecision{ExecutorID: "e-gone", TaskRef: "task-1", Alive: false}

	if cancelled := rt.verifyThenCancel(context.Background(), ee, d); !cancelled {
		t.Error("a task reassigned to another agent is positive cancel evidence")
	}
}

func TestVerifyThenCancel_GetTaskErrorKeeps(t *testing.T) {
	rt, ee, _ := engineForAgent(t, "agent-a")
	attach(rt, ee)
	setToolCaller(rt, &scriptedToolCaller{getTaskErr: errBoom})
	d := execReconcileDecision{ExecutorID: "e-uncertain", TaskRef: "task-1", Alive: true, PID: 424242}

	if cancelled := rt.verifyThenCancel(context.Background(), ee, d); cancelled {
		t.Error("an uncertain get_task must NOT cancel")
	}
	if _, ok := ee.snapshotOrphans()["e-uncertain"]; !ok {
		t.Error("uncertain + alive must be kept (adopted)")
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

// TestDecideBootSession pins the EXPORTED boot-session decision (T860 fold-in) — the
// wired counterpart the agent-runtime process consumes to enact autonomous session start.
func TestDecideBootSession(t *testing.T) {
	cases := []struct {
		name           string
		probe          supervisormanager.ProbeState
		desiredRunning bool
		want           BootSessionAction
	}{
		{"reattachable+running → reattach", supervisormanager.Reattachable, true, BootSessionReattach},
		{"reattachable+stopped → stop-reap", supervisormanager.Reattachable, false, BootSessionStopReap},
		{"unavailable+running → reap-relaunch", supervisormanager.Unavailable, true, BootSessionReapRelaunch},
		{"unavailable+stopped → reap-only", supervisormanager.Unavailable, false, BootSessionReapOnly},
	}
	for _, tc := range cases {
		if got := DecideBootSession(tc.probe, tc.desiredRunning); got != tc.want {
			t.Errorf("%s: DecideBootSession = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestStart_IdempotentWhenSessionRunning pins the T860 fold-in no-double-start guard: with
// a live session (autonomous boot start), a later control reconcile does NOT start a
// second session — a stale/dup trigger is dropped, a strictly-newer one records the
// version but keeps the live session. This is what converges the two trigger paths onto
// ONE session (no split-brain).
func TestStart_IdempotentWhenSessionRunning(t *testing.T) {
	rt, st, _ := newTestRuntime(t)
	st.Session = &fakeSession{} // a session is already live (boot self-start)
	st.Version = 5

	// Stale/duplicate reconcile (version ≤ current) → no-op, no second start, version kept.
	if err := rt.Start(context.Background(), StartSpec{AgentID: "agent-x", Version: 3}); err != nil {
		t.Fatalf("Start (running, stale) = %v, want nil", err)
	}
	if st.Version != 5 {
		t.Errorf("a stale reconcile must not lower the running version: got %d, want 5", st.Version)
	}
	// Strictly-newer reconcile → still no restart, but version recorded.
	if err := rt.Start(context.Background(), StartSpec{AgentID: "agent-x", Version: 8}); err != nil {
		t.Fatalf("Start (running, newer) = %v, want nil", err)
	}
	if st.Version != 8 {
		t.Errorf("a newer reconcile must record the version: got %d, want 8", st.Version)
	}
	if st.Session == nil {
		t.Error("the live session must be preserved (no second start / no teardown)")
	}
}

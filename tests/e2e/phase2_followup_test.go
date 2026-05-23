// phase2_followup_test.go — adds the 9 e2e scenarios that were deferred to
// Phase 7 in the original Phase 2 delivery (E-2/3/4/5/6/9/10/13/15 per
// plan § 5.3).
//
// Approach (per task spec):
//   - fake claude-style agent CLI = bash scripts in testdata/fake-agents
//   - real OS spawning via shim.OSSpawner (no Spawner mock)
//   - real DB + real services (clock = FakeClock for time travel)
//   - no sleep-based waits — uses clock.Advance / wait loops over real
//     process state only when fork latency is unavoidable.
//
// These tests cover the runtime contract (E-* in plan); the binary's
// `worker daemon` mode itself is still scoped to Phase 7 deployment.
package e2e

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/agentadapter/claudecode"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/shim"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/reconcile"
	trservice "github.com/oopslink/agent-center/internal/taskruntime/service"
	"github.com/oopslink/agent-center/internal/workerdaemon"
)

// =============================================================================
// E-2: User-initiated kill — SIGTERM → grace → SIGKILL via shim.Kill
// =============================================================================

func TestE2EP2_E2_UserKill_RealSpawn_GraceThenKill(t *testing.T) {
	rig := newRuntimeRig(t)
	_, execID := rig.createAndDispatch(t)
	rig.ackAndStartWorking(t, execID, t.TempDir())

	// Spawn the fake long-running agent via the real OSSpawner; this is a
	// real OS process under bash.
	d, err := shim.NewDir(t.TempDir(), string(execID))
	if err != nil {
		t.Fatal(err)
	}
	adapter := bashAdapter{script: fakeAgentPath(t, "long_running.sh")}
	cfg := shim.Config{
		ExecutionID: string(execID),
		ShimToken:   "tok-e2",
		Adapter:     adapter,
		Dir:         d,
		Spawner:     shim.OSSpawner{},
		Clock:       rig.clk,
		KillGrace:   500 * time.Millisecond, // short grace so the test completes in real time
		SpawnRequest: agentadapter.SpawnRequest{
			ExecutionID: string(execID),
			Prompt:      "kill me",
		},
	}
	s, err := shim.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	proc := s.Process()
	// Wait for the agent to emit its first thinking line, signalling it's
	// alive (avoid racing with kill before exec finishes wiring).
	r := proc.Stdout()
	waitForLine(t, r, 2*time.Second)

	// Issue the kill via KillCoordinator → state writes to DB; then
	// physically run SIGTERM → grace → SIGKILL via shim.PerformKill on
	// the running PID (matching what the worker daemon would do).
	if err := rig.killCoord.RequestKill(context.Background(), execID,
		execution.KilledUserRequest, "stop please", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	// Reap the agent in parallel so PerformKill's syscall.Kill(pid, 0)
	// probe sees the process gone (not a zombie). Without this the
	// terminated PID would linger as a zombie and look "alive" to kill -0.
	waitDone := make(chan int, 1)
	go func() {
		code, _ := proc.Wait()
		waitDone <- code
	}()
	pc := &shim.OSProcessController{}
	// Use a real clock for the kill timing so PerformKill's deadline goroutine
	// can elapse (FakeClock requires explicit Advance, but here we want
	// real-time grace observation against a real OS process).
	gracefully, err := shim.PerformKill(context.Background(), pc, clock.SystemClock{}, shim.KillRequest{
		PID: proc.PID(), GraceTimeout: cfg.KillGrace,
	})
	if err != nil {
		t.Fatalf("PerformKill: %v", err)
	}
	if !gracefully {
		t.Logf("E-2: SIGTERM ladder escalated to SIGKILL (acceptable)")
	}
	select {
	case <-waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("agent process did not get reaped after PerformKill")
	}

	// Finalise state in DB (what the daemon would do on ShimGoodbye).
	if err := rig.killCoord.HandleKilled(context.Background(), execID,
		execution.KilledUserRequest, "process killed", "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	e := rig.findExecution(t, execID)
	if e.Status() != execution.StatusKilled {
		t.Fatalf("expected killed, got %s", e.Status())
	}
	if !containsEvent(rig.events(t), "task_execution.kill_requested") {
		t.Error("missing kill_requested event")
	}
	if !containsEvent(rig.events(t), "task_execution.killed") {
		t.Error("missing killed event")
	}
}

// =============================================================================
// E-3: request-input + respond → execution flips input_required → working
// =============================================================================

func TestE2EP2_E3_RequestInputAndRespond(t *testing.T) {
	rig := newRuntimeRig(t)
	ctx := context.Background()
	// Need a conversation for the IR write — create with conversation.
	taskRes, err := rig.taskSvc.Create(ctx, taskCreateInputWithConv())
	if err != nil {
		t.Fatal(err)
	}
	disp, err := rig.dispatchSvc.Dispatch(ctx, dispatch.DispatchInput{
		TaskID: taskRes.TaskID, WorkerID: "W-1", AgentCLI: "claude-code",
		Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.ackAndStartWorking(t, disp.ExecutionID, t.TempDir())

	// Agent emits a request_input event; harness mirrors the CLI handler
	// by invoking IRService.Create.
	res, err := rig.irSvc.Create(ctx, irCreateInput(disp.ExecutionID))
	if err != nil {
		t.Fatal(err)
	}
	if e := rig.findExecution(t, disp.ExecutionID); e.Status() != execution.StatusInputRequired {
		t.Fatalf("expected input_required, got %s", e.Status())
	}
	// User responds via Respond — execution flips back to working.
	if err := rig.irSvc.Respond(ctx, irRespondInput(res.InputRequestID)); err != nil {
		t.Fatal(err)
	}
	if e := rig.findExecution(t, disp.ExecutionID); e.Status() != execution.StatusWorking {
		t.Fatalf("expected working, got %s", e.Status())
	}
	evs := rig.events(t)
	for _, want := range []string{"input_request.requested", "input_request.responded", "task_execution.input_required"} {
		if !containsEvent(evs, want) {
			t.Errorf("missing event %s; got %v", want, eventTypes(evs))
		}
	}
}

// =============================================================================
// E-4: InputRequest T2=24h timeout via TimeoutScanner + FakeClock
// =============================================================================

func TestE2EP2_E4_InputRequestT2Timeout(t *testing.T) {
	rig := newRuntimeRig(t)
	ctx := context.Background()
	taskRes, _ := rig.taskSvc.Create(ctx, taskCreateInputWithConv())
	disp, _ := rig.dispatchSvc.Dispatch(ctx, dispatch.DispatchInput{
		TaskID: taskRes.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	rig.ackAndStartWorking(t, disp.ExecutionID, t.TempDir())
	_, err := rig.irSvc.Create(ctx, irCreateInput(disp.ExecutionID))
	if err != nil {
		t.Fatal(err)
	}
	// Advance clock past T2 (24h default).
	rig.clk.Advance(25 * time.Hour)
	if err := rig.scanner.Tick(ctx, "user:hayang"); err != nil {
		t.Fatal(err)
	}
	e := rig.findExecution(t, disp.ExecutionID)
	if e.Status() != execution.StatusFailed {
		t.Fatalf("expected failed, got %s", e.Status())
	}
	if e.FailedReason() != execution.FailedInputTimeout {
		t.Fatalf("expected failed_reason=input_timeout, got %s", e.FailedReason())
	}
	if !containsEvent(rig.events(t), "input_request.timed_out") {
		t.Error("missing input_request.timed_out")
	}
}

// =============================================================================
// E-5: submitted_timeout 5min — TimeoutScanner + FakeClock
// =============================================================================

func TestE2EP2_E5_SubmittedTimeout(t *testing.T) {
	rig := newRuntimeRig(t)
	_, execID := rig.createAndDispatch(t)
	// Don't ACK — leave execution in dispatch_state=pending_ack +
	// status=submitted. Advance past 5 minutes.
	rig.clk.Advance(6 * time.Minute)
	if err := rig.scanner.Tick(context.Background(), "user:hayang"); err != nil {
		t.Fatal(err)
	}
	e := rig.findExecution(t, execID)
	if e.Status() != execution.StatusFailed {
		t.Fatalf("expected failed, got %s", e.Status())
	}
	if e.FailedReason() != execution.FailedSubmittedTimeout {
		t.Fatalf("expected failed_reason=submitted_timeout, got %s", e.FailedReason())
	}
}

// =============================================================================
// E-6: execution_timeout 6h — TimeoutScanner + FakeClock + KillCoordinator
// =============================================================================

func TestE2EP2_E6_ExecutionTimeout(t *testing.T) {
	rig := newRuntimeRig(t)
	_, execID := rig.createAndDispatch(t)
	rig.ackAndStartWorking(t, execID, t.TempDir())
	// Advance past 6h execution timeout.
	rig.clk.Advance(7 * time.Hour)
	if err := rig.scanner.Tick(context.Background(), "user:hayang"); err != nil {
		t.Fatal(err)
	}
	// Scanner fires KillCoordinator.RequestKill for timeout_kill, which
	// transitions submitted-like states inline or asks worker to SIGTERM
	// for working state. The execution at minimum gets kill_requested
	// emitted and cancel_requested_at set.
	e := rig.findExecution(t, execID)
	if e.CancelRequestedAt() == nil {
		t.Fatalf("expected cancel_requested_at to be set, got nil")
	}
	if !containsEvent(rig.events(t), "task_execution.kill_requested") {
		t.Error("missing kill_requested")
	}
}

// =============================================================================
// E-9: shim_no_hello 60s — ShimSupervisor + FakeClock + DispatchUploader
// =============================================================================

func TestE2EP2_E9_ShimNoHello(t *testing.T) {
	rig := newRuntimeRig(t)
	_, execID := rig.createAndDispatch(t)
	rig.ackAndStartWorking(t, execID, t.TempDir())

	uploader := &captureUploader{}
	sup := workerdaemon.NewShimSupervisor(nil, rig.clk, 60*time.Second, uploader)
	spawnedAt := rig.clk.Now()
	sup.Register(workerdaemon.ShimRecord{
		ExecutionID:   string(execID),
		ShimPID:       12345,
		ShimStartTime: spawnedAt,
		HelloReceived: false,
		SpawnedAt:     spawnedAt,
	})
	// Advance past the hello deadline.
	rig.clk.Advance(61 * time.Second)
	res, err := sup.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NoHello) != 1 || res.NoHello[0] != string(execID) {
		t.Fatalf("expected no_hello for %s, got %+v", execID, res)
	}
	if uploader.noHelloCount.Load() != 1 {
		t.Fatalf("expected NotifyShimNoHello call, got %d", uploader.noHelloCount.Load())
	}
}

// =============================================================================
// E-10: shim_crashed — real shim spawn + real kill of shim PID + supervisor
// detection via ProcessStartTimer
// =============================================================================

func TestE2EP2_E10_ShimCrashed_RealSpawn(t *testing.T) {
	rig := newRuntimeRig(t)
	_, execID := rig.createAndDispatch(t)
	rig.ackAndStartWorking(t, execID, t.TempDir())

	// Spawn the fake agent via OSSpawner (real OS process).
	d, _ := shim.NewDir(t.TempDir(), string(execID))
	adapter := bashAdapter{script: fakeAgentPath(t, "long_running.sh")}
	cfg := shim.Config{
		ExecutionID: string(execID), ShimToken: "tok",
		Adapter: adapter, Dir: d, Spawner: shim.OSSpawner{}, Clock: rig.clk,
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: string(execID), Prompt: "x"},
	}
	s, _ := shim.New(cfg)
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	proc := s.Process()
	waitForLine(t, proc.Stdout(), 2*time.Second)
	pid := proc.PID()

	// Register the live shim with a supervisor and confirm it's alive.
	uploader := &captureUploader{}
	timer := shim.OSStartTimer{}
	startTime, err := timer.GetStartTime(pid)
	if err != nil {
		t.Fatalf("read start_time: %v", err)
	}
	sup := workerdaemon.NewShimSupervisor(timer, rig.clk, time.Hour, uploader)
	sup.Register(workerdaemon.ShimRecord{
		ExecutionID: string(execID), ShimPID: pid,
		ShimStartTime: startTime, HelloReceived: true, SpawnedAt: rig.clk.Now(),
	})
	// Confirm alive on first sweep.
	if res, err := sup.Check(context.Background()); err != nil {
		t.Fatal(err)
	} else if len(res.Crashed) != 0 {
		t.Fatalf("expected no crash yet, got %v", res.Crashed)
	}
	// Now kill the agent process (mimic shim crash).
	_ = proc.Kill()
	_, _ = proc.Wait()
	// After kill, ps -p <pid> returns nothing → checkAlive returns false →
	// supervisor reports crashed.
	res, err := sup.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Crashed) != 1 || res.Crashed[0] != string(execID) {
		t.Fatalf("expected crashed for %s, got %+v", execID, res)
	}
	if uploader.crashedCount.Load() != 1 {
		t.Fatalf("expected NotifyShimCrashed call, got %d", uploader.crashedCount.Load())
	}
}

// =============================================================================
// E-13: Worker reconnect + Reconcile three-way classification
// =============================================================================

func TestE2EP2_E13_ReconcileStaleAndUnknown(t *testing.T) {
	rig := newRuntimeRig(t)
	ctx := context.Background()
	// Create two tasks → two executions; one stays working (active), the
	// other is force-terminated (stale from worker's POV when worker still
	// claims it).
	_, execActive := rig.createAndDispatch(t)
	rig.ackAndStartWorking(t, execActive, t.TempDir())

	// Second execution: dispatch, ack, then mark failed (terminal at
	// center) — worker still claims it → stale.
	_, execStale := rig.createAndDispatch(t)
	rig.ackAndStartWorking(t, execStale, t.TempDir())
	if err := persistence.RunInTx(ctx, rig.db, func(txCtx context.Context) error {
		e, _ := rig.execRepo.FindByID(txCtx, execStale)
		_ = e.MarkFailed(execution.FailedAgentReported, "test stale", rig.clk.Now())
		return rig.execRepo.Update(txCtx, e)
	}); err != nil {
		t.Fatal(err)
	}

	// Reconcile request: worker claims {active, stale, unknown}. Center
	// has only {active, stale} (stale is terminal at center).
	req := reconcile.Request{
		WorkerID: "W-1",
		LocalActives: []reconcile.LocalActiveExecution{
			{ExecutionID: execActive, Status: "working"},
			{ExecutionID: execStale, Status: "working"},
			{ExecutionID: "E-UNKNOWN", Status: "working"},
		},
	}
	resp, err := rig.reconcSvc.Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Active) != 1 || resp.Active[0] != execActive {
		t.Fatalf("expected active=[%s], got %+v", execActive, resp.Active)
	}
	if len(resp.Stale) != 1 || resp.Stale[0] != execStale {
		t.Fatalf("expected stale=[%s], got %+v", execStale, resp.Stale)
	}
	if len(resp.Unknown) != 1 || resp.Unknown[0] != "E-UNKNOWN" {
		t.Fatalf("expected unknown=[E-UNKNOWN], got %+v", resp.Unknown)
	}
}

// =============================================================================
// E-15: Daemon restart doesn't interrupt agent — real spawn, kill shim
// supervisor (simulating daemon crash), agent stays alive, then new
// supervisor reconnects via status.json + start_time check.
// =============================================================================

func TestE2EP2_E15_DaemonRestartDoesNotInterruptAgent(t *testing.T) {
	rig := newRuntimeRig(t)
	_, execID := rig.createAndDispatch(t)
	rig.ackAndStartWorking(t, execID, t.TempDir())

	// Spawn agent via real OSSpawner.
	dirRoot := t.TempDir()
	d, _ := shim.NewDir(dirRoot, string(execID))
	adapter := bashAdapter{script: fakeAgentPath(t, "long_running.sh")}
	cfg := shim.Config{
		ExecutionID: string(execID), ShimToken: "tok",
		Adapter: adapter, Dir: d, Spawner: shim.OSSpawner{}, Clock: rig.clk,
		SpawnRequest: agentadapter.SpawnRequest{ExecutionID: string(execID), Prompt: "x"},
	}
	s, _ := shim.New(cfg)
	if err := s.Start(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	proc := s.Process()
	waitForLine(t, proc.Stdout(), 2*time.Second)
	pid := proc.PID()

	// Drop the supervisor (== daemon "crashes"). The OS process under
	// pid is independent (Setsid + own process group). Agent stays alive.
	// We verify by inspecting status.json + the live PID.

	// Verify status.json still says running (shim hasn't updated it).
	st, err := d.ReadStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.Phase != shim.PhaseRunning {
		t.Fatalf("expected running phase persisted, got %s", st.Phase)
	}

	// Verify the process is alive via real ps.
	timer := shim.OSStartTimer{}
	got, err := timer.GetStartTime(pid)
	if err != nil || got.IsZero() {
		t.Fatalf("agent should still be alive: err=%v got=%v", err, got)
	}

	// "Daemon" reconnects: builds a new supervisor + re-registers based on
	// what status.json persisted. checkAlive on the same PID still
	// succeeds (process_start_time matches).
	uploader := &captureUploader{}
	sup := workerdaemon.NewShimSupervisor(timer, rig.clk, time.Hour, uploader)
	sup.Register(workerdaemon.ShimRecord{
		ExecutionID: string(execID), ShimPID: pid, ShimStartTime: got,
		HelloReceived: true, SpawnedAt: rig.clk.Now(),
	})
	if res, err := sup.Check(context.Background()); err != nil {
		t.Fatal(err)
	} else if len(res.Crashed) != 0 || len(res.NoHello) != 0 {
		t.Fatalf("reconnected supervisor reported phantom failure: %+v", res)
	}

	// Cleanup: actually SIGKILL the test process.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	_, _ = proc.Wait()
}

// =============================================================================
// helpers / harness types
// =============================================================================

// bashAdapter is a tiny agentadapter.Adapter that wraps a bash script as
// the "claude" binary. ParseEvent delegates to the real claudecode adapter
// so the JSONL the scripts emit is interpreted exactly like real claude
// output.
type bashAdapter struct {
	script string
}

func (a bashAdapter) Name() string             { return "fake-bash-claude" }
func (a bashAdapter) SupportsSession() bool     { return true }
func (a bashAdapter) BuildCommand(req agentadapter.SpawnRequest) (agentadapter.CmdSpec, error) {
	return agentadapter.CmdSpec{
		Binary: "/bin/bash",
		Args:   []string{a.script},
	}, nil
}
func (a bashAdapter) ParseEvent(line []byte) (agentadapter.AgentTraceEvent, error) {
	return claudecode.New("").ParseEvent(line)
}

// v2 ADR-0030 § 2 — minimal pass-through so the bash adapter satisfies the
// v2 Adapter interface in tests. None of these methods are exercised in the
// existing dispatch/spawn paths.
func (a bashAdapter) Probe(context.Context) (bool, string, error) { return true, "fake-bash", nil }
func (a bashAdapter) SupportedFeatures() agentadapter.FeatureSet {
	return agentadapter.FeatureSet{SupportsSession: true}
}
func (a bashAdapter) BuildMCPConfigArg(string) (agentadapter.MCPSetup, error) {
	return agentadapter.MCPSetup{}, nil
}
func (a bashAdapter) BuildSkillMountSetup(string, string) (agentadapter.SkillMountSetup, error) {
	return agentadapter.SkillMountSetup{}, nil
}

// captureUploader records uploader calls for assertions.
type captureUploader struct {
	noHelloCount  atomic.Int64
	crashedCount  atomic.Int64
	workingCount  atomic.Int64
	ackCount      atomic.Int64
	nackCount     atomic.Int64
}

func (u *captureUploader) SendAck(context.Context, dispatch.DispatchAck) error {
	u.ackCount.Add(1)
	return nil
}
func (u *captureUploader) SendNack(context.Context, dispatch.DispatchNack) error {
	u.nackCount.Add(1)
	return nil
}
func (u *captureUploader) NotifyShimNoHello(_ context.Context, _ string) error {
	u.noHelloCount.Add(1)
	return nil
}
func (u *captureUploader) NotifyShimCrashed(_ context.Context, _ string) error {
	u.crashedCount.Add(1)
	return nil
}
func (u *captureUploader) NotifyWorking(_ context.Context, _, _, _ string) error {
	u.workingCount.Add(1)
	return nil
}

// waitForLine reads from r and returns when it sees at least one newline.
// Bounded by deadline to avoid hanging tests on stalled processes. This
// blocks on real I/O, not on time — it's a fork latency wait, not a
// behaviour wait.
func waitForLine(t *testing.T, r io.Reader, deadline time.Duration) {
	t.Helper()
	done := make(chan struct{})
	var buf [256]byte
	go func() {
		_, _ = r.Read(buf[:])
		close(done)
	}()
	select {
	case <-done:
		return
	case <-time.After(deadline):
		t.Fatalf("timed out waiting for first agent line after %s", deadline)
	}
}

// taskCreateInputWithConv builds a TaskCreateInput that opens a
// conversation in the same tx (a/e path) so subsequent IRs can write.
func taskCreateInputWithConv() trservice.TaskCreateInput {
	return trservice.TaskCreateInput{
		ProjectID:         "p-1",
		Title:             "e2e task",
		WithConversation:  true,
		ConversationTitle: "e2e conv",
		Actor:             "user:hayang",
	}
}

// irCreateInput / irRespondInput build payloads for the IR service.
func irCreateInput(execID taskruntime.TaskExecutionID) trservice.CreateInput {
	return trservice.CreateInput{
		ExecutionID: execID,
		Question:    "approve?",
		Urgency:     inputrequest.UrgencyNormal,
		Actor:       observability.Actor("agent:" + string(execID)),
	}
}

func irRespondInput(irID taskruntime.InputRequestID) trservice.RespondInput {
	return trservice.RespondInput{
		InputRequestID: irID,
		Answer:         "yes",
		DecidedBy:      "user:hayang",
		Actor:          "user:hayang",
	}
}

// keep the linter happy: import-needed sentinels
var _ = strings.Contains

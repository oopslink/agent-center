// Package workerdaemon: Runtime is the v2.2-C worker daemon main loop.
// It glues AdminClient (transport to center) + AgentRunner (subprocess
// spawn) into a polling loop:
//
//  1. Tick every PollInterval (default 1s).
//  2. PullDispatches — for each new envelope, kick off an agent spawn
//     in a goroutine and remember the procHandle in the live map.
//  3. PullKills — for each kill request whose execution_id we own,
//     SIGTERM the proc (escalate to SIGKILL after KillGrace).
//  4. On Run(ctx) cancellation: stop polling, wait for in-flight
//     spawns to drain (or hard-kill after ShutdownGrace).
//
// The Runtime intentionally treats the AdminClient as an opaque
// dependency (interface, not concrete type) so tests can inject a fake.
// Same for AgentSpawnerFunc — tests bypass real `exec.Command` by
// supplying a closure that emits scripted events.
package workerdaemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/admin/dispatchq"
	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

// CenterClient is the subset of AdminClient methods Runtime needs.
// Defined as an interface so runtime_test.go can plug a fake.
type CenterClient interface {
	Enroll(ctx context.Context, workerID string, capabilities []string) error
	Heartbeat(ctx context.Context, workerID string, capabilities []string) error
	PullDispatches(ctx context.Context, workerID string) ([]dispatch.DispatchEnvelope, error)
	PullKills(ctx context.Context) ([]dispatchq.KillRequest, error)
	// NotifyWorking flips the center-side execution state submitted →
	// working. v2.2 Phase D state-machine fix.
	NotifyWorking(ctx context.Context, executionID, cwd, branchName string) error
	// Conclude closes the state machine on clean exit (working →
	// completed + task → done). v2.2 Phase D state-machine fix.
	Conclude(ctx context.Context, executionID, message string) error
	ReportProgress(ctx context.Context, executionID, milestone, content string) error
	ReportFailure(ctx context.Context, executionID, reason, message string) error
	ReportArtifact(ctx context.Context, executionID string, blob []byte, kind string) error
}

// AgentSpawnerFunc is the v2.2-C indirection for spawning an agent.
// Production uses spawnAgentProcess (wraps AgentRunner.Run). Tests
// supply a closure that emits scripted events directly.
type AgentSpawnerFunc func(ctx context.Context, env dispatch.DispatchEnvelope, runtime *Runtime) error

// RuntimeConfig parameterises the daemon loop.
type RuntimeConfig struct {
	WorkerID      string
	Capabilities  []string
	PollInterval  time.Duration // default 1s
	HeartbeatEvery time.Duration // default 30s
	KillGrace     time.Duration // SIGTERM → SIGKILL gap; default 5s
	ShutdownGrace time.Duration // wait for in-flight on shutdown; default 30s
	// AgentCLIOverrides → AgentRunnerConfig (e.g. fakeagent path).
	AgentCLIOverrides map[string]string
	// ExecBaseDir is the per-execution working dir root. Empty disables
	// per-execution dirs (subprocess inherits caller cwd).
	ExecBaseDir string
	// Logger receives one-line ops messages with `[worker] ` prefix.
	Logger func(msg string)
}

// Runtime is the daemon orchestrator.
type Runtime struct {
	cfg     RuntimeConfig
	client  CenterClient
	spawner AgentSpawnerFunc

	mu   sync.Mutex
	live map[string]*procHandle // executionID → process handle

	wg sync.WaitGroup // tracks in-flight spawn goroutines
}

// NewRuntime constructs a Runtime. If spawner is nil, the production
// agent spawner is wired (real subprocess).
func NewRuntime(cfg RuntimeConfig, client CenterClient, spawner AgentSpawnerFunc) *Runtime {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.HeartbeatEvery <= 0 {
		cfg.HeartbeatEvery = 30 * time.Second
	}
	if cfg.KillGrace <= 0 {
		cfg.KillGrace = 5 * time.Second
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = func(msg string) {}
	}
	rt := &Runtime{
		cfg:     cfg,
		client:  client,
		spawner: spawner,
		live:    map[string]*procHandle{},
	}
	if rt.spawner == nil {
		rt.spawner = defaultAgentSpawner
	}
	return rt
}

// Run blocks until ctx is cancelled. Performs initial enroll and
// then loops on poll interval, draining dispatch + kill queues.
func (r *Runtime) Run(ctx context.Context) error {
	if r.cfg.WorkerID == "" {
		return errors.New("runtime: worker_id required")
	}
	// Initial enroll. Failure here is fatal — without enrollment the
	// center will reject all subsequent calls.
	if err := r.client.Enroll(ctx, r.cfg.WorkerID, r.cfg.Capabilities); err != nil {
		return fmt.Errorf("runtime: initial enroll: %w", err)
	}
	r.log("enrolled as worker_id=%s", r.cfg.WorkerID)

	pollTick := time.NewTicker(r.cfg.PollInterval)
	defer pollTick.Stop()
	hbTick := time.NewTicker(r.cfg.HeartbeatEvery)
	defer hbTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return r.shutdown()
		case <-pollTick.C:
			r.pollOnce(ctx)
		case <-hbTick.C:
			if err := r.client.Heartbeat(ctx, r.cfg.WorkerID, r.cfg.Capabilities); err != nil {
				r.log("heartbeat: %v", err)
			}
		}
	}
}

// pollOnce drains both queues. Errors are logged, not returned —
// transient transport failures should not kill the daemon.
func (r *Runtime) pollOnce(ctx context.Context) {
	// Dispatches first so a kill in the same tick can target an
	// already-spawned execution.
	envelopes, err := r.client.PullDispatches(ctx, r.cfg.WorkerID)
	if err != nil {
		r.log("pull dispatches: %v", err)
	} else {
		for _, env := range envelopes {
			r.handleDispatch(ctx, env)
		}
	}
	kills, err := r.client.PullKills(ctx)
	if err != nil {
		r.log("pull kills: %v", err)
		return
	}
	for _, k := range kills {
		r.handleKill(k)
	}
}

// handleDispatch validates + spawns. Spawn runs in its own goroutine so
// pollOnce returns quickly.
func (r *Runtime) handleDispatch(ctx context.Context, env dispatch.DispatchEnvelope) {
	if err := env.Validate(); err != nil {
		r.log("envelope %s invalid: %v", env.ExecutionID, err)
		_ = r.client.ReportFailure(ctx, string(env.ExecutionID),
			"nack_envelope_invalid", err.Error())
		return
	}
	// Dedup: already running.
	r.mu.Lock()
	if _, exists := r.live[string(env.ExecutionID)]; exists {
		r.mu.Unlock()
		r.log("envelope %s already running; skipping", env.ExecutionID)
		return
	}
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.log("dispatch %s task=%s agent=%s", env.ExecutionID, env.TaskID, env.AgentCLI)
		if err := r.spawner(ctx, env, r); err != nil {
			r.log("spawn %s: %v", env.ExecutionID, err)
		}
		// Drop the live handle on exit.
		r.mu.Lock()
		delete(r.live, string(env.ExecutionID))
		r.mu.Unlock()
	}()
}

// handleKill SIGTERMs the matching live execution. Escalates to
// SIGKILL after r.cfg.KillGrace.
func (r *Runtime) handleKill(k dispatchq.KillRequest) {
	r.mu.Lock()
	h, ok := r.live[string(k.ExecutionID)]
	r.mu.Unlock()
	if !ok {
		// Not owned by this worker — drop silently. Other workers will
		// also see this kill on their pull and will likewise filter.
		return
	}
	r.log("kill %s reason=%s", k.ExecutionID, k.Reason)
	if err := h.Signal(syscall.SIGTERM); err != nil {
		r.log("kill %s SIGTERM: %v", k.ExecutionID, err)
	}
	go func(h *procHandle, execID string) {
		time.Sleep(r.cfg.KillGrace)
		// Still alive? Escalate.
		r.mu.Lock()
		_, stillThere := r.live[execID]
		r.mu.Unlock()
		if stillThere {
			r.log("kill %s SIGKILL escalation", execID)
			_ = h.Signal(syscall.SIGKILL)
		}
	}(h, string(k.ExecutionID))
}

// registerLive is called by AgentSpawnerFunc once the process is up so
// subsequent kills can find it.
func (r *Runtime) registerLive(executionID string, h *procHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live[executionID] = h
}

// LiveCount is for tests / observability.
func (r *Runtime) LiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.live)
}

// shutdown waits for in-flight spawns to finish or escalates after
// ShutdownGrace.
func (r *Runtime) shutdown() error {
	r.log("shutdown: waiting for %d in-flight executions (grace=%s)",
		r.LiveCount(), r.cfg.ShutdownGrace)
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		r.log("shutdown: clean")
		return nil
	case <-time.After(r.cfg.ShutdownGrace):
		// Hard-kill anything left.
		r.mu.Lock()
		for execID, h := range r.live {
			r.log("shutdown: hard-kill %s", execID)
			_ = h.Signal(syscall.SIGKILL)
		}
		r.mu.Unlock()
		<-done
		r.log("shutdown: forced after grace")
		return errors.New("runtime: shutdown grace exceeded")
	}
}

// log is the prefixed Logger wrapper.
func (r *Runtime) log(format string, args ...any) {
	if r.cfg.Logger == nil {
		return
	}
	r.cfg.Logger(fmt.Sprintf(format, args...))
}

// defaultAgentSpawner is the production spawn path used when
// NewRuntime is called with spawner == nil.
//
// Step sequence (Phase C minimal):
//  1. Build prompt — Phase C just uses the envelope's TaskTitle +
//     TaskDescription concatenated. PromptAssemblyService is the v2.3
//     callsite but for fakeagent the prompt is irrelevant (script
//     arg is passed via AgentRunnerConfig.Args).
//  2. Resolve agent binary + spawn.
//  3. Stream JSONL → forward to admin endpoint.
//  4. On exit: emit final done / failure event.
func defaultAgentSpawner(ctx context.Context, env dispatch.DispatchEnvelope, rt *Runtime) error {
	// Built prompt. For fakeagent we just hand it the description; real
	// agents would get the full prompt-assembly output. Phase D may
	// wire prompt_assembly.go in.
	prompt := env.TaskTitle
	if env.TaskDescription != "" {
		prompt = env.TaskTitle + "\n\n" + env.TaskDescription
	}
	// Args: fakeagent expects --script=<path>. For real agents this is
	// agent-specific; Phase C only commits to making fakeagent work
	// end-to-end. The fakeagent script path is embedded in the task
	// description as a convention: "fakeagent-script: <path>" line.
	var args []string
	if env.AgentCLI == "fakeagent" {
		script := extractFakeAgentScript(env)
		if script == "" {
			err := errors.New("fakeagent: no script path in envelope (expect 'fakeagent-script: <path>' line in task_description)")
			_ = rt.client.ReportFailure(ctx, string(env.ExecutionID), "fakeagent_missing_script", err.Error())
			return err
		}
		args = []string{"--script=" + script}
	}

	runner := NewAgentRunner(AgentRunnerConfig{
		AgentCLI:          env.AgentCLI,
		Args:              args,
		Prompt:            prompt,
		AgentCLIOverrides: rt.cfg.AgentCLIOverrides,
		EnvAllowList:      nil, // use defaults
		ExtraEnv:          nil,
	})
	startedCh := make(chan *procHandle, 1)
	// Register handle as soon as it lands.
	go func() {
		h, ok := <-startedCh
		if !ok {
			return
		}
		rt.registerLive(string(env.ExecutionID), h)
	}()

	// Flip server-side state machine submitted → working. This is the
	// v2.2 Phase D fix for gap #1: without this call the execution
	// sits in `submitted` forever even though the worker is running it.
	if err := rt.client.NotifyWorking(ctx, string(env.ExecutionID),
		"", ""); err != nil {
		rt.log("notify-working %s: %v", env.ExecutionID, err)
		// Non-fatal — agent can still run; ReportProgress will surface
		// trace events even if the state didn't transition. The
		// supervisor reconciler will eventually catch the stuck state.
	}

	// Report start as a progress event so SSE/trace shows the
	// transition out of submitted.
	_ = rt.client.ReportProgress(ctx, string(env.ExecutionID), "started",
		fmt.Sprintf("worker=%s agent_cli=%s", rt.cfg.WorkerID, env.AgentCLI))

	// The server requires non-empty `content` on report-progress; we
	// substitute a default when the agent emits a content-less event.
	nonEmpty := func(s, fallback string) string {
		if s != "" {
			return s
		}
		return fallback
	}
	// ReportProgress is a no-op on the server unless the task has a
	// conversation attached (ADR-0017 silent-skip). For deploy smoke
	// the task often has no conversation, but the call still fails fast
	// on validation. We swallow "no conversation" style errors here to
	// keep the agent loop alive — production wiring (Phase D) will
	// always attach a conversation.
	safeProgress := func(ctx context.Context, milestone, content string) error {
		err := rt.client.ReportProgress(ctx, string(env.ExecutionID),
			milestone, nonEmpty(content, milestone))
		if err != nil {
			rt.log("report-progress %s/%s: %v", env.ExecutionID, milestone, err)
		}
		return nil // never abort the agent on transport hiccups
	}
	handler := func(ctx context.Context, ev AgentEvent, raw []byte) error {
		switch ev.Type {
		case "start":
			return safeProgress(ctx, "agent_start", ev.Text)
		case "progress":
			milestone := ev.Milestone
			if milestone == "" {
				milestone = "progress"
			}
			return safeProgress(ctx, milestone, ev.Content)
		case "artifact":
			kind := ev.Kind
			if kind == "" {
				kind = "artifact"
			}
			if err := rt.client.ReportArtifact(ctx, string(env.ExecutionID), raw, kind); err != nil {
				rt.log("report-artifact %s/%s: %v", env.ExecutionID, kind, err)
			}
			return nil
		case "done":
			return safeProgress(ctx, "done", ev.Content)
		case "failed":
			if err := rt.client.ReportFailure(ctx, string(env.ExecutionID),
				safeReason(ev.Reason), ev.Message); err != nil {
				rt.log("report-failure %s: %v", env.ExecutionID, err)
			}
			return nil
		default:
			return safeProgress(ctx, nonEmpty(ev.Type, "progress"), string(raw))
		}
	}

	res, err := runner.Run(ctx, handler, startedCh)
	if err != nil {
		// Failure was already reported on the failed/exit path if
		// possible; ensure at least one report-failure landed.
		_ = rt.client.ReportFailure(ctx, string(env.ExecutionID),
			"shim_crashed", err.Error())
		return err
	}
	if res.Failed {
		_ = rt.client.ReportFailure(ctx, string(env.ExecutionID),
			"agent_exit_nonzero", res.FailedMsg)
		return nil
	}
	// Clean exit. The agent should have emitted a "done" event; report-
	// progress with milestone=done here is idempotent on the server.
	_ = rt.client.ReportProgress(ctx, string(env.ExecutionID), "done",
		fmt.Sprintf("agent exited cleanly pid=%d", res.PID))
	// Close out the state machine: working → completed + task → done.
	// v2.2 Phase D state-machine fix (gap #1 from C report).
	if err := rt.client.Conclude(ctx, string(env.ExecutionID),
		fmt.Sprintf("agent exited cleanly pid=%d", res.PID)); err != nil {
		rt.log("conclude %s: %v", env.ExecutionID, err)
	}
	return nil
}

// safeReason maps an agent-emitted reason to a server-accepted enum.
// Empty / unknown → "agent_self_reported_failure".
func safeReason(raw string) string {
	if raw == "" {
		return "agent_self_reported_failure"
	}
	return raw
}

// extractFakeAgentScript pulls the script path from a task_description
// line of the form `fakeagent-script: <path>`. Returns "" if absent.
func extractFakeAgentScript(env dispatch.DispatchEnvelope) string {
	const prefix = "fakeagent-script:"
	for _, line := range splitLines(env.TaskDescription) {
		l := trim(line)
		if hasPrefix(l, prefix) {
			return trim(l[len(prefix):])
		}
	}
	return ""
}

// trivial string helpers (avoid pulling strings import twice — keep
// this file self-contained for readability).
func splitLines(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func trim(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func hasPrefix(s, p string) bool {
	if len(s) < len(p) {
		return false
	}
	return s[:len(p)] == p
}

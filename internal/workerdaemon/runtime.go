// Package workerdaemon: Runtime is the worker daemon main loop.
//
// As of v2.7 #107 slice-2 the daemon runs the control-stream path
// UNCONDITIONALLY: Run enrolls (unless SkipInitialEnroll), performs a
// best-effort boot-reconcile, starts the ControlLoop goroutine, and then
// heartbeats on a ticker until ctx is cancelled. The legacy taskruntime
// dispatch poll has been removed.
//
// The Runtime intentionally treats the AdminClient as an opaque
// dependency (interface, not concrete type) so tests can inject a fake.
package workerdaemon

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

// CenterClient is the subset of AdminClient methods Runtime needs.
// Defined as an interface so runtime_test.go can plug a fake.
type CenterClient interface {
	Enroll(ctx context.Context, workerID string, capabilities []string) error
	// Heartbeat asserts liveness and ships the optional per-agent concurrency
	// snapshots (v2.19.0; nil/empty when no agent is running the concurrent path —
	// a back-compat-safe empty field on the wire).
	Heartbeat(ctx context.Context, workerID string, capabilities []string, snapshots map[string]concurrency.AgentSnapshot) error
}

// concurrencySnapshotter is the ControlHandler capability the runtime uses to gather
// the per-agent live executor view for the heartbeat (v2.19.0). The production
// AgentController implements it; a handler that doesn't is simply skipped.
type concurrencySnapshotter interface {
	SnapshotConcurrency() map[string]concurrency.AgentSnapshot
}

// RuntimeConfig parameterises the daemon loop.
type RuntimeConfig struct {
	WorkerID       string
	Capabilities   []string
	PollInterval   time.Duration // default 1s
	HeartbeatEvery time.Duration // idle heartbeat cadence; default 30s
	// ActiveHeartbeatEvery is the FAST cadence used while any agent on this worker
	// has a live executor (v2.19.0: keeps the real-time concurrency view fresh).
	// Default 4s; falls back to HeartbeatEvery if set larger than it.
	ActiveHeartbeatEvery time.Duration
	ShutdownGrace        time.Duration // wait for in-flight on shutdown; default 30s
	// AgentCLIOverrides → AgentRunnerConfig (e.g. fakeagent path).
	AgentCLIOverrides map[string]string
	// Logger receives one-line ops messages with `[worker] ` prefix.
	Logger func(msg string)
	// SkipInitialEnroll lets main.go own the enroll + long-term token
	// exchange (v2.4-D B5 fix). When true, Run skips its initial
	// Enroll call and goes straight to the loop. main.go must
	// have ensured the AdminClient bearer is the long-term token
	// before calling Run.
	SkipInitialEnroll bool

	// ControlClient drives the control-stream poll loop for the
	// Environment BC (ADR-0050, task #102). Always wired in production
	// (RunDaemon passes the daemon's *AdminClient, whose
	// ConnectControl/PullCommands/AckControl satisfy ControlClient).
	ControlClient ControlClient
	// ControlPollInterval overrides the control loop's poll cadence. Default
	// reuses PollInterval.
	ControlPollInterval time.Duration
	// ControlHandler is the pluggable command executor. Nil →
	// NoopCommandHandler (logs, does nothing real). Production plugs the
	// AgentController here.
	ControlHandler CommandHandler

	// ControlStreamClient, when non-nil and not disabled, makes the control loop
	// STREAM-FIRST (SSE down-push) with poll as fallback (v2.7 D5 slice-2).
	// Production wires the daemon's *AdminClient here (its StreamCommands
	// satisfies StreamClient). Nil → poll-only.
	ControlStreamClient StreamClient
	// DisableControlStream forces the poll-only path even when ControlStreamClient
	// is wired. The stream is default-on for v2.7; this is the operator escape
	// hatch back to pure poll (same delivery contract).
	DisableControlStream bool
	// ControlStreamIdleTimeout overrides the stream no-frame fallback timeout.
	// Default defaultStreamIdleTimeout (≈2× the 30s server heartbeat).
	ControlStreamIdleTimeout time.Duration
}

// Runtime is the daemon orchestrator.
type Runtime struct {
	cfg    RuntimeConfig
	client CenterClient

	wg sync.WaitGroup // tracks in-flight goroutines (control loop)
}

// NewRuntime constructs a Runtime.
func NewRuntime(cfg RuntimeConfig, client CenterClient) *Runtime {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.HeartbeatEvery <= 0 {
		cfg.HeartbeatEvery = 30 * time.Second
	}
	if cfg.ActiveHeartbeatEvery <= 0 {
		cfg.ActiveHeartbeatEvery = 4 * time.Second
	}
	if cfg.ActiveHeartbeatEvery > cfg.HeartbeatEvery {
		cfg.ActiveHeartbeatEvery = cfg.HeartbeatEvery
	}
	if cfg.ShutdownGrace <= 0 {
		cfg.ShutdownGrace = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = func(msg string) {}
	}
	return &Runtime{
		cfg:    cfg,
		client: client,
	}
}

// Run blocks until ctx is cancelled. Performs initial enroll, a
// best-effort boot-reconcile, starts the control loop, then heartbeats.
func (r *Runtime) Run(ctx context.Context) error {
	if r.cfg.WorkerID == "" {
		return errors.New("runtime: worker_id required")
	}
	if !r.cfg.SkipInitialEnroll {
		// Initial enroll. Failure here is fatal — without enrollment the
		// center will reject all subsequent calls.
		if err := r.client.Enroll(ctx, r.cfg.WorkerID, r.cfg.Capabilities); err != nil {
			return fmt.Errorf("runtime: initial enroll: %w", err)
		}
		r.log("enrolled as worker_id=%s", r.cfg.WorkerID)
	}

	// Environment-BC control-stream poll loop (ADR-0050, task #102). This is
	// the unconditional execution path as of #107 slice-2.

	interval := r.cfg.ControlPollInterval
	if interval <= 0 {
		interval = r.cfg.PollInterval
	}
	cl := NewControlLoop(ControlLoopConfig{
		WorkerID:          r.cfg.WorkerID,
		PollInterval:      interval,
		Handler:           r.cfg.ControlHandler, // nil → NoopCommandHandler
		Logger:            r.cfg.Logger,
		StreamClient:      r.cfg.ControlStreamClient, // nil → poll-only
		DisableStream:     r.cfg.DisableControlStream,
		StreamIdleTimeout: r.cfg.ControlStreamIdleTimeout,
	}, r.cfg.ControlClient)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if err := cl.Run(ctx); err != nil {
			r.log("control loop: %v", err)
		}
	}()
	r.log("control loop started worker_id=%s", r.cfg.WorkerID)

	// v2.19.0: the heartbeat cadence is ADAPTIVE — fast (ActiveHeartbeatEvery) while
	// any agent has a live executor (keeps the real-time concurrency view fresh),
	// idle (HeartbeatEvery) otherwise. A timer (reset per beat) replaces the fixed
	// ticker so the interval can change between beats.
	//
	// v2.7 #154: assert liveness IMMEDIATELY on startup. Without this the worker
	// stays offline until the first interval — the center marks a worker online on
	// its first heartbeat. An immediate heartbeat brings "online" to ~1 RTT.
	next := r.beat(ctx)
	hbTimer := time.NewTimer(next)
	defer hbTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return r.shutdown()
		case <-hbTimer.C:
			hbTimer.Reset(r.beat(ctx))
		}
	}
}

// beat gathers the per-agent concurrency snapshots (when the ControlHandler supports
// it), sends one heartbeat, and returns the interval until the next beat: the fast
// active cadence when any agent has a live executor, else the idle cadence.
func (r *Runtime) beat(ctx context.Context) time.Duration {
	snaps := r.gatherSnapshots()
	if err := r.client.Heartbeat(ctx, r.cfg.WorkerID, r.cfg.Capabilities, snaps); err != nil {
		r.log("heartbeat: %v", err)
	}
	if anyActiveExecutor(snaps) {
		return r.cfg.ActiveHeartbeatEvery
	}
	return r.cfg.HeartbeatEvery
}

// gatherSnapshots pulls the per-agent live executor view from the ControlHandler
// (nil when the handler doesn't support it — e.g. tests / noop).
func (r *Runtime) gatherSnapshots() map[string]concurrency.AgentSnapshot {
	if s, ok := r.cfg.ControlHandler.(concurrencySnapshotter); ok {
		return s.SnapshotConcurrency()
	}
	return nil
}

// anyActiveExecutor reports whether any agent currently has ≥1 live executor.
func anyActiveExecutor(snaps map[string]concurrency.AgentSnapshot) bool {
	for _, s := range snaps {
		if s.Active > 0 {
			return true
		}
	}
	return false
}

// shutdown waits for in-flight goroutines to finish or escalates after
// ShutdownGrace.
func (r *Runtime) shutdown() error {
	r.log("shutdown: waiting for in-flight goroutines (grace=%s)", r.cfg.ShutdownGrace)
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

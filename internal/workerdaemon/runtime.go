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
)

// CenterClient is the subset of AdminClient methods Runtime needs.
// Defined as an interface so runtime_test.go can plug a fake.
type CenterClient interface {
	Enroll(ctx context.Context, workerID string, capabilities []string) error
	Heartbeat(ctx context.Context, workerID string, capabilities []string) error
}

// RuntimeConfig parameterises the daemon loop.
type RuntimeConfig struct {
	WorkerID       string
	Capabilities   []string
	PollInterval   time.Duration // default 1s
	HeartbeatEvery time.Duration // default 30s
	ShutdownGrace  time.Duration // wait for in-flight on shutdown; default 30s
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
	//
	// BOOT RECONCILE — before the poll loop starts, reconcile this worker's
	// agents (re-attach survivors / relaunch the dead / stop the unwanted) by
	// joining center resume-state with local supervisor probes. Run
	// SYNCHRONOUSLY: the attach/start paths are only safe for the
	// single-threaded ControlLoop caller, which has not started yet.
	// Best-effort — a failure is logged, never crashes the daemon (the poll
	// loop still starts; the center re-reconciles via commands). Skipped when
	// the handler is not a bootReconciler.
	if br, ok := r.cfg.ControlHandler.(bootReconciler); ok {
		if err := br.ReconcileOnBoot(ctx); err != nil {
			r.log("boot reconcile: %v (continuing — poll loop will reconcile)", err)
		}
	}

	interval := r.cfg.ControlPollInterval
	if interval <= 0 {
		interval = r.cfg.PollInterval
	}
	cl := NewControlLoop(ControlLoopConfig{
		WorkerID:     r.cfg.WorkerID,
		PollInterval: interval,
		Handler:      r.cfg.ControlHandler, // nil → NoopCommandHandler
		Logger:       r.cfg.Logger,
	}, r.cfg.ControlClient)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		if err := cl.Run(ctx); err != nil {
			r.log("control loop: %v", err)
		}
	}()
	r.log("control loop started worker_id=%s", r.cfg.WorkerID)

	hbTick := time.NewTicker(r.cfg.HeartbeatEvery)
	defer hbTick.Stop()

	for {
		select {
		case <-ctx.Done():
			return r.shutdown()
		case <-hbTick.C:
			if err := r.client.Heartbeat(ctx, r.cfg.WorkerID, r.cfg.Capabilities); err != nil {
				r.log("heartbeat: %v", err)
			}
		}
	}
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

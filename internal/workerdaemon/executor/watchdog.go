package executor

// watchdog.go — F5 (watchdog) stall detection + graceful kill (design §9).
//
// An executor refreshes status.last_progress_at every time it streams progress
// (run.go). The orchestrator's watchdog watches that timestamp: when it goes
// stale beyond StallTimeout while the executor still claims state=running, the
// executor is judged STALLED and killed gracefully (SIGTERM → grace → SIGKILL).
// The kill makes the executor's process exit, so the normal completion path
// (monitor.AwaitCompletion) then classifies it as a failure (§9 "按失败处理").
//
// The watchdog itself never decides the Outcome — it only ends a stuck process;
// classification stays with completion.go's single source of truth.

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// DefaultStallTimeout is how long status.last_progress_at may stand still before
// a running executor is judged stalled. Chosen well above a normal progress
// cadence so a slow-but-live step is not killed. Raised 5m→10m (T877): with the
// runner now heart-beating on streamed stdout (run.go), an active claude refreshes
// progress continuously; the larger window is defense-in-depth for the narrow case
// of a single long SILENT command (e.g. a first `go build`) that emits no stdout
// for minutes. (A deeper "child-process-alive also heartbeats" fix is a follow-up.)
const DefaultStallTimeout = 10 * time.Minute

// DefaultGraceTimeout is how long a SIGTERM'd executor is given to exit on its
// own before the watchdog escalates to SIGKILL.
const DefaultGraceTimeout = 10 * time.Second

// Watchdog detects stalled executors and kills them gracefully. It is stateless
// beyond its config + injected clock/sleeper, so one instance is safe to share.
type Watchdog struct {
	stall time.Duration
	grace time.Duration
	clk   clock.Clock
	// sleep blocks for d (the SIGTERM→SIGKILL grace window). Injected so tests do
	// not actually sleep; defaults to clock.SleepWith(clk, d).
	sleep func(d time.Duration)
}

// WatchdogConfig configures a Watchdog. Zero/negative durations fall back to the
// package defaults.
type WatchdogConfig struct {
	StallTimeout time.Duration
	GraceTimeout time.Duration
	Clock        clock.Clock
	// Sleep overrides the grace-window sleeper (tests). Nil → clock.SleepWith.
	Sleep func(d time.Duration)
}

// NewWatchdog builds a Watchdog from cfg, applying defaults.
func NewWatchdog(cfg WatchdogConfig) *Watchdog {
	stall := cfg.StallTimeout
	if stall <= 0 {
		stall = DefaultStallTimeout
	}
	grace := cfg.GraceTimeout
	if grace <= 0 {
		grace = DefaultGraceTimeout
	}
	clk := cfg.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = func(d time.Duration) { clock.SleepWith(clk, d) }
	}
	return &Watchdog{stall: stall, grace: grace, clk: clk, sleep: sleep}
}

// StallTimeout exposes the configured stall threshold.
func (w *Watchdog) StallTimeout() time.Duration { return w.stall }

// StallVerdict is the result of one liveness check against a status file.
type StallVerdict struct {
	// Stalled is true when a running executor's progress is older than StallTimeout.
	Stalled bool
	// Idle is how long since last_progress_at at the moment of the check.
	Idle time.Duration
}

// Check judges a single status against now. Only a state=running executor can be
// stalled — a done/failed status is terminal and handled by completion, never
// killed (idempotency: re-checking a finished executor is a no-op verdict).
func (w *Watchdog) Check(st Status, now time.Time) StallVerdict {
	if st.State != StateRunning {
		return StallVerdict{}
	}
	idle := now.Sub(st.LastProgressAt)
	return StallVerdict{Stalled: idle > w.stall, Idle: idle}
}

// GracefulKill ends a stalled executor: SIGTERM to its process group, a grace
// window for clean shutdown, then SIGKILL to guarantee termination (design §9
// "graceful kill"). The final SIGKILL tolerates ESRCH — the executor exiting
// during the grace window is the success case, not an error.
func (w *Watchdog) GracefulKill(ctx context.Context, h *Handle) error {
	if h == nil {
		return errors.New("executor: watchdog kill nil handle")
	}
	if err := h.Terminate(); err != nil && !isNoSuchProcess(err) {
		return fmt.Errorf("executor: watchdog SIGTERM %s: %w", h.ExecutorID, err)
	}
	// Wait the grace window (or until the caller's context is cancelled) before
	// escalating, so a well-behaved executor can flush output.json + status=failed.
	w.sleepCtx(ctx)
	if err := h.Kill(); err != nil && !isNoSuchProcess(err) {
		return fmt.Errorf("executor: watchdog SIGKILL %s: %w", h.ExecutorID, err)
	}
	return nil
}

// sleepCtx blocks for the grace window but returns early if ctx is cancelled.
func (w *Watchdog) sleepCtx(ctx context.Context) {
	if ctx == nil {
		w.sleep(w.grace)
		return
	}
	done := make(chan struct{})
	go func() {
		w.sleep(w.grace)
		close(done)
	}()
	select {
	case <-ctx.Done():
	case <-done:
	}
}

// isNoSuchProcess reports whether err is a "process already gone" signal result
// (ESRCH), which the kill path tolerates as the desired end state.
func isNoSuchProcess(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}

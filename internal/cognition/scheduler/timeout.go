package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TimeoutConfig captures the knobs.
type TimeoutConfig struct {
	// TickInterval is the scan cadence (default 1s in tests, 5s in prod).
	TickInterval time.Duration
	// Grace is the SIGTERM → SIGKILL window (default 5s).
	Grace time.Duration
}

// DefaultTimeoutConfig returns v1 defaults.
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		TickInterval: 5 * time.Second,
		Grace:        5 * time.Second,
	}
}

// TimeoutHandler kills + transitions running invocations that exceed
// their hard timeout (plan-6 § 3.9).
type TimeoutHandler struct {
	cfg     TimeoutConfig
	db      *sql.DB
	repo    cognition.SupervisorInvocationRepository
	spawner *Spawner
	sink    *observability.EventSink
	clk     clock.Clock
}

// NewTimeoutHandler wires a handler.
func NewTimeoutHandler(
	cfg TimeoutConfig,
	db *sql.DB,
	repo cognition.SupervisorInvocationRepository,
	spawner *Spawner,
	sink *observability.EventSink,
	clk clock.Clock,
) (*TimeoutHandler, error) {
	if db == nil {
		return nil, errors.New("timeout: db required")
	}
	if repo == nil {
		return nil, errors.New("timeout: repo required")
	}
	if sink == nil {
		return nil, errors.New("timeout: sink required")
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 5 * time.Second
	}
	if cfg.Grace == 0 {
		cfg.Grace = 5 * time.Second
	}
	return &TimeoutHandler{cfg: cfg, db: db, repo: repo, spawner: spawner, sink: sink, clk: clk}, nil
}

// Tick scans running invocations for timeout and signals as needed. Returns
// the count of invocations transitioned to timed_out.
func (h *TimeoutHandler) Tick(ctx context.Context) (int, error) {
	rows, err := h.repo.FindRunning(ctx)
	if err != nil {
		return 0, err
	}
	now := h.clk.Now()
	transitioned := 0
	for _, inv := range rows {
		deadline := inv.StartedAt().Add(time.Duration(inv.HardTimeoutSeconds()) * time.Second)
		if now.Before(deadline) {
			continue
		}
		// Try to SIGTERM live process; spawner=nil means we're running in
		// a test or daemon-less context, so we just write the terminal
		// row.
		if h.spawner != nil {
			h.spawner.SignalAndKill(inv.ID(), h.cfg.Grace, h.clk)
		}
		// Re-fetch in case finalize already wrote a different terminal.
		fresh, ferr := h.repo.FindByID(ctx, inv.ID())
		if ferr != nil {
			continue
		}
		if fresh.IsTerminal() {
			continue
		}
		if merr := fresh.MarkTimedOut(now); merr != nil {
			continue
		}
		err := persistence.RunInTx(ctx, h.db, func(txCtx context.Context) error {
			if uerr := h.repo.UpdateStatusToTerminal(txCtx, fresh); uerr != nil {
				return uerr
			}
			_, eerr := h.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "supervisor.invocation_timed_out",
				Refs:      refsForScope(fresh.Scope()),
				Actor:     observability.Actor("supervisor:" + string(fresh.ID())),
				Payload: map[string]any{
					"invocation_id":        string(fresh.ID()),
					"timed_out_at":         now.UTC().Format(time.RFC3339Nano),
					"hard_timeout_seconds": fresh.HardTimeoutSeconds(),
				},
				CorrelationID: string(fresh.ID()),
			})
			return eerr
		})
		if err != nil {
			continue
		}
		transitioned++
	}
	return transitioned, nil
}

// Run blocks until ctx done, scanning every TickInterval.
func (h *TimeoutHandler) Run(ctx context.Context, errorHook func(error)) error {
	if errorHook == nil {
		errorHook = func(error) {}
	}
	t := time.NewTicker(h.cfg.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if _, err := h.Tick(ctx); err != nil {
				errorHook(err)
			}
		}
	}
}

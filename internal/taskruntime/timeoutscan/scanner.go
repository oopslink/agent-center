// Package timeoutscan hosts the TimeoutScanner domain service that runs
// the 4 classes of timeout (00-overview § 3.3).
package timeoutscan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/kill"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
)

// Config bundles all four timeout knobs (04-configuration § 7.6).
type Config struct {
	SubmittedTimeout    time.Duration // default 5min
	ExecutionTimeout    time.Duration // default 6h
	InputRequestPingT1  time.Duration // default 4h
	InputRequestTimeoutT2 time.Duration // default 24h
	WorkerHeartbeatTimeout time.Duration // default 60s
	TickInterval        time.Duration // default 30s
}

// DefaultConfig returns v1 defaults.
func DefaultConfig() Config {
	return Config{
		SubmittedTimeout:       5 * time.Minute,
		ExecutionTimeout:       6 * time.Hour,
		InputRequestPingT1:     4 * time.Hour,
		InputRequestTimeoutT2:  24 * time.Hour,
		WorkerHeartbeatTimeout: 60 * time.Second,
		TickInterval:           30 * time.Second,
	}
}

// Scanner is the TimeoutScanner domain service.
type Scanner struct {
	db          *sql.DB
	execRepo    execution.Repository
	taskRepo    task.Repository
	irRepo      inputrequest.Repository
	sink        *observability.EventSink
	killCoord   *kill.Coordinator
	clock       clock.Clock
	cfg         Config
}

// NewScanner constructs a TimeoutScanner.
func NewScanner(
	db *sql.DB,
	execRepo execution.Repository,
	taskRepo task.Repository,
	irRepo inputrequest.Repository,
	sink *observability.EventSink,
	killCoord *kill.Coordinator,
	clk clock.Clock,
	cfg Config,
) *Scanner {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 30 * time.Second
	}
	if cfg.SubmittedTimeout == 0 {
		cfg.SubmittedTimeout = 5 * time.Minute
	}
	if cfg.ExecutionTimeout == 0 {
		cfg.ExecutionTimeout = 6 * time.Hour
	}
	if cfg.InputRequestTimeoutT2 == 0 {
		cfg.InputRequestTimeoutT2 = 24 * time.Hour
	}
	if cfg.InputRequestPingT1 == 0 {
		cfg.InputRequestPingT1 = 4 * time.Hour
	}
	return &Scanner{
		db:        db,
		execRepo:  execRepo,
		taskRepo:  taskRepo,
		irRepo:    irRepo,
		sink:      sink,
		killCoord: killCoord,
		clock:     clk,
		cfg:       cfg,
	}
}

// Tick performs one cycle of all 4 timeout scans. The Run loop wraps Tick
// in a ticker; tests call Tick directly with a FakeClock advanced.
func (s *Scanner) Tick(ctx context.Context, actor observability.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if err := s.scanSubmittedTimeout(ctx, actor); err != nil {
		return fmt.Errorf("submitted_timeout: %w", err)
	}
	if err := s.scanExecutionTimeout(ctx, actor); err != nil {
		return fmt.Errorf("execution_timeout: %w", err)
	}
	if err := s.scanInputRequestTimeout(ctx, actor); err != nil {
		return fmt.Errorf("input_timeout: %w", err)
	}
	return nil
}

// Run blocks until ctx done, calling Tick at TickInterval.
func (s *Scanner) Run(ctx context.Context, actor observability.Actor) error {
	t := time.NewTicker(s.cfg.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := s.Tick(ctx, actor); err != nil {
				_, _ = s.sink.Emit(ctx, observability.EmitCommand{
					EventType: "task_execution.timeout_scan_failed",
					Actor:     actor,
					Payload: map[string]any{
						"reason":  "timeout_scan_failed",
						"message": err.Error(),
					},
				})
			}
		}
	}
}

func (s *Scanner) scanSubmittedTimeout(ctx context.Context, actor observability.Actor) error {
	cutoff := s.clock.Now().Add(-s.cfg.SubmittedTimeout).UTC().Format(time.RFC3339Nano)
	overdues, err := s.execRepo.FindSubmittedOlderThan(ctx, cutoff)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, e := range overdues {
		if e.IsTerminal() {
			continue
		}
		txErr := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			fresh, err := s.execRepo.FindByID(txCtx, e.ID())
			if err != nil {
				return err
			}
			if fresh.IsTerminal() || fresh.Status() != execution.StatusSubmitted {
				return nil
			}
			if err := fresh.MarkFailed(execution.FailedSubmittedTimeout,
				fmt.Sprintf("execution stuck in submitted longer than %s", s.cfg.SubmittedTimeout), now); err != nil {
				return err
			}
			if err := s.execRepo.Update(txCtx, fresh); err != nil {
				return err
			}
			if err := s.clearTaskCurrent(txCtx, fresh.TaskID(), now); err != nil {
				return err
			}
			_, err = s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "task_execution.failed",
				Refs: observability.EventRefs{
					TaskID:      string(fresh.TaskID()),
					ExecutionID: string(fresh.ID()),
				},
				Actor: actor,
				Payload: map[string]any{
					"execution_id": string(fresh.ID()),
					"reason":       string(execution.FailedSubmittedTimeout),
					"message":      fresh.FailedMessage(),
				},
			})
			return err
		})
		if txErr != nil {
			return txErr
		}
	}
	return nil
}

func (s *Scanner) scanExecutionTimeout(ctx context.Context, actor observability.Actor) error {
	actives, err := s.execRepo.FindActive(ctx)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, e := range actives {
		if e.Status() != execution.StatusWorking {
			continue
		}
		// Compute accumulated working duration. Worker reports working_seconds
		// via projection in Phase 4; in Phase 2 we approximate using
		// working_started_at if present.
		acc := time.Duration(e.WorkingSecondsAccumulated()) * time.Second
		if ws := e.WorkingStartedAt(); ws != nil {
			acc += now.Sub(*ws)
		}
		limit := s.cfg.ExecutionTimeout
		if override := e.ExecutionTimeoutOverride(); override != nil {
			limit = *override
		}
		if acc < limit {
			continue
		}
		// Trigger KillCoordinator with timeout_kill reason.
		killErr := s.killCoord.RequestKill(ctx, e.ID(), execution.KilledTimeoutKill,
			fmt.Sprintf("execution exceeded timeout (%s)", limit), actor)
		if killErr != nil && !errors.Is(killErr, execution.ErrTaskExecutionAlreadyTerminated) {
			return killErr
		}
	}
	return nil
}

func (s *Scanner) scanInputRequestTimeout(ctx context.Context, actor observability.Actor) error {
	now := s.clock.Now()
	// T2 = hard fail
	t2Cutoff := now.Add(-s.cfg.InputRequestTimeoutT2)
	t2List, err := s.irRepo.FindPending(ctx, t2Cutoff)
	if err != nil {
		return err
	}
	for _, ir := range t2List {
		txErr := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			fresh, err := s.irRepo.FindByID(txCtx, ir.ID())
			if err != nil {
				return err
			}
			if fresh.Status() != inputrequest.StatusPending {
				return nil
			}
			msg := fmt.Sprintf("input request exceeded T2 timeout (%s)", s.cfg.InputRequestTimeoutT2)
			if err := fresh.MarkTimedOut("input_timeout_t2", msg, now); err != nil {
				return err
			}
			if err := s.irRepo.Update(txCtx, fresh); err != nil {
				return err
			}
			if _, err := s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "input_request.timed_out",
				Refs: observability.EventRefs{
					InputRequestID: string(fresh.ID()),
					ExecutionID:    string(fresh.TaskExecutionID()),
				},
				Actor: actor,
				Payload: map[string]any{
					"input_request_id": string(fresh.ID()),
					"reason":           "input_timeout_t2",
					"message":          msg,
				},
			}); err != nil {
				return err
			}
			// Cascade: execution → failed(input_timeout)
			e, err := s.execRepo.FindByID(txCtx, fresh.TaskExecutionID())
			if err != nil {
				return err
			}
			if e.IsTerminal() {
				return nil
			}
			if err := e.MarkFailed(execution.FailedInputTimeout, msg, now); err != nil {
				return err
			}
			if err := s.execRepo.Update(txCtx, e); err != nil {
				return err
			}
			if err := s.clearTaskCurrent(txCtx, e.TaskID(), now); err != nil {
				return err
			}
			_, err = s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "task_execution.failed",
				Refs: observability.EventRefs{
					TaskID:      string(e.TaskID()),
					ExecutionID: string(e.ID()),
				},
				Actor: actor,
				Payload: map[string]any{
					"execution_id": string(e.ID()),
					"reason":       string(execution.FailedInputTimeout),
					"message":      msg,
				},
			})
			return err
		})
		if txErr != nil {
			return txErr
		}
	}
	// T1 = ping (best-effort signal; v1 only emits an audit event)
	t1Cutoff := now.Add(-s.cfg.InputRequestPingT1)
	t1List, err := s.irRepo.FindPending(ctx, t1Cutoff)
	if err != nil {
		return err
	}
	for _, ir := range t1List {
		// Skip those already past T2 (already handled)
		if !ir.RequestedAt().Before(t2Cutoff) {
			_, _ = s.sink.Emit(ctx, observability.EmitCommand{
				EventType: "input_request.ping_t1",
				Refs: observability.EventRefs{
					InputRequestID: string(ir.ID()),
					ExecutionID:    string(ir.TaskExecutionID()),
				},
				Actor: actor,
				Payload: map[string]any{
					"input_request_id": string(ir.ID()),
				},
			})
		}
	}
	return nil
}

// HandleWorkerOffline marks all active executions on a worker as
// failed(worker_lost). Called from Workforce BC's worker.offline event.
func (s *Scanner) HandleWorkerOffline(ctx context.Context, workerID string, actor observability.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	actives, err := s.execRepo.FindByWorkerID(ctx, workerID,
		execution.StatusSubmitted, execution.StatusWorking, execution.StatusInputRequired)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, e := range actives {
		txErr := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			fresh, err := s.execRepo.FindByID(txCtx, e.ID())
			if err != nil {
				return err
			}
			if fresh.IsTerminal() {
				return nil
			}
			msg := "worker " + workerID + " went offline"
			if err := fresh.MarkFailed(execution.FailedWorkerLost, msg, now); err != nil {
				return err
			}
			if err := s.execRepo.Update(txCtx, fresh); err != nil {
				return err
			}
			if err := s.clearTaskCurrent(txCtx, fresh.TaskID(), now); err != nil {
				return err
			}
			_, err = s.sink.Emit(txCtx, observability.EmitCommand{
				EventType: "task_execution.failed",
				Refs: observability.EventRefs{
					TaskID:      string(fresh.TaskID()),
					ExecutionID: string(fresh.ID()),
					WorkerID:    fresh.WorkerID(),
				},
				Actor: actor,
				Payload: map[string]any{
					"execution_id": string(fresh.ID()),
					"reason":       string(execution.FailedWorkerLost),
					"message":      msg,
				},
			})
			return err
		})
		if txErr != nil {
			return txErr
		}
	}
	return nil
}

func (s *Scanner) clearTaskCurrent(txCtx context.Context, taskID taskruntime.TaskID, now time.Time) error {
	t, err := s.taskRepo.FindByID(txCtx, taskID)
	if err != nil {
		if errors.Is(err, task.ErrTaskNotFound) {
			return nil
		}
		return err
	}
	if string(t.CurrentExecutionID()) == "" {
		return nil
	}
	t.ClearCurrentExecutionID(now)
	return s.taskRepo.Update(txCtx, t)
}

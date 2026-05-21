package projection

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// TaskExecutionExistenceChecker is the port the projection service uses to
// validate that the task execution referenced by a push exists (application-
// layer referential integrity — conventions § 9.w).
type TaskExecutionExistenceChecker interface {
	TaskExecutionExists(ctx context.Context, id taskruntime.TaskExecutionID) (bool, error)
}

// ErrTaskExecutionNotFound is returned by UpdateProjection when the caller
// references a non-existent execution. Caller surfaces this to the
// report-progress CLI which translates to ExitNotFound (17).
var ErrTaskExecutionNotFound = errors.New("observability/projection: task execution not found")

// TaskExecutionProjectionService is the Domain Service that owns writes to
// the task_execution_projections table (BC-owned per conventions § 9.z).
//
// Per plan-4 § 3.1: worker daemon push path goes through this service →
// Repository.UpsertIfFresh; staleness emits
// observability.projection_stale_drop (same tx as the dropped write attempt).
type TaskExecutionProjectionService struct {
	repo    Repository
	sink    *observability.EventSink
	checker TaskExecutionExistenceChecker
	clk     clock.Clock
}

// NewTaskExecutionProjectionService wires the service. checker may be nil
// for tests / contexts where the caller already validated existence; in
// production the worker-daemon push path should pass a real checker.
func NewTaskExecutionProjectionService(repo Repository, sink *observability.EventSink, checker TaskExecutionExistenceChecker, clk clock.Clock) *TaskExecutionProjectionService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &TaskExecutionProjectionService{repo: repo, sink: sink, checker: checker, clk: clk}
}

// UpdateProjection applies a worker-daemon push.
//
// Path:
//  1. Validate input + (optionally) verify execution exists.
//  2. Repository.UpsertIfFresh.
//     - ErrProjectionStale → emit observability.projection_stale_drop,
//       return nil (the drop is observable and is not surfaced as an
//       error to caller, mirroring the "ignore-but-emit" pattern in
//       conventions § 17 white list).
//     - other error → return as-is.
//  3. Success → return nil.
func (s *TaskExecutionProjectionService) UpdateProjection(ctx context.Context, id taskruntime.TaskExecutionID, update ProjectionUpdate) error {
	if s == nil || s.repo == nil {
		return errors.New("projection service: nil receiver / repo")
	}
	if id == "" {
		return errors.New("projection service: task_execution_id required")
	}
	if err := update.Validate(); err != nil {
		return err
	}
	if s.checker != nil {
		ok, err := s.checker.TaskExecutionExists(ctx, id)
		if err != nil {
			return fmt.Errorf("projection service: existence check: %w", err)
		}
		if !ok {
			return ErrTaskExecutionNotFound
		}
	}
	existing, fresh, err := s.repo.UpsertIfFresh(ctx, id, update)
	if errors.Is(err, ErrProjectionStale) {
		if s.sink != nil {
			_, emitErr := s.sink.Emit(ctx, observability.EmitCommand{
				EventType: "observability.projection_stale_drop",
				Refs:      observability.EventRefs{ExecutionID: string(id)},
				Actor:     observability.Actor("system"),
				Payload: map[string]any{
					"execution_id":           string(id),
					"dropped_at":             s.clk.Now().UTC().Format(time.RFC3339Nano),
					"existing_last_push_at":  existing.LastPushAt.UTC().Format(time.RFC3339Nano),
					"incoming_last_push_at":  update.LastPushAt.UTC().Format(time.RFC3339Nano),
					"reason":                 "out_of_order_push",
					"message":                fmt.Sprintf("incoming push %s older than existing %s", update.LastPushAt.UTC().Format(time.RFC3339Nano), existing.LastPushAt.UTC().Format(time.RFC3339Nano)),
				},
			})
			if emitErr != nil {
				return fmt.Errorf("projection service: stale drop emit: %w", emitErr)
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	_ = fresh
	return nil
}

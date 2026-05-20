// Package service hosts Workforce BC domain services
// (workforce/00 § 3 + plan-1 § 3.4).
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// WorkerEnrollService enrolls a new Worker into the system
// (workforce/00 § 3.1, plan § 3.4.1).
//
// Phase 1 simplification (plan § 6 R5): we skip real bootstrap/session
// token exchange. The CLI hands us a WorkerID + capabilities; we Save the
// Worker and emit `workforce.worker.enrolled` in the same tx.
type WorkerEnrollService struct {
	db    *sql.DB
	repo  workforce.WorkerRepository
	sink  *observability.EventSink
	clock clock.Clock
}

// NewWorkerEnrollService constructs the service.
func NewWorkerEnrollService(db *sql.DB, repo workforce.WorkerRepository, sink *observability.EventSink, clk clock.Clock) *WorkerEnrollService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WorkerEnrollService{db: db, repo: repo, sink: sink, clock: clk}
}

// EnrollCommand captures the CLI input.
type EnrollCommand struct {
	WorkerID       workforce.WorkerID
	Capabilities   []string
	ActorIdentity  observability.Actor
}

// EnrollResult is what the service returns.
type EnrollResult struct {
	WorkerID workforce.WorkerID
	EventID  observability.EventID
	Version  int
}

// Enroll persists the worker and emits a domain event in one tx.
func (s *WorkerEnrollService) Enroll(ctx context.Context, cmd EnrollCommand) (EnrollResult, error) {
	if err := cmd.ActorIdentity.Validate(); err != nil {
		return EnrollResult{}, fmt.Errorf("enroll: %w", err)
	}
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:           cmd.WorkerID,
		Capabilities: cmd.Capabilities,
		EnrolledAt:   s.clock.Now(),
	})
	if err != nil {
		return EnrollResult{}, err
	}
	var (
		eventID observability.EventID
	)
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.repo.Save(txCtx, w); err != nil {
			return err
		}
		evID, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "workforce.worker.enrolled",
			Refs:      observability.EventRefs{WorkerID: string(w.ID())},
			Actor:     cmd.ActorIdentity,
			Payload: map[string]any{
				"worker_id":    string(w.ID()),
				"capabilities": w.Capabilities(),
			},
		})
		if err != nil {
			return err
		}
		eventID = evID
		return nil
	})
	if err != nil {
		return EnrollResult{}, err
	}
	return EnrollResult{
		WorkerID: w.ID(),
		EventID:  eventID,
		Version:  w.Version(),
	}, nil
}

// Static error so callers can detect the "service uninitialised" case via
// errors.Is. Tests rely on this rather than string matching.
var ErrEnrollMisconfigured = errors.New("workforce: enroll service misconfigured (nil dep)")

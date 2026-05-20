package reconcile

import (
	"context"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// Service is the ReconcileService domain service (00-overview § 3.2).
type Service struct {
	execRepo execution.Repository
}

// NewService constructs the ReconcileService.
func NewService(execRepo execution.Repository) *Service {
	return &Service{execRepo: execRepo}
}

// Handle classifies the worker's claimed-active executions into 3 groups
// (active / stale / unknown).
func (s *Service) Handle(ctx context.Context, req Request) (Response, error) {
	if strings.TrimSpace(req.WorkerID) == "" {
		return Response{}, errors.New("reconcile: worker_id required")
	}
	// Build set of worker's claimed actives
	claimed := make(map[taskruntime.TaskExecutionID]struct{}, len(req.LocalActives))
	for _, la := range req.LocalActives {
		claimed[la.ExecutionID] = struct{}{}
	}
	// Look up center-side active executions for this worker
	centerActives, err := s.execRepo.FindByWorkerID(ctx, req.WorkerID,
		execution.StatusSubmitted, execution.StatusWorking, execution.StatusInputRequired)
	if err != nil {
		return Response{}, err
	}
	centerSet := make(map[taskruntime.TaskExecutionID]struct{}, len(centerActives))
	for _, e := range centerActives {
		centerSet[e.ID()] = struct{}{}
	}

	resp := Response{}
	// Active: in both
	// Stale: claimed by worker but center says terminal or not assigned to this worker
	for execID := range claimed {
		if _, ok := centerSet[execID]; ok {
			resp.Active = append(resp.Active, execID)
			continue
		}
		// Center has no active record. Could be:
		// - execution exists but terminal (stale)
		// - execution doesn't exist or belongs to other worker (unknown)
		e, err := s.execRepo.FindByID(ctx, execID)
		if err != nil {
			if errors.Is(err, execution.ErrTaskExecutionNotFound) {
				resp.Unknown = append(resp.Unknown, execID)
				continue
			}
			return Response{}, err
		}
		if e.WorkerID() != req.WorkerID {
			resp.Unknown = append(resp.Unknown, execID)
			continue
		}
		// Terminal → stale
		resp.Stale = append(resp.Stale, execID)
	}
	// Center actives not claimed by worker — also stale (worker forgot
	// them; center expected them but worker doesn't report; we treat as
	// "active still per center" but log as gap to user).
	for execID := range centerSet {
		if _, ok := claimed[execID]; !ok {
			// Center thinks active; worker didn't claim. Treat as active
			// from worker's perspective (worker will accept enrollment
			// without doing anything; supervisor follows up with health
			// check / timeout).
			resp.Active = append(resp.Active, execID)
		}
	}
	return resp, nil
}

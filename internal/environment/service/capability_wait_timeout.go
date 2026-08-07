package service

import (
	"context"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
)

type CapabilityWaitTimeoutReconciler struct {
	waits environment.CapabilityWaitRepository
	clock clock.Clock
	limit int
}

func NewCapabilityWaitTimeoutReconciler(waits environment.CapabilityWaitRepository, clk clock.Clock, limit int) *CapabilityWaitTimeoutReconciler {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if limit <= 0 {
		limit = 100
	}
	return &CapabilityWaitTimeoutReconciler{waits: waits, clock: clk, limit: limit}
}

func (r *CapabilityWaitTimeoutReconciler) ReconcileOnce(ctx context.Context) (int, error) {
	if r == nil || r.waits == nil {
		return 0, nil
	}
	now := r.clock.Now().UTC()
	expired, err := r.waits.ListExpiredWaiting(ctx, now, r.limit)
	if err != nil {
		return 0, err
	}
	for _, wait := range expired {
		if err := r.waits.MarkTimedOut(ctx, wait.TaskID, wait.AgentID, "capability_wait_timeout", now); err != nil {
			return 0, err
		}
	}
	return len(expired), nil
}

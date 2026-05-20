package workerdaemon

import (
	"context"
	"errors"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/reconcile"
)

// ReconcileResponder handles a ReconcileResponse on the worker side
// (02-task-execution § 11): SIGTERM stale/unknown agents, emit
// audit-only killed events.
type ReconcileResponder struct {
	uploader    DispatchUploader
	killHandler func(executionID string, reason execution.KilledReason) error
}

// NewReconcileResponder constructs a responder. killHandler is the
// daemon-local action (e.g. SIGTERM the shim process for execID).
func NewReconcileResponder(uploader DispatchUploader, killHandler func(string, execution.KilledReason) error) *ReconcileResponder {
	if uploader == nil {
		uploader = NoopUploader{}
	}
	return &ReconcileResponder{uploader: uploader, killHandler: killHandler}
}

// Apply processes the center's reconcile response.
func (r *ReconcileResponder) Apply(ctx context.Context, resp reconcile.Response) error {
	if r.killHandler == nil {
		return errors.New("workerdaemon: nil killHandler")
	}
	for _, execID := range resp.Stale {
		if err := r.killHandler(string(execID), execution.KilledReconcileStale); err != nil {
			return err
		}
	}
	for _, execID := range resp.Unknown {
		if err := r.killHandler(string(execID), execution.KilledReconcileUnknown); err != nil {
			return err
		}
	}
	_ = ctx
	return nil
}

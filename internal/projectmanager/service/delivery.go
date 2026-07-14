package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// RecordDelivery persists the last forked executor's terminal git status for a task
// (issue-f30b7e7b). It is called by the report_delivery agent-tool from the worker's
// CenterWriteback after the executor finishes, so downstream (the writeback auto-block
// B②, and audit) can tell a durable pushed delivery from a zero-delivery run
// (committed-but-not-pushed / no-commit) that must be auto-blocked rather than
// re-nudged/re-dispatched.
//
// Latest-wins: a terminal report overwrites any prior one. It is a pure sidecar write —
// it does NOT touch the task's status / lease / updated_at, so it never perturbs the
// stuck-node liveness accounting or the state machine. Best-effort by contract (the
// worker swallows errors), so it must never wedge the agent loop.
//
// Only the task's own assignee — the agent whose executor produced the delivery — may
// report it (defense in depth against a cross-agent write; mirrors the WorkerRenewLease
// / HeartbeatTask trust boundary).
func (s *Service) RecordDelivery(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef, d *pm.Delivery) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if t.Assignee() != actor {
			return pm.ErrNotTaskAssignee
		}
		t.SetDelivery(d)
		return s.tasks.Update(txCtx, t)
	})
}

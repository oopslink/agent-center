package service

import (
	"context"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Assign/state-flow AppServices (B2-c). All write ONLY ProjectManager state +
// an outbox event in one tx (OQ1). The cross-BC effects — creating/superseding
// AgentWorkItems and syncing Conversation participants — are handled by the
// projectors (ParticipantProjector + WorkItemProjector) consuming these events.

// AssignTask assigns (or reassigns) a Task to an identity. open→assigned, or
// re-target an already-assigned Task. On reassign the previous assignee leaves
// the effective subscriber set unless they are creator or a manual subscriber
// (falls out of EffectiveTaskSubscribers automatically). Emits pm.task.assigned
// (first assignment) or pm.task.reassigned.
func (s *Service) AssignTask(ctx context.Context, taskID pm.TaskID, assignee, actor pm.IdentityRef) error {
	if err := assignee.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		prev := t.Assignee()
		if err := t.Assign(assignee, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		evt := EvtTaskAssigned
		if prev != "" {
			evt = EvtTaskReassigned
		}
		return s.emitTaskAssignEvent(txCtx, t, evt, string(prev))
	})
}

// StartTask moves an assigned Task to running (the explicit "picked up/started"
// transition — needed before block/complete are reachable).
func (s *Service) StartTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error { return t.Start(now) }, "")
}

// CancelTask cancels a non-terminal Task.
func (s *Service) CancelTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error { return t.Cancel(now) }, "")
}

// BlockTask moves running→blocked with a required reason (plan §2.2).
func (s *Service) BlockTask(ctx context.Context, taskID pm.TaskID, reason string, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error { return t.Block(reason, now) }, reason)
}

// UnblockTask moves blocked→running. Per the locked口径, unblocking re-dispatches
// the work: it emits pm.task.reassigned so the WorkItemProjector supersedes the
// old WorkItem and creates a new one (AgentWorkItem.blocked has no return edge).
func (s *Service) UnblockTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if err := t.Unblock(now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		return s.emitTaskAssignEvent(txCtx, t, EvtTaskReassigned, string(t.Assignee()))
	})
}

// CompleteTask moves running→completed and records the completer.
func (s *Service) CompleteTask(ctx context.Context, taskID pm.TaskID, by pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, by, func(t *pm.Task, now time.Time) error { return t.Complete(by, now) }, "")
}

// VerifyTask moves completed→verified. The verifier must NOT be the completer
// (ErrSelfVerify from the AR).
func (s *Service) VerifyTask(ctx context.Context, taskID pm.TaskID, by pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, by, func(t *pm.Task, now time.Time) error { return t.Verify(by, now) }, "")
}

// taskStateOp is the shared "load → gate → mutate → persist → emit
// state_changed" path for status-only transitions.
func (s *Service) taskStateOp(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef, mutate func(*pm.Task, time.Time) error, reason string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if err := mutate(t, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		return s.emitTaskStateChanged(txCtx, t, reason)
	})
}

// emitTaskAssignEvent emits an assign/reassign event carrying the current
// assignee, previous assignee, and the recomputed effective subscriber set.
func (s *Service) emitTaskAssignEvent(ctx context.Context, t *pm.Task, evt, previous string) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, evt,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()),
			PreviousAssignee: previous, Status: string(t.Status()),
			EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
		})
}

// emitTaskStateChanged emits pm.task.state_changed (no participant change).
func (s *Service) emitTaskStateChanged(ctx context.Context, t *pm.Task, reason string) error {
	return s.emit(ctx, EvtTaskStateChanged,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()),
			Status: string(t.Status()), Reason: reason,
		})
}

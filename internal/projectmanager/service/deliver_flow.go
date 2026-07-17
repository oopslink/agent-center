package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// deliver_flow.go — the I107 ① application entrypoints for the `delivered` non-terminal
// state: DeliverTask (running→delivered, the assignee hands the work to an EXTERNAL
// acceptance) and ReworkTask (delivered→running, the acceptance rejected it).
//
// WHY this exists at all: the orchestration reality is multi-segment (deliver → review →
// accept → merge), but the task model only had states for "an executor is running" and
// "it is over". A task whose work was done but unaccepted had no true state, so callers
// picked the least-wrong one: `running` (nothing is running), `completed` (a FALSE GREEN
// — nobody accepted it, yet every complete-side downstream fires), or `blocked` (a FALSE
// ALARM — reads as "stuck, come rescue me" when nothing is wrong). Each of those is a
// lie the system then acts on. `delivered` is the missing true state.
//
// It deliberately mirrors BlockTask's shape (same tx, same membership/mutable gates, same
// state_changed emission) because it has the same job: leave `running` in a way that
// stops dispatch WITHOUT claiming the work is concluded. The difference is only what it
// asserts — "handed over, waiting on a verdict" vs "stuck, needs a human".

// DeliverTask moves a RUNNING task to `delivered` (I107 ①): its assignee reports the work
// is done and handed over, and it now waits on an acceptance (review / verification /
// merge) that is somebody else's call.
//
// It is NOT a completion and does not pretend to be: `delivered` is non-terminal, so no
// complete-side downstream (issue-derived-done rollup, plan advance, stage-barrier
// release) fires — an un-accepted delivery must not turn a board green. It is NOT a block
// either: no blocked_reason is written, so the overdue-block escalation does not treat a
// healthy delivery as an incident needing rescue.
//
// The task KEEPS its assignee (a reject comes back to them via ReworkTask) but releases
// its run slot — the executor is gone, so pinning the agent behind a queue it cannot
// advance would strand a live agent. The domain (Task.Deliver) enforces assignee-only +
// a non-empty summary; this layer adds the project gates and the event/audit trail.
func (s *Service) DeliverTask(ctx context.Context, taskID pm.TaskID, summary string, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status()
		// agentRef is the task's own assignee — mirrors BlockTask: start_work already
		// established that the running agent owns the run, so the domain's assignee check is
		// satisfied from the row rather than requiring actor==assignee at this layer.
		agentRef := t.Assignee()
		if err := t.Deliver(summary, agentRef, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		// Persist the `delivered` action-log entry. This is the ONLY storage of the delivery
		// summary (there is no delivery_summary column on purpose — the log already is an
		// append-only per-task record, so the summary needs no schema change to survive).
		if err := s.flushActionLogs(txCtx, t); err != nil {
			return err
		}
		// The state_changed emission is load-bearing beyond notification: the dispatch-wake
		// re-push arm keys on it to hand this now-free agent its next OPEN task (the same way
		// it reacts to a block). Without it a delivering agent would idle until the next
		// 60s sweep.
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, summary); err != nil {
			return err
		}
		s.auditTaskDelivered(txCtx, t, summary, actor)
		return nil
	})
}

// ReworkTask moves a DELIVERED task back to `running` (I107 ①): the external acceptance
// REJECTED the delivery, so the work returns to its (unchanged) assignee with the reject
// note in blocked_comment — the same channel UnblockTask uses to hand a human's words back
// to an agent on resume.
//
// This is the reject half of the acceptance verdict; CompleteTask is the accept half. It
// exists so a reject does not have to be expressed as a block: a rejected delivery is not
// "stuck waiting on the outside world", it is actionable work back in the agent's court,
// and conflating the two is what made `blocked` read as noise.
//
// Re-running the concurrency cap is required and deliberate: the task is re-entering
// `running`, so it re-occupies a run slot that DeliverTask released — the exact mirror of
// UnblockTask, which re-caps for the same reason. Skipping it would let an agent exceed
// its cap by way of a reject.
func (s *Service) ReworkTask(ctx context.Context, taskID pm.TaskID, comment string, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status()
		if err := t.Rework(comment, actor, now); err != nil {
			return err
		}
		if err := s.enforceConcurrencyCap(txCtx, t); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		if err := s.flushActionLogs(txCtx, t); err != nil {
			return err
		}
		// Reopen the wedged plan node + clear its stale dispatch record, exactly as
		// UnblockTask does: a structured-plan node whose task parked keeps a dispatch record
		// that would otherwise stop it from ever being re-dispatched, so a rework would put
		// the task back in `running` while the graph quietly never advances it.
		if err := s.reopenStuckPlanNode(txCtx, t, "task_rework"); err != nil {
			return err
		}
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, comment); err != nil {
			return err
		}
		// EvtTaskAssigned is the RE-DISPATCH wake (the DispatchWakeProjector assign arm) —
		// the same signal UnblockTask emits to re-wake the assignee. state_changed alone
		// does not re-drive the agent.
		if err := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); err != nil {
			return err
		}
		s.auditTaskRework(txCtx, t, comment, actor)
		return nil
	})
}

package cli

import (
	"context"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// dispatchPMReads is the PM read surface the dispatch-wake resolvers need (a narrowed
// subset of *pmservice.Service, for testability). It mirrors sweepPMReads' style.
type dispatchPMReads interface {
	// EnsureTaskRunnable returns nil iff the task can be started/resumed now (open + deps
	// satisfied, not backlog/blocked) — the SAME gate start_work uses (pm.ErrTaskNotRunnable
	// otherwise).
	EnsureTaskRunnable(ctx context.Context, taskID pm.TaskID) error
	// AgentFreedFromTask reports whether the task no longer occupies its agent's single-active
	// slot (terminal, or blocked-running) — the re-push "is the agent free now?" gate.
	AgentFreedFromTask(ctx context.Context, taskID pm.TaskID) (bool, error)
	// ListRunnableAgentTasks lists the assignee's deps-satisfied dispatchable tasks (the
	// same query list_my_tasks uses); firstQueuedTaskID picks the next startable one.
	ListRunnableAgentTasks(ctx context.Context, assignee pm.IdentityRef) ([]*pm.Task, error)
}

// buildAssignTarget produces the AssignTarget resolver for the DispatchWakeProjector's
// assign/reassign triggers: an assignee that is a desired-running agent with a bound worker
// and a CURRENTLY runnable target task → its control-stream target (entity id + worker),
// else ok=false. A not-yet-runnable assignment (deps pending) is intentionally skipped — it
// will emit pm.task.assigned again when the plan engine dispatches it ready.
func buildAssignTarget(pmr dispatchPMReads, ar sweepAgentReads) func(context.Context, string, string) (envservice.DispatchWakeTarget, bool, error) {
	return buildAssignTargetWithCapabilities(pmr, ar, nil)
}

func buildAssignTargetWithCapabilities(pmr dispatchPMReads, ar sweepAgentReads, wr dispatchWorkerReads) func(context.Context, string, string) (envservice.DispatchWakeTarget, bool, error) {
	return buildAssignTargetWithCapabilityWaits(pmr, ar, wr, nil)
}

func buildAssignTargetWithCapabilityWaits(pmr dispatchPMReads, ar sweepAgentReads, wr dispatchWorkerReads, waits environment.CapabilityWaitRepository) func(context.Context, string, string) (envservice.DispatchWakeTarget, bool, error) {
	gate := newDispatchCapabilityGate(wr, waits, nil)
	return func(ctx context.Context, assigneeRef, taskID string) (envservice.DispatchWakeTarget, bool, error) {
		if !strings.HasPrefix(assigneeRef, "agent:") || taskID == "" {
			return envservice.DispatchWakeTarget{}, false, nil
		}
		if err := pmr.EnsureTaskRunnable(ctx, pm.TaskID(taskID)); err != nil {
			if errors.Is(err, pm.ErrTaskNotRunnable) {
				return envservice.DispatchWakeTarget{}, false, nil
			}
			return envservice.DispatchWakeTarget{}, false, err
		}
		ag := resolveDispatchAgent(ctx, ar, assigneeRef)
		tgt, ok := dispatchTargetForAgent(ag, taskID)
		if !ok {
			return envservice.DispatchWakeTarget{}, false, nil
		}
		decision, err := gate.allows(ctx, ag, taskForCapabilityGate(ctx, pmr, taskID))
		if err != nil {
			return envservice.DispatchWakeTarget{}, false, err
		}
		if !decision.OK {
			if err := gate.enterWait(ctx, assigneeRef, taskID, ag, decision); err != nil {
				return envservice.DispatchWakeTarget{}, false, err
			}
			return envservice.DispatchWakeTarget{}, false, nil
		}
		if err := gate.resolveWait(ctx, taskID, ag); err != nil {
			return envservice.DispatchWakeTarget{}, false, err
		}
		return tgt, true, nil
	}
}

// buildRepushTarget produces the RepushTarget resolver for the re-push trigger: when an
// agent's task frees its slot (AgentFreedFromTask) AND the agent has another queued runnable
// assigned task, return that next task's wake target; else ok=false. The status/prevStatus
// hint lets us skip the common open→running start without any read (a fresh start can never
// free the slot, and ListRunnableAgentTasks would otherwise count the just-started running
// task as "runnable").
func buildRepushTarget(pmr dispatchPMReads, ar sweepAgentReads) func(context.Context, string, string, string, string) (envservice.DispatchWakeTarget, bool, error) {
	return buildRepushTargetWithCapabilities(pmr, ar, nil)
}

func buildRepushTargetWithCapabilities(pmr dispatchPMReads, ar sweepAgentReads, wr dispatchWorkerReads) func(context.Context, string, string, string, string) (envservice.DispatchWakeTarget, bool, error) {
	return buildRepushTargetWithCapabilityWaits(pmr, ar, wr, nil)
}

func buildRepushTargetWithCapabilityWaits(pmr dispatchPMReads, ar sweepAgentReads, wr dispatchWorkerReads, waits environment.CapabilityWaitRepository) func(context.Context, string, string, string, string) (envservice.DispatchWakeTarget, bool, error) {
	gate := newDispatchCapabilityGate(wr, waits, nil)
	return func(ctx context.Context, assigneeRef, finishedTaskID, status, prevStatus string) (envservice.DispatchWakeTarget, bool, error) {
		if !strings.HasPrefix(assigneeRef, "agent:") || finishedTaskID == "" {
			return envservice.DispatchWakeTarget{}, false, nil
		}
		// Cheap pre-filter: a transition INTO running from a non-running state is a
		// start/claim/resume — it cannot free the slot, so skip before any read. A block
		// keeps status=running with prevStatus=running, so it survives this filter and is
		// confirmed by AgentFreedFromTask below.
		if status == string(pm.TaskRunning) && prevStatus != string(pm.TaskRunning) {
			return envservice.DispatchWakeTarget{}, false, nil
		}
		freed, err := pmr.AgentFreedFromTask(ctx, pm.TaskID(finishedTaskID))
		if err != nil {
			return envservice.DispatchWakeTarget{}, false, err
		}
		if !freed {
			return envservice.DispatchWakeTarget{}, false, nil
		}
		runnable, err := pmr.ListRunnableAgentTasks(ctx, pm.IdentityRef(assigneeRef))
		if err != nil {
			return envservice.DispatchWakeTarget{}, false, err
		}
		next := firstQueuedTaskID(runnable)
		if next == "" {
			return envservice.DispatchWakeTarget{}, false, nil // nothing else startable
		}
		ag := resolveDispatchAgent(ctx, ar, assigneeRef)
		tgt, ok := dispatchTargetForAgent(ag, next)
		if !ok {
			return envservice.DispatchWakeTarget{}, false, nil
		}
		decision, err := gate.allows(ctx, ag, firstQueuedTask(runnable))
		if err != nil {
			return envservice.DispatchWakeTarget{}, false, err
		}
		if !decision.OK {
			if err := gate.enterWait(ctx, assigneeRef, next, ag, decision); err != nil {
				return envservice.DispatchWakeTarget{}, false, err
			}
			return envservice.DispatchWakeTarget{}, false, nil
		}
		if err := gate.resolveWait(ctx, next, ag); err != nil {
			return envservice.DispatchWakeTarget{}, false, err
		}
		return tgt, true, nil
	}
}

func buildCapabilityRedriveTarget(pmr dispatchPMReads, ar sweepAgentReads, wr dispatchWorkerReads) func(context.Context, environment.CapabilityWait) (envservice.DispatchWakeTarget, bool, error) {
	assign := buildAssignTargetWithCapabilities(pmr, ar, wr)
	return func(ctx context.Context, wait environment.CapabilityWait) (envservice.DispatchWakeTarget, bool, error) {
		if wait.TaskID == "" || wait.AssigneeRef == "" {
			return envservice.DispatchWakeTarget{}, false, nil
		}
		if tr, ok := any(pmr).(dispatchTaskReads); ok {
			task, err := tr.GetTask(ctx, pm.TaskID(wait.TaskID))
			if err != nil {
				return envservice.DispatchWakeTarget{}, false, err
			}
			if string(task.Assignee()) != wait.AssigneeRef {
				return envservice.DispatchWakeTarget{}, false, nil
			}
		}
		return assign(ctx, wait.AssigneeRef, wait.TaskID)
	}
}

// resolveDispatchAgent bridges a task-assignee ref to the control-stream target: it resolves
// the execution Agent (resolveSweepAgent handles identity-member-id vs entity-id refs),
// requires it desired-running with a bound worker, and anchors the wake on taskID. Returns
// ok=false for a stopped/archived/unbound/unresolvable agent (mirrors the sweep's gates).
func resolveDispatchAgent(ctx context.Context, ar sweepAgentReads, assigneeRef string) *agent.Agent {
	ag := resolveSweepAgent(ctx, ar, strings.TrimPrefix(assigneeRef, "agent:"))
	if ag == nil || ag.Lifecycle() != agent.LifecycleRunning {
		return nil
	}
	return ag
}

func dispatchTargetForAgent(ag *agent.Agent, taskID string) (envservice.DispatchWakeTarget, bool) {
	if ag == nil {
		return envservice.DispatchWakeTarget{}, false
	}
	worker := strings.TrimSpace(ag.WorkerID())
	if worker == "" {
		return envservice.DispatchWakeTarget{}, false
	}
	return envservice.DispatchWakeTarget{WorkerID: worker, AgentID: string(ag.ID()), TaskID: taskID}, true
}

package service

import (
	"context"
	"database/sql"
	"encoding/json"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TaskStatusSyncProjector (v2.7 #111 #2) keeps pm.Task status in sync with the
// agent work-item lifecycle for the transitions the agent does NOT declare via
// tools. It consumes agent.work_item_transitioned.
//
// v2.7 scope: active → Task.Start (assigned→running) — the keystone that makes
// the agent-declared complete_task / block_task reachable (both require the task
// to be running). done is owned by the complete_task tool; B3 circuit-break →
// blocked is added in a follow-up keyed on cause="agent_death". The other
// transitions (superseded/canceled/waiting_input, and L2 single-turn failed) do
// NOT flip the task (guards).
//
// Idempotent via AppliedStore; the task transition + the AppliedStore mark share
// one tx (mirrors WorkItemProjector).
type TaskStatusSyncProjector struct {
	db      *sql.DB
	svc     *Service
	applied outbox.AppliedStore
	clock   clock.Clock
}

// NewTaskStatusSyncProjector wires the projector. svc supplies the pm task repo +
// emit; applied dedups redelivery.
func NewTaskStatusSyncProjector(db *sql.DB, svc *Service, applied outbox.AppliedStore, clk clock.Clock) *TaskStatusSyncProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &TaskStatusSyncProjector{db: db, svc: svc, applied: applied, clock: clk}
}

// Name is the AppliedStore key.
func (p *TaskStatusSyncProjector) Name() string { return "pm-task-status-sync" }

// Project turns a work-item active transition into Task.Start. Other event types
// and non-active transitions are no-ops.
func (p *TaskStatusSyncProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != agentsvc.EvtAgentWorkItemTransitioned {
		return nil
	}
	var pl agentsvc.WorkItemTransitionPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	taskID, ok := taskIDFromRef(pl.TaskRef)
	if !ok {
		return nil
	}
	// Map the work-item transition to its task-status effect:
	//   - active          → Task.Start (assigned→running)
	//   - failed (ANY cause) → Task.Block (running→blocked) — v2.8.1 #278 (b) fix.
	//     Covers BOTH WorkItem-failed sources: agent crash/circuit-break (cause=
	//     agent_death) AND L2 turn errors (no cause, e.g. rate_limit) AND the
	//     reconciler's stuck-release (FailFromAgentDeath). Driven by the WI status
	//     transition (not a specific fail path) → both sources covered. Before this,
	//     L2 single-turn failed (no cause) was a no-op → task stuck running (limbo).
	// Everything else is a no-op: done (owned by complete_task),
	// canceled/superseded/waiting_input, and creation.
	var apply func(context.Context, pm.TaskID) error
	switch {
	case pl.Status == string(agentpkg.WorkItemActive):
		apply = p.svc.startTaskIfAssigned
	case pl.Status == string(agentpkg.WorkItemFailed):
		apply = p.svc.blockTaskOnFailure
	default:
		return nil
	}
	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if err := apply(txCtx, pm.TaskID(taskID)); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

var _ outbox.Projector = (*TaskStatusSyncProjector)(nil)

// startTaskIfAssigned moves an assigned Task to running (system path driven by the
// work-item active transition — NO project-member check, unlike StartTask which
// is a user action). A no-op when the Task is not assigned (e.g. already running
// on a wake re-activation), which keeps the projector idempotent across the
// multi-interaction wait→wake loop. Same-tx: the row update + state_changed emit
// join the caller's tx.
func (s *Service) startTaskIfAssigned(ctx context.Context, taskID pm.TaskID) error {
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Status() != pm.TaskAssigned {
		return nil
	}
	if err := t.Start(s.clock.Now()); err != nil {
		return err
	}
	if err := s.tasks.Update(ctx, t); err != nil {
		return err
	}
	return s.emitTaskStateChanged(ctx, t, "")
}

// taskBlockedOnFailureReason is the block reason stamped when a Task is blocked
// because its WorkItem failed (any cause — agent crash/circuit-break, reconciler
// stuck-release, or an L2 turn error like rate_limit). Generic so it is accurate
// across all failure sources (v2.8.1 #278 (b) fix).
const taskBlockedOnFailureReason = "agent execution failed"

// blockTaskOnFailure moves a running Task to blocked when its WorkItem failed (any
// cause; system path, no project-member check). A no-op when the Task is not
// running: assigned (the WorkItem never activated → failed before pickup) is left
// assigned per the 口径 edge (agent-level failed already surfaces, the user
// reassigns); already-blocked is idempotent; terminal is skipped. The user/agent
// unblocks via reopened or re-dispatch.
func (s *Service) blockTaskOnFailure(ctx context.Context, taskID pm.TaskID) error {
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return err
	}
	if t.Status() != pm.TaskRunning {
		return nil
	}
	if err := t.Block(taskBlockedOnFailureReason, s.clock.Now()); err != nil {
		return err
	}
	if err := s.tasks.Update(ctx, t); err != nil {
		return err
	}
	return s.emitTaskStateChanged(ctx, t, taskBlockedOnFailureReason)
}

package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/outbox"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

// DispatchWakeProjector restores the IMMEDIATE "you have work" wake signal that was
// lost when v2.14.0 F7 retired the WorkItemProjector (issue I34 / T465).
//
// Background: dispatch (plan ready-set, direct AssignTask, reassign, unblock) still
// emits pm.task.assigned / pm.task.reassigned, but the projector that used to consume
// those to emit agent.work_available was deleted with AgentWorkItem. The only remaining
// emitter became the 60s WakeReconcileLoop sweep (+60s grace) → 60–120s dispatch-to-start
// latency, and a reassigned task's NEW assignee had no proactive wake at all.
//
// This projector is the replacement WAKE side ONLY: it does NOT revive AgentWorkItem and
// does NOT carry a payload of the work — the wake is a content-free "you have work" signal
// (sweepWakePayload is just agent_id + task_id as the dedup anchor); the agent pulls the
// actual work via its MCP loop (list_my_tasks / get_task). It emits onto the agent's Worker
// control stream via the SAME controlLog.AppendCommand → workAvailable→relaunchForWake path
// the sweep uses (live session gets the nudge injected; a down desired-running session is
// relaunched).
//
// Three triggers (all gated on: assignee is an agent, the agent is desired-running with a
// bound worker, and the target task is runnable — resolution injected by composition so the
// pm/agent reads stay out of this BC, mirroring the sweep's SweepCandidates injection):
//   - (a) pm.task.assigned   → wake the assignee for the assigned task. Covers BOTH a direct
//     AssignTask and a plan ready-set dispatch (both emit this event), and an unblock
//     re-dispatch (UnblockTask emits pm.task.assigned, not state_changed).
//   - (b) pm.task.reassigned → wake the NEW assignee only (event.Assignee is the new one;
//     the previous assignee is never signalled).
//   - (c) pm.task.state_changed → when the assignee's task frees its single-active slot
//     (terminal completed/discarded, or a blocked lease-free running task) AND the agent has
//     another OPEN runnable assigned task, re-push immediately rather than waiting for the
//     sweep. The "freed + next task" decision is the injected RepushTarget (authoritative
//     re-read), so a plain open→running start/claim does NOT misfire as a re-push.
//
// Idempotency: ControlLog dedups on UNIQUE(worker_id, idempotency_key), and the Relay's
// AppliedStore prevents re-projecting the same event. assign/reassign keys include the
// triggering event id (one wake per assignment action); the re-push key intentionally omits
// it and anchors on agent+next-task so concurrent/sequential done-events that resolve to the
// SAME next task fold into a single wake.
//
// The 60s sweep is unchanged and remains the down-session fallback (a lost immediate signal
// or a session that dies right after still gets recovered there); DispatchRecord is untouched.
type DispatchWakeProjector struct {
	controlLog   *environment.ControlLog
	assignTarget func(ctx context.Context, assigneeRef, taskID string) (DispatchWakeTarget, bool, error)
	repushTarget func(ctx context.Context, assigneeRef, finishedTaskID, status, prevStatus string) (DispatchWakeTarget, bool, error)
}

// DispatchWakeTarget is the resolved control-stream destination for a wake signal: the
// agent's ENTITY id (the daemon session key — NOT the assignee/identity ref) + its bound
// WorkerID, plus the task the wake is anchored to (the idempotency anchor / payload task_id).
type DispatchWakeTarget struct {
	WorkerID string
	AgentID  string
	TaskID   string
}

// DispatchWakeProjectorDeps wires the projector. ControlLog is required; the two resolvers
// are injected by composition (cli) where the pm.Service + agent.Repository live — a nil
// resolver makes its trigger a graceful no-op (dormant), matching the WakeProjector's
// optional-deps convention.
type DispatchWakeProjectorDeps struct {
	ControlLog *environment.ControlLog
	// AssignTarget resolves the wake target for an assign/reassign of taskID to assigneeRef,
	// or ok=false to skip (not a live agent / no worker / task not runnable now).
	AssignTarget func(ctx context.Context, assigneeRef, taskID string) (DispatchWakeTarget, bool, error)
	// RepushTarget resolves the NEXT-task wake target after assigneeRef's finishedTaskID
	// changed state, or ok=false (agent not freed by this transition / no further open
	// runnable assigned task). status/prevStatus are the event's task status fields, passed
	// through so the resolver can cheaply skip non-freeing transitions before any read.
	RepushTarget func(ctx context.Context, assigneeRef, finishedTaskID, status, prevStatus string) (DispatchWakeTarget, bool, error)
}

// NewDispatchWakeProjector constructs the projector. It is safe to register even with nil
// resolvers (each trigger no-ops), so the relay wiring never needs a conditional.
func NewDispatchWakeProjector(deps DispatchWakeProjectorDeps) *DispatchWakeProjector {
	return &DispatchWakeProjector{
		controlLog:   deps.ControlLog,
		assignTarget: deps.AssignTarget,
		repushTarget: deps.RepushTarget,
	}
}

// Name is the stable AppliedStore key for this projector.
func (p *DispatchWakeProjector) Name() string { return "task-dispatch-wake" }

// taskWakePayload mirrors the JSON keys pmservice writes on the task events this projector
// consumes (env BC copy — we mirror the keys, we do not import the pm payload type).
type taskWakePayload struct {
	TaskID     string `json:"task_id"`
	Assignee   string `json:"assignee"`
	Status     string `json:"status"`
	PrevStatus string `json:"prev_status"`
}

const agentRefPrefix = "agent:"

// Project routes the three task events to their wake trigger; everything else is ignored.
func (p *DispatchWakeProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case pmservice.EvtTaskAssigned:
		return p.projectAssign(ctx, e, "assign")
	case pmservice.EvtTaskReassigned:
		return p.projectAssign(ctx, e, "reassign")
	case pmservice.EvtTaskStateChanged:
		return p.projectStateChanged(ctx, e)
	default:
		return nil
	}
}

func (p *DispatchWakeProjector) projectAssign(ctx context.Context, e outbox.Event, kind string) error {
	if p.assignTarget == nil {
		return nil
	}
	var pl taskWakePayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if !strings.HasPrefix(pl.Assignee, agentRefPrefix) || pl.TaskID == "" {
		return nil // human assignee or malformed → no agent wake
	}
	tgt, ok, err := p.assignTarget(ctx, pl.Assignee, pl.TaskID)
	if err != nil || !ok {
		return err
	}
	// One wake per assignment EVENT: the event id keys it, so a later re-assignment of the
	// same task to the same agent still wakes (a stable agent+task key would suppress it).
	return p.emit(ctx, tgt, "dispatch.wake:"+kind+":"+e.ID+":"+tgt.AgentID+":"+tgt.TaskID)
}

func (p *DispatchWakeProjector) projectStateChanged(ctx context.Context, e outbox.Event) error {
	if p.repushTarget == nil {
		return nil
	}
	var pl taskWakePayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if !strings.HasPrefix(pl.Assignee, agentRefPrefix) || pl.TaskID == "" {
		return nil
	}
	tgt, ok, err := p.repushTarget(ctx, pl.Assignee, pl.TaskID, pl.Status, pl.PrevStatus)
	if err != nil || !ok {
		return err
	}
	// No event id in the key: concurrent/sequential done-events that resolve to the SAME
	// next task fold into one wake (the agent only needs to be told about that task once).
	return p.emit(ctx, tgt, "dispatch.wake:repush:"+tgt.AgentID+":"+tgt.TaskID)
}

func (p *DispatchWakeProjector) emit(ctx context.Context, tgt DispatchWakeTarget, key string) error {
	if p.controlLog == nil || tgt.WorkerID == "" {
		return nil
	}
	payload, err := json.Marshal(sweepWakePayload{AgentID: tgt.AgentID, TaskID: tgt.TaskID})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(tgt.WorkerID),
		CommandType:    commandTypeWorkAvailable,
		Payload:        string(payload),
		IdempotencyKey: key,
	})
	return err
}

var _ outbox.Projector = (*DispatchWakeProjector)(nil)

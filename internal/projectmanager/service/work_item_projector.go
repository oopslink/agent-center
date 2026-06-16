package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// commandTypeAgentWork is the D2-c-i work-delivery command the projector
// enqueues onto the assignee Agent's Worker control stream when a queued
// AgentWorkItem is created. The (future, D2-c-ii) daemon AgentController
// interprets it; D1's NoopHandler acks it today — fully additive, zero real
// effect yet.
const commandTypeAgentWork = "agent.work"

// commandTypeWorkAvailable is the v2.8.1 #278 D (pull model) WAKE signal: a
// lightweight per-agent "you have new work — pull your queue" notification
// emitted alongside agent.work when a WorkItem is enqueued. ADDITIVE in PR2 —
// the old agent.work push still fires (DB-UNIQUE-gated to single-active) and the
// daemon log+skips this unknown type until PR3 wires the wake handler (agent
// pulls via get_my_work + start_work). The PR6 cutover removes agent.work and
// keeps this as the sole activation trigger.
const commandTypeWorkAvailable = "agent.work_available"

// WorkItemProjector is the B2-c projector that turns Task assignment into Agent
// work (ADR-0049 §3, plan §4.2). It consumes pm.task.assigned / pm.task.reassigned
// and, when the assignee is an Agent, supersedes any prior live AgentWorkItem
// for the Task and creates a fresh queued one — so reassignment (and unblock,
// which we model as reassign) produces a new work segment while the Task keeps
// one stable Conversation. This completes the outbox wiring C2 (#100) deferred.
//
// Side effect + AppliedStore.MarkApplied run in the SAME transaction (finding 2).
//
// D2-c-i (work delivery): when this projector creates a `queued` AgentWorkItem
// it ALSO enqueues an `agent.work` command onto the assignee Agent's Worker
// control stream, in the SAME projection tx (mirroring AgentControlProjector's
// reconcile+MarkApplied same-tx pattern). The agents Repository resolves the
// assignee Agent → its WorkerID(); the tasks repo supplies the brief
// (title+description). Both extra deps are OPTIONAL (nil) so test fixtures that
// don't exercise work delivery keep working — a nil controlLog/agents/tasks
// simply skips the enqueue.
type WorkItemProjector struct {
	db         *sql.DB
	workItems  agent.WorkItemRepository
	applied    outbox.AppliedStore
	idgen      idgen.Generator
	clock      clock.Clock
	controlLog *environment.ControlLog // optional; nil → skip agent.work enqueue
	agents     agent.Repository        // optional; resolves assignee → worker
	tasks      pm.TaskRepository       // optional; supplies the work brief
}

// WorkItemProjectorDeps bundles the projector's dependencies. controlLog/agents/
// tasks are OPTIONAL (D2-c-i work delivery); leaving them nil keeps the legacy
// supersede/create behavior with no agent.work enqueue.
type WorkItemProjectorDeps struct {
	DB         *sql.DB
	WorkItems  agent.WorkItemRepository
	Applied    outbox.AppliedStore
	IDGen      idgen.Generator
	Clock      clock.Clock
	ControlLog *environment.ControlLog
	Agents     agent.Repository
	Tasks      pm.TaskRepository
}

// NewWorkItemProjector constructs the projector. controlLog/agents/tasks are
// optional (pass nil to skip the D2-c-i agent.work enqueue).
func NewWorkItemProjector(db *sql.DB, workItems agent.WorkItemRepository, applied outbox.AppliedStore, gen idgen.Generator, clk clock.Clock) *WorkItemProjector {
	return NewWorkItemProjectorWithDeps(WorkItemProjectorDeps{
		DB: db, WorkItems: workItems, Applied: applied, IDGen: gen, Clock: clk,
	})
}

// NewWorkItemProjectorWithDeps constructs the projector with the full dep bag,
// including the optional D2-c-i work-delivery deps (controlLog/agents/tasks).
func NewWorkItemProjectorWithDeps(d WorkItemProjectorDeps) *WorkItemProjector {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WorkItemProjector{
		db: d.DB, workItems: d.WorkItems, applied: d.Applied, idgen: d.IDGen, clock: clk,
		controlLog: d.ControlLog, agents: d.Agents, tasks: d.Tasks,
	}
}

// Name is the AppliedStore key.
func (p *WorkItemProjector) Name() string { return "pm-workitem-sync" }

type workItemEvtPayload struct {
	OwnerRef string `json:"owner_ref"` // pm://tasks/{id} — used as the WorkItem.TaskRef
	Assignee string `json:"assignee"`
	Status   string `json:"status"`
}

// Project turns Task events into AgentWorkItem effects (plan §10 OQ11):
//   - assigned/reassigned → supersede prior live WorkItem + create a fresh
//     queued one when the assignee is an Agent (a new dispatch attempt).
//   - state_changed to blocked/canceled → CANCEL the live WorkItem (there is no
//     WorkItem `blocked`; a blocked/canceled Task ends the current attempt, and
//     the Agent goes idle → availability returns to available).
//
// Other events are a no-op.
func (p *WorkItemProjector) Project(ctx context.Context, e outbox.Event) error {
	var pl workItemEvtPayload
	dispatch := false
	cancelLive := false
	finishLive := false
	switch e.EventType {
	case EvtTaskAssigned, EvtTaskReassigned:
		dispatch = true
	case EvtTaskStateChanged:
		// fall through to parse + decide on status
	default:
		return nil
	}
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	if e.EventType == EvtTaskStateChanged {
		switch pl.Status {
		case string(pm.TaskDiscarded), string(pm.TaskOpen):
			// discarded ends the attempt; →open is reopen, which also drops any live
			// attempt. Reopen has no live WorkItem so it is a no-op (terminal items are
			// skipped below). ADR-0046: "blocked" is no longer a status transition (it
			// is a running-task annotation), so it no longer appears here.
			cancelLive = true
		case string(pm.TaskCompleted):
			// v2.7 #111 ④ (Q1): the Task is done → FINISH the live WorkItem so it
			// does not linger `active` (the #1 projection would otherwise show a
			// completed task's WI as stale-active). active → done; a waiting_input
			// (or queued) WI → canceled, since waiting_input/queued → done is illegal.
			finishLive = true
		default:
			return nil // other state changes don't affect WorkItems
		}
	}
	taskRef := pl.OwnerRef
	agentID, isAgent := agentIDFromRef(pl.Assignee)

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		existing, err := p.workItems.ListByTask(txCtx, taskRef)
		if err != nil {
			return err
		}
		for _, w := range existing {
			if w.Status().IsTerminal() {
				continue
			}
			// v2.8.1 #278 PR5: capture the loaded version for the CAS below — guards
			// this projection against the WorkItemReconciler concurrently releasing
			// the SAME active item (the 7a-iii complete/cancel/supersede-vs-release
			// race; the reconciler is the "releaser" that makes this race real).
			preV := w.Version()
			switch {
			case dispatch:
				if err := w.Supersede(now); err != nil { // reassignment ends the prior attempt
					return err
				}
			case cancelLive:
				if err := w.Cancel(now); err != nil { // Task blocked/canceled ends the attempt
					return err
				}
			case finishLive:
				// Task completed → the work is done. active → done; otherwise
				// (waiting_input / queued, which cannot legally → done) → canceled.
				if w.Status() == agent.WorkItemActive {
					if err := w.Done(now); err != nil {
						return err
					}
				} else if err := w.Cancel(now); err != nil {
					return err
				}
			}
			// UpdateCAS: if the reconciler released this item first (active→failed,
			// version bumped), the CAS conflicts → skip it. The reconciler's terminal
			// release wins; this projection is moot for that item (the task-level
			// status still settles at the pm layer).
			if err := p.workItems.UpdateCAS(txCtx, w, preV); err != nil {
				if errors.Is(err, agent.ErrWorkItemReassigned) {
					continue
				}
				return err
			}
		}
		// On dispatch, create a fresh queued WorkItem when the assignee is an
		// Agent (a Task may be assigned to a human, which has no AgentWorkItem).
		// v2.7 #185: the assignee ref may carry the business-layer member id;
		// resolve it to the execution-entity id so WorkItem.AgentID (and all
		// downstream wake/daemon/dispatch keying) stays the internal entity id.
		if dispatch && isAgent {
			entityID := agentID
			if p.agents != nil {
				if a, rerr := resolveAgentByEither(txCtx, p.agents, string(agentID)); rerr == nil {
					entityID = a.ID()
				} else {
					slog.Warn("workitem projector: assignee agent unresolved; keeping raw id",
						"assignee", pl.Assignee, "err", rerr)
				}
			}
			nw, nerr := agent.NewWorkItem(agent.NewWorkItemInput{
				ID: p.idgen.NewULID(), AgentID: entityID, TaskRef: taskRef, CreatedAt: now,
			})
			if nerr != nil {
				return nerr
			}
			if serr := p.workItems.Save(txCtx, nw); serr != nil {
				return serr
			}
			// D2-c-i work delivery: enqueue an agent.work command on the assignee
			// Agent's Worker control stream IN THIS SAME TX. Idempotent on
			// (worker, "agent.work:<workItemID>") so re-projection never double-
			// enqueues. A nil controlLog (test fixtures) or an Agent with no worker
			// binding skips the enqueue without failing the projection.
			if ferr := p.enqueueWork(txCtx, nw, entityID, taskRef); ferr != nil {
				return ferr
			}
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// workCommandPayload is the agent.work command payload the (future) daemon
// AgentController consumes to learn it has work + a brief. Mirrors
// reconcileCommandPayload in agent_control_projector.go.
type workCommandPayload struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
	TaskRef    string `json:"task_ref"`
	Brief      string `json:"brief"`
}

// workAvailablePayload is the agent.work_available (wake) command payload. It is
// deliberately per-AGENT (not per-WorkItem): the wake just tells the agent "your
// queue changed — pull it (get_my_work) and start_work the next item". The
// WorkItemID is carried only for idempotency-key determinism + observability.
type workAvailablePayload struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
}

// enqueueWork appends the agent.work command for a freshly-queued WorkItem onto
// the assignee Agent's Worker control stream (same tx as the caller). It is a
// best-effort SIGNAL: if work delivery is not wired (nil controlLog/agents), or
// the agent has no worker binding, it logs and skips rather than failing the
// projection. The brief (title+description) is captured at enqueue time so the
// command is replayable-deterministic.
func (p *WorkItemProjector) enqueueWork(ctx context.Context, wi *agent.AgentWorkItem, agentID agent.AgentID, taskRef string) error {
	if p.controlLog == nil || p.agents == nil {
		return nil // work delivery not wired (e.g. test fixtures)
	}
	a, err := p.agents.FindByID(ctx, agentID)
	if err != nil {
		// The assignee Agent could not be resolved — skip the signal rather than
		// stall the WorkItem projection (the WorkItem itself is already created).
		slog.Warn("workitem projector: agent.work enqueue skipped (agent lookup failed)",
			"agent_id", string(agentID), "work_item_id", wi.ID(), "err", err)
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("workitem projector: agent.work enqueue skipped (agent has no worker binding)",
			"agent_id", string(agentID), "work_item_id", wi.ID())
		return nil
	}
	// FINDING-1 lifecycle guard (task #115): only deliver work to a RUNNING agent.
	// A session-less (stopped/stopping/resetting/error/failed) agent has no running
	// session, so the daemon's work() returns "no running session (retry after
	// reconcile)" forever, head-of-line-blocking the worker's whole control stream.
	// Graceful-skip here (no command, no error): the WorkItem stays created/active
	// and delivery is DEFERRED to AgentControlProjector's re-emit on lifecycle→running.
	if a.Lifecycle() != agent.LifecycleRunning {
		slog.Info("workitem projector: agent.work enqueue skipped (agent not running)",
			"agent_id", string(agentID), "work_item_id", wi.ID(), "lifecycle", string(a.Lifecycle()))
		return nil
	}
	// v2.8.1 #278 D PR6 CUTOVER: the old agent.work PUSH (auto-activate) is removed.
	// The center now ONLY emits the per-agent wake (agent.work_available) — the agent
	// pulls its queue (get_my_work / start_task) and is the SOLE
	// path that marks a WorkItem active (single-active by construction, not by the old
	// DB-UNIQUE gate on a racing push). Same tx + same lifecycle/binding guards above.
	wakePayload, err := json.Marshal(workAvailablePayload{
		AgentID:    string(agentID),
		WorkItemID: wi.ID(),
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeWorkAvailable,
		Payload:        string(wakePayload),
		IdempotencyKey: "agent.work_available:" + wi.ID(),
	})
	return err
}

// brief assembles the work brief — the task's title + description — captured at
// enqueue time so the agent.work command is replayable-deterministic. Format:
// "title\n\ndescription" (description omitted when empty). When the tasks repo
// is unwired or the task can't be loaded, the brief degrades to an empty string
// (the WorkItem's task_ref still lets the controller resolve detail later).
func (p *WorkItemProjector) brief(ctx context.Context, taskRef string) string {
	if p.tasks == nil {
		return ""
	}
	id, ok := taskIDFromRef(taskRef)
	if !ok {
		return ""
	}
	t, err := p.tasks.FindByID(ctx, pm.TaskID(id))
	if err != nil || t == nil {
		slog.Info("workitem projector: brief unavailable (task lookup failed)",
			"task_ref", taskRef, "err", err)
		return ""
	}
	title := strings.TrimSpace(t.Title())
	desc := strings.TrimSpace(t.Description())
	if desc == "" {
		return title
	}
	return title + "\n\n" + desc
}

// taskIDFromRef extracts the Task id from a "pm://tasks/{id}" owner ref.
func taskIDFromRef(ref string) (string, bool) {
	const p = "pm://tasks/"
	if strings.HasPrefix(ref, p) && len(ref) > len(p) {
		return strings.TrimPrefix(ref, p), true
	}
	return "", false
}

// resolveAgentByEither resolves rawID — which may be the execution-entity id OR
// the identity-member id ("agent-<ulid>", v2.7 #185 — the business-layer id the
// assign path now carries) — to the Agent. Entity-id first (cheap, no collision),
// then the member→entity bridge (FindByIdentityMemberID).
func resolveAgentByEither(ctx context.Context, repo agent.Repository, rawID string) (*agent.Agent, error) {
	a, err := repo.FindByID(ctx, agent.AgentID(rawID))
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, agent.ErrAgentNotFound) {
		return nil, err
	}
	return repo.FindByIdentityMemberID(ctx, rawID)
}

// agentIDFromRef extracts the Agent id from an "agent:<id>" identity ref.
// Returns ok=false for non-agent assignees (humans).
func agentIDFromRef(ref string) (agent.AgentID, bool) {
	const p = "agent:"
	if strings.HasPrefix(ref, p) && len(ref) > len(p) {
		return agent.AgentID(strings.TrimPrefix(ref, p)), true
	}
	return "", false
}

var _ outbox.Projector = (*WorkItemProjector)(nil)

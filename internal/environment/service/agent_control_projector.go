// Package service hosts the Environment-BC projectors/services (v2.7 D2). The
// AgentControlProjector is the D2-a reconcile projector: it consumes the C3
// agent.lifecycle_changed outbox events and ENQUEUES a declarative reconcile
// command onto the agent's Worker control stream (D1's WorkerControlEvent log),
// so the future AgentController (D2-c) can reconcile the real process to the
// desired lifecycle. D2-a only ENQUEUES — D1's daemon NoopHandler no-op-acks the
// commands, so there is zero real effect yet (fully additive; the old
// taskruntime execution path is untouched).
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// commandTypeAgentReconcile is the declarative reconcile command the projector
// enqueues. The AgentController (D2-c) interprets it; D1's NoopHandler acks it.
const commandTypeAgentReconcile = "agent.reconcile"

// commandTypeAgentWork is the work-delivery command (FINDING-1 task #115 PART ②).
// It MUST match pm WorkItemProjector's commandTypeAgentWork ("agent.work") and the
// workCommandPayload shape below MUST match its workCommandPayload — both build the
// IDENTICAL command keyed by "agent.work:<workItemID>". A shared helper across the
// pm and environment BCs would couple the two projectors awkwardly (each owns its
// command construction today, e.g. WakeProjector replicates its own payload struct),
// so we REPLICATE here and pin the equivalence with a cross-package test.
const commandTypeAgentWork = "agent.work"

// AgentControlProjector turns Agent lifecycle intent changes into reconcile
// commands on the Worker control stream (ADR-0050 §4 / plan D2-a).
//
// Two-layer idempotency (matching work_item_projector's same-tx pattern):
//   - The AppliedStore dedups the SOURCE outbox event (projector, event_id) — a
//     re-delivered outbox.Event with the same ID enqueues nothing the 2nd time.
//   - ControlLog.AppendCommand is itself idempotent on UNIQUE(worker_id,
//     idempotency_key); we key by agent+version so a re-issued reconcile for the
//     same version collapses into one stream entry, while a version BUMP
//     (start/stop/restart/reset) yields a NEW command.
//
// The side effect (AppendCommand) AND AppliedStore.MarkApplied run in the SAME
// transaction (AppendCommand uses persistence.ExecutorFromCtx → the tx), so the
// projector is at-most-once even though the relay's outer guard is two-step.
type AgentControlProjector struct {
	db         *sql.DB
	controlLog *environment.ControlLog
	applied    outbox.AppliedStore
	clock      clock.Clock
	// workItems is a READ-ONLY cross-BC dependency (FINDING-1 PART ②): on
	// lifecycle→running it lists the agent's in-flight work-items so each ACTIVE
	// one can be (re-)delivered AFTER the reconcile command, in the same tx, in
	// control-log order (session before work → no deadlock). OPTIONAL (nil → the
	// re-emit is skipped, keeping legacy reconcile-only behavior for fixtures).
	// This mirrors WakeProjector's existing agent.WorkItemRepository read dep, so
	// it introduces no new import cycle / BC-layering violation.
	workItems agent.WorkItemRepository
}

// NewAgentControlProjector constructs the projector WITHOUT the optional
// work-reemit dep (legacy reconcile-only behavior).
func NewAgentControlProjector(db *sql.DB, controlLog *environment.ControlLog, applied outbox.AppliedStore, clk clock.Clock) *AgentControlProjector {
	return NewAgentControlProjectorWithWork(db, controlLog, applied, clk, nil)
}

// NewAgentControlProjectorWithWork constructs the projector with the optional
// read-only workItems dep enabling the FINDING-1 PART ② work re-emit on
// lifecycle→running. Passing nil workItems reproduces NewAgentControlProjector.
func NewAgentControlProjectorWithWork(db *sql.DB, controlLog *environment.ControlLog, applied outbox.AppliedStore, clk clock.Clock, workItems agent.WorkItemRepository) *AgentControlProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &AgentControlProjector{db: db, controlLog: controlLog, applied: applied, clock: clk, workItems: workItems}
}

// Name is the AppliedStore key (its own namespace, separate from other
// projectors consuming the same events).
func (p *AgentControlProjector) Name() string { return "env-agent-control" }

// agentLifecycleEvtPayload mirrors the JSON keys the C3 agent service writes
// (agent_id/worker_id/lifecycle/version/reset_scope). We define our own struct
// rather than depend on the unexported agentEventPayload.
type agentLifecycleEvtPayload struct {
	AgentID    string `json:"agent_id"`
	WorkerID   string `json:"worker_id"`
	Lifecycle  string `json:"lifecycle"`
	Version    int    `json:"version"`
	ResetScope string `json:"reset_scope,omitempty"`
	Model      string `json:"model,omitempty"`
}

// reconcileCommandPayload is the declarative command payload the AgentController
// (D2-c) will reconcile against.
type reconcileCommandPayload struct {
	AgentID          string `json:"agent_id"`
	DesiredLifecycle string `json:"desired_lifecycle"`
	Model            string `json:"model,omitempty"`
	Version          int    `json:"version"`
	ResetScope       string `json:"reset_scope,omitempty"`
}

// workCommandPayload MUST stay byte-identical to pm WorkItemProjector's
// workCommandPayload (same JSON keys/order) — the daemon AgentController consumes
// one command type regardless of which projector emitted it. NOTE: the re-emit does
// not re-load the task brief (this BC has no tasks repo); Brief is left empty and
// the daemon resolves detail from task_ref (matching enqueueWork's degraded-brief path).
type workCommandPayload struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
	TaskRef    string `json:"task_ref"`
	Brief      string `json:"brief"`
}

// Project enqueues a reconcile command for an agent.lifecycle_changed event.
//   - agent.lifecycle_changed → enqueue agent.reconcile on the agent's Worker.
//   - agent.created → no-op (a created Agent is `stopped`, no process to
//     reconcile yet; the first real intent change emits lifecycle_changed).
//   - anything else → no-op.
func (p *AgentControlProjector) Project(ctx context.Context, e outbox.Event) error {
	switch e.EventType {
	case agentsvc.EvtAgentLifecycleChanged:
		// handled below
	case agentsvc.EvtAgentCreated:
		return nil
	default:
		return nil
	}

	var pl agentLifecycleEvtPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}

	cmdPayload, err := json.Marshal(reconcileCommandPayload{
		AgentID:          pl.AgentID,
		DesiredLifecycle: pl.Lifecycle,
		Model:            pl.Model, // passthrough (pure event-driven; no Agent-repo read)
		Version:          pl.Version,
		ResetScope:       pl.ResetScope,
	})
	if err != nil {
		return err
	}
	idempotencyKey := "agent.lifecycle:" + pl.AgentID + ":" + strconv.Itoa(pl.Version)

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if _, err := p.controlLog.AppendCommand(txCtx, environment.AppendCommandInput{
			WorkerID:       environment.WorkerID(pl.WorkerID),
			CommandType:    commandTypeAgentReconcile,
			Payload:        string(cmdPayload),
			IdempotencyKey: idempotencyKey,
		}); err != nil {
			return err
		}
		// FINDING-1 PART ② (deliver-on-start companion): when the agent transitions
		// to running, (re-)deliver work for each ACTIVE in-flight WorkItem AFTER the
		// reconcile above — same tx, same call → guaranteed control-log order
		// (reconcile/session before work, so the daemon never deadlocks on "no
		// running session"). This is what un-defers the work that PART ①'s lifecycle
		// guard skipped while the agent was not running.
		if err := p.reemitWorkOnRunning(txCtx, pl); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// reemitWorkOnRunning appends an agent.work command for every ready-to-dispatch
// (QUEUED or ACTIVE) in-flight WorkItem of the agent, called only on a
// lifecycle→running transition and only when the read-only workItems dep is wired.
//
// CAVEAT 3 (ready-to-dispatch = queued + active): re-emit for `queued` (the primary
// deliver-on-start case — PART ①'s guard skipped the original enqueue while the
// agent was not running, so the WI sits queued, never delivered) AND `active` (flap
// re-delivery; collapses on the stable key). SKIP `waiting_input` (waits on a human
// reply, delivered via the wake path) and terminal. NOTE: this was originally
// active-only, which silently dropped guard-skipped QUEUED WIs (= lost work); fixed
// per Tester's #115 outcome verification.
//
// CAVEAT 1 (flap idempotency): the command is keyed by "agent.work:<workItemID>",
// EXACTLY matching pm WorkItemProjector.enqueueWork. ControlLog.AppendCommand is
// idempotent on UNIQUE(worker_id, idempotency_key), so on a lifecycle FLAP
// (running→stopped→running) — distinct →running outbox events that the AppliedStore
// does NOT dedup — the re-emit collapses into the EXISTING stream entry: no new
// offset, no re-delivery, hence no double-inject by the (non-idempotent-by-WI)
// daemon work(). The center-side stable key IS the flap-dedup; no extra store needed.
func (p *AgentControlProjector) reemitWorkOnRunning(ctx context.Context, pl agentLifecycleEvtPayload) error {
	if p.workItems == nil || pl.Lifecycle != string(agent.LifecycleRunning) {
		return nil
	}
	items, err := p.workItems.ListByAgent(ctx, agent.AgentID(pl.AgentID))
	if err != nil {
		// A read failure must not stall the reconcile (already appended above); log
		// and skip — the WorkItems remain active and a later →running re-emits.
		slog.Warn("agent control projector: work re-emit skipped (work-item lookup failed)",
			"agent_id", pl.AgentID, "err", err)
		return nil
	}
	for _, wi := range items {
		// CAVEAT 3 (ready-to-dispatch): re-emit for QUEUED and ACTIVE work items.
		// QUEUED is the primary case — PART ①'s guard skipped the original enqueue
		// while the agent was not running, so the WI sits queued (never delivered,
		// never activated); this re-emit is the deliver-on-start that un-defers it.
		// ACTIVE covers a flap re-delivery (already-delivered actives collapse on the
		// stable idempotency key → no double-inject). Skip waiting_input (waits on a
		// human reply, delivered via the wake path) and terminal. (active-only was a
		// bug: it left guard-skipped QUEUED WIs undelivered = silent lost work —
		// Tester #115 outcome catch.)
		if s := wi.Status(); s != agent.WorkItemQueued && s != agent.WorkItemActive {
			continue
		}
		payload, err := json.Marshal(workCommandPayload{
			AgentID:    pl.AgentID,
			WorkItemID: wi.ID(),
			TaskRef:    wi.TaskRef(),
			Brief:      "", // re-emit has no tasks repo; daemon resolves from task_ref
		})
		if err != nil {
			return err
		}
		if _, err := p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
			WorkerID:       environment.WorkerID(pl.WorkerID),
			CommandType:    commandTypeAgentWork,
			Payload:        string(payload),
			IdempotencyKey: "agent.work:" + wi.ID(),
		}); err != nil {
			return err
		}
	}
	return nil
}

var _ outbox.Projector = (*AgentControlProjector)(nil)

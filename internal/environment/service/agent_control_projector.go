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
	"strconv"

	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// commandTypeAgentReconcile is the declarative reconcile command the projector
// enqueues. The AgentController (D2-c) interprets it; D1's NoopHandler acks it.
const commandTypeAgentReconcile = "agent.reconcile"

// v2.14.0 F7 (issue I14): the per-WorkItem agent.work / agent.work_available
// re-emit on lifecycle→running (and the read-only WorkItemRepository dep it used)
// was removed — AgentWorkItem retired. The projector now only drives the
// agent-control reconcile loop; work delivery is the Task model's concern.

// AgentControlProjector turns Agent lifecycle intent changes into reconcile
// commands on the Worker control stream (ADR-0050 §4 / plan D2-a).
//
// Two-layer idempotency (matching the same-tx pattern):
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
}

// NewAgentControlProjector constructs the projector.
func NewAgentControlProjector(db *sql.DB, controlLog *environment.ControlLog, applied outbox.AppliedStore, clk clock.Clock) *AgentControlProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &AgentControlProjector{db: db, controlLog: controlLog, applied: applied, clock: clk}
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
	CLI        string `json:"cli,omitempty"`
	Reasoning  string `json:"reasoning,omitempty"` // T236
	Mode       string `json:"mode,omitempty"`      // T236
	Provider   string `json:"provider,omitempty"`  // T236
}

// reconcileCommandPayload is the declarative command payload the AgentController
// (D2-c) will reconcile against.
type reconcileCommandPayload struct {
	AgentID          string `json:"agent_id"`
	DesiredLifecycle string `json:"desired_lifecycle"`
	Model            string `json:"model,omitempty"`
	// CLI selects the per-CLI session starter on the worker ("codex" → codex exec
	// session; empty/"claude-code" → claude supervisor). Passthrough from the
	// lifecycle event, same as Model.
	CLI string `json:"cli,omitempty"`
	// T236 LLM tuning — passthrough to the daemon session config, same as Model/CLI.
	Reasoning  string `json:"reasoning,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Version    int    `json:"version"`
	ResetScope string `json:"reset_scope,omitempty"`
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
		CLI:              pl.CLI,   // passthrough — per-CLI starter selection on the worker
		Reasoning:        pl.Reasoning, // T236 passthrough
		Mode:             pl.Mode,      // T236 passthrough
		Provider:         pl.Provider,  // T236 passthrough
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
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

var _ outbox.Projector = (*AgentControlProjector)(nil)

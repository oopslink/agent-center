package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	convservice "github.com/oopslink/agent-center/internal/conversation/service"
	"github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// commandTypeAgentWake is the D2-e-i immediate-wakeup command the WakeProjector
// enqueues onto a waiting_input agent's Worker control stream when a human (or
// another agent) posts a message into the agent's TASK conversation (OQ5). The
// daemon AgentController interprets it (injects the message into the long-lived
// claude session + reports the WorkItem active); D1's NoopHandler acks it today —
// fully additive, the control loop stays DORMANT (ControlClient nil).
const commandTypeAgentWake = "agent.wake"

// ownerRefTasksPrefix is the task-owned conversation owner_ref scheme.
const ownerRefTasksPrefix = "pm://tasks/"

// WakeProjector turns a `conversation.message_added` outbox event for a TASK
// conversation into `agent.wake` control commands for every agent whose
// AgentWorkItem on that task is currently waiting_input (v2.7 D2-e-i / OQ5). It
// mirrors AgentControlProjector's same-tx idempotency exactly: the side effect
// (ControlLog.AppendCommand) AND AppliedStore.MarkApplied run in ONE tx, so a
// re-delivered outbox event enqueues nothing the second time.
//
// SCOPE (e-i only — immediate wake): it handles ONLY WorkItems already in
// waiting_input. The busy-buffering + merge-on-next-waiting (read-cursor batch)
// path is the NEXT slice (e-ii) and is intentionally NOT built here.
//
// SELF-EXCLUSION: an agent never wakes itself — when the message sender is the
// agent owning the WorkItem (sender == "agent:<id>"), that agent is skipped.
// This is what keeps request_input (the agent's own question, sender=agent:<id>,
// posted in the same tx as the WaitInput) from immediately re-waking the asker.
type WakeProjector struct {
	db         *sql.DB
	workItems  agent.WorkItemRepository
	agents     agent.Repository
	controlLog *environment.ControlLog
	applied    outbox.AppliedStore
	clock      clock.Clock
}

// WakeProjectorDeps bundles the projector's dependencies.
type WakeProjectorDeps struct {
	DB         *sql.DB
	WorkItems  agent.WorkItemRepository
	Agents     agent.Repository
	ControlLog *environment.ControlLog
	Applied    outbox.AppliedStore
	Clock      clock.Clock
}

// NewWakeProjector constructs the projector.
func NewWakeProjector(d WakeProjectorDeps) *WakeProjector {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WakeProjector{
		db:         d.DB,
		workItems:  d.WorkItems,
		agents:     d.Agents,
		controlLog: d.ControlLog,
		applied:    d.Applied,
		clock:      clk,
	}
}

// Name is the AppliedStore key (its own namespace, separate from the other
// projectors consuming the outbox).
func (p *WakeProjector) Name() string { return "conv-agent-wake" }

// messageAddedPayload mirrors the JSON keys MessageWriter.AddMessage writes for
// the EvtConversationMessageAdded outbox event.
type messageAddedPayload struct {
	ConversationID string `json:"conversation_id"`
	OwnerRef       string `json:"owner_ref"`
	MessageID      string `json:"message_id"`
	Sender         string `json:"sender"`
	Text           string `json:"text"`
}

// wakeCommandPayload is the agent.wake command payload the daemon AgentController
// consumes to inject the message into the agent's running session.
type wakeCommandPayload struct {
	AgentID     string `json:"agent_id"`
	WorkItemID  string `json:"work_item_id"`
	TaskRef     string `json:"task_ref"`
	MessageID   string `json:"message_id"`
	MessageText string `json:"message_text"`
}

// Project enqueues an agent.wake command for each waiting_input WorkItem on the
// task whose conversation received the message (OQ5 immediate wake).
//
//   - Only conversation.message_added events are handled (else no-op).
//   - owner_ref must be a task ref (pm://tasks/{id}); else no-op (defensive — the
//     producer already filters to task conversations).
//   - For each WorkItem on the task that is waiting_input: resolve the agent,
//     EXCLUDE the message's own sender (no self-wake), resolve the worker (skip +
//     log when unresolved / no worker binding), and enqueue agent.wake keyed by
//     "agent.wake:<workItemID>:<messageID>" so re-projection never double-enqueues.
func (p *WakeProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != convservice.EvtConversationMessageAdded {
		return nil
	}
	var pl messageAddedPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	// Defensive: the producer only emits for task conversations, but guard here
	// too so a stray non-task event is a clean no-op.
	if !strings.HasPrefix(pl.OwnerRef, ownerRefTasksPrefix) {
		return nil
	}
	taskRef := pl.OwnerRef

	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		workItems, err := p.workItems.ListByTask(txCtx, taskRef)
		if err != nil {
			return err
		}
		for _, wi := range workItems {
			// e-i: ONLY immediate (already waiting_input) wake. Other statuses
			// (active/queued/terminal) are out of scope (active-buffering is e-ii).
			if wi.Status() != agent.WorkItemWaitingInput {
				continue
			}
			if err := p.enqueueWake(txCtx, wi, taskRef, pl); err != nil {
				return err
			}
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// enqueueWake appends an agent.wake command for one waiting_input WorkItem (same
// tx as the caller). Self-exclusion: when the message sender IS the agent that
// owns this WorkItem, no command is enqueued (an agent never wakes itself). When
// the agent can't be resolved or has no worker binding, it logs + skips rather
// than failing the projection (mirrors work_item_projector.enqueueWork).
func (p *WakeProjector) enqueueWake(ctx context.Context, wi *agent.AgentWorkItem, taskRef string, pl messageAddedPayload) error {
	agentID := wi.AgentID()

	// Self-exclusion: the agent's own message (sender == "agent:<id>") never wakes
	// it — this keeps request_input's same-tx question from re-waking the asker.
	if pl.Sender == "agent:"+string(agentID) {
		return nil
	}

	if p.controlLog == nil || p.agents == nil {
		return nil // wake delivery not wired (e.g. test fixtures)
	}
	a, err := p.agents.FindByID(ctx, agentID)
	if err != nil {
		// Could not resolve the agent — skip the wake rather than stall the
		// projection (the WorkItem state is unaffected by skipping the signal).
		slog.Warn("wake projector: agent.wake enqueue skipped (agent lookup failed)",
			"agent_id", string(agentID), "work_item_id", wi.ID(), "err", err)
		return nil
	}
	workerID := a.WorkerID()
	if strings.TrimSpace(workerID) == "" {
		slog.Info("wake projector: agent.wake enqueue skipped (agent has no worker binding)",
			"agent_id", string(agentID), "work_item_id", wi.ID())
		return nil
	}
	payload, err := json.Marshal(wakeCommandPayload{
		AgentID:     string(agentID),
		WorkItemID:  wi.ID(),
		TaskRef:     taskRef,
		MessageID:   pl.MessageID,
		MessageText: pl.Text,
	})
	if err != nil {
		return err
	}
	_, err = p.controlLog.AppendCommand(ctx, environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(workerID),
		CommandType:    commandTypeAgentWake,
		Payload:        string(payload),
		IdempotencyKey: "agent.wake:" + wi.ID() + ":" + pl.MessageID,
	})
	return err
}

var _ outbox.Projector = (*WakeProjector)(nil)

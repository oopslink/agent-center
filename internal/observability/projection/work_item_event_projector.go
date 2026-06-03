package projection

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// workItemEventEmitter is the slice of observability.EventSink this projector
// needs (Emit). An interface keeps the projector unit-testable and the dep
// explicit; *observability.EventSink satisfies it.
type workItemEventEmitter interface {
	Emit(ctx context.Context, cmd observability.EmitCommand) (observability.EventID, error)
}

// WorkItemEventProjector (v2.7 #111 #3b) fans every agent.work_item_transitioned
// out to the observability Event store as one `agent.work_item.transitioned`
// Event, so the append-only stats stream sees work-item lifecycle. PRODUCER
// only: it does not touch the stats query side (executions-scope repoint to the
// pm model is Phase-2). The generic "events" stats scope can already aggregate
// these. Idempotent via AppliedStore; Emit joins the projection tx.
type WorkItemEventProjector struct {
	db      *sql.DB
	sink    workItemEventEmitter
	applied outbox.AppliedStore
	clock   clock.Clock
}

// NewWorkItemEventProjector wires the projector. sink is the observability
// EventSink; applied dedups redelivery.
func NewWorkItemEventProjector(db *sql.DB, sink workItemEventEmitter, applied outbox.AppliedStore, clk clock.Clock) *WorkItemEventProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &WorkItemEventProjector{db: db, sink: sink, applied: applied, clock: clk}
}

// Name is the AppliedStore key.
func (p *WorkItemEventProjector) Name() string { return "obs-workitem-events" }

// Project records the work-item transition as an observability Event. Other
// event types are no-ops.
func (p *WorkItemEventProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != agentsvc.EvtAgentWorkItemTransitioned {
		return nil
	}
	var pl agentsvc.WorkItemTransitionPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	actor := observability.Actor("system")
	if pl.AgentID != "" {
		actor = observability.Actor("agent:" + pl.AgentID)
	}
	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		if _, err := p.sink.Emit(txCtx, observability.EmitCommand{
			EventType: observability.EventType("agent.work_item.transitioned"),
			Actor:     actor,
			Refs: observability.EventRefs{
				AgentID:    pl.AgentID,
				TaskID:     taskIDFromTaskRef(pl.TaskRef),
				WorkItemID: pl.WorkItemID,
			},
			Payload: map[string]any{
				"prev_status": pl.PrevStatus,
				"status":      pl.Status,
				"version":     pl.Version,
				"cause":       pl.Cause,
			},
			OccurredAt: pl.OccurredAt,
		}); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// taskIDFromTaskRef extracts the task id from a "pm://tasks/{id}" ref so the
// observability Event's task_id ref is joinable; non-matching refs pass through
// unchanged.
func taskIDFromTaskRef(ref string) string {
	const p = "pm://tasks/"
	if strings.HasPrefix(ref, p) {
		return strings.TrimPrefix(ref, p)
	}
	return ref
}

var _ outbox.Projector = (*WorkItemEventProjector)(nil)

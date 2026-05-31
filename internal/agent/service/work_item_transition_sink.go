package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
)

// WorkItemTransitionPayload is the JSON payload of an EvtAgentWorkItemTransitioned
// outbox event. prev_status distinguishes the transition kind for #2 (e.g.
// assigned→running vs running→done); version is the AR version AFTER the
// transition, used by outbox projectors for idempotency / ordering. prev_status
// is "" for creation (→queued).
type WorkItemTransitionPayload struct {
	WorkItemID string    `json:"work_item_id"`
	AgentID    string    `json:"agent_id"`
	TaskRef    string    `json:"task_ref"`
	PrevStatus string    `json:"prev_status"`
	Status     string    `json:"status"`
	Version    int       `json:"version"`
	OccurredAt time.Time `json:"occurred_at"`
}

// OutboxWorkItemTransitionSink implements agent.WorkItemTransitionSink by
// appending one EvtAgentWorkItemTransitioned outbox event per drained
// transition. It is wired into the WorkItem repository at the composition root,
// so the persistence adapter stays free of any outbox dependency (hex boundary).
//
// Append uses the ctx-bound executor (via outbox.Repository), so when the repo
// calls it inside the persisting tx the event INSERT joins that tx: rollback
// drops both the row write and the event; commit makes them atomic.
type OutboxWorkItemTransitionSink struct {
	outbox outbox.Repository
	idgen  idgen.Generator
}

// NewOutboxWorkItemTransitionSink wires the sink. outbox and gen must be non-nil.
func NewOutboxWorkItemTransitionSink(ob outbox.Repository, gen idgen.Generator) *OutboxWorkItemTransitionSink {
	return &OutboxWorkItemTransitionSink{outbox: ob, idgen: gen}
}

// AppendTransitions emits one outbox event per transition, in order, within the
// caller's ctx/tx. Any append error is returned so the caller's tx rolls back
// (no partial emit, no row-without-event).
func (s *OutboxWorkItemTransitionSink) AppendTransitions(ctx context.Context, transitions []agent.WorkItemTransition) error {
	for _, tr := range transitions {
		payload, err := json.Marshal(WorkItemTransitionPayload{
			WorkItemID: tr.WorkItemID,
			AgentID:    string(tr.AgentID),
			TaskRef:    tr.TaskRef,
			PrevStatus: string(tr.PrevStatus),
			Status:     string(tr.Status),
			Version:    tr.Version,
			OccurredAt: tr.OccurredAt,
		})
		if err != nil {
			return err
		}
		refs, err := json.Marshal(map[string]string{
			"work_item_id": tr.WorkItemID,
			"agent_id":     string(tr.AgentID),
			"task_ref":     tr.TaskRef,
		})
		if err != nil {
			return err
		}
		if err := s.outbox.Append(ctx, outbox.Event{
			ID:        s.idgen.NewULID(),
			EventType: EvtAgentWorkItemTransitioned,
			Refs:      string(refs),
			Payload:   string(payload),
			CreatedAt: tr.OccurredAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

var _ agent.WorkItemTransitionSink = (*OutboxWorkItemTransitionSink)(nil)

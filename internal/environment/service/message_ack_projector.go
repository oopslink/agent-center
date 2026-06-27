package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// evtReadStateChanged is the conversation BC event this projector subscribes to.
// (Mirror of the literal emitted in conversation/service/read_state.go MarkSeen.)
const evtReadStateChanged = "conversation.read_state.changed"

// markSeenTriggerAgentTool is the only trigger that yields a message_acknowledged
// activity — i.e. an agent DELIBERATELY confirmed a message it pulled via
// get_my_unread (PULL path). PUSH-path auto-advance (trigger=delivery) and human
// reads (trigger=human) are skipped: the delivery moment is already shown by the
// worker daemon's message_delivered event, so an auto-ack would be redundant noise.
const markSeenTriggerAgentTool = "agent_tool"

// MessageAckProjector turns an agent's deliberate mark_seen (conversation
// read_state.changed, trigger=agent_tool) into a message_acknowledged entry in
// that agent's append-only activity stream — closing the PULL-path "agent
// confirmed it read this" loop in the operator-facing timeline. It performs the
// Append AND records AppliedStore.MarkApplied in ONE tx so redelivery is a true
// no-op (the event id is the idempotency key).
type MessageAckProjector struct {
	db       *sql.DB
	activity agent.ActivityEventRepository
	applied  outbox.AppliedStore
	idgen    idgen.Generator
	clock    clock.Clock
}

// NewMessageAckProjector constructs the projector.
func NewMessageAckProjector(db *sql.DB, activity agent.ActivityEventRepository, applied outbox.AppliedStore, gen idgen.Generator, clk clock.Clock) *MessageAckProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &MessageAckProjector{db: db, activity: activity, applied: applied, idgen: gen, clock: clk}
}

// Name is the AppliedStore key.
func (p *MessageAckProjector) Name() string { return "conv-agent-msg-ack" }

type readStateChangedPayload struct {
	ConversationID string `json:"conversation_id"`
	UserID         string `json:"user_id"`
	LastSeenMsgID  string `json:"last_seen_message_id"`
	PreviousMsgID  string `json:"previous_last_seen_message_id"`
	Trigger        string `json:"trigger"`
}

// Project appends a message_acknowledged activity for an agent's deliberate
// mark_seen. Any non-matching event/trigger/user is a no-op.
func (p *MessageAckProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != evtReadStateChanged {
		return nil
	}
	var pl readStateChangedPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	// Whitelist: only an agent's DELIBERATE mark_seen produces an ack event.
	if pl.Trigger != markSeenTriggerAgentTool {
		return nil
	}
	if !strings.HasPrefix(pl.UserID, "agent:") {
		return nil
	}
	agentID := agent.AgentID(strings.TrimPrefix(pl.UserID, "agent:"))
	now := p.clock.Now()

	payload, err := json.Marshal(map[string]any{
		"conversation_id":               pl.ConversationID,
		"message_id":                    pl.LastSeenMsgID,
		"previous_last_seen_message_id": pl.PreviousMsgID,
	})
	if err != nil {
		return err
	}

	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		ev, err := agent.NewActivityEvent(agent.NewActivityEventInput{
			ID:         p.idgen.NewULID(),
			AgentID:    agentID,
			EventType:  agent.EventTypeMessageAcknowledged,
			Payload:    string(payload),
			OccurredAt: now,
		})
		if err != nil {
			return err
		}
		if err := p.activity.Append(txCtx, ev); err != nil {
			return err
		}
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

package projection

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

// currentActivityMaxRunes bounds the current_activity text snapshot so a long
// assistant_text / thinking turn does not bloat the projection row. Truncation
// is rune-safe (no split mid-codepoint).
const currentActivityMaxRunes = 200

// AgentWorkItemProjector is the v2.7 #111 Phase-1 outbox.Projector that fills
// the agent_work_item_projections read model. It is the transition-driven
// ("Opt1") design: it consumes EvtAgentWorkItemTransitioned and, for each
// transition, (a) sets status/agent_id from the event payload and (b)
// re-aggregates the work item's agent_activity_events stream into the metric
// columns (tool calls, token totals, current activity).
//
// Mirroring the canonical pm WorkItemProjector: the read load + UpsertIfFresh +
// AppliedStore.MarkApplied all run in the SAME transaction (persistence.RunInTx),
// so redelivery is a no-op and the projection write + dedup mark are atomic.
//
// A stale push (UpsertIfFresh → ErrProjectionStale, e.g. an out-of-order /
// replayed transition older than the stored last_activity_at) is benign: the
// event is still MarkApplied and Project returns nil, so the relay does not
// retry-loop on a write that has been legitimately superseded.
type AgentWorkItemProjector struct {
	db       *sql.DB
	repo     AgentWorkItemProjectionRepository
	activity agent.ActivityEventRepository
	applied  outbox.AppliedStore
	clock    clock.Clock
}

// NewAgentWorkItemProjector wires the projector. db is the tx root (the read
// load + UpsertIfFresh + MarkApplied compose in one tx). clk may be nil
// (defaults to SystemClock) — it is only used for the MarkApplied timestamp.
func NewAgentWorkItemProjector(db *sql.DB, repo AgentWorkItemProjectionRepository, activity agent.ActivityEventRepository, applied outbox.AppliedStore, clk clock.Clock) *AgentWorkItemProjector {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &AgentWorkItemProjector{db: db, repo: repo, activity: activity, applied: applied, clock: clk}
}

// Name is the AppliedStore dedup key for this projector.
func (p *AgentWorkItemProjector) Name() string { return "obs-agent-workitem-projection" }

// activityPayload is the FIXED shape produced by workerdaemon.streamActivityPayload
// (agent_controller.go). We only parse the fields this projector aggregates;
// unknown fields are ignored. The "type" field mirrors the event_type column.
type activityPayload struct {
	ToolName  string `json:"tool_name"`
	Text      string `json:"text"`
	TokensIn  int64  `json:"tokens_in"`
	TokensOut int64  `json:"tokens_out"`
}

// Project applies one EvtAgentWorkItemTransitioned event. Non-matching event
// types are a no-op (returns nil). See the type doc for the same-tx idempotency
// + stale-drop contract.
func (p *AgentWorkItemProjector) Project(ctx context.Context, e outbox.Event) error {
	if e.EventType != agentsvc.EvtAgentWorkItemTransitioned {
		return nil
	}
	var pl agentsvc.WorkItemTransitionPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		return err
	}
	now := p.clock.Now()
	return persistence.RunInTx(ctx, p.db, func(txCtx context.Context) error {
		if done, err := p.applied.IsApplied(txCtx, p.Name(), e.ID); err != nil {
			return err
		} else if done {
			return nil
		}
		events, err := p.activity.ListByWorkItem(txCtx, pl.WorkItemID)
		if err != nil {
			return err
		}
		update := aggregateWorkItemProjection(pl, events)
		_, _, err = p.repo.UpsertIfFresh(txCtx, pl.WorkItemID, update)
		if err != nil && !errors.Is(err, ErrProjectionStale) {
			return err
		}
		// On ErrProjectionStale the stored row is fresher; the write was a benign
		// no-op. We still MarkApplied so the relay does not redeliver this event.
		return p.applied.MarkApplied(txCtx, p.Name(), e.ID, now)
	})
}

// aggregateWorkItemProjection is the crux: it folds the transition event +
// the work item's activity stream into a single projection update.
//
//   - total_tool_calls          = # events with EventType()=="tool_use".
//   - total_tokens_input/output = Σ tokens_in / tokens_out over "result" rows.
//   - current_activity(_at)      = from the NEWEST (by OccurredAt) event among
//     {assistant_text, thinking, tool_use}: tool_use → tool_name; text types →
//     the (rune-truncated) text. Falls back to empty when no such event exists.
//   - last_activity_at           = max(transition OccurredAt, newest activity
//     OccurredAt); the transition time alone when there are no activity events.
//   - working_seconds_accumulated = 0 (no source on StreamEvent; deferred v2.8).
//   - status / agent_id          = straight from the transition payload.
func aggregateWorkItemProjection(pl agentsvc.WorkItemTransitionPayload, events []*agent.AgentActivityEvent) AgentWorkItemProjectionUpdate {
	var (
		toolCalls int64
		toksIn    int64
		toksOut   int64

		curActivity   string
		curActivityAt time.Time
		newestActAt   time.Time
	)
	for _, ev := range events {
		occurred := ev.OccurredAt()
		if occurred.After(newestActAt) {
			newestActAt = occurred
		}
		var ap activityPayload
		// A malformed payload simply contributes nothing to the aggregates.
		_ = json.Unmarshal([]byte(ev.Payload()), &ap)

		switch ev.EventType() {
		case "tool_use":
			toolCalls++
		case "result":
			toksIn += ap.TokensIn
			toksOut += ap.TokensOut
		}

		switch ev.EventType() {
		case "tool_use", "assistant_text", "thinking":
			// Newest-wins among the human-meaningful activity types. ">=" keeps
			// the later of two same-instant events (stable: events arrive oldest
			// first, so the last seen at a tie is the newest in arrival order).
			if !occurred.Before(curActivityAt) {
				curActivityAt = occurred
				if ev.EventType() == "tool_use" {
					curActivity = ap.ToolName
				} else {
					curActivity = truncateRunes(ap.Text, currentActivityMaxRunes)
				}
			}
		}
	}

	lastActivityAt := pl.OccurredAt
	if newestActAt.After(lastActivityAt) {
		lastActivityAt = newestActAt
	}

	return AgentWorkItemProjectionUpdate{
		AgentID:                   pl.AgentID,
		Status:                    pl.Status,
		CurrentActivity:           curActivity,
		CurrentActivityAt:         curActivityAt,
		TotalToolCalls:            toolCalls,
		TotalTokensInput:          toksIn,
		TotalTokensOutput:         toksOut,
		WorkingSecondsAccumulated: 0, // no source yet; deferred to v2.8.
		LastActivityAt:            lastActivityAt,
	}
}

// truncateRunes returns s clipped to at most max runes (codepoint-safe).
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

var _ outbox.Projector = (*AgentWorkItemProjector)(nil)

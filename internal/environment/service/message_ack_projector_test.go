package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	agentsql "github.com/oopslink/agent-center/internal/agent/sqlite"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type ackHarness struct {
	proj     *MessageAckProjector
	activity agent.ActivityEventRepository
	db       *sql.DB
	ctx      context.Context
}

func newAckHarness(t *testing.T) *ackHarness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	activityRepo := agentsql.NewActivityEventRepo(db)
	applied := outboxsql.NewAppliedRepo(db)
	proj := NewMessageAckProjector(db, activityRepo, applied, gen, clk)

	return &ackHarness{
		proj:     proj,
		activity: activityRepo,
		db:       db,
		ctx:      context.Background(),
	}
}

func (h *ackHarness) activityCount(t *testing.T, agentID string, eventType string) int {
	t.Helper()
	events, err := h.activity.ListByAgent(h.ctx, agent.AgentID(agentID), 0, "")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	count := 0
	for _, e := range events {
		if e.EventType() == eventType {
			count++
		}
	}
	return count
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestMessageAckProjector(t *testing.T) {
	cases := []struct {
		name    string
		trigger string
		userID  string
		wantApp bool // 是否应 Append 一条 message_acknowledged
	}{
		{"agent_tool_agent_user", "agent_tool", "agent:ag1", true},
		{"delivery_skipped", "delivery", "agent:ag1", false},
		{"human_skipped", "human", "user:u1", false},
		{"agent_tool_but_human_user", "agent_tool", "user:u1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newAckHarness(t) // db + activityRepo + appliedRepo + idgen + fixed clock
			ev := outbox.Event{
				ID:        "evt-" + tc.name,
				EventType: "conversation.read_state.changed",
				Payload: mustJSON(map[string]any{
					"conversation_id":               "conv1",
					"user_id":                       tc.userID,
					"last_seen_message_id":          "msg9",
					"previous_last_seen_message_id": "msg5",
					"trigger":                       tc.trigger,
				}),
			}
			if err := h.proj.Project(context.Background(), ev); err != nil {
				t.Fatalf("Project: %v", err)
			}
			got := h.activityCount(t, "ag1", "message_acknowledged") // ListByAgent → count event_type
			if tc.wantApp && got != 1 {
				t.Fatalf("want 1 ack, got %d", got)
			}
			if !tc.wantApp && got != 0 {
				t.Fatalf("want 0 ack, got %d", got)
			}
		})
	}
}

func TestMessageAckProjector_Idempotent(t *testing.T) {
	h := newAckHarness(t)
	ev := outbox.Event{ID: "evt-1", EventType: "conversation.read_state.changed",
		Payload: mustJSON(map[string]any{"conversation_id": "c1", "user_id": "agent:ag1",
			"last_seen_message_id": "m9", "previous_last_seen_message_id": "m5", "trigger": "agent_tool"})}
	_ = h.proj.Project(context.Background(), ev)
	_ = h.proj.Project(context.Background(), ev) // replay
	if got := h.activityCount(t, "ag1", "message_acknowledged"); got != 1 {
		t.Fatalf("replay should be no-op; got %d acks", got)
	}
}

func TestMessageAckProjector_IgnoresUnrelatedEvent(t *testing.T) {
	h := newAckHarness(t)
	ev := outbox.Event{ID: "evt-x", EventType: "conversation.message_added", Payload: "{}"}
	if err := h.proj.Project(context.Background(), ev); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if got := h.activityCount(t, "ag1", "message_acknowledged"); got != 0 {
		t.Fatalf("unrelated event must be no-op; got %d", got)
	}
}

package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newActivityDB(t *testing.T) *ActivityEventRepo {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewActivityEventRepo(d)
}

func mkActivity(t *testing.T, id string, agentID agent.AgentID, occurredAt time.Time, payload string) *agent.AgentActivityEvent {
	t.Helper()
	e, err := agent.NewActivityEvent(agent.NewActivityEventInput{
		ID: id, AgentID: agentID, EventType: agent.EventTypeAssistantText,
		Payload: payload, OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// v2.8.1 #278: LatestByAgents returns the single most-recent event per agent across
// multiple agents in ONE window-function query.
func TestActivityEventRepo_LatestByAgents_Batch(t *testing.T) {
	r := newActivityDB(t)
	ctx := context.Background()
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	// A1 has 3 events; A2 has 1; A3 has none.
	for i, ts := range []time.Duration{0, time.Minute, 2 * time.Minute} {
		if err := r.Append(ctx, mkActivity(t, "a1-"+string(rune('a'+i)), "A1", base.Add(ts), `{"text":"hi"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Append(ctx, mkActivity(t, "a2-a", "A2", base.Add(30*time.Second), `{"text":"yo"}`)); err != nil {
		t.Fatal(err)
	}

	got, err := r.LatestByAgents(ctx, []agent.AgentID{"A1", "A2", "A3"})
	if err != nil {
		t.Fatal(err)
	}
	// A1: latest is the +2min event (id a1-c).
	if e := got["A1"]; e == nil || e.ID() != "a1-c" {
		t.Fatalf("A1 latest = %v, want a1-c", e)
	}
	// A2: only event.
	if e := got["A2"]; e == nil || e.ID() != "a2-a" {
		t.Fatalf("A2 latest = %v, want a2-a", e)
	}
	// A3: no events → no entry.
	if _, ok := got["A3"]; ok {
		t.Fatal("A3 should have no entry")
	}
}

func TestActivityEventRepo_LatestByAgents_Empty(t *testing.T) {
	r := newActivityDB(t)
	got, err := r.LatestByAgents(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty input want empty map, got %d", len(got))
	}
}

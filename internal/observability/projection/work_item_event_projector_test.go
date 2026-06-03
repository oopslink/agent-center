package projection

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentsvc "github.com/oopslink/agent-center/internal/agent/service"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type fakeEmitter struct {
	cmds []observability.EmitCommand
	err  error
}

func (f *fakeEmitter) Emit(_ context.Context, cmd observability.EmitCommand) (observability.EventID, error) {
	if f.err != nil {
		return "", f.err
	}
	f.cmds = append(f.cmds, cmd)
	return observability.EventID("E-fake"), nil
}

var t0wie = time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)

func wieSetup(t *testing.T) (*WorkItemEventProjector, *fakeEmitter, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	em := &fakeEmitter{}
	proj := NewWorkItemEventProjector(db, em, outboxsql.NewAppliedRepo(db), clock.NewFakeClock(t0wie))
	return proj, em, context.Background()
}

func wieEvent(t *testing.T, id string, pl agentsvc.WorkItemTransitionPayload) outbox.Event {
	t.Helper()
	b, _ := json.Marshal(pl)
	return outbox.Event{ID: id, EventType: agentsvc.EvtAgentWorkItemTransitioned, Payload: string(b)}
}

func TestWorkItemEventProjector_EmitsObservabilityEvent(t *testing.T) {
	proj, em, ctx := wieSetup(t)
	pl := agentsvc.WorkItemTransitionPayload{
		WorkItemID: "WI1", AgentID: "AG1", TaskRef: "pm://tasks/T1",
		PrevStatus: "active", Status: "failed", Version: 3, Cause: "agent_death", OccurredAt: t0wie,
	}
	if err := proj.Project(ctx, wieEvent(t, "E-1", pl)); err != nil {
		t.Fatal(err)
	}
	if len(em.cmds) != 1 {
		t.Fatalf("want 1 emitted observability event, got %d", len(em.cmds))
	}
	c := em.cmds[0]
	if c.EventType != observability.EventType("agent.work_item.transitioned") {
		t.Fatalf("event_type = %q", c.EventType)
	}
	if c.Actor != observability.Actor("agent:AG1") {
		t.Fatalf("actor = %q (want agent:AG1)", c.Actor)
	}
	if c.Refs.AgentID != "AG1" || c.Refs.TaskID != "T1" || c.Refs.WorkItemID != "WI1" {
		t.Fatalf("refs wrong: %+v (TaskID must be extracted from pm://tasks/T1)", c.Refs)
	}
	if !c.OccurredAt.Equal(t0wie) {
		t.Fatalf("occurred_at = %v, want %v", c.OccurredAt, t0wie)
	}
	if c.Payload["status"] != "failed" || c.Payload["prev_status"] != "active" || c.Payload["cause"] != "agent_death" {
		t.Fatalf("payload wrong: %+v", c.Payload)
	}
	if v, ok := c.Payload["version"].(int); !ok || v != 3 {
		t.Fatalf("payload version wrong: %+v", c.Payload["version"])
	}
}

func TestWorkItemEventProjector_Idempotent(t *testing.T) {
	proj, em, ctx := wieSetup(t)
	ev := wieEvent(t, "E-1", agentsvc.WorkItemTransitionPayload{WorkItemID: "WI1", AgentID: "AG1", TaskRef: "pm://tasks/T1", Status: "active", Version: 2, OccurredAt: t0wie})
	if err := proj.Project(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if err := proj.Project(ctx, ev); err != nil { // redelivery
		t.Fatal(err)
	}
	if len(em.cmds) != 1 {
		t.Fatalf("redelivery must not double-emit; got %d", len(em.cmds))
	}
}

func TestWorkItemEventProjector_IgnoresNonMatching(t *testing.T) {
	proj, em, ctx := wieSetup(t)
	if err := proj.Project(ctx, outbox.Event{ID: "E-x", EventType: "pm.task.assigned", Payload: "{}"}); err != nil {
		t.Fatal(err)
	}
	if len(em.cmds) != 0 {
		t.Fatalf("unrelated event must be ignored, got %d emits", len(em.cmds))
	}
}

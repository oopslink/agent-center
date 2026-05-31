package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/outbox"
)

type capOutbox struct {
	events []outbox.Event
	err    error
}

func (c *capOutbox) Append(_ context.Context, e outbox.Event) error {
	if c.err != nil {
		return c.err
	}
	c.events = append(c.events, e)
	return nil
}
func (c *capOutbox) FetchUnprocessed(context.Context, int) ([]outbox.Event, error) { return nil, nil }
func (c *capOutbox) MarkProcessed(context.Context, string, time.Time) error        { return nil }

type seqGen struct{ n int }

func (g *seqGen) NewULID() string { g.n++; return "E-" + string(rune('0'+g.n)) }

func TestOutboxWorkItemTransitionSink_AppendsTransitionedEvents(t *testing.T) {
	co := &capOutbox{}
	sink := NewOutboxWorkItemTransitionSink(co, &seqGen{})
	ts := []agent.WorkItemTransition{
		{WorkItemID: "WI1", AgentID: "A1", TaskRef: "pm://tasks/T1", PrevStatus: agent.WorkItemQueued, Status: agent.WorkItemActive, Version: 2, OccurredAt: tNow},
		{WorkItemID: "WI1", AgentID: "A1", TaskRef: "pm://tasks/T1", PrevStatus: agent.WorkItemActive, Status: agent.WorkItemDone, Version: 3, OccurredAt: tNow},
	}
	if err := sink.AppendTransitions(context.Background(), ts); err != nil {
		t.Fatal(err)
	}
	if len(co.events) != 2 {
		t.Fatalf("want one outbox event per transition (2), got %d", len(co.events))
	}
	e := co.events[0]
	if e.EventType != EvtAgentWorkItemTransitioned {
		t.Fatalf("event_type want %q, got %q", EvtAgentWorkItemTransitioned, e.EventType)
	}
	if e.ID == "" {
		t.Fatal("event id must be set (from generator)")
	}
	if !e.CreatedAt.Equal(tNow) {
		t.Fatalf("created_at want %v, got %v", tNow, e.CreatedAt)
	}
	var pl WorkItemTransitionPayload
	if err := json.Unmarshal([]byte(e.Payload), &pl); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if pl.WorkItemID != "WI1" || pl.AgentID != "A1" || pl.TaskRef != "pm://tasks/T1" {
		t.Fatalf("payload identity wrong: %+v", pl)
	}
	if pl.PrevStatus != "queued" || pl.Status != "active" || pl.Version != 2 {
		t.Fatalf("payload transition wrong: %+v", pl)
	}
	// Refs must carry the cross-BC keys consumers/relay use.
	var refs map[string]string
	if err := json.Unmarshal([]byte(e.Refs), &refs); err != nil {
		t.Fatalf("refs not valid JSON: %v", err)
	}
	if refs["work_item_id"] != "WI1" || refs["agent_id"] != "A1" || refs["task_ref"] != "pm://tasks/T1" {
		t.Fatalf("refs wrong: %v", refs)
	}
	// distinct ids per event
	if co.events[0].ID == co.events[1].ID {
		t.Fatal("each event must get a distinct id")
	}
}

func TestOutboxWorkItemTransitionSink_PropagatesError(t *testing.T) {
	co := &capOutbox{err: errors.New("boom")}
	sink := NewOutboxWorkItemTransitionSink(co, &seqGen{})
	err := sink.AppendTransitions(context.Background(), []agent.WorkItemTransition{
		{WorkItemID: "WI1", AgentID: "A1", TaskRef: "t", PrevStatus: "", Status: agent.WorkItemQueued, Version: 1, OccurredAt: tNow},
	})
	if err == nil {
		t.Fatal("sink must propagate outbox append error (so the tx rolls back)")
	}
}

func TestOutboxWorkItemTransitionSink_EmptyNoop(t *testing.T) {
	co := &capOutbox{}
	sink := NewOutboxWorkItemTransitionSink(co, &seqGen{})
	if err := sink.AppendTransitions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(co.events) != 0 {
		t.Fatalf("empty transitions must append nothing, got %d", len(co.events))
	}
}

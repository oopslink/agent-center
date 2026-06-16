package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/outbox"
)

type fakeDeliverer struct {
	calls   int
	lastTo  string
	lastMsg string
	err     error
}

func (f *fakeDeliverer) Deliver(_ context.Context, remindeeAgentID, content, _ string) error {
	f.calls++
	f.lastTo = remindeeAgentID
	f.lastMsg = content
	return f.err
}

func TestDeliveryProjector_FiredEvent_Delivers(t *testing.T) {
	d := &fakeDeliverer{}
	p := NewReminderDeliveryProjector(d)
	e := outbox.Event{
		ID:        "ev-1",
		EventType: string(EventReminderFired),
		Payload:   `{"reminder_id":"rmd-1","remindee_agent_id":"AG2","content":"standup time"}`,
	}
	if err := p.Project(context.Background(), e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if d.calls != 1 || d.lastTo != "AG2" || d.lastMsg != "standup time" {
		t.Errorf("delivery: calls=%d to=%q msg=%q", d.calls, d.lastTo, d.lastMsg)
	}
}

func TestDeliveryProjector_IgnoresOtherEvents(t *testing.T) {
	d := &fakeDeliverer{}
	p := NewReminderDeliveryProjector(d)
	e := outbox.Event{ID: "ev-2", EventType: "pm.task.assigned", Payload: `{}`}
	if err := p.Project(context.Background(), e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if d.calls != 0 {
		t.Errorf("non-fired event must not deliver: calls=%d", d.calls)
	}
}

func TestDeliveryProjector_DeliveryError_Propagates(t *testing.T) {
	d := &fakeDeliverer{err: errors.New("boom")}
	p := NewReminderDeliveryProjector(d)
	e := outbox.Event{
		ID:        "ev-3",
		EventType: string(EventReminderFired),
		Payload:   `{"reminder_id":"r","remindee_agent_id":"AG2","content":"x"}`,
	}
	if err := p.Project(context.Background(), e); err == nil {
		t.Errorf("delivery error must propagate so the relay retries")
	}
}

func TestDeliveryProjector_BadPayload_Errors(t *testing.T) {
	p := NewReminderDeliveryProjector(&fakeDeliverer{})
	e := outbox.Event{ID: "ev-4", EventType: string(EventReminderFired), Payload: `not json`}
	if err := p.Project(context.Background(), e); err == nil {
		t.Errorf("bad payload should error")
	}
	// missing remindee → error.
	e2 := outbox.Event{ID: "ev-5", EventType: string(EventReminderFired), Payload: `{"content":"x"}`}
	if err := p.Project(context.Background(), e2); err == nil {
		t.Errorf("missing remindee should error")
	}
}

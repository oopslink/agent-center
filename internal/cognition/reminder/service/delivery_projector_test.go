package service

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	"github.com/oopslink/agent-center/internal/outbox"
)

type fakeDeliverer struct {
	calls   int
	lastTo  string
	lastMsg string
	lastReq DeliveryRequest
	err     error
}

func (f *fakeDeliverer) Deliver(_ context.Context, req DeliveryRequest) error {
	f.calls++
	f.lastTo = req.RemindeeAgentID
	f.lastMsg = req.Content
	f.lastReq = req
	return f.err
}

// fakeFiringMarker records pending→delivered write-backs.
type fakeFiringMarker struct {
	calls   int
	lastID  string
	lastOut reminder.FiringOutcome
	err     error
}

func (f *fakeFiringMarker) UpdateFiringOutcome(_ context.Context, firingID string, outcome reminder.FiringOutcome) error {
	f.calls++
	f.lastID = firingID
	f.lastOut = outcome
	return f.err
}

func TestDeliveryProjector_FiredEvent_Delivers(t *testing.T) {
	d := &fakeDeliverer{}
	m := &fakeFiringMarker{}
	p := NewReminderDeliveryProjector(d, m)
	e := outbox.Event{
		ID:        "ev-1",
		EventType: string(EventReminderFired),
		Payload:   `{"reminder_id":"rmd-1","remindee_agent_id":"AG2","content":"standup time","firing_id":"fire-1"}`,
	}
	if err := p.Project(context.Background(), e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if d.calls != 1 || d.lastTo != "AG2" || d.lastMsg != "standup time" {
		t.Errorf("delivery: calls=%d to=%q msg=%q", d.calls, d.lastTo, d.lastMsg)
	}
	// After delivery the firing is resolved pending→delivered.
	if m.calls != 1 || m.lastID != "fire-1" || m.lastOut != reminder.OutcomeDelivered {
		t.Errorf("write-back: calls=%d id=%q outcome=%q, want 1/fire-1/delivered", m.calls, m.lastID, m.lastOut)
	}
}

func TestDeliveryProjector_PassesDeliveryIdentity(t *testing.T) {
	d := &fakeDeliverer{}
	p := NewReminderDeliveryProjector(d, &fakeFiringMarker{})
	e := outbox.Event{
		ID:        "ev-fb",
		EventType: string(EventReminderFired),
		Payload:   `{"reminder_id":"rmd-1","remindee_agent_id":"AG2","content":"x","creator_ref":"agent:AG1","deliver_as_creator":true,"firing_id":"fire-1"}`,
	}
	if err := p.Project(context.Background(), e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if !d.lastReq.DeliverAsCreator || d.lastReq.CreatorRef != "agent:AG1" {
		t.Errorf("F-B identity not forwarded: deliver_as_creator=%v creator_ref=%q",
			d.lastReq.DeliverAsCreator, d.lastReq.CreatorRef)
	}
}

func TestDeliveryProjector_IgnoresOtherEvents(t *testing.T) {
	d := &fakeDeliverer{}
	p := NewReminderDeliveryProjector(d, &fakeFiringMarker{})
	e := outbox.Event{ID: "ev-2", EventType: "pm.task.assigned", Payload: `{}`}
	if err := p.Project(context.Background(), e); err != nil {
		t.Fatalf("Project: %v", err)
	}
	if d.calls != 0 {
		t.Errorf("non-fired event must not deliver: calls=%d", d.calls)
	}
}

func TestDeliveryProjector_DeliveryError_Propagates_NoWriteBack(t *testing.T) {
	d := &fakeDeliverer{err: errors.New("boom")}
	m := &fakeFiringMarker{}
	p := NewReminderDeliveryProjector(d, m)
	e := outbox.Event{
		ID:        "ev-3",
		EventType: string(EventReminderFired),
		Payload:   `{"reminder_id":"r","remindee_agent_id":"AG2","content":"x","firing_id":"fire-9"}`,
	}
	if err := p.Project(context.Background(), e); err == nil {
		t.Errorf("delivery error must propagate so the relay retries")
	}
	// Delivery failed → firing must stay pending (no write-back).
	if m.calls != 0 {
		t.Errorf("failed delivery must not mark delivered: calls=%d", m.calls)
	}
}

func TestDeliveryProjector_BadPayload_Errors(t *testing.T) {
	p := NewReminderDeliveryProjector(&fakeDeliverer{}, &fakeFiringMarker{})
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

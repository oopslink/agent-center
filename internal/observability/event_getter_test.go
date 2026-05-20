package observability

import (
	"testing"
	"time"
)

func TestEvent_RefsGetter(t *testing.T) {
	e, err := NewEvent(NewEventInput{
		ID:         EventID("01HQK6XHRJV2J5TY1JNN1V8DPK"),
		OccurredAt: time.Now(),
		Seq:        1,
		EventType:  EventType("test.event"),
		Actor:      Actor("system"),
		Refs:       EventRefs{TaskID: "T-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if e.Refs().TaskID != "T-1" {
		t.Fatal("Refs getter")
	}
}

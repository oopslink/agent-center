package observability

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
)

func validULID() EventID {
	return EventID(idgen.MustNewULID())
}

func validInput() NewEventInput {
	return NewEventInput{
		ID:         validULID(),
		OccurredAt: time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC),
		Seq:        1,
		EventType:  "workforce.worker.enrolled",
		Refs:       EventRefs{WorkerID: "W-1"},
		Actor:      "user:hayang",
		Payload:    map[string]any{"capabilities": []string{"claude-code"}},
	}
}

func TestNewEvent_Happy(t *testing.T) {
	in := validInput()
	e, err := NewEvent(in)
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if e.ID() != in.ID {
		t.Fatalf("ID: got %q want %q", e.ID(), in.ID)
	}
	if !e.OccurredAt().Equal(in.OccurredAt) {
		t.Fatalf("OccurredAt mismatch")
	}
	if e.Seq() != 1 {
		t.Fatal("Seq")
	}
	if e.Type() != "workforce.worker.enrolled" {
		t.Fatal("Type")
	}
}

func TestNewEvent_RejectsEmptyType(t *testing.T) {
	in := validInput()
	in.EventType = ""
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for empty type")
	}
}

func TestNewEvent_RejectsBadTypeNoSeparator(t *testing.T) {
	in := validInput()
	in.EventType = "noseparator"
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for missing dot")
	}
}

func TestNewEvent_RejectsBadTypeUppercase(t *testing.T) {
	in := validInput()
	in.EventType = "Workforce.worker.enrolled"
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for uppercase")
	}
}

func TestNewEvent_RejectsBadActorPrefix(t *testing.T) {
	in := validInput()
	in.Actor = "foo:bar"
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for bad actor")
	}
}

func TestNewEvent_RejectsActorEmpty(t *testing.T) {
	in := validInput()
	in.Actor = ""
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for empty actor")
	}
}

func TestNewEvent_AcceptsSystemActor(t *testing.T) {
	in := validInput()
	in.Actor = "system"
	if _, err := NewEvent(in); err != nil {
		t.Fatalf("system actor rejected: %v", err)
	}
}

func TestNewEvent_AcceptsAllPrefixedActors(t *testing.T) {
	for _, a := range []string{"user:hayang", "supervisor:inv-1", "worker:W-1", "agent:a-1"} {
		in := validInput()
		in.Actor = Actor(a)
		if _, err := NewEvent(in); err != nil {
			t.Fatalf("actor %q rejected: %v", a, err)
		}
	}
}

func TestNewEvent_RejectsZeroOccurredAt(t *testing.T) {
	in := validInput()
	in.OccurredAt = time.Time{}
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for zero occurred_at")
	}
}

func TestNewEvent_RejectsZeroSeq(t *testing.T) {
	in := validInput()
	in.Seq = 0
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for zero seq")
	}
}

func TestNewEvent_RejectsEmptyID(t *testing.T) {
	in := validInput()
	in.ID = ""
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestNewEvent_RejectsNonULIDID(t *testing.T) {
	in := validInput()
	in.ID = "not-a-ulid"
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error for non-ULID id")
	}
}

func TestNewEvent_RejectsReasonWithoutMessage(t *testing.T) {
	in := validInput()
	in.Payload = map[string]any{"reason": "worker_lost"}
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error: reason without message")
	}
}

func TestNewEvent_RejectsReasonEmptyMessage(t *testing.T) {
	in := validInput()
	in.Payload = map[string]any{"reason": "worker_lost", "message": ""}
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error: empty message")
	}
}

func TestNewEvent_AcceptsReasonWithMessage(t *testing.T) {
	in := validInput()
	in.Payload = map[string]any{"reason": "worker_lost", "message": "heartbeat 60s silent"}
	if _, err := NewEvent(in); err != nil {
		t.Fatalf("rejected reason+message: %v", err)
	}
}

func TestNewEvent_RejectsReasonNonString(t *testing.T) {
	in := validInput()
	in.Payload = map[string]any{"reason": 42, "message": "x"}
	_, err := NewEvent(in)
	if err == nil {
		t.Fatal("expected error: non-string reason")
	}
}

func TestNewEvent_DefaultsCreatedAtToOccurredAt(t *testing.T) {
	in := validInput()
	in.CreatedAt = time.Time{}
	e, err := NewEvent(in)
	if err != nil {
		t.Fatal(err)
	}
	if !e.CreatedAt().Equal(in.OccurredAt) {
		t.Fatalf("CreatedAt default: got %v want %v", e.CreatedAt(), in.OccurredAt)
	}
}

func TestEvent_PayloadIsCopy(t *testing.T) {
	in := validInput()
	in.Payload = map[string]any{"a": 1}
	e, _ := NewEvent(in)
	got := e.Payload()
	got["a"] = 999
	got["new"] = 2
	again := e.Payload()
	if again["a"] != 1 {
		t.Fatal("mutation leaked back into event")
	}
	if _, ok := again["new"]; ok {
		t.Fatal("new key leaked back into event")
	}
}

func TestEvent_RefsJSON(t *testing.T) {
	in := validInput()
	in.Refs = EventRefs{WorkerID: "W-1", ProjectID: "p1"}
	e, _ := NewEvent(in)
	b, err := e.RefsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["worker_id"] != "W-1" || m["project_id"] != "p1" {
		t.Fatalf("refs marshalled wrong: %s", string(b))
	}
}

func TestEvent_PayloadJSON(t *testing.T) {
	in := validInput()
	in.Payload = map[string]any{"foo": "bar"}
	e, _ := NewEvent(in)
	b, err := e.PayloadJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "foo") {
		t.Fatalf("payload marshalled wrong: %s", string(b))
	}
}

func TestEvent_OccurredAtUTC(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")
	in := validInput()
	in.OccurredAt = time.Date(2026, 5, 20, 10, 0, 0, 0, loc)
	e, _ := NewEvent(in)
	if e.OccurredAt().Location().String() != "UTC" {
		t.Fatalf("OccurredAt not UTC: %v", e.OccurredAt())
	}
}

func TestEventType_Validate(t *testing.T) {
	cases := []struct {
		in EventType
		ok bool
	}{
		{"", false},
		{"noseparator", false},
		{"Workforce.worker.enrolled", false},
		{"workforce.worker.enrolled", true},
		{"x.y", true},
		{"x-y.z", false},
	}
	for _, c := range cases {
		err := c.in.Validate()
		if (err == nil) != c.ok {
			t.Fatalf("EventType(%q).Validate() ok=%v err=%v", c.in, c.ok, err)
		}
	}
}

func TestActor_Validate(t *testing.T) {
	cases := []struct {
		in Actor
		ok bool
	}{
		{"", false},
		{"system", true},
		{"user:hayang", true},
		{"user:", false},
		{"foo:bar", false},
		{"supervisor:inv-1", true},
	}
	for _, c := range cases {
		err := c.in.Validate()
		if (err == nil) != c.ok {
			t.Fatalf("Actor(%q).Validate() ok=%v err=%v", c.in, c.ok, err)
		}
	}
}

func TestEvent_NoMutatorFieldsExposed(t *testing.T) {
	// Reflection sanity: confirm Event has no exported setter methods (only
	// constructor + getters). This is the closest we can come to enforcing
	// immutability of the AR.
	in := validInput()
	e, err := NewEvent(in)
	if err != nil {
		t.Fatal(err)
	}
	rt := reflect.TypeOf(e)
	for i := 0; i < rt.NumMethod(); i++ {
		name := rt.Method(i).Name
		if strings.HasPrefix(name, "Set") {
			t.Fatalf("Event exposes setter method %q (must be immutable)", name)
		}
	}
}

func TestEventID_String(t *testing.T) {
	id := EventID("01ABC")
	if id.String() != "01ABC" {
		t.Fatal("EventID.String")
	}
}

func TestEventType_String(t *testing.T) {
	if EventType("x.y").String() != "x.y" {
		t.Fatal()
	}
}

func TestActor_String(t *testing.T) {
	if Actor("system").String() != "system" {
		t.Fatal()
	}
}

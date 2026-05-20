package inputrequest

import (
	"errors"
	"testing"
	"time"
)

var ref = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

func mkPending(t *testing.T) *InputRequest {
	t.Helper()
	ir, err := New(NewInput{
		ID:              "IR-1",
		TaskExecutionID: "E-1",
		Question:        "proceed?",
		Options:         []string{"yes", "no"},
		Urgency:         UrgencyNormal,
		Now:             ref,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return ir
}

func TestNew_Happy(t *testing.T) {
	ir := mkPending(t)
	if ir.Status() != StatusPending {
		t.Fatalf("status: %s", ir.Status())
	}
	if ir.Urgency() != UrgencyNormal {
		t.Fatalf("urgency: %s", ir.Urgency())
	}
	js, err := ir.OptionsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if js != `["yes","no"]` {
		t.Fatalf("options: %s", js)
	}
}

func TestNew_Rejects(t *testing.T) {
	cases := []struct {
		name string
		in   NewInput
	}{
		{"no id", NewInput{TaskExecutionID: "E", Question: "q", Now: ref}},
		{"no exec", NewInput{ID: "IR", Question: "q", Now: ref}},
		{"no question", NewInput{ID: "IR", TaskExecutionID: "E", Now: ref}},
		{"no now", NewInput{ID: "IR", TaskExecutionID: "E", Question: "q"}},
		{"bad urgency", NewInput{ID: "IR", TaskExecutionID: "E", Question: "q", Urgency: "WAT", Now: ref}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := New(c.in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestRespond_Happy(t *testing.T) {
	ir := mkPending(t)
	if err := ir.Respond(InputResponse{Answer: "yes", DecidedBy: "human:hayang", DecidedAt: ref}); err != nil {
		t.Fatal(err)
	}
	if ir.Status() != StatusResponded {
		t.Fatalf("status: %s", ir.Status())
	}
	if ir.ResponseText() != "yes" {
		t.Fatalf("resp: %s", ir.ResponseText())
	}
}

func TestRespond_Validation(t *testing.T) {
	ir := mkPending(t)
	if err := ir.Respond(InputResponse{Answer: "", DecidedBy: "u", DecidedAt: ref}); err == nil {
		t.Fatal("expected answer")
	}
	if err := ir.Respond(InputResponse{Answer: "y", DecidedBy: "", DecidedAt: ref}); err == nil {
		t.Fatal("expected decided_by")
	}
	if err := ir.Respond(InputResponse{Answer: "y", DecidedBy: "u"}); err == nil {
		t.Fatal("expected decided_at")
	}
}

func TestRespond_OnTerminal(t *testing.T) {
	ir := mkPending(t)
	_ = ir.MarkTimedOut("input_timeout_t2", "no answer", ref)
	if err := ir.Respond(InputResponse{Answer: "x", DecidedBy: "u", DecidedAt: ref}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition")
	}
}

func TestMarkTimedOut_Happy(t *testing.T) {
	ir := mkPending(t)
	if err := ir.MarkTimedOut("input_timeout_t2", "24h", ref); err != nil {
		t.Fatal(err)
	}
	if ir.Status() != StatusTimedOut {
		t.Fatalf("status: %s", ir.Status())
	}
	if ir.EndedReason() != "input_timeout_t2" || ir.EndedMessage() != "24h" {
		t.Fatalf("ended: %s/%s", ir.EndedReason(), ir.EndedMessage())
	}
}

func TestMarkTimedOut_Validation(t *testing.T) {
	ir := mkPending(t)
	if err := ir.MarkTimedOut("", "m", ref); err == nil {
		t.Fatal("expected reason")
	}
	if err := ir.MarkTimedOut("r", "", ref); err == nil {
		t.Fatal("expected message")
	}
	_ = ir.Respond(InputResponse{Answer: "y", DecidedBy: "u", DecidedAt: ref})
	if err := ir.MarkTimedOut("r", "m", ref); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("expected invalid transition")
	}
}

func TestMarkCanceled_HappyAndValidation(t *testing.T) {
	ir := mkPending(t)
	if err := ir.MarkCanceled("kill_precondition", "abandon", ref); err != nil {
		t.Fatal(err)
	}
	if ir.Status() != StatusCanceled {
		t.Fatalf("status: %s", ir.Status())
	}
	if err := ir.MarkCanceled("r", "m", ref); !errors.Is(err, ErrInvalidTransition) {
		t.Fatal("expected invalid")
	}
	// Validation
	ir2 := mkPending(t)
	if err := ir2.MarkCanceled("", "m", ref); err == nil {
		t.Fatal("expected reason")
	}
	if err := ir2.MarkCanceled("r", "", ref); err == nil {
		t.Fatal("expected message")
	}
}

func TestRehydrate(t *testing.T) {
	in := RehydrateInput{
		ID:              "IR-1",
		TaskExecutionID: "E-1",
		Status:          StatusPending,
		Question:        "q",
		Urgency:         UrgencyNormal,
		RequestedAt:     ref,
		CreatedAt:       ref,
		UpdatedAt:       ref,
		Version:         1,
	}
	got, err := Rehydrate(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != StatusPending {
		t.Fatalf("status: %s", got.Status())
	}
	in.Status = "garbage"
	if _, err := Rehydrate(in); !errors.Is(err, ErrInvalidStatus) {
		t.Fatal("expected invalid status")
	}
	in.Status = StatusPending
	in.Urgency = "wat"
	if _, err := Rehydrate(in); !errors.Is(err, ErrInvalidUrgency) {
		t.Fatal("expected invalid urgency")
	}
	in.Urgency = UrgencyNormal
	in.Version = 0
	if _, err := Rehydrate(in); err == nil {
		t.Fatal("expected version")
	}
}

func TestParseUrgency(t *testing.T) {
	u, err := ParseUrgency("")
	if err != nil || u != UrgencyNormal {
		t.Fatalf("default: %v / %s", err, u)
	}
	u, err = ParseUrgency("urgent")
	if err != nil || u != UrgencyUrgent {
		t.Fatalf("urgent: %v / %s", err, u)
	}
	if _, err := ParseUrgency("nope"); !errors.Is(err, ErrInvalidUrgency) {
		t.Fatal("expected invalid")
	}
}

func TestStatus_Enum(t *testing.T) {
	if !StatusResponded.IsTerminal() || !StatusTimedOut.IsTerminal() || !StatusCanceled.IsTerminal() {
		t.Fatal("expected terminal")
	}
	if StatusPending.IsTerminal() {
		t.Fatal("pending should not be terminal")
	}
}

package workerdaemon

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

func TestReconcileResponder_Apply(t *testing.T) {
	type call struct {
		ID     string
		Reason execution.KilledReason
	}
	var calls []call
	r := NewReconcileResponder(NoopUploader{}, func(id string, reason execution.KilledReason) error {
		calls = append(calls, call{id, reason})
		return nil
	})
	if err := r.Apply(context.Background(), reconcileResponseHelper()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls: %d", len(calls))
	}
	if calls[0].Reason != execution.KilledReconcileStale {
		t.Fatalf("stale: %s", calls[0].Reason)
	}
	if calls[1].Reason != execution.KilledReconcileUnknown {
		t.Fatalf("unknown: %s", calls[1].Reason)
	}
}

func TestReconcileResponder_NilKillHandler(t *testing.T) {
	r := NewReconcileResponder(NoopUploader{}, nil)
	if err := r.Apply(context.Background(), reconcileResponseHelper()); err == nil {
		t.Fatal("expected error")
	}
}

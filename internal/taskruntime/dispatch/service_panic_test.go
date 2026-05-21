package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestScanPendingAck_TaskMissing_PanicsAsInvariant exercises the panic
// branch in Service.clearTaskCurrent: when an execution refers to a task
// that has been deleted, the application layer treats it as an invariant
// violation (per conventions § 9.w + § 17) rather than silently no-op.
//
// We construct the scenario by hand-deleting the task row after a
// successful Dispatch and before ScanPendingAck fires.
func TestScanPendingAck_TaskMissing_PanicsAsInvariant(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	_, err := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a referential-integrity violation: task gone, execution
	// still pending_ack. Schema declares no FK (conventions § 9.w), so
	// this DELETE succeeds.
	if _, err := h.db.ExecContext(context.Background(), `DELETE FROM tasks WHERE id = ?`, "T-1"); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(35 * time.Second)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from invariant violation")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "invariant violated") || !strings.Contains(msg, "task") {
			t.Fatalf("unexpected panic message: %s", msg)
		}
	}()
	_, _ = h.svc.ScanPendingAck(context.Background(), "system")
	t.Fatal("ScanPendingAck should have panicked")
}

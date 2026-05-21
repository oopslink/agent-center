package timeoutscan

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// TestScanner_TaskMissing_PanicsAsInvariant pins the panic branch in
// Scanner.clearTaskCurrent introduced for conventions § 9.w + § 17.
// We hand-delete the parent task while its execution is still active so
// the scanner's submitted_timeout path tries to clear the now-missing
// task — that's an invariant violation, so it panics.
func TestScanner_TaskMissing_PanicsAsInvariant(t *testing.T) {
	h := setupHarness(t)
	seedTaskAndExec(t, h, "T-1", "E-1", execution.StatusSubmitted, h.clk.Now())
	// Delete the parent task row (no FK, conventions § 9.w) — simulates
	// a referential-integrity violation upstream.
	if _, err := h.db.ExecContext(context.Background(), `DELETE FROM tasks WHERE id = ?`, "T-1"); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(6 * time.Minute)
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
	_ = h.scanner.Tick(context.Background(), "system")
	t.Fatal("Scanner.Tick should have panicked")
}

package kill

import (
	"context"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// TestMarkKilledInTx_TaskMissing_PanicsAsInvariant pins the panic branch
// introduced for conventions § 9.w + § 17 in markKilledInTx: if the
// task referenced by the execution is gone when we go to clear
// task.current_execution_id, it's an invariant violation.
func TestMarkKilledInTx_TaskMissing_PanicsAsInvariant(t *testing.T) {
	h := setupKill(t)
	_, execID := seed(t, h, execution.StatusWorking)
	// Request kill normally (RequestKill loads task fine for execution
	// in working state).
	if err := h.coord.RequestKill(context.Background(), execID,
		execution.KilledUserRequest, "stop", "user:hayang"); err != nil {
		t.Fatal(err)
	}
	// Now delete the task so HandleKilled's markKilledInTx step trips
	// the invariant (no FK at schema, conventions § 9.w).
	if _, err := h.db.ExecContext(context.Background(), `DELETE FROM tasks WHERE id = ?`, "T-1"); err != nil {
		t.Fatal(err)
	}
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
	_ = h.coord.HandleKilled(context.Background(), execID,
		execution.KilledUserRequest, "done", "worker:W-1")
	t.Fatal("HandleKilled should have panicked")
}

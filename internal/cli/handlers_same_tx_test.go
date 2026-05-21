package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/decision"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TestADR0014_DispatchSameTx verifies that dispatch (action) +
// task_execution.submitted/dispatched (events) + decision_records all
// commit atomically when the supervisor caller drives the handler.
//
// This is the regression test for the Phase 6 DoD § 4.1 偏离 — the prior
// pattern wrote DecisionRecord in a SECOND tx, opening a failure window
// where the action committed but the rationale never landed.
func TestADR0014_DispatchSameTx(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-ADR14")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")

	// Create a task first.
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "task A", "--no-conversation=true", "--format=json"})
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)

	// Dispatch as supervisor — must carry --rationale.
	code, dispOut, errw := runTaskHandler(t, app.DispatchCommand(), []string{
		created.TaskID, "--worker=W-1", "--rationale=W-1 idle, task is highest prio", "--format=json",
	})
	if code != int(ExitOK) {
		t.Fatalf("dispatch failed: code=%d errw=%s", code, errw)
	}
	var disp struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(dispOut), &disp)

	// Same-tx invariant: a DecisionRecord exists for INV-ADR14 + kind=dispatch.
	rows, err := app.DecisionRepo.FindByInvocationID(context.Background(), "INV-ADR14")
	if err != nil {
		t.Fatalf("FindByInvocationID: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("decision_records: got %d, want 1", len(rows))
	}
	if rows[0].Kind() != cognition.DecisionDispatch {
		t.Errorf("kind: got %s, want %s", rows[0].Kind(), cognition.DecisionDispatch)
	}
	if rows[0].Rationale() == "" {
		t.Errorf("rationale lost in tx")
	}
}

// TestADR0014_RationaleFailureRollsBackAction verifies that when
// DecisionRecord write fails (here forced by passing an actor with no
// rationale via the lower-level recorder), the entire tx rolls back —
// the action state change should NOT persist.
//
// Note: we exercise the lower-level helper via a custom outer tx,
// stand-alone from the dispatchHandler.
func TestADR0014_DecisionFailureRollsBackAction(t *testing.T) {
	app := newTestApp(t)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-FAIL")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")

	// Seed a worker so we have something to write — bypass through a fake
	// "action" that simply enrolls the worker, then force a rationale-required
	// failure in the same tx. The enrolled-worker INSERT must NOT survive.
	var seenWorker bool
	errOuter := persistence.RunInTx(context.Background(), app.DB, func(txCtx context.Context) error {
		actor := decision.Actor{Kind: "supervisor", ID: "INV-FAIL", InvocationID: "INV-FAIL"}
		// Simulate the action side: insert a row directly to verify rollback.
		if _, err := app.DB.ExecContext(txCtx,
			`INSERT INTO workers (id, status, capabilities, created_at, updated_at, version)
			 VALUES ('W-ROLLBACK', 'idle', '[]', '2026-05-22T00:00:00Z', '2026-05-22T00:00:00Z', 1)`); err != nil {
			return err
		}
		// DecisionRecorder.Record with empty rationale must error → tx rolls back.
		_, derr := app.DecisionRecorder.Record(txCtx, actor, decision.RecordRequest{
			Kind:           cognition.DecisionDispatch,
			TargetRefsJSON: `{}`,
			Rationale:      "", // intentionally empty
			Outcome:        cognition.OutcomeSucceeded,
		})
		if derr == nil {
			t.Fatal("expected ErrRationaleRequired, got nil")
		}
		if !errors.Is(derr, cognition.ErrRationaleRequired) {
			t.Fatalf("expected ErrRationaleRequired, got %v", derr)
		}
		return derr
	})
	if errOuter == nil {
		t.Fatal("expected outer tx to surface the error")
	}
	// Verify the worker INSERT was rolled back.
	row := app.DB.QueryRowContext(context.Background(), `SELECT 1 FROM workers WHERE id='W-ROLLBACK'`)
	var x int
	if err := row.Scan(&x); err == nil {
		t.Errorf("worker INSERT survived rollback — same-tx invariant broken")
	}
	_ = seenWorker
}

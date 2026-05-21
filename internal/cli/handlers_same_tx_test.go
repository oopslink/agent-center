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

// TestRunSupervisorActionTx_NilApp guards the nil-app sentinel branch.
func TestRunSupervisorActionTx_NilApp(t *testing.T) {
	err := runSupervisorActionTx(context.Background(), nil, func(_ context.Context) error {
		return nil
	}, cognition.DecisionNoOp, "{}", "x")
	if err == nil {
		t.Error("nil app must error")
	}
}

// TestRunSupervisorActionTx_UserPathSkipsRecord verifies that when the
// caller is a user (no env), the action runs in a tx but no
// DecisionRecord is written — silent no-op.
func TestRunSupervisorActionTx_UserPathSkipsRecord(t *testing.T) {
	app := newTestApp(t)
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	err := runSupervisorActionTx(context.Background(), app, func(_ context.Context) error {
		return nil
	}, cognition.DecisionNoOp, "{}", "")
	if err != nil {
		t.Fatalf("user no-op tx: %v", err)
	}
	rows, _ := app.DecisionRepo.Find(context.Background(), cognition.DecisionFilter{Limit: 10})
	if len(rows) != 0 {
		t.Errorf("user path wrote %d decisions, want 0", len(rows))
	}
}

// TestRunSupervisorActionTx_PropagatesActionError verifies that an
// action-side error rolls back the tx and the DecisionRecord is never
// attempted.
func TestRunSupervisorActionTx_PropagatesActionError(t *testing.T) {
	app := newTestApp(t)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-ERR")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	wantErr := errors.New("action layer fail")
	gotErr := runSupervisorActionTx(context.Background(), app, func(_ context.Context) error {
		return wantErr
	}, cognition.DecisionDispatch, "{}", "reason")
	if !errors.Is(gotErr, wantErr) {
		t.Errorf("got %v, want %v", gotErr, wantErr)
	}
	rows, _ := app.DecisionRepo.Find(context.Background(), cognition.DecisionFilter{Limit: 10})
	if len(rows) != 0 {
		t.Errorf("decisions written despite action error: %d", len(rows))
	}
}

// TestRecordSupervisorDecisionInTx_NilApp guards the nil-app branch.
func TestRecordSupervisorDecisionInTx_NilApp(t *testing.T) {
	if err := recordSupervisorDecisionInTx(context.Background(), nil, cognition.DecisionNoOp, "{}", "x"); err != nil {
		t.Errorf("nil app should silently no-op: %v", err)
	}
}

// TestRecordSupervisorDecision_NilApp exercises the legacy helper's
// nil-app guard.
func TestRecordSupervisorDecision_NilApp(t *testing.T) {
	if err := recordSupervisorDecision(context.Background(), nil, cognition.DecisionNoOp, "{}", "x"); err != nil {
		t.Errorf("nil app should silently no-op: %v", err)
	}
}

// TestADR0014_KillExecution_AbandonPrecondition_DecisionKind verifies
// that the supervisor-driven kill-execution path with reason=
// abandon_precondition writes a DecisionRecord with kind=abandon_task
// (not kill_execution) — covers the kind-mapping branch added by the
// same-tx refactor.
func TestADR0014_KillExecution_AbandonPrecondition_DecisionKind(t *testing.T) {
	app := newTestApp(t)
	seedProjectAndWorker(t, app)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-ABN")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")

	// Create task + dispatch (as user, no rationale needed).
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	createCmd := findCmd(app.TaskCommands(), "create")
	_, out, _ := runTaskHandler(t, createCmd, []string{"p-1", "abnd", "--no-conversation=true", "--format=json"})
	var created struct {
		TaskID string `json:"task_id"`
	}
	_ = json.Unmarshal([]byte(out), &created)
	_, dispOut, _ := runTaskHandler(t, app.DispatchCommand(), []string{
		created.TaskID, "--worker=W-1", "--format=json",
	})
	var disp struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(dispOut), &disp)

	// Now supervise-kill with abandon_precondition.
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV-ABN")
	code, _, errw := runTaskHandler(t, app.KillExecutionCommand(), []string{
		disp.ExecutionID,
		"--reason=abandon_precondition",
		"--message=task cannot succeed",
		"--rationale=blocked by upstream",
		"--format=json",
	})
	if code != int(ExitOK) {
		t.Fatalf("kill code=%d errw=%s", code, errw)
	}
	rows, err := app.DecisionRepo.FindByInvocationID(context.Background(), "INV-ABN")
	if err != nil {
		t.Fatalf("FindByInvocationID: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("decisions: got %d, want 1", len(rows))
	}
	if rows[0].Kind() != cognition.DecisionAbandonTask {
		t.Errorf("kind: got %s, want abandon_task", rows[0].Kind())
	}
}

// TestRecordSupervisorDecisionInTx_UserActorSkip verifies user actor
// short-circuits without touching the DB.
func TestRecordSupervisorDecisionInTx_UserActorSkip(t *testing.T) {
	app := newTestApp(t)
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	if err := persistence.RunInTx(context.Background(), app.DB, func(txCtx context.Context) error {
		return recordSupervisorDecisionInTx(txCtx, app, cognition.DecisionDispatch, "{}", "x")
	}); err != nil {
		t.Errorf("user actor no-op: %v", err)
	}
}

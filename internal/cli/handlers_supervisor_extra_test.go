package cli

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestRequireSupervisorRationale_NoEnvNoOp(t *testing.T) {
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	if err := requireSupervisorRationale(""); err != nil {
		t.Errorf("user caller no-op: %v", err)
	}
}

func TestRequireSupervisorRationale_SupervisorNeedsRationale(t *testing.T) {
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV1")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	if err := requireSupervisorRationale(""); err == nil {
		t.Error("supervisor must have rationale")
	}
	if err := requireSupervisorRationale("   "); err == nil {
		t.Error("whitespace not a rationale")
	}
	if err := requireSupervisorRationale("good reason"); err != nil {
		t.Errorf("happy: %v", err)
	}
}

func TestRecordSupervisorDecision_NoOpForUser(t *testing.T) {
	a := newTestApp(t)
	os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	if err := recordSupervisorDecision(context.Background(), a, cognition.DecisionDispatch, "{}", "x"); err != nil {
		t.Errorf("user no-op: %v", err)
	}
}

func TestRecordSupervisorDecision_WritesForSupervisor(t *testing.T) {
	a := newTestApp(t)
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INVX")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	if err := recordSupervisorDecision(context.Background(), a, cognition.DecisionDispatch, `{"task_id":"T-1"}`, "W idle"); err != nil {
		t.Fatalf("write: %v", err)
	}
	rows, err := a.DecisionRepo.FindByInvocationID(context.Background(), "INVX")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("rows = %d", len(rows))
	}
}

func TestEscalateInputRequestCommand_HappyPath(t *testing.T) {
	a := newTestApp(t)
	// seed an input request using the actual schema
	if _, err := a.DB.ExecContext(context.Background(),
		`INSERT INTO input_requests (id, task_execution_id, status, question, options, urgency, requested_at, created_at, updated_at, version)
		 VALUES ('IR-1','E-1','pending','q?','[]','normal','2026-05-22T00:00:00Z','2026-05-22T00:00:00Z','2026-05-22T00:00:00Z',1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = os.Setenv("AGENT_CENTER_INVOCATION_ID", "INV5")
	defer os.Unsetenv("AGENT_CENTER_INVOCATION_ID")
	_, _, code := runHandler(t, a.EscalateInputRequestCommand(), []string{
		"IR-1", "--channel=web", "--rationale=user idle for 4h",
	})
	if code != ExitOK {
		t.Errorf("code = %d", code)
	}
	// verify decision recorded
	rows, _ := a.DecisionRepo.FindByInvocationID(context.Background(), "INV5")
	if len(rows) != 1 {
		t.Errorf("decisions = %d", len(rows))
	}
	if rows[0].Kind() != cognition.DecisionEscalateInputRequest {
		t.Errorf("kind = %s", rows[0].Kind())
	}
}

func TestEscalateInputRequestCommand_NotFound(t *testing.T) {
	a := newTestApp(t)
	_, _, code := runHandler(t, a.EscalateInputRequestCommand(), []string{
		"DOES_NOT_EXIST", "--rationale=x",
	})
	if code != ExitNotFound {
		t.Errorf("code = %d", code)
	}
}

func TestSupervisorRetriggerCommand_Happy_NoSpawnerWired(t *testing.T) {
	a := newTestApp(t)
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-X")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E1"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INVZ", Scope: scope, TriggerEvents: tes, StartedAt: time.Now().UTC()})
	if err := a.InvocationRepo.Save(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	// transition to failed
	if err := inv.MarkFailed(cognition.FailedReasonClaudeNonZero, "exit 1", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := a.InvocationRepo.UpdateStatusToTerminal(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	// retrigger without spawner wired → expects not_implemented exit
	_, _, code := runHandler(t, a.SupervisorRetriggerCommand(), []string{"INVZ"})
	if code != ExitNotImplemented {
		t.Errorf("expected ExitNotImplemented, got %d", code)
	}
}

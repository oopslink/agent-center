// Package cli — admin_client_atomicity_test.go: end-to-end tests for the
// v2.3-2 composite endpoints (dispatch-with-decision +
// kill-with-decision) that bundle the action + DecisionRecord in one
// server-side tx (ADR-0014 § 2).
//
// The atomicity claim under test: a failure in either half rolls back
// both. Without this, supervisor-driven CLI flows in Client mode could
// land a dispatch envelope without a DecisionRecord (or vice versa)
// when the second HTTP roundtrip failed — the v2.2 split-endpoint hole.
package cli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

// seedInvocationForTests inserts a minimal SupervisorInvocation so the
// decision_records FK passes. Returns the inserted invocation ID.
func seedInvocationForTests(t *testing.T, app *App, id string) cognition.InvocationID {
	t.Helper()
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-seed")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E-seed"})
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID:            cognition.InvocationID(id),
		Scope:         scope,
		TriggerEvents: tes,
		StartedAt:     app.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if err := app.InvocationRepo.Save(context.Background(), inv); err != nil {
		t.Fatalf("InvocationRepo.Save: %v", err)
	}
	return inv.ID()
}

// TestClient_DispatchWithDecision_CommitsBoth checks the happy path:
// dispatch + decision_record both land on a single successful call.
func TestClient_DispatchWithDecision_CommitsBoth(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	// Create a task so dispatch has something to target.
	create := findCmd(app.TaskCommands(), "create")
	out, _, code := runHandler(t, create, []string{"p-1", "do thing", "--no-conversation=true", "--format=json"})
	if code != ExitOK {
		t.Fatalf("create exit=%d out=%s", code, out)
	}
	var created struct{ TaskID string `json:"task_id"` }
	_ = json.Unmarshal([]byte(out), &created)

	invID := seedInvocationForTests(t, app, "INV-DSP-1")

	res, err := app.Client.DispatchWithDecision(context.Background(), DispatchWithDecisionRequest{
		Dispatch: DispatchRequest{TaskID: created.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", BaseBranch: "main"},
		Decision: DecisionRecordRequest{
			InvocationID:   string(invID),
			Kind:           string(cognition.DecisionDispatch),
			TargetRefsJSON: `{"task_id":"` + created.TaskID + `"}`,
			Rationale:      "supervisor decided to dispatch",
		},
	})
	if err != nil {
		t.Fatalf("DispatchWithDecision: %v", err)
	}
	if res.ExecutionID == "" {
		t.Fatal("expected execution_id")
	}
	if res.DecisionID == "" {
		t.Fatal("expected decision_id")
	}
	// Verify both rows landed.
	decisions, err := app.DecisionRepo.FindByInvocationID(context.Background(), invID)
	if err != nil {
		t.Fatalf("FindByInvocationID: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision row, got %d", len(decisions))
	}
}

// TestClient_DispatchWithDecision_RollsBackOnDispatchFailure is the
// load-bearing atomicity test. We trigger a dispatch failure (single-
// active violation by dispatching the same task twice) and assert that
// the SECOND call's DecisionRecord did NOT persist — proving the
// composite handler rolled back the whole tx instead of writing the
// audit row regardless.
func TestClient_DispatchWithDecision_RollsBackOnDispatchFailure(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	create := findCmd(app.TaskCommands(), "create")
	out, _, code := runHandler(t, create, []string{"p-1", "do thing", "--no-conversation=true", "--format=json"})
	if code != ExitOK {
		t.Fatalf("create exit=%d", code)
	}
	var created struct{ TaskID string `json:"task_id"` }
	_ = json.Unmarshal([]byte(out), &created)

	invID := seedInvocationForTests(t, app, "INV-DSP-RB")

	// First dispatch succeeds.
	if _, err := app.Client.DispatchWithDecision(context.Background(), DispatchWithDecisionRequest{
		Dispatch: DispatchRequest{TaskID: created.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", BaseBranch: "main"},
		Decision: DecisionRecordRequest{
			InvocationID: string(invID), Kind: string(cognition.DecisionDispatch),
			TargetRefsJSON: `{}`, Rationale: "first",
		},
	}); err != nil {
		t.Fatalf("first DispatchWithDecision: %v", err)
	}

	// Second dispatch against the same task MUST fail (single-active
	// invariant) AND must NOT persist a second DecisionRecord.
	if _, err := app.Client.DispatchWithDecision(context.Background(), DispatchWithDecisionRequest{
		Dispatch: DispatchRequest{TaskID: created.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", BaseBranch: "main"},
		Decision: DecisionRecordRequest{
			InvocationID: string(invID), Kind: string(cognition.DecisionDispatch),
			TargetRefsJSON: `{}`, Rationale: "second-should-rollback",
		},
	}); err == nil {
		t.Fatal("expected second dispatch to fail")
	}
	decisions, err := app.DecisionRepo.FindByInvocationID(context.Background(), invID)
	if err != nil {
		t.Fatalf("FindByInvocationID: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected exactly 1 decision (second rolled back), got %d", len(decisions))
	}
	if decisions[0].Rationale() == "second-should-rollback" {
		t.Fatal("second decision incorrectly persisted — atomicity broken")
	}
}

// TestClient_DispatchWithDecision_Rejects400OnMissingRationale verifies
// the endpoint rejects ill-formed requests before touching the DB.
func TestClient_DispatchWithDecision_Rejects400OnMissingRationale(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	create := findCmd(app.TaskCommands(), "create")
	out, _, code := runHandler(t, create, []string{"p-1", "do thing", "--no-conversation=true", "--format=json"})
	if code != ExitOK {
		t.Fatalf("create exit=%d", code)
	}
	var created struct{ TaskID string `json:"task_id"` }
	_ = json.Unmarshal([]byte(out), &created)

	_, err := app.Client.DispatchWithDecision(context.Background(), DispatchWithDecisionRequest{
		Dispatch: DispatchRequest{TaskID: created.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", BaseBranch: "main"},
		Decision: DecisionRecordRequest{
			InvocationID: "INV-X", Kind: string(cognition.DecisionDispatch),
			Rationale: "", // missing — endpoint should 400
		},
	})
	if err == nil {
		t.Fatal("expected 400 for missing rationale")
	}
	// And NO execution row should have been written.
	tasks, _ := app.Client.TaskFindByID(context.Background(), created.TaskID)
	if tasks.CurrentExecutionID != "" {
		t.Fatalf("400 path should not have created an execution; got %q", tasks.CurrentExecutionID)
	}
}

// TestClient_KillWithDecision_CommitsBoth covers the kill composite
// endpoint's happy path.
func TestClient_KillWithDecision_CommitsBoth(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	create := findCmd(app.TaskCommands(), "create")
	out, _, code := runHandler(t, create, []string{"p-1", "do thing", "--no-conversation=true", "--format=json"})
	if code != ExitOK {
		t.Fatalf("create exit=%d", code)
	}
	var created struct{ TaskID string `json:"task_id"` }
	_ = json.Unmarshal([]byte(out), &created)

	invID := seedInvocationForTests(t, app, "INV-KILL-1")

	// Need an execution first — dispatch via the simple endpoint.
	dres, err := app.Client.Dispatch(context.Background(), DispatchRequest{
		TaskID: created.TaskID, WorkerID: "W-1", AgentCLI: "claude-code", BaseBranch: "main",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	res, err := app.Client.KillWithDecision(context.Background(), KillWithDecisionRequest{
		Kill: KillExecutionRequest{
			ExecutionID: dres.ExecutionID, Reason: "user_request", Message: "stop",
		},
		Decision: DecisionRecordRequest{
			InvocationID: string(invID), Kind: string(cognition.DecisionKillExecution),
			TargetRefsJSON: `{"execution_id":"` + dres.ExecutionID + `"}`,
			Rationale:      "supervisor decided to kill",
		},
	})
	if err != nil {
		t.Fatalf("KillWithDecision: %v", err)
	}
	if res.DecisionID == "" {
		t.Fatal("expected decision_id")
	}
	if res.Status != "kill_requested" {
		t.Fatalf("status=%q", res.Status)
	}
	decisions, err := app.DecisionRepo.FindByInvocationID(context.Background(), invID)
	if err != nil {
		t.Fatalf("FindByInvocationID: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision row, got %d", len(decisions))
	}
}

// TestClient_KillWithDecision_RollsBackOnKillFailure: kill an unknown
// execution → kill fails → decision must not persist.
func TestClient_KillWithDecision_RollsBackOnKillFailure(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()
	seedProjectAndWorkerClient(t, app)

	invID := seedInvocationForTests(t, app, "INV-KILL-RB")

	_, err := app.Client.KillWithDecision(context.Background(), KillWithDecisionRequest{
		Kill: KillExecutionRequest{
			ExecutionID: "EX-DOES-NOT-EXIST", Reason: "user_request", Message: "stop",
		},
		Decision: DecisionRecordRequest{
			InvocationID: string(invID), Kind: string(cognition.DecisionKillExecution),
			Rationale: "should-rollback",
		},
	})
	if err == nil {
		t.Fatal("expected error for kill on unknown execution")
	}
	decisions, derr := app.DecisionRepo.FindByInvocationID(context.Background(), invID)
	if derr != nil {
		t.Fatalf("FindByInvocationID: %v", derr)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected 0 decisions (kill failed → tx rolled back), got %d", len(decisions))
	}
}

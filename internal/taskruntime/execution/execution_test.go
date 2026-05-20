package execution

import (
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

var ref = time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

func mkSubmitted(t *testing.T) *TaskExecution {
	t.Helper()
	e, err := New(NewInput{
		ID:            "E-1",
		TaskID:        "T-1",
		WorkerID:      "W-1",
		AgentCLI:      "claude-code",
		WorkspaceMode: WorkspaceWorktree,
		BaseBranch:    "main",
		Now:           ref,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return e
}

func TestNew_Happy(t *testing.T) {
	e := mkSubmitted(t)
	if e.Status() != StatusSubmitted {
		t.Fatalf("status: %s", e.Status())
	}
	if e.DispatchState() != DispatchPendingAck {
		t.Fatalf("dispatch_state: %s", e.DispatchState())
	}
	if e.Version() != 1 {
		t.Fatalf("version: %d", e.Version())
	}
}

func TestNew_Rejects(t *testing.T) {
	cases := []struct {
		name string
		in   NewInput
	}{
		{"no id", NewInput{TaskID: "T", WorkerID: "W", AgentCLI: "c", WorkspaceMode: WorkspaceDirect, Now: ref}},
		{"no task", NewInput{ID: "E", WorkerID: "W", AgentCLI: "c", WorkspaceMode: WorkspaceDirect, Now: ref}},
		{"no worker", NewInput{ID: "E", TaskID: "T", AgentCLI: "c", WorkspaceMode: WorkspaceDirect, Now: ref}},
		{"no cli", NewInput{ID: "E", TaskID: "T", WorkerID: "W", WorkspaceMode: WorkspaceDirect, Now: ref}},
		{"bad mode", NewInput{ID: "E", TaskID: "T", WorkerID: "W", AgentCLI: "c", WorkspaceMode: "LOL", Now: ref}},
		{"no now", NewInput{ID: "E", TaskID: "T", WorkerID: "W", AgentCLI: "c", WorkspaceMode: WorkspaceDirect}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := New(c.in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAck_HappyAndTerminal(t *testing.T) {
	e := mkSubmitted(t)
	if err := e.AckDispatch(ref); err != nil {
		t.Fatal(err)
	}
	if e.DispatchState() != DispatchAcked {
		t.Fatalf("dispatch: %s", e.DispatchState())
	}
	// idempotent
	if err := e.AckDispatch(ref); err != nil {
		t.Fatalf("idempotent ack: %v", err)
	}
	_ = e.MarkFailed(FailedAgentCrashed, "agent died", ref)
	if err := e.AckDispatch(ref); !errors.Is(err, ErrTaskExecutionAlreadyTerminated) {
		t.Fatalf("expected terminated: %v", err)
	}
}

func TestStartWorking_Happy(t *testing.T) {
	e := mkSubmitted(t)
	if err := e.StartWorking("/repo", ref.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if e.Status() != StatusWorking || e.CWD() != "/repo" {
		t.Fatalf("wrong: %+v", e)
	}
	if e.WorkingStartedAt() == nil {
		t.Fatal("expected working_started_at")
	}
	if err := e.StartWorking("/x", ref); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition: %v", err)
	}
}

func TestInputRequiredFlow(t *testing.T) {
	e := mkSubmitted(t)
	_ = e.StartWorking("/r", ref)
	if err := e.EnterInputRequired("IR-1", ref); err != nil {
		t.Fatal(err)
	}
	if e.Status() != StatusInputRequired || e.PendingInputRequestID() != "IR-1" {
		t.Fatalf("wrong: %+v", e)
	}
	if err := e.LeaveInputRequired(ref); err != nil {
		t.Fatal(err)
	}
	if e.Status() != StatusWorking || e.PendingInputRequestID() != "" {
		t.Fatalf("wrong: %+v", e)
	}
}

func TestMarkCompleted_ReasonValidation(t *testing.T) {
	e := mkSubmitted(t)
	_ = e.StartWorking("/r", ref)
	if err := e.MarkCompleted("BOGUS_REASON", "m", ref); !errors.Is(err, ErrUnknownReason) {
		t.Fatalf("expected unknown reason, got %v", err)
	}
	if err := e.MarkCompleted(CompletedAgentReportedSuccess, "", ref); err == nil {
		t.Fatal("expected message required")
	}
	if err := e.MarkCompleted(CompletedAgentReportedSuccess, "ok", ref); err != nil {
		t.Fatal(err)
	}
	if !e.IsTerminal() || e.Status() != StatusCompleted {
		t.Fatalf("expected completed terminal")
	}
	if err := e.MarkCompleted("", "no", ref); !errors.Is(err, ErrTaskExecutionAlreadyTerminated) {
		t.Fatalf("expected terminated: %v", err)
	}
}

func TestMarkFailed_AllReasons(t *testing.T) {
	reasons := []FailedReason{
		FailedAgentExitNonzero, FailedAgentReported, FailedAgentCrashed,
		FailedWorktreeSetup, FailedSubmittedTimeout, FailedExecutionTimeout,
		FailedInputTimeout, FailedWorkerLost, FailedDispatchNoAck,
		FailedNoInputChannel, FailedShimNoHello, FailedShimCrashed,
		FailedJsonlParseError, FailedAdapterInternalError,
		DispatchNack(NackWorkerAtCapacity),
		DispatchNack(NackMappingMissing),
		DispatchNack(NackAgentCliUnsupported),
		DispatchNack(NackWorktreePathBusy),
		DispatchNack(NackBaseBranchMissing),
		DispatchNack(NackEnvelopeVersionUnsupported),
	}
	for _, r := range reasons {
		t.Run(r.String(), func(t *testing.T) {
			e := mkSubmitted(t)
			if err := e.MarkFailed(r, "msg", ref); err != nil {
				t.Fatalf("mark: %v", err)
			}
			if e.Status() != StatusFailed || e.FailedReason() != r {
				t.Fatalf("wrong: %s/%s", e.Status(), e.FailedReason())
			}
		})
	}
}

func TestMarkFailed_Validation(t *testing.T) {
	e := mkSubmitted(t)
	if err := e.MarkFailed("garbage", "msg", ref); !errors.Is(err, ErrUnknownReason) {
		t.Fatalf("expected unknown")
	}
	if err := e.MarkFailed(FailedAgentCrashed, "", ref); err == nil {
		t.Fatal("expected message required")
	}
	if err := e.MarkFailed(FailedReason("dispatch_nack:foo"), "msg", ref); !errors.Is(err, ErrUnknownReason) {
		t.Fatal("expected unknown sub-reason")
	}
}

func TestRequestKillAndMarkKilled(t *testing.T) {
	e := mkSubmitted(t)
	_ = e.StartWorking("/r", ref)
	if err := e.RequestKill(string(KilledUserRequest), "stop now", ref); err != nil {
		t.Fatal(err)
	}
	if e.CancelRequestedAt() == nil {
		t.Fatal("expected cancel_requested_at")
	}
	// idempotent
	if err := e.RequestKill(string(KilledUserRequest), "again", ref); err != nil {
		t.Fatalf("expected idempotent: %v", err)
	}
	if err := e.MarkKilled(KilledUserRequest, "killed", ref); err != nil {
		t.Fatal(err)
	}
	if e.Status() != StatusKilled || e.KilledReason() != KilledUserRequest {
		t.Fatalf("wrong: %s", e.Status())
	}
	if err := e.MarkKilled(KilledUserRequest, "again", ref); !errors.Is(err, ErrTaskExecutionAlreadyTerminated) {
		t.Fatalf("expected terminated")
	}
}

func TestRequestKill_RequiresReasonMessage(t *testing.T) {
	e := mkSubmitted(t)
	if err := e.RequestKill("", "m", ref); err == nil {
		t.Fatal("expected reason")
	}
	if err := e.RequestKill("r", "", ref); err == nil {
		t.Fatal("expected message")
	}
}

func TestMarkKilled_FromSubmitted(t *testing.T) {
	e := mkSubmitted(t)
	// kill submitted directly (no SIGTERM)
	if err := e.MarkKilled(KilledAbandonPrecondition, "abandon", ref); err != nil {
		t.Fatalf("kill submitted: %v", err)
	}
	if e.Status() != StatusKilled {
		t.Fatalf("status: %s", e.Status())
	}
}

func TestReasonEnums_Validate(t *testing.T) {
	if err := CompletedReason("").Validate(); err != nil {
		t.Fatal(err)
	}
	if err := CompletedReason("invented").Validate(); !errors.Is(err, ErrUnknownReason) {
		t.Fatalf("expected unknown")
	}
	if err := KilledReason("").Validate(); err == nil {
		t.Fatal("expected required")
	}
	if err := KilledReason("garbage").Validate(); !errors.Is(err, ErrUnknownReason) {
		t.Fatal("expected unknown")
	}
	if err := NackSubReason("garbage").Validate(); !errors.Is(err, ErrUnknownReason) {
		t.Fatal("expected unknown")
	}
	if err := NackSubReason(NackWorkerAtCapacity).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestParseWorkspaceMode(t *testing.T) {
	m, err := ParseWorkspaceMode("worktree")
	if err != nil || m != WorkspaceWorktree {
		t.Fatalf("worktree: %v / %s", err, m)
	}
	if _, err := ParseWorkspaceMode("nope"); !errors.Is(err, ErrUnknownWorkspaceMode) {
		t.Fatalf("expected unknown")
	}
}

func TestRehydrate(t *testing.T) {
	in := RehydrateInput{
		ID:            "E-1",
		TaskID:        "T-1",
		WorkerID:      "W-1",
		AgentCLI:      "claude-code",
		WorkspaceMode: WorkspaceDirect,
		Priority:      "high",
		Status:        StatusWorking,
		DispatchState: DispatchAcked,
		StartedAt:     ref,
		CreatedAt:     ref,
		UpdatedAt:     ref,
		Version:       2,
	}
	got, err := Rehydrate(in)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	if got.WorkspaceMode() != WorkspaceDirect || got.Status() != StatusWorking {
		t.Fatalf("wrong: %+v", got)
	}
	in.Status = "garbage"
	if _, err := Rehydrate(in); !errors.Is(err, ErrInvalidStatus) {
		t.Fatal("expected invalid")
	}
	in.Status = StatusWorking
	in.DispatchState = "wat"
	if _, err := Rehydrate(in); err == nil {
		t.Fatal("expected dispatch_state error")
	}
	in.DispatchState = DispatchAcked
	in.WorkspaceMode = "garbage"
	if _, err := Rehydrate(in); !errors.Is(err, ErrUnknownWorkspaceMode) {
		t.Fatal("expected workspace mode")
	}
	in.WorkspaceMode = WorkspaceDirect
	in.Version = 0
	if _, err := Rehydrate(in); err == nil {
		t.Fatal("expected version")
	}
}

func TestAccumulateWorking(t *testing.T) {
	e := mkSubmitted(t)
	e.AccumulateWorking(100)
	e.AccumulateWorking(-10) // ignored
	e.AccumulateWorking(50)
	if e.WorkingSecondsAccumulated() != 150 {
		t.Fatalf("acc: %d", e.WorkingSecondsAccumulated())
	}
}

func TestArtifact_HappyAndValidation(t *testing.T) {
	a, err := NewArtifact(NewArtifactInput{
		ID:          "A-1",
		TaskID:      "T-1",
		ExecutionID: "E-1",
		Kind:        "pr_url",
		Title:       "feat: x",
		URL:         "https://github.com/foo/bar/pull/1",
		CreatedBy:   "agent:E-1",
		Now:         ref,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID() != "A-1" || a.Kind() != "pr_url" || a.MetadataJSON() != "{}" {
		t.Fatalf("wrong: %+v", a)
	}
	cases := []struct {
		name string
		in   NewArtifactInput
	}{
		{"no id", NewArtifactInput{TaskID: "T", ExecutionID: "E", Kind: "k", Title: "t", CreatedBy: "u", Now: ref}},
		{"no task", NewArtifactInput{ID: "A", ExecutionID: "E", Kind: "k", Title: "t", CreatedBy: "u", Now: ref}},
		{"no exec", NewArtifactInput{ID: "A", TaskID: "T", Kind: "k", Title: "t", CreatedBy: "u", Now: ref}},
		{"no kind", NewArtifactInput{ID: "A", TaskID: "T", ExecutionID: "E", Title: "t", CreatedBy: "u", Now: ref}},
		{"no title", NewArtifactInput{ID: "A", TaskID: "T", ExecutionID: "E", Kind: "k", CreatedBy: "u", Now: ref}},
		{"no by", NewArtifactInput{ID: "A", TaskID: "T", ExecutionID: "E", Kind: "k", Title: "t", Now: ref}},
		{"no now", NewArtifactInput{ID: "A", TaskID: "T", ExecutionID: "E", Kind: "k", Title: "t", CreatedBy: "u"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewArtifact(c.in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	_ = taskruntime.ArtifactID("") // keep alias referenced
}

func TestArtifact_Rehydrate(t *testing.T) {
	a, err := RehydrateArtifact(RehydrateArtifactInput{
		ID:           "A-1",
		TaskID:       "T-1",
		ExecutionID:  "E-1",
		Kind:         "file",
		Title:        "design.md",
		BlobRef:      "blobs/x",
		MetadataJSON: `{"k":"v"}`,
		CreatedAt:    ref,
		CreatedBy:    "agent:E-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.BlobRef() != "blobs/x" || a.MetadataJSON() != `{"k":"v"}` {
		t.Fatalf("wrong: %+v", a)
	}
}

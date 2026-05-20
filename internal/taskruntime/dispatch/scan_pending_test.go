package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

func TestScanPendingAck_TerminalSkipped(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	res, _ := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	// Make execution terminal directly
	e, _ := h.execRepo.FindByID(context.Background(), res.ExecutionID)
	_ = e.MarkFailed(execution.FailedAgentCrashed, "boom", h.clk.Now())
	if err := h.execRepo.Update(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(35 * time.Second)
	count, err := h.svc.ScanPendingAck(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 (terminal): %d", count)
	}
}

func TestScanPendingAck_AlreadyAckedSkipped(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	res, _ := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	ack := DispatchAck{ExecutionID: res.ExecutionID, Accepted: true, AckedAt: h.clk.Now()}
	if err := h.svc.HandleAck(context.Background(), ack, "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(35 * time.Second)
	count, err := h.svc.ScanPendingAck(context.Background(), "system")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 (acked): %d", count)
	}
}

func TestDispatch_ConversationIDPropagatedInEnvelope(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	// Inject a conversation_id on the task
	tt, _ := h.taskRepo.FindByID(context.Background(), "T-1")
	_ = tt.BindConversation("C-1", h.clk.Now())
	if err := h.taskRepo.Update(context.Background(), tt); err != nil {
		t.Fatal(err)
	}
	res, err := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Envelope.ConversationID != "C-1" {
		t.Fatalf("envelope conv: %s", res.Envelope.ConversationID)
	}
}

func TestDispatch_EnvelopeTimeoutOverridePropagated(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	d := 90 * time.Minute
	res, err := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID:                   "T-1",
		WorkerID:                 "W-1",
		AgentCLI:                 "claude-code",
		ExecutionTimeoutOverride: &d,
		Actor:                    "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Envelope.ExecutionTimeoutOverride == nil || *res.Envelope.ExecutionTimeoutOverride != int64((90 * time.Minute).Seconds()) {
		t.Fatalf("envelope override: %+v", res.Envelope.ExecutionTimeoutOverride)
	}
	_ = observability.EventQueryFilter{}
}

func TestIssueConcludeSpawn_EmptyTasksRejected(t *testing.T) {
	h := setup(t)
	stub := NewIssueConcludeSpawn(h.db, h.taskRepo, h.sink, h.idgen, h.clk)
	if _, err := stub.Spawn(context.Background(), IssueConcludeSpec{
		IssueID: "I-1", ProjectID: "p-1", Resolution: "done", ActorID: "user:hayang",
	}); err == nil {
		t.Fatal("expected error on empty tasks")
	}
}

func TestIssueConcludeSpawn_BadActorRejected(t *testing.T) {
	h := setup(t)
	stub := NewIssueConcludeSpawn(h.db, h.taskRepo, h.sink, h.idgen, h.clk)
	if _, err := stub.Spawn(context.Background(), IssueConcludeSpec{
		IssueID: "I-1", ProjectID: "p-1", Resolution: "done", ActorID: "BAD-format",
		Tasks: []IssueConcludeTaskSpec{{LocalID: "a", Title: "t"}},
	}); err == nil {
		t.Fatal("expected actor error")
	}
}

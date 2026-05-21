package query_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestInspectExecution_WithArtifacts(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	now := env.clk.Now()
	art, err := execution.NewArtifact(execution.NewArtifactInput{
		ID:          "A-1",
		TaskID:      "T-1",
		ExecutionID: "E-1",
		Kind:        "pr_url",
		Title:       "PR-42",
		CreatedBy:   "agent:foo",
		Now:         now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.Artifacts.Append(context.Background(), art); err != nil {
		t.Fatal(err)
	}
	res, err := env.svc.Inspect(context.Background(), "execution", "E-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	arts := data["artifacts"].([]any)
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
}

func TestInspectConversation_WithMessages(t *testing.T) {
	env := newQEnv(t)
	env.seedConversation(t, "C-1", conversation.ConversationKindDM)
	msg, err := conversation.NewMessage(conversation.NewMessageInput{
		ID:                "M-1",
		ConversationID:    "C-1",
		SenderIdentityID:  conversation.IdentityRef("user:t"),
		ContentKind:       conversation.MessageContentKind("text"),
		Content:           "hi",
		Direction:         conversation.MessageDirection("internal"),
		PostedAt:          env.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.Messages.Append(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	res, err := env.svc.Inspect(context.Background(), "conversation", "C-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	msgs := data["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestInspectIssue_WithEventsAndMessages(t *testing.T) {
	env := newQEnv(t)
	// Issue with conversation_id binding
	env.seedConversation(t, "C-issue", conversation.ConversationKindIssue)
	i := env.seedIssue(t, "I-1", "p", "x")
	_ = i
	if err := env.deps.Issues.UpdateConversationID(context.Background(), "I-1", "C-issue", i.Version(), env.clk.Now()); err != nil {
		t.Fatal(err)
	}
	msg, _ := conversation.NewMessage(conversation.NewMessageInput{
		ID: "M-x", ConversationID: "C-issue", SenderIdentityID: "user:t",
		ContentKind: "text", Content: "hello", Direction: "internal", PostedAt: env.clk.Now(),
	})
	_ = env.deps.Messages.Append(context.Background(), msg)
	res, err := env.svc.Inspect(context.Background(), "issue", "I-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if _, ok := data["messages"]; !ok {
		t.Fatalf("expected messages key: %+v", data)
	}
}

func TestInspectInputRequest_NotFound(t *testing.T) {
	env := newQEnv(t)
	_, err := env.svc.Inspect(context.Background(), "input_request", "IR-x")
	if err == nil {
		t.Fatal("expected not found error since IR not saved")
	}
}

func TestInspect_Worker_NoMappings(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-x", workforce.WorkerOnline)
	res, err := env.svc.Inspect(context.Background(), "worker", "W-x")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["id"] != "W-x" {
		t.Fatal("worker id mismatch")
	}
}

func TestInspect_Project_WithMappingsAndTasks(t *testing.T) {
	env := newQEnv(t)
	env.seedProject(t, "p", "P")
	env.seedTask(t, "T-1", "p", "a")
	env.seedTask(t, "T-2", "p", "b")
	res, err := env.svc.Inspect(context.Background(), "project", "p")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	tasks := data["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestInspect_Worktree_AllFields(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusWorking)
	now := env.clk.Now()
	_, _, _ = env.deps.Projection.UpsertIfFresh(context.Background(), "E-1", projection.ProjectionUpdate{LastPushAt: now})
	res, err := env.svc.Inspect(context.Background(), "worktree", "E-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["execution_id"] != "E-1" {
		t.Fatal("wt id")
	}
}

// silence unused
var _ = taskruntime.TaskExecutionID("")
var _ = task.PriorityHigh
var _ = time.Time{}

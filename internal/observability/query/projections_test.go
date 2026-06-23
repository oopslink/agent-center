package query_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/workforce"
)

// v2.14.0 F7 (issue I14): inspect "execution" reads pm_tasks (the task is the
// unit of agent work). The id is a TASK id; status is the mapped
// execution-status vocab (a running agent-assigned task → "active"); detail
// comes from the projection row. The artifacts segment is dropped (no
// work-item equivalent).
func TestInspectExecution_Task(t *testing.T) {
	env := newQEnv(t)
	env.seedAgentTask(t, "WI-1", "AG-1", "p", "active")
	res, err := env.svc.Inspect(context.Background(), "execution", "WI-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["status"] != "active" || data["task_id"] != "WI-1" {
		t.Fatalf("inspect execution: %+v", data)
	}
	if _, ok := data["projection"]; !ok {
		t.Fatal("expected projection key (execution row detail)")
	}
	if _, ok := data["artifacts"]; ok {
		t.Fatal("artifacts segment must be dropped (no work-item equiv)")
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

// v2.7 #131: inspect-project reads pm_projects; the mappings segment + tags
// output are dropped (workforce model retired). The tasks segment (pm_tasks)
// survives — pin it via the new pm project source.
func TestInspect_Project_WithTasks(t *testing.T) {
	env := newQEnv(t)
	env.seedOrgProject(t, "p", "org-1")
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
	if _, ok := data["mappings"]; ok {
		t.Fatal("mappings segment must be dropped (workforce model retired)")
	}
	if _, ok := data["tags"]; ok {
		t.Fatal("tags must be dropped (pm.Project has no Tags)")
	}
}


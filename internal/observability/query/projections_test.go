package query_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/workforce"
)

// v2.7 #107 Phase-2 (proj-A): inspect "execution" now inspects a work item.
// The id is a work-item id; rich detail comes from the work-item projection;
// the artifacts segment is dropped (execution-keyed, no work-item equivalent).
func TestInspectExecution_WorkItem(t *testing.T) {
	env := newQEnv(t)
	env.seedTask(t, "T-1", "p", "x")
	env.seedWorkItem(t, "WI-1", "AG-1", "T-1")
	env.seedWorkItemProjection(t, "WI-1", "AG-1", "active")
	res, err := env.svc.Inspect(context.Background(), "execution", "WI-1")
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	// status is the work-item DOMAIN status (a freshly-seeded work item is
	// "queued"); the projection (status=active here) is the activity detail.
	if data["work_item_id"] != "WI-1" || data["status"] != "queued" || data["task_id"] != "T-1" {
		t.Fatalf("inspect work item: %+v", data)
	}
	if _, ok := data["projection"]; !ok {
		t.Fatal("expected projection key (work-item projection detail)")
	}
	if _, ok := data["artifacts"]; ok {
		t.Fatal("artifacts segment must be dropped (execution-keyed, no work-item equiv)")
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


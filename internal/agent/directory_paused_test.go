package agent

import (
	"context"
	"testing"
	"time"
)

// stubWorkItemRepo implements WorkItemRepository for the paused-provider test —
// only ListByStatus is exercised; the embedded interface satisfies the rest (and
// would panic if any other method were called, which it is not).
type stubWorkItemRepo struct {
	WorkItemRepository
	byStatus map[WorkItemStatus][]*AgentWorkItem
	gotQuery WorkItemStatus
}

func (s *stubWorkItemRepo) ListByStatus(_ context.Context, status WorkItemStatus) ([]*AgentWorkItem, error) {
	s.gotQuery = status
	return s.byStatus[status], nil
}

func pausedItem(t *testing.T, id, taskRef string) *AgentWorkItem {
	t.Helper()
	wi, err := RehydrateWorkItem(RehydrateWorkItemInput{
		ID: id, AgentID: "AG", TaskRef: taskRef, Status: WorkItemPaused,
		CreatedAt: time.Unix(0, 0), UpdatedAt: time.Unix(0, 0), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return wi
}

func TestWorkItemPausedProvider_IntersectsRequestedTasks(t *testing.T) {
	repo := &stubWorkItemRepo{byStatus: map[WorkItemStatus][]*AgentWorkItem{
		WorkItemPaused: {
			pausedItem(t, "w1", "pm://tasks/task-a"),
			pausedItem(t, "w2", "pm://tasks/task-c"),
			pausedItem(t, "w3", "ac://other/xyz"), // non-pm ref → ignored
		},
	}}
	p := NewWorkItemPausedProvider(repo)

	got, err := p.PausedTasks(context.Background(), []string{"task-a", "task-b"})
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotQuery != WorkItemPaused {
		t.Fatalf("queried status = %q, want paused (one ListByStatus, N+1-free)", repo.gotQuery)
	}
	// task-a is paused AND requested → true; task-b requested but not paused → absent;
	// task-c paused but NOT requested → absent (only the intersection is returned).
	if !got["task-a"] {
		t.Fatalf("task-a should be paused; got %v", got)
	}
	if got["task-b"] {
		t.Fatalf("task-b is not paused; got %v", got)
	}
	if _, ok := got["task-c"]; ok {
		t.Fatalf("task-c not requested; must not appear; got %v", got)
	}
}

func TestWorkItemPausedProvider_EmptyInput_NoQuery(t *testing.T) {
	repo := &stubWorkItemRepo{}
	p := NewWorkItemPausedProvider(repo)
	got, err := p.PausedTasks(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty input → empty map, got %v", got)
	}
	if repo.gotQuery != "" {
		t.Fatalf("empty input must short-circuit without a query; queried %q", repo.gotQuery)
	}
}

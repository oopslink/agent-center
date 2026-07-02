package orchestration

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

func TestNodeStatus_Transitions(t *testing.T) {
	allowed := []struct{ from, to NodeStatus }{
		{NodeOpen, NodeRunning},
		{NodeRunning, NodeCompleted},
		{NodeCompleted, NodeReopen},
		{NodeReopen, NodeRunning},
		{NodeOpen, NodeDiscarded},
		{NodeRunning, NodeDiscarded},
	}
	for _, tt := range allowed {
		if !tt.from.CanTransitionTo(tt.to) {
			t.Errorf("%s -> %s should be allowed", tt.from, tt.to)
		}
	}

	denied := []struct{ from, to NodeStatus }{
		{NodeOpen, NodeCompleted},
		{NodeCompleted, NodeRunning},
		{NodeDiscarded, NodeOpen},
		{NodeDiscarded, NodeRunning},
		{NodeReopen, NodeCompleted},
	}
	for _, tt := range denied {
		if tt.from.CanTransitionTo(tt.to) {
			t.Errorf("%s -> %s should be denied", tt.from, tt.to)
		}
	}
}

func TestNewNode_BusinessNode(t *testing.T) {
	n, err := NewNode(NewNodeInput{
		ID:       "n1",
		GraphID:  "g1",
		Category: NodeCategoryBusiness,
		Title:    "Dev task",
		Metadata: map[string]any{"branch": "feature-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.ID() != "n1" || n.GraphID() != "g1" || n.Category() != NodeCategoryBusiness {
		t.Fatalf("unexpected fields: id=%s graph=%s cat=%s", n.ID(), n.GraphID(), n.Category())
	}
	if n.Status() != NodeOpen {
		t.Fatalf("status = %s, want open", n.Status())
	}
	if n.ControlKind() != "" {
		t.Fatalf("business node should have empty controlKind, got %s", n.ControlKind())
	}
	if n.Metadata()["branch"] != "feature-1" {
		t.Fatal("metadata not preserved")
	}
}

func TestNewNode_ControlNode(t *testing.T) {
	n, err := NewNode(NewNodeInput{
		ID:          "n2",
		GraphID:     "g1",
		Category:    NodeCategoryControl,
		ControlKind: ControlKindCondition,
		Title:       "Review gate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n.ControlKind() != ControlKindCondition {
		t.Fatalf("controlKind = %s, want condition", n.ControlKind())
	}
}

func TestNewNode_Validation(t *testing.T) {
	// missing id
	if _, err := NewNode(NewNodeInput{GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"}); err == nil {
		t.Fatal("expected error for missing id")
	}
	// missing graphID
	if _, err := NewNode(NewNodeInput{ID: "n1", Category: NodeCategoryBusiness, Title: "x"}); err == nil {
		t.Fatal("expected error for missing graphID")
	}
	// missing title
	if _, err := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness}); err == nil {
		t.Fatal("expected error for missing title")
	}
	// invalid category
	if _, err := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: "bad", Title: "x"}); err == nil {
		t.Fatal("expected error for invalid category")
	}
	// control node without controlKind
	if _, err := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryControl, Title: "x"}); err == nil {
		t.Fatal("expected error for control node without controlKind")
	}
}

func TestNode_Start_Complete_Reopen(t *testing.T) {
	n, _ := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"})

	if err := n.Start(t0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n.Status() != NodeRunning {
		t.Fatalf("status = %s, want running", n.Status())
	}

	if err := n.Complete("success", t0.Add(time.Hour)); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if n.Status() != NodeCompleted || n.Outcome() != "success" {
		t.Fatalf("status=%s outcome=%s, want completed/success", n.Status(), n.Outcome())
	}

	if err := n.Reopen("condition failed", t0.Add(2*time.Hour)); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if n.Status() != NodeReopen {
		t.Fatalf("status = %s, want reopen", n.Status())
	}
	if n.Outcome() != "" {
		t.Fatalf("outcome should be cleared on reopen, got %s", n.Outcome())
	}
	if len(n.ActionLogs()) != 3 {
		t.Fatalf("action logs = %d, want 3", len(n.ActionLogs()))
	}

	// reopen -> running again
	if err := n.Start(t0.Add(3 * time.Hour)); err != nil {
		t.Fatalf("Start after reopen: %v", err)
	}
	if n.Status() != NodeRunning {
		t.Fatalf("status = %s, want running", n.Status())
	}
}

func TestNode_Discard(t *testing.T) {
	n, _ := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"})
	if err := n.Discard(t0); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if n.Status() != NodeDiscarded {
		t.Fatalf("status = %s, want discarded", n.Status())
	}
	// discarded is terminal
	if err := n.Start(t0); err == nil {
		t.Fatal("expected error starting discarded node")
	}
}

func TestNode_IllegalTransition(t *testing.T) {
	n, _ := NewNode(NewNodeInput{ID: "n1", GraphID: "g1", Category: NodeCategoryBusiness, Title: "x"})
	// open -> completed is illegal
	if err := n.Complete("ok", t0); err == nil {
		t.Fatal("expected error for open -> completed")
	}
}

func TestRehydrateNode_HappyPath(t *testing.T) {
	logs := []ActionLog{
		{OccurredAt: t0, Action: "started", Detail: ""},
		{OccurredAt: t0.Add(time.Hour), Action: "completed", Detail: "ok"},
	}
	n, err := RehydrateNode(RehydrateNodeInput{
		ID:          "n42",
		GraphID:     "g99",
		Category:    NodeCategoryBusiness,
		ControlKind: "",
		Title:       "Rehydrated task",
		Status:      NodeCompleted,
		Outcome:     "ok",
		Metadata:    map[string]any{"env": "prod"},
		ActionLogs:  logs,
		CreatedAt:   t0,
		UpdatedAt:   t0.Add(time.Hour),
		Version:     3,
	})
	if err != nil {
		t.Fatalf("RehydrateNode: %v", err)
	}
	if n.ID() != "n42" {
		t.Errorf("ID = %s, want n42", n.ID())
	}
	if n.GraphID() != "g99" {
		t.Errorf("GraphID = %s, want g99", n.GraphID())
	}
	if n.Category() != NodeCategoryBusiness {
		t.Errorf("Category = %s, want business", n.Category())
	}
	if n.ControlKind() != "" {
		t.Errorf("ControlKind = %s, want empty", n.ControlKind())
	}
	if n.Title() != "Rehydrated task" {
		t.Errorf("Title = %s, want 'Rehydrated task'", n.Title())
	}
	if n.Status() != NodeCompleted {
		t.Errorf("Status = %s, want completed", n.Status())
	}
	if n.Outcome() != "ok" {
		t.Errorf("Outcome = %s, want ok", n.Outcome())
	}
	if n.Metadata()["env"] != "prod" {
		t.Errorf("Metadata[env] = %v, want prod", n.Metadata()["env"])
	}
	if len(n.ActionLogs()) != 2 {
		t.Errorf("ActionLogs len = %d, want 2", len(n.ActionLogs()))
	}
	if !n.CreatedAt().Equal(t0.UTC()) {
		t.Errorf("CreatedAt = %v, want %v", n.CreatedAt(), t0.UTC())
	}
	if !n.UpdatedAt().Equal(t0.Add(time.Hour).UTC()) {
		t.Errorf("UpdatedAt = %v, want %v", n.UpdatedAt(), t0.Add(time.Hour).UTC())
	}
	if n.Version() != 3 {
		t.Errorf("Version = %d, want 3", n.Version())
	}
}

func TestRehydrateNode_InvalidStatus(t *testing.T) {
	_, err := RehydrateNode(RehydrateNodeInput{
		ID:      "n1",
		GraphID: "g1",
		Title:   "x",
		Status:  NodeStatus("bogus"),
		Version: 1,
	})
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestRehydrateNode_InvalidVersion(t *testing.T) {
	_, err := RehydrateNode(RehydrateNodeInput{
		ID:      "n1",
		GraphID: "g1",
		Title:   "x",
		Status:  NodeOpen,
		Version: 0,
	})
	if err == nil {
		t.Fatal("expected error for version < 1")
	}
}

package projectmanager

import (
	"errors"
	"testing"
	"time"
)

func TestTaskTerminalHistoryCannotReopen(t *testing.T) {
	at := time.Unix(1000, 0).UTC()
	task, err := NewTask(NewTaskInput{ID: "task-1", ProjectID: "project-1", Title: "ship", CreatedBy: "user:a", CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := task.Assign("user:a", at); err != nil {
		t.Fatal(err)
	}
	if err := task.Start(at); err != nil {
		t.Fatal(err)
	}
	if err := task.Complete("user:a", at); err != nil {
		t.Fatal(err)
	}
	if err := task.Reopen(at); !errors.Is(err, ErrTaskReopenRetired) {
		t.Fatalf("Reopen = %v", err)
	}
	if err := task.SetStatus(TaskOpen, at); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("terminal SetStatus(open) = %v", err)
	}
	if task.Status() != TaskCompleted {
		t.Fatalf("terminal history changed to %s", task.Status())
	}
}

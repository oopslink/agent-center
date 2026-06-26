package taskexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeVerifier simulates Center API responses.
type fakeVerifier struct {
	assigned map[string]bool // taskID → still assigned to this agent
	err      error            // error to return from IsTaskAssigned
}

func (f *fakeVerifier) IsTaskAssigned(ctx context.Context, agentID, taskID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.assigned[taskID], nil
}

func TestReconcileTasksOnBoot_KeepsAssigned(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	v := &fakeVerifier{assigned: map[string]bool{"t-1": true}}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", now)

	if result.Kept != 1 {
		t.Errorf("Kept = %d, want 1", result.Kept)
	}
	if result.Aborted != 0 {
		t.Errorf("Aborted = %d, want 0", result.Aborted)
	}
}

func TestReconcileTasksOnBoot_AbortsReassigned(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	v := &fakeVerifier{assigned: map[string]bool{"t-1": false}}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", now)

	if result.Aborted != 1 {
		t.Errorf("Aborted = %d, want 1", result.Aborted)
	}
}

func TestReconcileTasksOnBoot_EmptyTasksDir(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	v := &fakeVerifier{}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", time.Now())
	if result.Kept != 0 && result.Aborted != 0 {
		t.Errorf("expected zero result, got %+v", result)
	}
}

func TestReconcileTasksOnBoot_VerifierError(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	verifierErr := errors.New("Center API error")
	v := &fakeVerifier{err: verifierErr}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", now)

	if result.Kept != 0 {
		t.Errorf("Kept = %d, want 0", result.Kept)
	}
	if result.Aborted != 0 {
		t.Errorf("Aborted = %d, want 0", result.Aborted)
	}
	if len(result.Errors) != 1 {
		t.Errorf("Errors length = %d, want 1", len(result.Errors))
	}
	if result.Errors[0] != verifierErr {
		t.Errorf("Error = %v, want %v", result.Errors[0], verifierErr)
	}
}

func TestReconcileTasksOnBoot_MultipleTasksMixed(t *testing.T) {
	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-2", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-3", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	v := &fakeVerifier{assigned: map[string]bool{
		"t-1": true,  // kept
		"t-2": false, // aborted
		"t-3": true,  // kept
	}}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", now)

	if result.Kept != 2 {
		t.Errorf("Kept = %d, want 2", result.Kept)
	}
	if result.Aborted != 1 {
		t.Errorf("Aborted = %d, want 1", result.Aborted)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want none", result.Errors)
	}
}

func TestReconcileTasksOnBoot_NoErrorOnEmptyDirWhenReassigned(t *testing.T) {
	// Verify that scanning an empty tasks dir doesn't produce errors
	badTasksDir := filepath.Join(t.TempDir(), "nonexistent", "tasks")

	v := &fakeVerifier{assigned: map[string]bool{}}
	result := ReconcileTasksOnBoot(context.Background(), badTasksDir, v, "agent-1", time.Now())

	// Empty/nonexistent dir, so no errors
	if len(result.Errors) != 0 {
		t.Errorf("Errors for empty dir = %v, want none", result.Errors)
	}
}

func TestReconcileTasksOnBoot_AbortTaskFailure(t *testing.T) {
	// Test case: task is reassigned but abort fails
	// We can't easily mock AbortTask, but we can test with a read-only directory
	// that would prevent the rename operation from succeeding
	if os.Geteuid() == 0 {
		t.Skip("skipping test that requires non-root user (current uid=0)")
	}

	tasksDir := filepath.Join(t.TempDir(), "tasks")
	dm := NewDirManager()
	now := time.Now().UTC()
	dm.Create(tasksDir, TaskExecutionMeta{TaskID: "t-1", Status: StatusRunning, CreatedAt: now, UpdatedAt: now}, ExecutionContext{})

	// Make the tasks dir read-only to force abort failure
	os.Chmod(tasksDir, 0o500)
	defer os.Chmod(tasksDir, 0o700)

	v := &fakeVerifier{assigned: map[string]bool{"t-1": false}}
	result := ReconcileTasksOnBoot(context.Background(), tasksDir, v, "agent-1", now)

	// Should have error when abort fails
	if len(result.Errors) != 1 {
		t.Errorf("Errors length = %d, want 1, got %v", len(result.Errors), result.Errors)
	}
	if result.Aborted != 0 {
		t.Errorf("Aborted = %d, want 0 (failed aborts don't count as success)", result.Aborted)
	}
}

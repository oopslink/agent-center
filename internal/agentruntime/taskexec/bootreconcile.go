package taskexec

import (
	"context"
	"time"
)

// TaskVerifier is the port for checking task assignment with Center (design §3.2).
type TaskVerifier interface {
	IsTaskAssigned(ctx context.Context, agentID, taskID string) (bool, error)
}

// ReconcileResult reports the boot reconciliation outcome.
type ReconcileResult struct {
	Kept    int
	Aborted int
	Errors  []error
}

// ReconcileTasksOnBoot scans tasks/ for standard execution directories,
// verifies each against the Center, and aborts orphaned tasks (design §3.2).
func ReconcileTasksOnBoot(ctx context.Context, tasksDir string, verifier TaskVerifier, agentID string, now time.Time) ReconcileResult {
	var result ReconcileResult
	dm := NewDirManager()
	active := dm.ScanActive(tasksDir)

	for _, meta := range active {
		assigned, err := verifier.IsTaskAssigned(ctx, agentID, meta.TaskID)
		if err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		if assigned {
			result.Kept++
			continue
		}
		// Task no longer assigned to this agent → abort
		if _, err := AbortTask(tasksDir, meta.TaskID, now); err != nil {
			result.Errors = append(result.Errors, err)
			continue
		}
		result.Aborted++
	}
	return result
}

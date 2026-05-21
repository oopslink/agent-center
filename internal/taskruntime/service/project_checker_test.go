package service

import (
	"context"
	"errors"
	"testing"
)

// fakeProjectChecker drives the ProjectExistenceChecker port for tests.
type fakeProjectChecker struct {
	exists bool
	err    error
	calls  int
}

func (f *fakeProjectChecker) ProjectExists(_ context.Context, _ string) (bool, error) {
	f.calls++
	return f.exists, f.err
}

// TestTaskService_Create_AppLayerProjectCheck_Missing pins the app-layer
// referential check introduced for conventions § 9.w (FK removal):
// TaskService.Create should reject with ErrProjectNotFound when the
// existence checker reports false, instead of relying on a defunct FK.
func TestTaskService_Create_AppLayerProjectCheck_Missing(t *testing.T) {
	rig := setupRig(t, "")
	checker := &fakeProjectChecker{exists: false}
	rig.taskSvc.WithProjectExistenceChecker(checker)
	_, err := rig.taskSvc.Create(context.Background(), TaskCreateInput{
		ProjectID: "missing", Title: "t", Actor: "user:hayang",
	})
	if !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("expected ErrProjectNotFound, got: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("expected 1 checker call, got %d", checker.calls)
	}
}

// TestTaskService_Create_AppLayerProjectCheck_Exists confirms that when
// the checker reports the project exists, Create succeeds and the
// checker is consulted exactly once.
func TestTaskService_Create_AppLayerProjectCheck_Exists(t *testing.T) {
	rig := setupRig(t, "")
	checker := &fakeProjectChecker{exists: true}
	rig.taskSvc.WithProjectExistenceChecker(checker)
	if _, err := rig.taskSvc.Create(context.Background(), TaskCreateInput{
		ProjectID: "p-1", Title: "t", Actor: "user:hayang",
	}); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if checker.calls != 1 {
		t.Fatalf("expected 1 checker call, got %d", checker.calls)
	}
}

// TestTaskService_Create_AppLayerProjectCheck_PropagatesError ensures
// that a non-NotFound error from the checker bubbles up wrapped (caller
// can distinguish from the sentinel).
func TestTaskService_Create_AppLayerProjectCheck_PropagatesError(t *testing.T) {
	rig := setupRig(t, "")
	wantErr := errors.New("boom")
	checker := &fakeProjectChecker{err: wantErr}
	rig.taskSvc.WithProjectExistenceChecker(checker)
	_, err := rig.taskSvc.Create(context.Background(), TaskCreateInput{
		ProjectID: "p-1", Title: "t", Actor: "user:hayang",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wrapped boom, got: %v", err)
	}
}

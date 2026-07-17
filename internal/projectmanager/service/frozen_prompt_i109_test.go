package service

import (
	"context"
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// I109 ① regression lock: a RUNNING task's executor prompt was frozen at spawn, so a
// description edit cannot reach it. The write must FAIL LOUD rather than silently
// no-op — the defect is the editor believing a mid-flight re-scope landed (T1102: an
// appended requirement reached the executor 0%).
//
// These tests assert the REAL EFFECT — the stored description, i.e. what a subsequent
// spawn would actually render — not that some warn()/guard function was called. A test
// that asserts the call and not the outcome would still pass if the guard ran and the
// write landed anyway.

// runningTask returns a task in status==running (assigned + started), the state that
// means "an executor is in flight against a frozen prompt".
func runningTask(t *testing.T, svc *Service, ctx context.Context, desc string) (pm.ProjectID, pm.TaskID) {
	t.Helper()
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Actor: "user:a"}); err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", Description: desc, CreatedBy: "user:a"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := svc.AssignTask(ctx, tid, "user:b", "user:a"); err != nil {
		t.Fatalf("AssignTask: %v", err)
	}
	if err := svc.StartTask(ctx, tid, "user:b"); err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	tk, err := svc.tasks.FindByID(ctx, tid)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if tk.Status() != pm.TaskRunning {
		t.Fatalf("precondition: status = %q, want running (task must have an executor in flight)", tk.Status())
	}
	return pid, tid
}

func descOf(t *testing.T, svc *Service, ctx context.Context, tid pm.TaskID) string {
	t.Helper()
	tk, err := svc.tasks.FindByID(ctx, tid)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	return tk.Description()
}

// The spawned case: UpdateTask must REJECT and must leave the description byte-identical
// — the running executor's prompt was rendered from it and cannot be re-fed.
func TestUpdateTask_RunningTask_DescriptionEditRejected_AndPromptTextUnchanged(t *testing.T) {
	svc, _, ctx := setup(t)
	_, tid := runningTask(t, svc, ctx, "original scope")

	appended := "original scope\n\nALSO: clean up tmp_pack_* on fetch cancel"
	err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, Description: &appended, Actor: "user:a"})
	if !errors.Is(err, pm.ErrTaskDescriptionFrozen) {
		t.Fatalf("UpdateTask on a running task: err = %v, want ErrTaskDescriptionFrozen (a silent accept is the I109 ① bug)", err)
	}
	if got := descOf(t, svc, ctx, tid); got != "original scope" {
		t.Fatalf("description = %q, want it unchanged (%q) — a rejected edit must not persist", got, "original scope")
	}
}

// Same gate on the batch path — otherwise the edit just walks around the guard.
func TestBatchUpdateTask_RunningTask_DescriptionEditRejected_AndPromptTextUnchanged(t *testing.T) {
	svc, _, ctx := setup(t)
	_, tid := runningTask(t, svc, ctx, "original scope")

	appended := "original scope + a mid-flight requirement"
	err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Description: &appended}, "user:a")
	if !errors.Is(err, pm.ErrTaskDescriptionFrozen) {
		t.Fatalf("BatchUpdateTask on a running task: err = %v, want ErrTaskDescriptionFrozen", err)
	}
	if got := descOf(t, svc, ctx, tid); got != "original scope" {
		t.Fatalf("description = %q, want unchanged", got)
	}
}

// A batch patch must not be able to launder a description edit past the guard by moving
// the status out of `running` in the SAME tx: the executor was in flight when the edit
// arrived, which is what the guard judges. All-or-none — the status must not move either.
func TestBatchUpdateTask_RunningTask_CannotLaunderDescriptionEditViaStatusChange(t *testing.T) {
	svc, _, ctx := setup(t)
	_, tid := runningTask(t, svc, ctx, "original scope")

	appended, completed := "smuggled new scope", string(pm.TaskCompleted)
	err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Status: &completed, Description: &appended}, "user:a")
	if !errors.Is(err, pm.ErrTaskDescriptionFrozen) {
		t.Fatalf("batch status+description on a running task: err = %v, want ErrTaskDescriptionFrozen", err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Description() != "original scope" {
		t.Fatalf("description = %q, want unchanged", tk.Description())
	}
	if tk.Status() != pm.TaskRunning {
		t.Fatalf("status = %q, want running — the rejected tx must roll back whole (all-or-none)", tk.Status())
	}
}

// Do NOT maim the legitimate path: a task that has NOT been spawned renders its brief at
// the NEXT dispatch, so the edit genuinely lands and must be allowed. This is the
// over-blocking half of the lock — the guard must bite "an executor is in flight", not
// the mere existence of a task.
func TestUpdateTask_NotSpawnedTask_DescriptionEditStillLands(t *testing.T) {
	svc, _, ctx := setup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", Description: "d0", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Status() != pm.TaskOpen {
		t.Fatalf("precondition: status = %q, want open (not yet spawned)", tk.Status())
	}

	newDesc := "a re-scoped, not-yet-dispatched task"
	if err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, Description: &newDesc, Actor: "user:a"}); err != nil {
		t.Fatalf("UpdateTask on a NOT-yet-spawned task must be allowed, got %v", err)
	}
	if got := descOf(t, svc, ctx, tid); got != newDesc {
		t.Fatalf("description = %q, want %q — the legitimate edit path must keep working", got, newDesc)
	}
	// ...and via the batch path too.
	batchDesc := "re-scoped again, via batch"
	if err := svc.BatchUpdateTask(ctx, tid, BatchTaskPatch{Description: &batchDesc}, "user:a"); err != nil {
		t.Fatalf("BatchUpdateTask on a NOT-yet-spawned task must be allowed, got %v", err)
	}
	if got := descOf(t, svc, ctx, tid); got != batchDesc {
		t.Fatalf("description = %q, want %q", got, batchDesc)
	}
}

// A running task's TITLE / other metadata stay editable: they make no claim to re-scope
// the run, and blocking them would be collateral damage.
func TestUpdateTask_RunningTask_TitleEditStillAllowed(t *testing.T) {
	svc, _, ctx := setup(t)
	_, tid := runningTask(t, svc, ctx, "original scope")

	newTitle := "clearer title"
	if err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, Title: &newTitle, Actor: "user:a"}); err != nil {
		t.Fatalf("title-only edit on a running task must stay legal, got %v", err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Title() != newTitle {
		t.Fatalf("title = %q, want %q", tk.Title(), newTitle)
	}
}

// A no-op patch (description == nil) on a running task must not trip the guard.
func TestUpdateTask_RunningTask_NilDescriptionDoesNotTripGuard(t *testing.T) {
	svc, _, ctx := setup(t)
	_, tid := runningTask(t, svc, ctx, "original scope")

	if err := svc.UpdateTask(ctx, UpdateTaskCommand{TaskID: tid, Actor: "user:a"}); err != nil {
		t.Fatalf("nil-description patch on a running task must not be rejected, got %v", err)
	}
}

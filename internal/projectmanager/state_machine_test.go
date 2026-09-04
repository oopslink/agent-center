package projectmanager

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)

// TestTaskStatus_IsTerminal_Partition pins the terminal/active partition that
// the observability default task-query relies on (v2.7 #107 proj-B). ADR-0054
// terminal = {completed, failed, discarded}; active (non-terminal) =
// {open, running}. "blocked", "verified" and "reopened" stay retired. v2.8.1: no
// "assigned" state (assignee is metadata). Iterating every enum value guards against a
// new status silently landing on the wrong side (the proj-A "core-enum" §-1 lesson) —
// which is why AllTaskStatuses is the single source it iterates, rather than a list
// copied here that a later status could quietly fall off.
func TestTaskStatus_IsTerminal_Partition(t *testing.T) {
	terminal := map[TaskStatus]bool{TaskCompleted: true, TaskFailed: true, TaskDiscarded: true}
	for _, s := range AllTaskStatuses() {
		if !s.IsValid() {
			t.Fatalf("%s not IsValid — enum drift", s)
		}
		if got := s.IsTerminal(); got != terminal[s] {
			t.Fatalf("IsTerminal(%s) = %v, want %v", s, got, terminal[s])
		}
	}
	// Exactly 3 terminal, 2 active.
	var nTerminal int
	for _, s := range AllTaskStatuses() {
		if s.IsTerminal() {
			nTerminal++
		}
	}
	if nTerminal != 3 {
		t.Fatalf("expected 3 terminal statuses, got %d", nTerminal)
	}
	if n := len(AllTaskStatuses()); n != 5 {
		t.Fatalf("expected 5 statuses, got %d — update this partition deliberately, never incidentally", n)
	}
}

func newTask(t *testing.T) *Task {
	t.Helper()
	tk, err := NewTask(NewTaskInput{ID: "T1", ProjectID: "P1", Title: "do", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// --- scope invariants: no global / cross-project work items (ADR-0046 §3) ---

func TestNewTask_RequiresProject(t *testing.T) {
	if _, err := NewTask(NewTaskInput{ID: "T1", Title: "x", CreatedBy: "user:a", CreatedAt: t0}); err != ErrEmptyProjectScope {
		t.Fatalf("want ErrEmptyProjectScope, got %v", err)
	}
}

func TestNewIssue_RequiresProject(t *testing.T) {
	if _, err := NewIssue(NewIssueInput{ID: "I1", Title: "x", CreatedBy: "user:a", CreatedAt: t0}); err != ErrEmptyProjectScope {
		t.Fatalf("want ErrEmptyProjectScope, got %v", err)
	}
}

// --- Issue state machine ---

func TestIssueStateMachine(t *testing.T) {
	i, _ := NewIssue(NewIssueInput{ID: "I1", ProjectID: "P1", Title: "x", CreatedBy: "user:a", CreatedAt: t0})
	if i.Status() != IssueOpen {
		t.Fatal("new issue should be open")
	}
	// legal: open → in_progress → resolved → closed → reopened → open
	for _, to := range []IssueStatus{IssueInProgress, IssueResolved, IssueClosed, IssueReopened, IssueOpen} {
		if err := i.Transition(to, t0); err != nil {
			t.Fatalf("legal transition to %s failed: %v", to, err)
		}
	}
	// illegal: open → resolved (skips in_progress)
	if err := i.Transition(IssueResolved, t0); err != ErrIllegalTransition {
		t.Fatalf("want ErrIllegalTransition open→resolved, got %v", err)
	}
	// discarded is terminal
	_ = i.Transition(IssueInProgress, t0)
	if err := i.Transition(IssueDiscarded, t0); err != nil {
		t.Fatalf("in_progress→discarded should be legal: %v", err)
	}
	if err := i.Transition(IssueOpen, t0); err != ErrIllegalTransition {
		t.Fatalf("discarded is terminal, want ErrIllegalTransition, got %v", err)
	}
}

// --- Task state machine: happy path + version bump ---

func TestTaskHappyPath(t *testing.T) {
	tk := newTask(t)
	if tk.Status() != TaskOpen || tk.Version() != 1 {
		t.Fatal("new task open v1")
	}
	if err := tk.Assign("agent:c", t0); err != nil {
		t.Fatal(err)
	}
	// v2.8.1: assign is metadata — status stays open (no "assigned" state).
	if tk.Status() != TaskOpen || tk.Assignee() != "agent:c" {
		t.Fatalf("assignee=agent:c + status open, got %s/%s", tk.Status(), tk.Assignee())
	}
	if err := tk.Start(t0); err != nil {
		t.Fatal(err)
	}
	if err := tk.Complete("agent:c", t0); err != nil {
		t.Fatal(err)
	}
	if tk.Status() != TaskCompleted || tk.CompletedBy() != "agent:c" {
		t.Fatalf("completed by agent:c, got %s/%s", tk.Status(), tk.CompletedBy())
	}
	if tk.Version() <= 1 {
		t.Fatal("version should bump on transitions")
	}
}

// ADR-0046: verification (Verify / ErrSelfVerify / TaskVerified) is DELETED —
// the former TestTaskNoSelfVerify was removed with the capability.

// --- Task: blocked requires a reason (plan §2.2) ---

func TestTaskBlockRequiresReason(t *testing.T) {
	tk := newTask(t)
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	if err := tk.Block("", BlockReasonObstacle, "agent:c", t0); err != ErrBlockReasonRequired {
		t.Fatalf("block without reason must fail, got %v", err)
	}
	if err := tk.Block("waiting on API key", BlockReasonObstacle, "agent:c", t0); err != nil {
		t.Fatal(err)
	}
	if tk.Status() != TaskFailed || tk.FailedReason() == "" {
		t.Fatalf("Block must fail the task and set failed_reason, got %s / %q", tk.Status(), tk.FailedReason())
	}
}

// --- Task: unassign + illegal transitions ---

func TestTaskUnassignAndIllegal(t *testing.T) {
	tk := newTask(t)
	// v2.8.1: assign is metadata — status stays open.
	_ = tk.Assign("agent:c", t0)
	if tk.Status() != TaskOpen || tk.Assignee() != "agent:c" {
		t.Fatal("assign sets assignee metadata, status stays open")
	}
	if err := tk.Unassign(t0); err != nil {
		t.Fatal(err)
	}
	if tk.Status() != TaskOpen || tk.Assignee() != "" {
		t.Fatal("unassign clears assignee, status stays open")
	}
	// can't complete an open task (must be running first)
	if err := tk.Complete("agent:c", t0); err != ErrIllegalTransition {
		t.Fatalf("open→completed illegal, got %v", err)
	}
}

// --- Task: terminal history is immutable ---

func TestTaskTerminalStatusesCannotReopen(t *testing.T) {
	tk := newTask(t)
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	if err := tk.Discard(t0); err != nil {
		t.Fatal(err)
	}
	if err := tk.Start(t0); err != ErrIllegalTransition {
		t.Fatalf("discarded is terminal, got %v", err)
	}

	// Completed is equally terminal: remediation is represented by a new task.
	tk2 := newTask(t)
	_ = tk2.Assign("agent:c", t0)
	_ = tk2.Start(t0)
	_ = tk2.Complete("agent:c", t0)
	if err := tk2.Reopen(t0); err != ErrTaskReopenRetired {
		t.Fatalf("Reopen(completed) = %v want ErrTaskReopenRetired", err)
	}
	if err := tk2.ToOpenFromReopened(t0); err != ErrTaskReopenRetired {
		t.Fatalf("ToOpenFromReopened = %v want ErrTaskReopenRetired", err)
	}
	if tk2.Status() != TaskCompleted || tk2.Assignee() != "agent:c" || tk2.CompletedBy() != "agent:c" {
		t.Fatalf("retired reopen mutated terminal task: %s/%s/%s", tk2.Status(), tk2.Assignee(), tk2.CompletedBy())
	}
}

func TestTaskRetryFailedStandaloneOnly(t *testing.T) {
	tk := newTask(t)
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	tk.SetDelivery(&Delivery{Source: "executor", ExecutorID: "exec-old", Pushed: true, Probed: true, BaseKnown: true, AheadOfBase: 1, Branch: "ac-exec/old", HeadSHA: "abc"})
	if err := tk.Fail("old attempt failed", "agent:c", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := tk.RetryFailed("user:a", t0.Add(2*time.Minute)); err != nil {
		t.Fatalf("RetryFailed standalone failed = %v", err)
	}
	if tk.Status() != TaskOpen || tk.FailedReason() != "" || tk.BlockedReason() != "" || tk.ExecutionLeaseExpiresAt() != nil || tk.Delivery() != nil {
		t.Fatalf("retry must reopen and clear current attempt state: status=%s failed=%q blocked=%q lease=%v delivery=%v",
			tk.Status(), tk.FailedReason(), tk.BlockedReason(), tk.ExecutionLeaseExpiresAt(), tk.Delivery())
	}
	logs := tk.ActionLogs()
	if len(logs) < 2 || logs[len(logs)-2].Action != TaskActionFailed || logs[len(logs)-1].Action != TaskActionRetried {
		t.Fatalf("retry must append without erasing failure evidence, logs=%+v", logs)
	}
	if err := tk.Assign("agent:new", t0.Add(3*time.Minute)); err != nil {
		t.Fatalf("retry result should be assignable: %v", err)
	}
	if err := tk.Start(t0.Add(4 * time.Minute)); err != nil {
		t.Fatalf("retry result should be startable: %v", err)
	}
}

func TestTaskRetryFailedRejectsPlanBoundAndOtherStatuses(t *testing.T) {
	planBound := newTask(t)
	_ = planBound.SetPlan("PL1", t0)
	_ = planBound.Assign("agent:c", t0)
	_ = planBound.Start(t0)
	_ = planBound.Fail("failed in plan", "agent:c", t0)
	if err := planBound.RetryFailed("user:a", t0); err != ErrTaskRetryPlanBound {
		t.Fatalf("plan-bound failed retry = %v want ErrTaskRetryPlanBound", err)
	}
	if planBound.Status() != TaskFailed || planBound.FailedReason() == "" {
		t.Fatalf("rejected plan-bound retry mutated task: %s/%q", planBound.Status(), planBound.FailedReason())
	}

	open := newTask(t)
	v := open.Version()
	if err := open.RetryFailed("user:a", t0); err != nil {
		t.Fatalf("standalone open retry should be idempotent: %v", err)
	}
	if open.Status() != TaskOpen || open.Version() != v {
		t.Fatalf("idempotent open retry mutated task: status=%s version=%d→%d", open.Status(), v, open.Version())
	}
	planOpen := newTask(t)
	_ = planOpen.SetPlan("PL2", t0)
	if err := planOpen.RetryFailed("user:a", t0); err != ErrTaskRetryPlanBound {
		t.Fatalf("plan-bound open retry = %v want ErrTaskRetryPlanBound", err)
	}
	if planOpen.Status() != TaskOpen {
		t.Fatalf("rejected plan-bound open retry mutated task to %s", planOpen.Status())
	}

	for _, status := range []TaskStatus{TaskRunning, TaskCompleted, TaskDiscarded} {
		tk := newTask(t)
		_ = tk.Assign("agent:c", t0)
		_ = tk.Start(t0)
		switch status {
		case TaskCompleted:
			_ = tk.Complete("agent:c", t0)
		case TaskDiscarded:
			_ = tk.Discard(t0)
		}
		if err := tk.RetryFailed("user:a", t0); err != ErrTaskRetryNotApplicable {
			t.Fatalf("RetryFailed(%s) = %v want ErrTaskRetryNotApplicable", status, err)
		}
		if tk.Status() != status {
			t.Fatalf("rejected retry mutated %s task to %s", status, tk.Status())
		}
	}
}

func TestTaskReopenedIsRetired(t *testing.T) {
	if TaskStatus("reopened").IsValid() {
		t.Fatal("reopened must not be a valid writable/persisted Task status")
	}
	if TaskStatus("reopened").IsDispatchable() || TaskStatus("reopened").CanTransitionTo(TaskOpen) || TaskStatus("reopened").CanTransitionTo(TaskRunning) {
		t.Fatal("retired reopened status must not be dispatchable or have exits")
	}
}

func TestProjectMemberRoleDefault(t *testing.T) {
	m, err := NewProjectMember(NewProjectMemberInput{ID: "M1", ProjectID: "P1", IdentityID: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if m.Role() != RoleMember {
		t.Fatalf("default role should be member, got %s", m.Role())
	}
}

func TestProject_LifecycleAndRehydrate(t *testing.T) {
	p, err := NewProject(NewProjectInput{ID: "P1", OrganizationID: "org-1", Name: "Acme", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status() != ProjectActive {
		t.Fatal("new project active")
	}
	if err := p.Rename("Acme Corp", t0); err != nil {
		t.Fatal(err)
	}
	p.Archive(t0)
	if p.Status() != ProjectArchived || p.Version() < 3 {
		t.Fatalf("archive + version bumps: %s v%d", p.Status(), p.Version())
	}
	if _, err := NewProject(NewProjectInput{ID: "P2", Name: "x", CreatedBy: "user:a", CreatedAt: t0}); err == nil {
		t.Fatal("project without org should fail")
	}
}

// SetStatus permits non-terminal operator corrections, but terminal facts never
// become active again and `reopened` cannot be written.
func TestTaskSetStatus_TerminalMonotonic(t *testing.T) {
	tk := newTask(t) // open
	for _, target := range []TaskStatus{TaskRunning, TaskOpen, TaskCompleted} {
		if err := tk.SetStatus(target, t0); err != nil {
			t.Fatalf("SetStatus(%s) non-terminal correction failed: %v", target, err)
		}
		if tk.Status() != target {
			t.Fatalf("status=%s want %s", tk.Status(), target)
		}
	}
	if err := tk.SetStatus(TaskOpen, t0); err != ErrIllegalTransition {
		t.Fatalf("completed→open = %v want ErrIllegalTransition", err)
	}
	if err := tk.SetStatus(TaskStatus("reopened"), t0); err != ErrTaskReopenRetired {
		t.Fatalf("completed→reopened = %v want ErrTaskReopenRetired", err)
	}
	// Invalid enum value rejected.
	if err := tk.SetStatus(TaskStatus("bogus"), t0); err != ErrInvalidStatus {
		t.Fatalf("invalid status: want ErrInvalidStatus, got %v", err)
	}
	// No-op (same status) is idempotent + no version bump.
	v := tk.Version()
	if err := tk.SetStatus(tk.Status(), t0); err != nil {
		t.Fatal(err)
	}
	if tk.Version() != v {
		t.Fatalf("no-op SetStatus must not bump version: %d→%d", v, tk.Version())
	}
}

// TestIssueSetStatus_FreeAnyValid — same for Issue.
func TestIssueSetStatus_FreeAnyValid(t *testing.T) {
	i, _ := NewIssue(NewIssueInput{ID: "I1", ProjectID: "P1", Title: "x", CreatedBy: "user:a", CreatedAt: t0})
	for _, target := range []IssueStatus{IssueClosed, IssueOpen, IssueDiscarded, IssueInProgress, IssueResolved, IssueReopened} {
		if err := i.SetStatus(target, t0); err != nil {
			t.Fatalf("SetStatus(%s) failed: %v", target, err)
		}
		if i.Status() != target {
			t.Fatalf("status=%s want %s", i.Status(), target)
		}
	}
	if err := i.SetStatus(IssueStatus("bogus"), t0); err != ErrInvalidStatus {
		t.Fatalf("want ErrInvalidStatus, got %v", err)
	}
}

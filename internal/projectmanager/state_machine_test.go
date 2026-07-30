package projectmanager

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)

// TestTaskStatus_IsTerminal_Partition pins the terminal/active partition that
// the observability default task-query relies on (v2.7 #107 proj-B). ADR-0054
// terminal = {completed, discarded}; active (non-terminal) =
// {open, running, blocked}. "verified" and "reopened" stay retired. v2.8.1: no
// "assigned" state (assignee is metadata). Iterating every enum value guards against a
// new status silently landing on the wrong side (the proj-A "core-enum" §-1 lesson) —
// which is why AllTaskStatuses is the single source it iterates, rather than a list
// copied here that a later status could quietly fall off.
func TestTaskStatus_IsTerminal_Partition(t *testing.T) {
	terminal := map[TaskStatus]bool{TaskCompleted: true, TaskDiscarded: true}
	for _, s := range AllTaskStatuses() {
		if !s.IsValid() {
			t.Fatalf("%s not IsValid — enum drift", s)
		}
		if got := s.IsTerminal(); got != terminal[s] {
			t.Fatalf("IsTerminal(%s) = %v, want %v", s, got, terminal[s])
		}
	}
	// Exactly 2 terminal, 3 active.
	var nTerminal int
	for _, s := range AllTaskStatuses() {
		if s.IsTerminal() {
			nTerminal++
		}
	}
	if nTerminal != 2 {
		t.Fatalf("expected 2 terminal statuses, got %d", nTerminal)
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
	// ADR-0054: Block PARKS the task (status→blocked, which is what stops dispatch) AND
	// still writes the reason annotation every reason-keyed consumer reads.
	if tk.Status() != TaskBlocked || tk.BlockedReason() == "" {
		t.Fatalf("Block must park (status=blocked) and set the reason, got %s / %q", tk.Status(), tk.BlockedReason())
	}
	if err := tk.Unblock("", "agent:c", t0); err != nil {
		t.Fatal(err)
	}
	// Unblock is the recovery door: blocked→running with the reason cleared.
	if tk.Status() != TaskRunning || tk.BlockedReason() != "" {
		t.Fatal("unblock must un-park to running and clear the reason")
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
	for _, target := range []TaskStatus{TaskRunning, TaskBlocked, TaskOpen, TaskCompleted} {
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

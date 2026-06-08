package projectmanager

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)

// TestTaskStatus_IsTerminal_Partition pins the terminal/active partition that
// the observability default task-query relies on (v2.7 #107 proj-B): terminal =
// {completed, verified, discarded}; active (non-terminal) = {open, running,
// blocked, reopened}. v2.8.1: no "assigned" state (assignee is metadata).
// Iterating every enum value guards against a new status silently landing on the
// wrong side (the proj-A "core-enum" §-1 lesson).
func TestTaskStatus_IsTerminal_Partition(t *testing.T) {
	terminal := map[TaskStatus]bool{TaskCompleted: true, TaskVerified: true, TaskDiscarded: true}
	all := []TaskStatus{TaskOpen, TaskRunning, TaskBlocked, TaskCompleted, TaskVerified, TaskDiscarded, TaskReopened}
	for _, s := range all {
		if !s.IsValid() {
			t.Fatalf("%s not IsValid — enum drift", s)
		}
		if got := s.IsTerminal(); got != terminal[s] {
			t.Fatalf("IsTerminal(%s) = %v, want %v", s, got, terminal[s])
		}
	}
	// Exactly 3 terminal, 4 active.
	var nTerminal int
	for _, s := range all {
		if s.IsTerminal() {
			nTerminal++
		}
	}
	if nTerminal != 3 {
		t.Fatalf("expected 3 terminal statuses, got %d", nTerminal)
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

// --- Task: no self-verification (plan §2.2 / OQ4) ---

func TestTaskNoSelfVerify(t *testing.T) {
	tk := newTask(t)
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	_ = tk.Complete("agent:c", t0)
	if err := tk.Verify("agent:c", t0); err != ErrSelfVerify {
		t.Fatalf("self-verify must be rejected, got %v", err)
	}
	// a different identity can verify
	if err := tk.Verify("user:reviewer", t0); err != nil {
		t.Fatalf("peer verify should succeed: %v", err)
	}
	if tk.Status() != TaskVerified {
		t.Fatalf("status should be verified, got %s", tk.Status())
	}
}

// --- Task: blocked requires a reason (plan §2.2) ---

func TestTaskBlockRequiresReason(t *testing.T) {
	tk := newTask(t)
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	if err := tk.Block("", t0); err != ErrBlockReasonRequired {
		t.Fatalf("block without reason must fail, got %v", err)
	}
	if err := tk.Block("waiting on API key", t0); err != nil {
		t.Fatal(err)
	}
	if tk.Status() != TaskBlocked || tk.BlockedReason() == "" {
		t.Fatal("blocked with reason")
	}
	if err := tk.Unblock(t0); err != nil {
		t.Fatal(err)
	}
	if tk.Status() != TaskRunning || tk.BlockedReason() != "" {
		t.Fatal("unblock clears reason, returns to running")
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

// --- Task: discard terminal + reopen chain ---

func TestTaskDiscardAndReopen(t *testing.T) {
	tk := newTask(t)
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	if err := tk.Discard(t0); err != nil {
		t.Fatal(err)
	}
	if err := tk.Start(t0); err != ErrIllegalTransition {
		t.Fatalf("discarded is terminal, got %v", err)
	}

	// reopen chain from verified: completed→verified→reopened→open
	tk2 := newTask(t)
	_ = tk2.Assign("agent:c", t0)
	_ = tk2.Start(t0)
	_ = tk2.Complete("agent:c", t0)
	_ = tk2.Verify("user:r", t0)
	if err := tk2.Reopen(t0); err != nil {
		t.Fatal(err)
	}
	if err := tk2.ToOpenFromReopened(t0); err != nil {
		t.Fatal(err)
	}
	if tk2.Status() != TaskOpen || tk2.Assignee() != "" || tk2.CompletedBy() != "" {
		t.Fatalf("reopened task should reset to a fresh open: %s/%s/%s", tk2.Status(), tk2.Assignee(), tk2.CompletedBy())
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

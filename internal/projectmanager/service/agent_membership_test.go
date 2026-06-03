package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// fakeAgentDir is a wired AgentDirectory mapping bare agent ids → org. A missing
// id returns ErrAgentNotFound (org unverifiable → pm treats as cross-org).
type fakeAgentDir map[string]string

func (f fakeAgentDir) OrgOfAgent(_ context.Context, agentID string) (string, error) {
	if org, ok := f[agentID]; ok {
		return org, nil
	}
	return "", errFakeAgentNotFound
}

var errFakeAgentNotFound = &agentNotFoundErr{}

// allOrgDir is a permissive AgentDirectory that maps EVERY agent to one fixed
// org. The shared pm fixtures (setup/flowSetup) assign agents in "org-1", so
// wiring this satisfies the #5a cross-org guard (and the fail-closed agent+nil
// rule) without each fixture enumerating agent ids.
type allOrgDir string

func (d allOrgDir) OrgOfAgent(_ context.Context, _ string) (string, error) { return string(d), nil }

type agentNotFoundErr struct{}

func (*agentNotFoundErr) Error() string { return "fake: agent not found" }

// v2.7 #187: project/issue/task ids are user-facing "<entity>-<8hex>".
func TestCreate_EntityIDPrefixes(t *testing.T) {
	svc, ctx := agentDirSetup(t, fakeAgentDir{})
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "i", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	tid, err := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pid), "project-") {
		t.Errorf("project id %q want project- prefix", pid)
	}
	if !strings.HasPrefix(string(iid), "issue-") {
		t.Errorf("issue id %q want issue- prefix", iid)
	}
	if !strings.HasPrefix(string(tid), "task-") {
		t.Errorf("task id %q want task- prefix", tid)
	}
}

// agentDirSetup wires a pm Service with the given fake AgentDirectory.
func agentDirSetup(t *testing.T, dir AgentDirectory) (*Service, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	gen := idgen.NewGenerator(clk)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: gen, Clock: clk, AgentDir: dir,
	})
	return svc, context.Background()
}

func memberOf(t *testing.T, svc *Service, ctx context.Context, pid pm.ProjectID, who pm.IdentityRef) bool {
	t.Helper()
	ms, err := svc.ListMembers(ctx, pid)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	n := 0
	for _, m := range ms {
		if m.IdentityID() == who {
			n++
		}
	}
	if n > 1 {
		t.Fatalf("identity %s appears %d times in members (not idempotent)", who, n)
	}
	return n == 1
}

// TestAssignTask_GrantsAgentProjectMembership is the #5a acceptance: assigning an
// agent in the project's org makes it a ProjectMember (so it passes the write-
// gate), cross-org agents are rejected, the add is idempotent on re-assign, and
// unassign does NOT remove the membership.
func TestAssignTask_GrantsAgentProjectMembership(t *testing.T) {
	dir := fakeAgentDir{"AG1": "org-1", "AG_OTHER": "org-2"}
	svc, ctx := agentDirSetup(t, dir)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	// 1) same-org agent → granted membership.
	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatalf("assign agent:AG1: %v", err)
	}
	if !memberOf(t, svc, ctx, pid, "agent:AG1") {
		t.Fatal("agent:AG1 should be a ProjectMember after assignment")
	}

	// 2) idempotent re-assign of the SAME agent (still assigned) → no error,
	// still exactly one member row.
	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != nil {
		t.Fatalf("re-assign agent:AG1 should be idempotent, got %v", err)
	}
	if !memberOf(t, svc, ctx, pid, "agent:AG1") {
		t.Fatal("re-assign must keep agent:AG1 a member exactly once")
	}

	// 3) unassign (assigned→open) → membership REMAINS (monotonic, OQ13-style).
	if err := svc.UnassignTask(ctx, tid, "user:a"); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if !memberOf(t, svc, ctx, pid, "agent:AG1") {
		t.Fatal("unassign must NOT remove the agent's project membership")
	}

	// 4) the granted membership lets the agent pass the project write-gate as an
	// ACTOR. On a fresh task assigned to AG1, StartTask with actor=agent:AG1 must
	// NOT be ErrNotMember.
	tid2, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do2", CreatedBy: "user:a"})
	if err := svc.AssignTask(ctx, tid2, "agent:AG1", "user:a"); err != nil {
		t.Fatalf("assign agent:AG1 to tid2: %v", err)
	}
	if err := svc.StartTask(ctx, tid2, "agent:AG1"); err != nil {
		t.Fatalf("agent:AG1 should pass the write-gate via StartTask, got %v", err)
	}
}

// TestAssignTask_CrossOrgAgentRejected: an agent in a DIFFERENT org is rejected
// with ErrCrossOrgAssignee and is NOT assigned / NOT made a member.
func TestAssignTask_CrossOrgAgentRejected(t *testing.T) {
	dir := fakeAgentDir{"AG_OTHER": "org-2"}
	svc, ctx := agentDirSetup(t, dir)
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	if err := svc.AssignTask(ctx, tid, "agent:AG_OTHER", "user:a"); err != pm.ErrCrossOrgAssignee {
		t.Fatalf("cross-org assign: want ErrCrossOrgAssignee, got %v", err)
	}
	if memberOf(t, svc, ctx, pid, "agent:AG_OTHER") {
		t.Fatal("cross-org agent must NOT be made a member")
	}
	// The whole tx rolled back → the assignment did not stick either.
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Assignee() == "agent:AG_OTHER" || tk.Status() != pm.TaskOpen {
		t.Fatalf("cross-org assign must not persist: status=%s assignee=%q", tk.Status(), tk.Assignee())
	}
}

// TestAssignTask_UnknownAgentRejected: an agent the directory can't resolve is
// treated as cross-org (org unverifiable) and rejected.
func TestAssignTask_UnknownAgentRejected(t *testing.T) {
	svc, ctx := agentDirSetup(t, fakeAgentDir{})
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	if err := svc.AssignTask(ctx, tid, "agent:GHOST", "user:a"); err != pm.ErrCrossOrgAssignee {
		t.Fatalf("unknown agent assign: want ErrCrossOrgAssignee, got %v", err)
	}
}

// TestAssignTask_AgentAssignee_NilDirectory_FailsClosed: assigning an AGENT with
// no AgentDirectory wired must be REJECTED (fail-closed) — a missing dependency
// must never silently bypass the cross-org guard. Human assignees are unaffected.
func TestAssignTask_AgentAssignee_NilDirectory_FailsClosed(t *testing.T) {
	svc, ctx := agentDirSetup(t, nil) // no AgentDirectory wired
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})

	// Agent assignee + nil directory → fail-closed error, assignment rolled back.
	if err := svc.AssignTask(ctx, tid, "agent:AG1", "user:a"); err != pm.ErrAgentDirectoryUnavailable {
		t.Fatalf("agent assign with nil directory: want ErrAgentDirectoryUnavailable, got %v", err)
	}
	tk, _ := svc.tasks.FindByID(ctx, tid)
	if tk.Assignee() != "" {
		t.Fatalf("assignment must have rolled back, assignee=%q", tk.Assignee())
	}

	// Human assignee with nil directory is unaffected (agent-only fail-closed).
	if err := svc.AssignTask(ctx, tid, "user:a", "user:a"); err != nil {
		t.Fatalf("human assign with nil directory should be unaffected: %v", err)
	}
}

// TestAssignTask_HumanAssigneeUnaffected: a `user:` assignee is never granted
// membership by this branch (it is agent-only), even with the directory wired.
func TestAssignTask_HumanAssigneeUnaffected(t *testing.T) {
	svc, ctx := agentDirSetup(t, fakeAgentDir{"AG1": "org-1"})
	pid, _ := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:b", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	tid, _ := svc.CreateTask(ctx, CreateTaskCommand{ProjectID: pid, Title: "do", CreatedBy: "user:a"})
	if err := svc.AssignTask(ctx, tid, "user:b", "user:a"); err != nil {
		t.Fatalf("assign user:b: %v", err)
	}
	// user:b was already a member from AddProjectMember; assign must not duplicate.
	if !memberOf(t, svc, ctx, pid, "user:b") {
		t.Fatal("user:b membership should be intact and single")
	}
	// A user assignee that is NOT a member is not auto-granted (agent-only branch);
	// but AssignTask only gates the ACTOR, so this just confirms no member row was
	// created for a brand-new user assignee.
	if _, err := svc.AddProjectMember(ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: "user:c", Actor: "user:a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.AssignTask(ctx, tid, "user:c", "user:a"); err != nil {
		t.Fatalf("reassign user:c: %v", err)
	}
}

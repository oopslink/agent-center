package projectmanager

import (
	"testing"
)

func TestProject_GettersRehydrateAndErrors(t *testing.T) {
	if !ProjectActive.IsValid() || ProjectStatus("bogus").IsValid() {
		t.Fatal("ProjectStatus.IsValid wrong")
	}
	p, err := RehydrateProject(RehydrateProjectInput{
		ID: "P1", OrganizationID: "org", Name: "n", Description: "d",
		Status: ProjectActive, CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "P1" || p.OrganizationID() != "org" || p.Name() != "n" || p.Description() != "d" ||
		p.Status() != ProjectActive || p.CreatedBy() != "user:a" || p.Version() != 2 ||
		!p.CreatedAt().Equal(t0) || !p.UpdatedAt().Equal(t0) {
		t.Fatalf("project getters wrong: %+v", p)
	}
	p.SetDescription("new", t0)
	if p.Description() != "new" || p.Version() != 3 {
		t.Fatal("SetDescription")
	}
	if err := p.Rename("", t0); err == nil {
		t.Fatal("empty rename should fail")
	}
	if _, err := RehydrateProject(RehydrateProjectInput{Status: "bad", Version: 1}); err != ErrInvalidStatus {
		t.Fatalf("bad status want ErrInvalidStatus, got %v", err)
	}
	if _, err := RehydrateProject(RehydrateProjectInput{Status: ProjectActive, Version: 0}); err == nil {
		t.Fatal("version<1 should fail")
	}
}

func TestIssue_GettersRehydrate(t *testing.T) {
	i, err := RehydrateIssue(RehydrateIssueInput{
		ID: "I1", ProjectID: "P1", Title: "t", Description: "d",
		Status: IssueResolved, CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if i.ID() != "I1" || i.ProjectID() != "P1" || i.Title() != "t" || i.Description() != "d" ||
		i.Status() != IssueResolved || i.CreatedBy() != "user:a" || i.Version() != 3 ||
		!i.CreatedAt().Equal(t0) || !i.UpdatedAt().Equal(t0) {
		t.Fatalf("issue getters wrong: %+v", i)
	}
	if IssueStatus("x").IsValid() {
		t.Fatal("bad issue status valid")
	}
	if _, err := RehydrateIssue(RehydrateIssueInput{Status: "bad", Version: 1}); err != ErrInvalidStatus {
		t.Fatal("bad issue status rehydrate")
	}
}

func TestTask_GettersRehydrate(t *testing.T) {
	tk, err := RehydrateTask(RehydrateTaskInput{
		ID: "T1", ProjectID: "P1", Title: "t", Description: "d", Status: TaskRunning,
		Assignee: "agent:c", DerivedFromIssue: "I1", CompletedBy: "", BlockedReason: "r",
		CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	// ADR-0046: a "stuck" task is RUNNING with a blocked_reason annotation.
	if tk.ID() != "T1" || tk.ProjectID() != "P1" || tk.Title() != "t" || tk.Description() != "d" ||
		tk.Status() != TaskRunning || tk.Assignee() != "agent:c" || tk.DerivedFromIssue() != "I1" ||
		tk.BlockedReason() != "r" || tk.CreatedBy() != "user:a" || tk.Version() != 4 ||
		!tk.CreatedAt().Equal(t0) || !tk.UpdatedAt().Equal(t0) {
		t.Fatalf("task getters wrong: %+v", tk)
	}
	if TaskStatus("x").IsValid() {
		t.Fatal("bad task status valid")
	}
	if _, err := RehydrateTask(RehydrateTaskInput{Status: "bad", Version: 1}); err != ErrInvalidStatus {
		t.Fatal("bad task status rehydrate")
	}
}

func TestMemberSubscriberCodeRepo_GettersAndErrors(t *testing.T) {
	m, err := NewProjectMember(NewProjectMemberInput{ID: "M1", ProjectID: "P1", IdentityID: "user:a", Role: RoleOwner, AddedBy: "user:o", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID() != "M1" || m.ProjectID() != "P1" || m.IdentityID() != "user:a" || m.Role() != RoleOwner ||
		m.AddedBy() != "user:o" || !m.CreatedAt().Equal(t0) {
		t.Fatalf("member getters wrong: %+v", m)
	}
	if _, err := NewProjectMember(NewProjectMemberInput{ID: "M2", ProjectID: "P1", IdentityID: "bad-id", CreatedAt: t0}); err == nil {
		t.Fatal("bad identity should fail")
	}
	if _, err := NewProjectMember(NewProjectMemberInput{ID: "M3", ProjectID: "P1", IdentityID: "user:a", Role: "bogus", CreatedAt: t0}); err == nil {
		t.Fatal("bad role should fail")
	}

	ts, err := NewTaskSubscriber("T1", "user:a", "user:o", t0)
	if err != nil || ts.TaskID() != "T1" || ts.IdentityID() != "user:a" || ts.AddedBy() != "user:o" || !ts.CreatedAt().Equal(t0) {
		t.Fatalf("task subscriber getters wrong: %+v %v", ts, err)
	}
	if _, err := NewTaskSubscriber("", "user:a", "u", t0); err == nil {
		t.Fatal("empty task id should fail")
	}
	is, err := NewIssueSubscriber("I1", "user:a", "user:o", t0)
	if err != nil || is.IssueID() != "I1" || is.IdentityID() != "user:a" || is.AddedBy() != "user:o" || !is.CreatedAt().Equal(t0) {
		t.Fatalf("issue subscriber getters wrong: %+v %v", is, err)
	}
	if _, err := NewIssueSubscriber("I1", "bad", "u", t0); err == nil {
		t.Fatal("bad identity should fail")
	}

	c, err := NewCodeRepoRef(NewCodeRepoRefInput{ID: "R1", ProjectID: "P1", URL: "u", Label: "l", AddedBy: "user:o", CreatedAt: t0})
	if err != nil || c.ID() != "R1" || c.ProjectID() != "P1" || c.URL() != "u" || c.Label() != "l" || c.AddedBy() != "user:o" || !c.CreatedAt().Equal(t0) {
		t.Fatalf("coderepo getters wrong: %+v %v", c, err)
	}
	if _, err := NewCodeRepoRef(NewCodeRepoRefInput{ID: "R2", ProjectID: "P1", URL: "", CreatedAt: t0}); err == nil {
		t.Fatal("empty url should fail")
	}
	if _, err := NewCodeRepoRef(NewCodeRepoRefInput{ID: "R3", URL: "u", CreatedAt: t0}); err != ErrEmptyProjectScope {
		t.Fatalf("empty project want ErrEmptyProjectScope, got %v", err)
	}
}

func TestIdentityRefValidate(t *testing.T) {
	for _, ok := range []IdentityRef{"system", "user:x", "agent:y"} {
		if err := ok.Validate(); err != nil {
			t.Fatalf("%s should be valid: %v", ok, err)
		}
	}
	for _, bad := range []IdentityRef{"", "x", "user:", "bot:z"} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("%s should be invalid", bad)
		}
	}
	// String() helpers
	_ = ProjectID("p").String()
	_ = IssueID("i").String()
	_ = TaskID("t").String()
	_ = MemberID("m").String()
	_ = IdentityRef("user:x").String()
}

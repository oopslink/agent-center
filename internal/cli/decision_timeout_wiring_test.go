package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/reminder"
	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func TestResolveDecisionRemindee(t *testing.T) {
	cases := []struct {
		name, owner, planCreator, want string
		ok                             bool
	}{
		{"agent owner", "agent:reviewer", "user:a", "reviewer", true},
		{"unassigned falls back to agent plan creator", "", "agent:pm", "pm", true},
		{"user owner skipped", "user:pd", "user:a", "", false},
		{"system owner skipped", "system", "user:a", "", false},
		{"unassigned + user creator skipped", "", "user:a", "", false},
		{"empty agent id skipped", "agent:", "user:a", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := resolveDecisionRemindee(c.owner, c.planCreator)
			if ok != c.ok || got != c.want {
				t.Fatalf("resolveDecisionRemindee(%q,%q) = (%q,%v), want (%q,%v)", c.owner, c.planCreator, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestDecisionReminderCreator(t *testing.T) {
	if got := decisionReminderCreator("user:alice"); got != "user:alice" {
		t.Fatalf("user creator = %q, want passthrough", got)
	}
	// A non-user (agent/system/empty) plan creator maps to the system-user owner ref so
	// the reminder still clears the cross-project guard.
	for _, in := range []string{"agent:pm", "system", ""} {
		if got := decisionReminderCreator(in); got != "user:system" {
			t.Fatalf("decisionReminderCreator(%q) = %q, want user:system", in, got)
		}
		if !strings.HasPrefix(decisionReminderCreator(in), "user:") {
			t.Fatalf("creator for %q does not satisfy the owner (user:) guard", in)
		}
	}
}

func TestDecisionReminderContent(t *testing.T) {
	plain := decisionReminderContent("review X", false, 90*time.Minute)
	if strings.Contains(plain, "升级") || !strings.Contains(plain, "review X") {
		t.Fatalf("plain content wrong: %q", plain)
	}
	esc := decisionReminderContent("review X", true, 3*time.Hour)
	if !strings.Contains(esc, "升级") || !strings.Contains(esc, "review X") {
		t.Fatalf("escalate content wrong: %q", esc)
	}
}

// --- ArmDecisionReminder end-to-end over fake repos + a fake reminder creator ---------

type fakeReminderCreator struct {
	cmds []cogservice.CreateReminderCommand
}

func (f *fakeReminderCreator) CreateReminder(_ context.Context, cmd cogservice.CreateReminderCommand) (*reminder.Reminder, error) {
	f.cmds = append(f.cmds, cmd)
	return nil, nil
}

type fakeTaskFinder struct{ byID map[pm.TaskID]*pm.Task }

func (f fakeTaskFinder) FindByID(_ context.Context, id pm.TaskID) (*pm.Task, error) {
	return f.byID[id], nil
}

type fakePlanFinder struct{ p *pm.Plan }

func (f fakePlanFinder) FindByID(_ context.Context, _ pm.PlanID) (*pm.Plan, error) {
	return f.p, nil
}

type fakeProjectFinder struct{ p *pm.Project }

func (f fakeProjectFinder) FindByID(_ context.Context, _ pm.ProjectID) (*pm.Project, error) {
	return f.p, nil
}

func mkTask(t *testing.T, id, assignee string) *pm.Task {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	task, err := pm.NewTask(pm.NewTaskInput{ID: pm.TaskID(id), ProjectID: "proj-1", Title: "Decision " + id, CreatedBy: "user:a", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if assignee != "" {
		if err := task.Assign(pm.IdentityRef(assignee), now); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func newAdapterFixture(t *testing.T, tasks map[pm.TaskID]*pm.Task, planCreator string) (*decisionReminderAdapter, *fakeReminderCreator) {
	t.Helper()
	now := time.Unix(1_700_000_000, 0).UTC()
	proj, err := pm.NewProject(pm.NewProjectInput{ID: "proj-1", OrganizationID: "org-1", Name: "P", CreatedBy: "user:a", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := pm.NewPlan(pm.NewPlanInput{ID: "plan-1", ProjectID: "proj-1", Name: "PL", CreatorRef: pm.IdentityRef(planCreator), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeReminderCreator{}
	ad := &decisionReminderAdapter{
		reminders: fc,
		tasks:     fakeTaskFinder{byID: tasks},
		plans:     fakePlanFinder{p: plan},
		projects:  fakeProjectFinder{p: proj},
		now:       func() time.Time { return now },
		log:       func(string, ...any) {},
	}
	return ad, fc
}

func TestArmDecisionReminder_AgentOwner_CreatesReminder(t *testing.T) {
	dec := mkTask(t, "dec-1", "agent:reviewer")
	ad, fc := newAdapterFixture(t, map[pm.TaskID]*pm.Task{"dec-1": dec}, "user:a")
	err := ad.ArmDecisionReminder(context.Background(), pmservice.DecisionReminderRequest{
		PlanID: "plan-1", TaskID: "dec-1", DecisionKeys: []string{"dec-1"}, ProbeCount: 1, Overdue: 90 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ArmDecisionReminder err = %v", err)
	}
	if len(fc.cmds) != 1 {
		t.Fatalf("created %d reminders, want 1", len(fc.cmds))
	}
	cmd := fc.cmds[0]
	if cmd.RemindeeAgentID != "reviewer" {
		t.Fatalf("remindee = %q, want reviewer", cmd.RemindeeAgentID)
	}
	if cmd.OrganizationID != "org-1" {
		t.Fatalf("org = %q, want org-1", cmd.OrganizationID)
	}
	if !strings.HasPrefix(cmd.CreatorRef, "user:") {
		t.Fatalf("creator %q must be a user (guard bypass)", cmd.CreatorRef)
	}
	if cmd.Schedule.Kind != reminder.ScheduleOnce || !cmd.Schedule.OnceAt.After(time.Unix(1_700_000_000, 0).UTC()) {
		t.Fatalf("schedule = %+v, want a future once-fire", cmd.Schedule)
	}
	if !strings.Contains(cmd.Content, "Decision dec-1") {
		t.Fatalf("content should carry the decision title: %q", cmd.Content)
	}
}

func TestArmDecisionReminder_UserOwner_Skips(t *testing.T) {
	dec := mkTask(t, "dec-1", "user:pd")
	ad, fc := newAdapterFixture(t, map[pm.TaskID]*pm.Task{"dec-1": dec}, "user:a")
	if err := ad.ArmDecisionReminder(context.Background(), pmservice.DecisionReminderRequest{
		PlanID: "plan-1", TaskID: "dec-1", DecisionKeys: []string{"dec-1"}, ProbeCount: 1,
	}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(fc.cmds) != 0 {
		t.Fatalf("user-owned decision armed a reminder: %+v", fc.cmds)
	}
}

func TestArmDecisionReminder_EscalateContent(t *testing.T) {
	dec := mkTask(t, "dec-1", "agent:reviewer")
	ad, fc := newAdapterFixture(t, map[pm.TaskID]*pm.Task{"dec-1": dec}, "user:a")
	if err := ad.ArmDecisionReminder(context.Background(), pmservice.DecisionReminderRequest{
		PlanID: "plan-1", TaskID: "dec-1", DecisionKeys: []string{"dec-1"}, ProbeCount: 3, Escalate: true, Overdue: 3 * time.Hour,
	}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(fc.cmds) != 1 || !strings.Contains(fc.cmds[0].Content, "升级") {
		t.Fatalf("escalation reminder content wrong: %+v", fc.cmds)
	}
}

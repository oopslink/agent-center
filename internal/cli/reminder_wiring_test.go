package cli

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	cogservice "github.com/oopslink/agent-center/internal/cognition/reminder/service"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// TestDeliverySender covers F-B's delivery-identity selection + the self-reminder
// wake guard: deliver_as_creator posts as the creator EXCEPT when the creator is
// the remindee agent (a self-reminder), where the WakeProjector excludes the
// message's own sender from the wake — so we fall back to the system identity so
// the remindee is still woken.
func TestDeliverySender(t *testing.T) {
	remindee := conversation.IdentityRef("agent:AG2")
	const system = conversation.IdentityRef("system")

	cases := []struct {
		name string
		req  cogservice.DeliveryRequest
		want conversation.IdentityRef
	}{
		{"off → system",
			cogservice.DeliveryRequest{RemindeeAgentID: "AG2", CreatorRef: "user:owner", DeliverAsCreator: false}, system},
		{"on, owner creator → creator",
			cogservice.DeliveryRequest{RemindeeAgentID: "AG2", CreatorRef: "user:owner", DeliverAsCreator: true}, conversation.IdentityRef("user:owner")},
		{"on, agent creator (other) → creator",
			cogservice.DeliveryRequest{RemindeeAgentID: "AG2", CreatorRef: "agent:AG1", DeliverAsCreator: true}, conversation.IdentityRef("agent:AG1")},
		{"on, self-reminder → system (wake guard)",
			cogservice.DeliveryRequest{RemindeeAgentID: "AG2", CreatorRef: "agent:AG2", DeliverAsCreator: true}, system},
		{"on, empty creator → system",
			cogservice.DeliveryRequest{RemindeeAgentID: "AG2", CreatorRef: "", DeliverAsCreator: true}, system},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliverySender(tc.req, remindee); got != tc.want {
				t.Errorf("deliverySender = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestScanMember_SelfReminder guards T229: when creator == remindee (an agent
// setting a reminder for itself), a member matching that ref must mark BOTH
// remindeeIn and creatorIn true. A switch on the ref would run only its first
// case and leave creatorIn false → the project would not count as shared and
// ResolveReminderContext would let the aggregate reject with
// ErrCrossProjectReminder (HTTP 403) on the legitimate self-reminder use case.
func TestScanMember_SelfReminder(t *testing.T) {
	const ref = "agent:a1"
	remindeeIn, creatorIn := scanMember(ref, ref, ref, false, false)
	if !remindeeIn || !creatorIn {
		t.Fatalf("self-reminder: want remindeeIn && creatorIn, got remindeeIn=%v creatorIn=%v", remindeeIn, creatorIn)
	}
}

// TestScanMember_PeerReminder confirms the ordinary peer case (creator != remindee)
// still resolves each flag from its own member row independently.
func TestScanMember_PeerReminder(t *testing.T) {
	const (
		remindeeRef = "agent:remindee"
		creatorRef  = "agent:creator"
	)
	remindeeIn, creatorIn := false, false
	for _, ref := range []string{creatorRef, "agent:other", remindeeRef} {
		remindeeIn, creatorIn = scanMember(ref, remindeeRef, creatorRef, remindeeIn, creatorIn)
	}
	if !remindeeIn || !creatorIn {
		t.Fatalf("peer reminder: want both true, got remindeeIn=%v creatorIn=%v", remindeeIn, creatorIn)
	}
}

// TestScanMember_NonMember leaves both flags false for an unrelated member, and
// must not flip a flag already set true on a prior iteration.
func TestScanMember_NonMember(t *testing.T) {
	remindeeIn, creatorIn := scanMember("agent:stranger", "agent:remindee", "agent:creator", false, false)
	if remindeeIn || creatorIn {
		t.Fatalf("non-member: want both false, got remindeeIn=%v creatorIn=%v", remindeeIn, creatorIn)
	}
	// a hit on a later row must preserve, never clear, an earlier hit
	remindeeIn, creatorIn = scanMember("agent:stranger", "agent:remindee", "agent:creator", true, true)
	if !remindeeIn || !creatorIn {
		t.Fatalf("non-member must not clear prior hits, got remindeeIn=%v creatorIn=%v", remindeeIn, creatorIn)
	}
}

// fakeAgentDir maps bare agent ids → org for the pm AgentDirectory probe.
type fakeAgentDir map[string]string

func (f fakeAgentDir) OrgOfAgent(_ context.Context, id string) (string, error) {
	if org, ok := f[id]; ok {
		return org, nil
	}
	return "", errAgentNotFound
}

type agentNotFoundErr struct{}

func (*agentNotFoundErr) Error() string { return "fake: agent not found" }

var errAgentNotFound = &agentNotFoundErr{}

func newPMService(t *testing.T, dir pmservice.AgentDirectory) (*pmservice.Service, context.Context) {
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
	svc := pmservice.New(pmservice.Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: gen, Clock: clk, AgentDir: dir,
	})
	return svc, context.Background()
}

// memberProject creates a project in org and grants `agentID` membership (by
// assigning it a task — the #5a same-org grant). Returns the project id string.
func memberProject(t *testing.T, svc *pmservice.Service, ctx context.Context, org, name, agentID string) string {
	t.Helper()
	pid, err := svc.CreateProject(ctx, pmservice.CreateProjectCommand{OrganizationID: org, Name: name, CreatedBy: "user:owner"})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	tid, err := svc.CreateTask(ctx, pmservice.CreateTaskCommand{ProjectID: pid, Title: "t", CreatedBy: "user:owner"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := svc.AssignTask(ctx, tid, pm.IdentityRef("agent:"+agentID), "user:owner"); err != nil {
		t.Fatalf("AssignTask: %v", err)
	}
	return string(pid)
}

// TestResolveReminderContext_SelfReminder is the T229 run-real equivalent against
// the REAL wiring: an agent that shares a project with itself (creator==remindee)
// must resolve a context whose CreatorProjectID == ProjectID, so the aggregate's
// `CreatorProjectID != ProjectID` cross-project guard passes (no 403).
func TestResolveReminderContext_SelfReminder(t *testing.T) {
	svc, ctx := newPMService(t, fakeAgentDir{"AG1": "org-1"})
	pid := memberProject(t, svc, ctx, "org-1", "P1", "AG1")

	dir := &reminderDirectory{pmSvc: svc}
	rc, err := dir.ResolveReminderContext(ctx, "org-1", "agent:AG1", "AG1")
	if err != nil {
		t.Fatalf("self-reminder must resolve, got err: %v", err)
	}
	if rc.ProjectID != pid || rc.CreatorProjectID != pid {
		t.Fatalf("self-reminder: want ProjectID==CreatorProjectID==%q, got ProjectID=%q CreatorProjectID=%q",
			pid, rc.ProjectID, rc.CreatorProjectID)
	}
}

// TestResolveReminderContext_CrossProjectStillRejected confirms the fix does not
// weaken the guard: when an agent creator shares NO project with the remindee, the
// resolved context still has an empty CreatorProjectID (≠ ProjectID) so the
// aggregate rejects with ErrCrossProjectReminder.
func TestResolveReminderContext_CrossProjectStillRejected(t *testing.T) {
	svc, ctx := newPMService(t, fakeAgentDir{"REMINDEE": "org-1", "CREATOR": "org-1"})
	remindeeProject := memberProject(t, svc, ctx, "org-1", "P-remindee", "REMINDEE")
	_ = memberProject(t, svc, ctx, "org-1", "P-creator", "CREATOR") // creator in a different project

	dir := &reminderDirectory{pmSvc: svc}
	rc, err := dir.ResolveReminderContext(ctx, "org-1", "agent:CREATOR", "REMINDEE")
	if err != nil {
		t.Fatalf("cross-project resolve should defer to aggregate, got err: %v", err)
	}
	if rc.ProjectID != remindeeProject {
		t.Fatalf("want ProjectID==remindee project %q, got %q", remindeeProject, rc.ProjectID)
	}
	if rc.CreatorProjectID != "" {
		t.Fatalf("cross-project: CreatorProjectID must stay empty so the aggregate rejects, got %q", rc.CreatorProjectID)
	}
}

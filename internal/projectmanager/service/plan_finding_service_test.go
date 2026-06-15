package service

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// --- pure formatter unit tests (Stage 4) ------------------------------------

func mkFindingForFmt(t *testing.T, id, task string, kind pm.PlanFindingKind, content string) *pm.PlanFinding {
	t.Helper()
	f, err := pm.NewPlanFinding(pm.NewPlanFindingInput{
		ID: pm.PlanFindingID(id), PlanID: "PL-1", TaskID: pm.TaskID(task), ProjectID: "P-1",
		AuthorRef: "agent:ag1", Kind: kind, Content: content, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatalf("NewPlanFinding: %v", err)
	}
	return f
}

func TestFormatFindingsForDispatch_Empty(t *testing.T) {
	if got := formatFindingsForDispatch(nil, 0); got != "" {
		t.Fatalf("empty findings should format to empty string, got %q", got)
	}
	// total>0 but no rows shown (shouldn't happen, but must be safe) → "".
	if got := formatFindingsForDispatch(nil, 5); got != "" {
		t.Fatalf("no shown rows should format to empty string, got %q", got)
	}
}

func TestFormatFindingsForDispatch_Basic(t *testing.T) {
	fs := []*pm.PlanFinding{
		mkFindingForFmt(t, "f1", "T-1", pm.FindingFailure, "printer change did not affect output"),
		mkFindingForFmt(t, "f2", "T-1", pm.FindingFact, "the real bug is on the tuple path"),
	}
	got := formatFindingsForDispatch(fs, 2)
	if !strings.Contains(got, "2 finding(s) recorded in this plan so far") {
		t.Errorf("missing header: %q", got)
	}
	if !strings.Contains(got, "- [failure] (T-1) printer change did not affect output") {
		t.Errorf("missing failure bullet: %q", got)
	}
	if !strings.Contains(got, "- [fact] (T-1) the real bug is on the tuple path") {
		t.Errorf("missing fact bullet: %q", got)
	}
}

func TestFormatFindingsForDispatch_TruncationNotice(t *testing.T) {
	// The repo bounds the window now; the formatter is told `shown` (the capped
	// window) + `total` (full count) and renders the "latest N of M" notice.
	var shown []*pm.PlanFinding
	for i := 0; i < dispatchFindingsCap; i++ {
		shown = append(shown, mkFindingForFmt(t, "f"+strconv.Itoa(i), "T-1", pm.FindingFact, "c"+strconv.Itoa(i)))
	}
	total := dispatchFindingsCap + 5
	got := formatFindingsForDispatch(shown, total)
	wantHeader := "latest " + strconv.Itoa(dispatchFindingsCap) + " of " + strconv.Itoa(total) + " findings"
	if !strings.Contains(got, wantHeader) {
		t.Errorf("missing truncation notice %q in %q", wantHeader, got)
	}
	if n := strings.Count(got, "\n- "); n != dispatchFindingsCap {
		t.Errorf("want %d bullets, got %d", dispatchFindingsCap, n)
	}
}

func TestFindingOneLine(t *testing.T) {
	if got := findingOneLine("a\n\tb   c"); got != "a b c" {
		t.Errorf("whitespace not collapsed: %q", got)
	}
	long := strings.Repeat("x", 300)
	got := findingOneLine(long)
	if len([]rune(got)) != 241 || !strings.HasSuffix(got, "…") { // 240 runes + ellipsis
		t.Errorf("long ASCII line not truncated with ellipsis: len=%d", len([]rune(got)))
	}
	// review #1: Chinese (3-byte runes) must truncate on a rune boundary → valid UTF-8.
	cn := findingOneLine(strings.Repeat("发现", 200)) // 400 runes, 1200 bytes
	if !utf8.ValidString(cn) {
		t.Errorf("Chinese truncation produced invalid UTF-8: %q", cn)
	}
	if len([]rune(cn)) != 241 || !strings.HasSuffix(cn, "…") {
		t.Errorf("Chinese line not truncated to 240 runes + …: runes=%d", len([]rune(cn)))
	}
}

// --- service harness (Stage 3 + dispatch wiring) ----------------------------

type captureDispatcher struct{ calls []dispatchCall }

type dispatchCall struct{ conv, assignee, content string }

func (d *captureDispatcher) PostMention(_ context.Context, conv, assignee, content string) (string, error) {
	d.calls = append(d.calls, dispatchCall{conv: conv, assignee: assignee, content: content})
	return "msg-" + strconv.Itoa(len(d.calls)), nil
}

type findingHarness struct {
	svc        *Service
	plans      *pmsql.PlanRepo
	findings   *pmsql.PlanFindingRepo
	outbox     outbox.Repository
	dispatcher *captureDispatcher
	clk        *clock.FakeClock
	rawDB      *sql.DB
	ctx        context.Context
}

func findingSetup(t *testing.T) *findingHarness {
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
	ob := outboxsql.NewOutboxRepo(db)
	plans := pmsql.NewPlanRepo(db)
	findings := pmsql.NewPlanFindingRepo(db)
	disp := &captureDispatcher{}
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans, Outbox: ob, IDGen: gen, Clock: clk,
		AgentDir: allOrgDir("org-1"), PlanDispatcher: disp, Findings: findings,
	})
	return &findingHarness{svc: svc, plans: plans, findings: findings, outbox: ob, dispatcher: disp, clk: clk, rawDB: db, ctx: context.Background()}
}

// seedPlanWithAssignedTask creates a project, adds agent members, a plan (with a
// bound conversation), and one task assigned to agent:ag1 and selected into the
// plan, then starts the plan. Returns the ids.
func (h *findingHarness) seedPlanWithAssignedTask(t *testing.T) (pid pm.ProjectID, planID pm.PlanID, taskID pm.TaskID) {
	t.Helper()
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	// agent members (ag1 will be the assignee/author; ag2 is a member non-assignee).
	for _, ag := range []pm.IdentityRef{"agent:ag1", "agent:ag2"} {
		if _, err := h.svc.AddProjectMember(h.ctx, AddProjectMemberCommand{ProjectID: pid, IdentityID: ag, Role: pm.RoleMember, Actor: "user:a"}); err != nil {
			t.Fatalf("AddProjectMember %s: %v", ag, err)
		}
	}
	planID, err = h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "alpha", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	// bind a conversation id (production: PlanParticipantProjector; here: direct).
	p, _ := h.plans.FindByID(h.ctx, planID)
	p.SetConversationID("conv-"+string(planID), h.clk.Now())
	if err := h.plans.Update(h.ctx, p); err != nil {
		t.Fatal(err)
	}
	taskID, err = h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "investigate", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AssignTask(h.ctx, taskID, "agent:ag1", "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, planID, taskID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	return pid, planID, taskID
}

func (h *findingHarness) outboxCount(t *testing.T, eventType string) int {
	t.Helper()
	rows, err := h.rawDB.QueryContext(h.ctx, `SELECT COUNT(*) FROM outbox_events WHERE event_type = ?`, eventType)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var n int
	if rows.Next() {
		_ = rows.Scan(&n)
	}
	return n
}

func TestRecordFinding_Success_AndEvent(t *testing.T) {
	h := findingSetup(t)
	_, planID, taskID := h.seedPlanWithAssignedTask(t)

	id, err := h.svc.RecordFinding(h.ctx, RecordFindingCommand{
		PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1",
		Kind: pm.FindingFact, Content: "the real bug is on the tuple path",
	})
	if err != nil {
		t.Fatalf("RecordFinding: %v", err)
	}
	if id == "" {
		t.Fatal("expected a finding id")
	}
	list, err := h.svc.ListPlanFindings(h.ctx, planID, "user:a")
	if err != nil || len(list) != 1 || list[0].ID() != id {
		t.Fatalf("ListPlanFindings: %v list=%v", err, list)
	}
	if got := h.outboxCount(t, EvtPlanFindingRecorded); got != 1 {
		t.Fatalf("want 1 %s event, got %d", EvtPlanFindingRecorded, got)
	}
}

func TestListPlanFindings_Authz(t *testing.T) {
	h := findingSetup(t)
	_, planID, taskID := h.seedPlanWithAssignedTask(t)
	if _, err := h.svc.RecordFinding(h.ctx, RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1", Kind: pm.FindingFact, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	// a project member (the assignee) can read.
	if list, err := h.svc.ListPlanFindings(h.ctx, planID, "agent:ag1"); err != nil || len(list) != 1 {
		t.Fatalf("member read: err=%v len=%d", err, len(list))
	}
	// a non-member cannot read another project's findings by knowing the plan_id (review #2).
	if _, err := h.svc.ListPlanFindings(h.ctx, planID, "agent:stranger"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("non-member read: want ErrNotMember, got %v", err)
	}
	// an unknown plan is a 404, not a silent empty list.
	if _, err := h.svc.ListPlanFindings(h.ctx, "missing-plan", "user:a"); !errors.Is(err, pm.ErrPlanNotFound) {
		t.Fatalf("unknown plan: want ErrPlanNotFound, got %v", err)
	}
}

func TestRecordFinding_AdmissionMatrix(t *testing.T) {
	h := findingSetup(t)
	pid, planID, taskID := h.seedPlanWithAssignedTask(t)

	// a task NOT in the plan (backlog), assigned to ag1 → ErrFindingTaskNotInPlan.
	backlog, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "backlog", CreatedBy: "user:a"})
	_ = h.svc.AssignTask(h.ctx, backlog, "agent:ag1", "user:a")

	cases := []struct {
		name string
		cmd  RecordFindingCommand
		want error
	}{
		{"non-assignee member author", RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag2", Kind: pm.FindingFact, Content: "x"}, pm.ErrFindingNotTaskAssignee},
		{"non-member author", RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ghost", Kind: pm.FindingFact, Content: "x"}, ErrNotMember},
		{"task not in plan", RecordFindingCommand{PlanID: planID, TaskID: backlog, AuthorRef: "agent:ag1", Kind: pm.FindingFact, Content: "x"}, pm.ErrFindingTaskNotInPlan},
		{"empty content", RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1", Kind: pm.FindingFact, Content: "   "}, pm.ErrEmptyFindingContent},
		{"bad kind", RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1", Kind: "bug", Content: "x"}, pm.ErrInvalidFindingKind},
		{"plan not found", RecordFindingCommand{PlanID: "missing", TaskID: taskID, AuthorRef: "agent:ag1", Kind: pm.FindingFact, Content: "x"}, pm.ErrPlanNotFound},
		{"bad author ref", RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "nope", Kind: pm.FindingFact, Content: "x"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := h.svc.RecordFinding(h.ctx, c.cmd)
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if c.want != nil && !errors.Is(err, c.want) {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
	// no finding should have been admitted.
	if list, _ := h.svc.ListPlanFindings(h.ctx, planID, "user:a"); len(list) != 0 {
		t.Fatalf("no finding should be admitted, got %d", len(list))
	}
}

func TestRetractFinding(t *testing.T) {
	h := findingSetup(t)
	_, planID, taskID := h.seedPlanWithAssignedTask(t)
	id, err := h.svc.RecordFinding(h.ctx, RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1", Kind: pm.FindingFact, Content: "x"})
	if err != nil {
		t.Fatal(err)
	}

	// a non-author non-owner member cannot retract.
	if err := h.svc.RetractFinding(h.ctx, id, "agent:ag2"); !errors.Is(err, pm.ErrFindingForbidden) {
		t.Fatalf("want ErrFindingForbidden (member non-owner), got %v", err)
	}
	// a non-member stranger cannot retract either (ErrMemberNotFound → forbidden).
	if err := h.svc.RetractFinding(h.ctx, id, "agent:stranger"); !errors.Is(err, pm.ErrFindingForbidden) {
		t.Fatalf("want ErrFindingForbidden (non-member), got %v", err)
	}
	// a malformed actor ref is rejected by validation.
	if err := h.svc.RetractFinding(h.ctx, id, "nope"); err == nil {
		t.Fatalf("want validation error for bad actor ref")
	}
	// the project owner (user:a, creator) can retract someone else's finding.
	if err := h.svc.RetractFinding(h.ctx, id, "user:a"); err != nil {
		t.Fatalf("owner retract: %v", err)
	}
	if list, _ := h.svc.ListPlanFindings(h.ctx, planID, "user:a"); len(list) != 0 {
		t.Fatalf("finding should be gone, got %d", len(list))
	}
	if got := h.outboxCount(t, EvtPlanFindingRetracted); got != 1 {
		t.Fatalf("want 1 retracted event, got %d", got)
	}
	// retracting a missing finding → ErrPlanFindingNotFound.
	if err := h.svc.RetractFinding(h.ctx, id, "user:a"); !errors.Is(err, pm.ErrPlanFindingNotFound) {
		t.Fatalf("want ErrPlanFindingNotFound, got %v", err)
	}
}

func TestRetractFinding_ByAuthor(t *testing.T) {
	h := findingSetup(t)
	_, planID, taskID := h.seedPlanWithAssignedTask(t)
	id, _ := h.svc.RecordFinding(h.ctx, RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1", Kind: pm.FindingFact, Content: "x"})
	if err := h.svc.RetractFinding(h.ctx, id, "agent:ag1"); err != nil {
		t.Fatalf("author retract: %v", err)
	}
}

func TestDeletePlan_CascadesFindings(t *testing.T) {
	h := findingSetup(t)
	_, planID, taskID := h.seedPlanWithAssignedTask(t)
	if _, err := h.svc.RecordFinding(h.ctx, RecordFindingCommand{PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1", Kind: pm.FindingFact, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	// stop the plan (running → draft) so it can be deleted.
	if err := h.svc.StopPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.DeletePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	if list, _ := h.svc.ListPlanFindings(h.ctx, planID, "user:a"); len(list) != 0 {
		t.Fatalf("findings should cascade-delete with the plan, got %d", len(list))
	}
}

func TestFindingServices_NilRepoGuards(t *testing.T) {
	ctx := context.Background()
	// No findings repo wired ⇒ every finding AppService fails loud.
	noFindings := New(Deps{})
	if _, err := noFindings.RecordFinding(ctx, RecordFindingCommand{PlanID: "p", TaskID: "t", AuthorRef: "agent:a", Kind: pm.FindingFact, Content: "x"}); !errors.Is(err, ErrFindingsUnavailable) {
		t.Fatalf("RecordFinding nil-findings: want ErrFindingsUnavailable, got %v", err)
	}
	if _, err := noFindings.ListPlanFindings(ctx, "p", "user:a"); !errors.Is(err, ErrFindingsUnavailable) {
		t.Fatalf("ListPlanFindings nil-findings: want ErrFindingsUnavailable, got %v", err)
	}
	if err := noFindings.RetractFinding(ctx, "f", "user:a"); !errors.Is(err, ErrFindingsUnavailable) {
		t.Fatalf("RetractFinding nil-findings: want ErrFindingsUnavailable, got %v", err)
	}
	// Findings wired but no Plans repo ⇒ RecordFinding fails loud on plans.
	findingsOnly := New(Deps{Findings: pmsql.NewPlanFindingRepo(nil)})
	if _, err := findingsOnly.RecordFinding(ctx, RecordFindingCommand{PlanID: "p", TaskID: "t", AuthorRef: "agent:a", Kind: pm.FindingFact, Content: "x"}); !errors.Is(err, ErrPlansUnavailable) {
		t.Fatalf("RecordFinding nil-plans: want ErrPlansUnavailable, got %v", err)
	}
}

func TestDispatchReadyNodes_InjectsFindings(t *testing.T) {
	h := findingSetup(t)
	_, planID, taskID := h.seedPlanWithAssignedTask(t)

	// record a finding BEFORE the node is dispatched, then advance.
	if _, err := h.svc.RecordFinding(h.ctx, RecordFindingCommand{
		PlanID: planID, TaskID: taskID, AuthorRef: "agent:ag1",
		Kind: pm.FindingFailure, Content: "the printer layer is a red herring",
	}); err != nil {
		t.Fatal(err)
	}
	dispatched, err := h.svc.AdvancePlan(h.ctx, planID, "user:a")
	if err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	if len(dispatched) != 1 {
		t.Fatalf("want 1 dispatched node, got %d", len(dispatched))
	}
	if len(h.dispatcher.calls) != 1 {
		t.Fatalf("want 1 dispatch call, got %d", len(h.dispatcher.calls))
	}
	content := h.dispatcher.calls[0].content
	if !strings.Contains(content, "Shared context") || !strings.Contains(content, "the printer layer is a red herring") {
		t.Fatalf("dispatch content missing injected finding:\n%s", content)
	}
}

func TestDispatchReadyNodes_NoFindings_NoInjection(t *testing.T) {
	h := findingSetup(t)
	_, planID, _ := h.seedPlanWithAssignedTask(t)
	if _, err := h.svc.AdvancePlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("AdvancePlan: %v", err)
	}
	if len(h.dispatcher.calls) != 1 {
		t.Fatalf("want 1 dispatch call, got %d", len(h.dispatcher.calls))
	}
	if strings.Contains(h.dispatcher.calls[0].content, "Shared context") {
		t.Fatalf("no findings → no shared-context block, got:\n%s", h.dispatcher.calls[0].content)
	}
}

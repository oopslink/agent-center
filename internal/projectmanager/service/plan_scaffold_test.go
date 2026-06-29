package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	convsql "github.com/oopslink/agent-center/internal/conversation/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// scaffoldSetup mirrors planSetup but ALSO wires the OrgSequence repo, so created
// tasks get a per-org T<n> number — needed to exercise the default-branch (=T<n>)
// resolution in ScaffoldCyclePlan.
func scaffoldSetup(t *testing.T) (*Service, *pmsql.PlanRepo, *pmsql.TaskRepo, *outbox.Relay, context.Context) {
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
	applied := outboxsql.NewAppliedRepo(db)
	convRepo := convsql.NewConversationRepo(db)
	plans := pmsql.NewPlanRepo(db)
	tasks := pmsql.NewTaskRepo(db)
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: tasks,
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Plans: plans, Outbox: ob, IDGen: gen, Clock: clk,
		OrgSeq: pmsql.NewOrgSequenceRepo(db),
	})
	taskProj := NewParticipantProjector(db, convRepo, applied, gen, clk)
	planProj := NewPlanParticipantProjector(db, convRepo, plans, applied, gen, clk)
	relay := outbox.NewRelay(ob, applied, clk, taskProj, planProj)
	return svc, plans, tasks, relay, context.Background()
}

// scaffoldByTitle indexes a plan's tasks by title for assertions.
func scaffoldByTitle(t *testing.T, tasks []*pm.Task) map[string]*pm.Task {
	t.Helper()
	m := map[string]*pm.Task{}
	for _, tk := range tasks {
		m[tk.Title()] = tk
	}
	return m
}

func TestScaffoldCyclePlan_BuildsGraphMetadataAndEdges(t *testing.T) {
	svc, plans, tasks, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID: pid,
		Version:   "v2.13.0",
		Features: []CycleFeature{
			{Name: "F1 规格", Branch: "f1-spec"}, // explicit branch, full control-flow chain
			{Name: "F9 文档", DocOnly: true},     // doc-only: Dev-only, skip merge check
		},
		MaxReviewRounds: 5, // non-default loopback bound (asserted on the loopback edge)
		CreatedBy:       "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan: %v", err)
	}
	drain(t, relay, ctx)

	// Plan is a draft.
	p, err := plans.FindByID(ctx, res.PlanID)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if p.Status() != pm.PlanDraft {
		t.Fatalf("plan status = %s, want draft", p.Status())
	}

	all, err := tasks.ListByPlan(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	// S0 + (F1: Dev+Review+Decision+Integrate+Escape) + (F9 doc-only: Dev) + Gate +
	// Accept + Ship = 10 (B2 control-flow chain).
	if len(all) != 10 {
		t.Fatalf("node count = %d, want 10: %v", len(all), titles(all))
	}
	if len(res.Nodes) != 10 {
		t.Fatalf("result node count = %d, want 10", len(res.Nodes))
	}
	byTitle := scaffoldByTitle(t, all)

	const (
		f1Dev    = "F1 规格 · Dev"
		f1Review = "F1 规格 · Review"
		f1Dec    = "F1 规格 · Decision（评审结论 pass/reject）"
		f1Integ  = "F1 规格 · Integrate"
		f1Esc    = "F1 规格 · 逃生/人工兜底（评审打回超限）"
		f9Dev    = "F9 文档 · Dev"
		s0Title  = "S0 开发主分支 — 切 dev/v2.13.0"
		gate     = "集成完成 Gate — PD 关门核对"
		accept   = "Accept 验收（集成后主干整体）"
		shipT    = "Ship v2.13.0"
	)

	// Every node is UNASSIGNED (structure-only — PD assigns owners next).
	for _, tk := range all {
		if tk.Assignee() != "" {
			t.Errorf("node %q assignee = %q, want empty (scaffold leaves owners blank)", tk.Title(), tk.Assignee())
		}
	}

	// S0: branch=dev/v2.13.0, base=main, role=s0.
	s0 := byTitle[s0Title]
	if s0 == nil {
		t.Fatalf("S0 node missing: %v", titles(all))
	}
	if s0.Branch() != "dev/v2.13.0" || s0.Base() != "main" {
		t.Errorf("S0 meta = branch:%q base:%q, want dev/v2.13.0 / main", s0.Branch(), s0.Base())
	}
	if s0.Role() != pm.CycleRoleS0 {
		t.Errorf("S0 role = %q, want s0", s0.Role())
	}

	// v2.13.0 I18/F3+B2: every node persists its cycle ROLE (the discriminator F3's
	// merge guard + F4's board key on). Decision/Escape are the new B2 control-flow
	// roles. Assert the per-title role mapping survived Save+ListByPlan.
	wantRole := map[string]pm.CycleNodeRole{
		f1Dev:    pm.CycleRoleDev,
		f1Review: pm.CycleRoleReview,
		f1Dec:    pm.CycleRoleDecision,
		f1Integ:  pm.CycleRoleIntegrate,
		f1Esc:    pm.CycleRoleEscape,
		f9Dev:    pm.CycleRoleDev,
		gate:     pm.CycleRoleGate,
		accept:   pm.CycleRoleAccept,
		shipT:    pm.CycleRoleShip,
	}
	for title, want := range wantRole {
		n := byTitle[title]
		if n == nil {
			t.Fatalf("missing node %q for role check: %v", title, titles(all))
		}
		if n.Role() != want {
			t.Errorf("%q role = %q, want %q", title, n.Role(), want)
		}
	}

	// F1 git-chain (Dev/Review/Decision/Integrate) shares the explicit branch f1-spec,
	// base dev/v2.13.0, no skip. (The Escape node is a human node — branch left empty.)
	for _, title := range []string{f1Dev, f1Review, f1Dec, f1Integ} {
		n := byTitle[title]
		if n == nil {
			t.Fatalf("missing node %q: %v", title, titles(all))
		}
		if n.Branch() != "f1-spec" || n.Base() != "dev/v2.13.0" {
			t.Errorf("%q meta = branch:%q base:%q, want f1-spec / dev/v2.13.0", title, n.Branch(), n.Base())
		}
		if n.SkipMergeCheck() {
			t.Errorf("%q skip_merge_check = true, want false", title)
		}
	}
	if esc := byTitle[f1Esc]; esc == nil || esc.Branch() != "" {
		t.Errorf("escape node branch = %q, want empty (human node, no git branch)", esc.Branch())
	}

	// F9 is doc-only: a single Dev node, no Review/Decision/Integrate/Escape,
	// skip_merge_check=true, and its branch defaulted to its own T<n>.
	if byTitle["F9 文档 · Review"] != nil || byTitle["F9 文档 · Integrate"] != nil ||
		byTitle["F9 文档 · Decision（评审结论 pass/reject）"] != nil {
		t.Errorf("doc-only feature should have no Review/Decision/Integrate node: %v", titles(all))
	}
	f9 := byTitle[f9Dev]
	if f9 == nil {
		t.Fatalf("F9 Dev node missing: %v", titles(all))
	}
	if !f9.SkipMergeCheck() {
		t.Errorf("doc-only Dev skip_merge_check = false, want true")
	}
	if !strings.HasPrefix(f9.Branch(), "T") {
		t.Errorf("doc-only Dev default branch = %q, want a T<n> default", f9.Branch())
	}

	// Ship: branch=trunk, base=main.
	ship := byTitle[shipT]
	if ship == nil {
		t.Fatalf("Ship node missing: %v", titles(all))
	}
	if ship.Branch() != "dev/v2.13.0" || ship.Base() != "main" {
		t.Errorf("Ship meta = branch:%q base:%q, want dev/v2.13.0 / main", ship.Branch(), ship.Base())
	}

	// Edges: index persisted edges by (from→to) → the full Dependency (kind/when/max).
	deps, err := plans.ListDependencies(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	id := func(title string) pm.TaskID {
		n := byTitle[title]
		if n == nil {
			t.Fatalf("no node %q", title)
		}
		return n.ID()
	}
	edge := map[string]pm.Dependency{}
	for _, e := range deps {
		edge[string(e.FromTaskID)+"->"+string(e.ToTaskID)] = e
	}
	// wantEdge: from-title, to-title, kind, when, maxRounds.
	type we struct {
		from, to, kind, when string
		max                  int
	}
	wantEdges := []we{
		{f1Dev, s0Title, "seq", "", 0},
		{f1Review, f1Dev, "seq", "", 0},
		{f1Dec, f1Review, "seq", "", 0},
		{f1Integ, f1Dec, "conditional", "pass", 0},           // pass → Integrate
		{f1Esc, f1Dec, "conditional", "reject_exhausted", 0}, // exhausted → Escape
		{f1Dec, f1Dev, "loopback", "reject", 5},              // reject → bounded loop (max=5)
		{f9Dev, s0Title, "seq", "", 0},
		{gate, f1Integ, "seq", "", 0}, // Gate terminal = Integrate (pass path)
		{gate, f9Dev, "seq", "", 0},   // doc-only terminal = its Dev
		{accept, gate, "seq", "", 0},
		{shipT, accept, "seq", "", 0},
	}
	for _, w := range wantEdges {
		key := string(id(w.from)) + "->" + string(id(w.to))
		e, ok := edge[key]
		if !ok {
			t.Errorf("missing edge: %q → %q", w.from, w.to)
			continue
		}
		if string(pm.NormalizeEdgeKind(e.Kind)) != w.kind || e.When != w.when || e.MaxRounds != w.max {
			t.Errorf("edge %q→%q = kind:%q when:%q max:%d, want kind:%q when:%q max:%d",
				w.from, w.to, pm.NormalizeEdgeKind(e.Kind), e.When, e.MaxRounds, w.kind, w.when, w.max)
		}
	}
	if len(deps) != len(wantEdges) {
		t.Errorf("edge count = %d, want %d", len(deps), len(wantEdges))
	}
	if len(res.Edges) != len(wantEdges) {
		t.Errorf("result edge count = %d, want %d", len(res.Edges), len(wantEdges))
	}
}

// T330: SkipMergeCheck=true marks every Integrate node skip_merge_check at build
// time so F3's Integrate-complete merge guard stands down for the whole cycle.
// (Default false is covered by TestScaffoldCyclePlan_BuildsGraphMetadataAndEdges,
// which asserts the Integrate node carries skip=false.)
// T601: a caller-supplied Title becomes the plan name; an empty Title falls back to
// the version-derived default (backward compatible).
func TestScaffoldCyclePlan_TitleNamesThePlan(t *testing.T) {
	svc, plans, _, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}

	// With Title → the plan is named exactly that (the feature it delivers).
	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID: pid, Version: "v2.13.0",
		Features:  []CycleFeature{{Name: "F1", DocOnly: true}},
		Title:     "auto-assign reconciler",
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan with title: %v", err)
	}
	drain(t, relay, ctx)
	p, err := plans.FindByID(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "auto-assign reconciler" {
		t.Fatalf("plan name = %q, want the supplied title", p.Name())
	}

	// Without Title → the version-derived default (existing behavior unchanged).
	res2, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID: pid, Version: "v2.14.0",
		Features:  []CycleFeature{{Name: "F1", DocOnly: true}},
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan without title: %v", err)
	}
	drain(t, relay, ctx)
	p2, err := plans.FindByID(ctx, res2.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Name() != "v2.14.0 — cycle 控制流图" {
		t.Fatalf("plan name = %q, want the version-derived default", p2.Name())
	}

	// Whitespace-only title is treated as empty → default (trimmed).
	res3, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID: pid, Version: "v2.15.0",
		Features:  []CycleFeature{{Name: "F1", DocOnly: true}},
		Title:     "   ",
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan blank title: %v", err)
	}
	drain(t, relay, ctx)
	p3, _ := plans.FindByID(ctx, res3.PlanID)
	if p3.Name() != "v2.15.0 — cycle 控制流图" {
		t.Fatalf("blank-title plan name = %q, want the version-derived default", p3.Name())
	}
}

func TestScaffoldCyclePlan_SkipMergeCheckFlagsIntegrate(t *testing.T) {
	svc, _, tasks, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID:      pid,
		Version:        "v2.14.0",
		Features:       []CycleFeature{{Name: "F1 规格", Branch: "f1-spec"}},
		SkipMergeCheck: true,
		CreatedBy:      "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan: %v", err)
	}
	drain(t, relay, ctx)

	all, err := tasks.ListByPlan(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	integ := scaffoldByTitle(t, all)["F1 规格 · Integrate"]
	if integ == nil {
		t.Fatalf("Integrate node missing: %v", titles(all))
	}
	if !integ.SkipMergeCheck() {
		t.Errorf("Integrate skip_merge_check = false, want true (SkipMergeCheck cmd flag)")
	}
}

func TestScaffoldCyclePlan_ValidatesInput(t *testing.T) {
	svc, _, _, _, _, ctx := planSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		cmd  ScaffoldCyclePlanCommand
		want error
	}{
		{"empty version", ScaffoldCyclePlanCommand{ProjectID: pid, Version: "  ", Features: []CycleFeature{{Name: "F1"}}, CreatedBy: "user:pd"}, ErrScaffoldVersionRequired},
		{"no features", ScaffoldCyclePlanCommand{ProjectID: pid, Version: "v1.0.0", CreatedBy: "user:pd"}, ErrScaffoldNoFeatures},
		{"blank feature name", ScaffoldCyclePlanCommand{ProjectID: pid, Version: "v1.0.0", Features: []CycleFeature{{Name: " "}}, CreatedBy: "user:pd"}, ErrScaffoldFeatureNameRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ScaffoldCyclePlan(ctx, tc.cmd); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func titles(tasks []*pm.Task) []string {
	out := make([]string, 0, len(tasks))
	for _, tk := range tasks {
		out = append(out, tk.Title())
	}
	return out
}

// TestScaffoldCyclePlan_SourceIssueLinksEveryNodeAtCreate is T462's core acceptance:
// a scaffold with SourceIssue links EVERY generated node to that issue as
// derived_from_issue at create — so each node's owner can get_issue the spec (the
// get_issue derive-gate is satisfied) WITHOUT the PD set_task_issue-ing each node.
func TestScaffoldCyclePlan_SourceIssueLinksEveryNodeAtCreate(t *testing.T) {
	svc, _, tasks, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "cycle spec", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID:   pid,
		Version:     "v2.13.0",
		SourceIssue: iid,
		Features: []CycleFeature{
			{Name: "F1 规格", Branch: "f1-spec"},
			{Name: "F9 文档", DocOnly: true},
		},
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan: %v", err)
	}
	drain(t, relay, ctx)

	all, err := tasks.ListByPlan(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 10 {
		t.Fatalf("node count = %d, want 10", len(all))
	}
	// Every persisted node carries the source issue (the gate-satisfying link).
	for _, tk := range all {
		if tk.DerivedFromIssue() != iid {
			t.Errorf("node %q derived_from_issue = %q, want %q", tk.Title(), tk.DerivedFromIssue(), iid)
		}
	}
	// The returned summary reflects it too (self-evidencing for the tool caller).
	for _, n := range res.Nodes {
		if n.DerivedFromIssue != iid {
			t.Errorf("summary node %q derived_from_issue = %q, want %q", n.Title, n.DerivedFromIssue, iid)
		}
	}
}

// TestScaffoldCyclePlan_FeatureIssueOverridesPlanSource: a per-feature Issue overrides
// the plan-level SourceIssue for THAT feature's chain nodes, while the shared
// S0/Gate/Accept/Ship nodes (and other features) keep the plan-level source (T462).
func TestScaffoldCyclePlan_FeatureIssueOverridesPlanSource(t *testing.T) {
	svc, _, tasks, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	planIss, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "cycle spec", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	featIss, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "F1 spec", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID:   pid,
		Version:     "v2.13.0",
		SourceIssue: planIss,
		Features: []CycleFeature{
			{Name: "F1 规格", Branch: "f1-spec", Issue: featIss}, // override
			{Name: "F2 其它", Branch: "f2"},                      // inherits planIss
		},
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan: %v", err)
	}
	drain(t, relay, ctx)

	all, err := tasks.ListByPlan(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := scaffoldByTitle(t, all)
	// F1's chain nodes derive from featIss.
	for _, title := range []string{"F1 规格 · Dev", "F1 规格 · Review", "F1 规格 · Decision（评审结论 pass/reject）", "F1 规格 · Integrate", "F1 规格 · 逃生/人工兜底（评审打回超限）"} {
		if got := byTitle[title]; got == nil || got.DerivedFromIssue() != featIss {
			t.Errorf("node %q derived = %q, want feature override %q", title, derivedOf(got), featIss)
		}
	}
	// F2's Dev + the shared S0/Gate/Accept/Ship derive from the plan-level source.
	for _, title := range []string{"F2 其它 · Dev", "S0 开发主分支 — 切 dev/v2.13.0", "集成完成 Gate — PD 关门核对", "Accept 验收（集成后主干整体）", "Ship v2.13.0"} {
		if got := byTitle[title]; got == nil || got.DerivedFromIssue() != planIss {
			t.Errorf("node %q derived = %q, want plan source %q", title, derivedOf(got), planIss)
		}
	}
}

// TestScaffoldCyclePlan_NoSourceIssueNoLink pins the non-regression: omitting the
// source issue leaves every node UNLINKED, exactly as before T462.
func TestScaffoldCyclePlan_NoSourceIssueNoLink(t *testing.T) {
	svc, _, tasks, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID: pid, Version: "v2.13.0",
		Features:  []CycleFeature{{Name: "F1", Branch: "f1"}},
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan: %v", err)
	}
	drain(t, relay, ctx)
	all, err := tasks.ListByPlan(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range all {
		if tk.DerivedFromIssue() != "" {
			t.Errorf("node %q derived_from_issue = %q, want empty (no source issue → no link)", tk.Title(), tk.DerivedFromIssue())
		}
	}
}

// TestScaffoldCyclePlan_BadSourceIssueRejectedBeforeAnyNode: an unknown or
// cross-project source issue fails validation UP-FRONT (no nodes/plan minted) so a
// bad ref can never produce dangling-linked nodes (T462). derived_from_issue is
// immutable after create, making create-time validation the only safe gate.
func TestScaffoldCyclePlan_BadSourceIssueRejectedBeforeAnyNode(t *testing.T) {
	svc, plans, _, _, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	// An issue in ANOTHER project — exists, but wrong project.
	otherPid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "Q", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	otherIss, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: otherPid, Title: "x", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		cmd  ScaffoldCyclePlanCommand
		want error
	}{
		{
			name: "unknown plan source issue",
			cmd: ScaffoldCyclePlanCommand{ProjectID: pid, Version: "v1", SourceIssue: "issue-does-not-exist",
				Features: []CycleFeature{{Name: "F1"}}, CreatedBy: "user:pd"},
			want: pm.ErrIssueNotFound,
		},
		{
			name: "cross-project plan source issue",
			cmd: ScaffoldCyclePlanCommand{ProjectID: pid, Version: "v1", SourceIssue: otherIss,
				Features: []CycleFeature{{Name: "F1"}}, CreatedBy: "user:pd"},
			want: pm.ErrDerivedIssueProjectMismatch,
		},
		{
			name: "cross-project feature issue override",
			cmd: ScaffoldCyclePlanCommand{ProjectID: pid, Version: "v1",
				Features: []CycleFeature{{Name: "F1", Issue: otherIss}}, CreatedBy: "user:pd"},
			want: pm.ErrDerivedIssueProjectMismatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, err := plans.ListByProject(ctx, pid)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ScaffoldCyclePlan(ctx, tc.cmd); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			after, err := plans.ListByProject(ctx, pid)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(before) {
				t.Fatalf("a rejected scaffold must mint NO plan: before=%d after=%d", len(before), len(after))
			}
		})
	}
}

func derivedOf(t *pm.Task) pm.IssueID {
	if t == nil {
		return "<nil>"
	}
	return t.DerivedFromIssue()
}

// TestScaffoldCyclePlan_SpecLandsOnDevVerbatimWithPointers pins T466: a feature's
// spec is written VERBATIM as its Dev node's description, and the Review/Decision/
// Integrate nodes get a non-empty pointer back to it (+ the source issue). A feature
// WITHOUT a spec keeps empty descriptions on every node (no regression).
func TestScaffoldCyclePlan_SpecLandsOnDevVerbatimWithPointers(t *testing.T) {
	svc, _, tasks, relay, ctx := scaffoldSetup(t)
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "spec", CreatedBy: "user:pd"})
	if err != nil {
		t.Fatal(err)
	}
	const specText = "## F1 验收\n- 列表分页\n- 空态文案\n- a11y 焦点环"

	res, err := svc.ScaffoldCyclePlan(ctx, ScaffoldCyclePlanCommand{
		ProjectID:   pid,
		Version:     "v2.13.0",
		SourceIssue: iid,
		Features: []CycleFeature{
			{Name: "F1 列表", Branch: "f1", Spec: specText},
			{Name: "F2 无规格", Branch: "f2"}, // no spec → descriptions stay empty
		},
		CreatedBy: "user:pd",
	})
	if err != nil {
		t.Fatalf("ScaffoldCyclePlan: %v", err)
	}
	drain(t, relay, ctx)

	all, err := tasks.ListByPlan(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	byTitle := scaffoldByTitle(t, all)

	// F1 Dev description == spec VERBATIM.
	if dev := byTitle["F1 列表 · Dev"]; dev == nil || dev.Description() != specText {
		t.Fatalf("F1 Dev description = %q, want verbatim spec %q", descOf(byTitle["F1 列表 · Dev"]), specText)
	}
	// F1 Review/Decision/Integrate get a non-empty pointer mentioning the Dev node +
	// the source issue.
	for _, title := range []string{"F1 列表 · Review", "F1 列表 · Decision（评审结论 pass/reject）", "F1 列表 · Integrate"} {
		n := byTitle[title]
		if n == nil || n.Description() == "" {
			t.Fatalf("%q description empty, want a spec pointer", title)
		}
		if !strings.Contains(n.Description(), "Dev 节点") || !strings.Contains(n.Description(), string(iid)) {
			t.Errorf("%q pointer = %q, want mention of Dev 节点 + %s", title, n.Description(), iid)
		}
	}
	// F2 (no spec): every node description stays empty — no regression.
	for _, title := range []string{"F2 无规格 · Dev", "F2 无规格 · Review", "F2 无规格 · Decision（评审结论 pass/reject）", "F2 无规格 · Integrate"} {
		if n := byTitle[title]; n == nil || n.Description() != "" {
			t.Errorf("%q description = %q, want empty (no spec)", title, descOf(byTitle[title]))
		}
	}
	// Static nodes (S0/Gate/Accept/Ship) never get descriptions.
	for _, title := range []string{"S0 开发主分支 — 切 dev/v2.13.0", "集成完成 Gate — PD 关门核对", "Accept 验收（集成后主干整体）", "Ship v2.13.0"} {
		if n := byTitle[title]; n == nil || n.Description() != "" {
			t.Errorf("static node %q description = %q, want empty", title, descOf(byTitle[title]))
		}
	}
	// Summary reflects the Dev spec for the tool caller.
	var sawDevSpec bool
	for _, n := range res.Nodes {
		if n.Title == "F1 列表 · Dev" && n.Description == specText {
			sawDevSpec = true
		}
	}
	if !sawDevSpec {
		t.Error("result summary missing F1 Dev description == spec")
	}
}

func descOf(t *pm.Task) string {
	if t == nil {
		return "<nil>"
	}
	return t.Description()
}

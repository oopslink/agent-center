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
			{Name: "F1 规格", Branch: "f1-spec"}, // explicit branch, full chain
			{Name: "F9 文档", DocOnly: true},     // doc-only: Dev-only, skip merge check
		},
		CreatedBy: "user:pd",
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
	// S0 + (F1: Dev+Review+Integrate) + (F9 doc-only: Dev) + Gate + Accept + Ship = 8.
	if len(all) != 8 {
		t.Fatalf("node count = %d, want 8: %v", len(all), titles(all))
	}
	if len(res.Nodes) != 8 {
		t.Fatalf("result node count = %d, want 8", len(res.Nodes))
	}
	byTitle := scaffoldByTitle(t, all)

	// Every node is UNASSIGNED (structure-only — PD assigns owners next).
	for _, tk := range all {
		if tk.Assignee() != "" {
			t.Errorf("node %q assignee = %q, want empty (scaffold leaves owners blank)", tk.Title(), tk.Assignee())
		}
	}

	// S0: branch=dev/v2.13.0, base=main, role=s0.
	s0 := byTitle["S0 开发主分支 — 切 dev/v2.13.0"]
	if s0 == nil {
		t.Fatalf("S0 node missing: %v", titles(all))
	}
	if s0.Branch() != "dev/v2.13.0" || s0.Base() != "main" {
		t.Errorf("S0 meta = branch:%q base:%q, want dev/v2.13.0 / main", s0.Branch(), s0.Base())
	}
	if s0.Role() != pm.CycleRoleS0 {
		t.Errorf("S0 role = %q, want s0", s0.Role())
	}

	// v2.13.0 I18/F3: every node persists its cycle ROLE (the discriminator F3's
	// merge guard + F4's board key on). Assert the per-title role mapping survived
	// Save+ListByPlan (and, for the doc-only Dev, the resolveDefaultBranch re-stamp).
	wantRole := map[string]pm.CycleNodeRole{
		"F1 规格 · Dev":       pm.CycleRoleDev,
		"F1 规格 · Review":    pm.CycleRoleReview,
		"F1 规格 · Integrate": pm.CycleRoleIntegrate,
		"F9 文档 · Dev":       pm.CycleRoleDev,
		"集成完成 Gate — PD 关门核对": pm.CycleRoleGate,
		"Accept 验收（集成后主干整体）":  pm.CycleRoleAccept,
		"Ship v2.13.0":       pm.CycleRoleShip,
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

	// F1 chain shares the explicit branch f1-spec, base dev/v2.13.0, no skip.
	for _, title := range []string{"F1 规格 · Dev", "F1 规格 · Review", "F1 规格 · Integrate"} {
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

	// F9 is doc-only: a single Dev node, no Review/Integrate, skip_merge_check=true,
	// and its branch defaulted to its own T<n>.
	if byTitle["F9 文档 · Review"] != nil || byTitle["F9 文档 · Integrate"] != nil {
		t.Errorf("doc-only feature should have no Review/Integrate node: %v", titles(all))
	}
	f9dev := byTitle["F9 文档 · Dev"]
	if f9dev == nil {
		t.Fatalf("F9 Dev node missing: %v", titles(all))
	}
	if !f9dev.SkipMergeCheck() {
		t.Errorf("doc-only Dev skip_merge_check = false, want true")
	}
	if !strings.HasPrefix(f9dev.Branch(), "T") {
		t.Errorf("doc-only Dev default branch = %q, want a T<n> default", f9dev.Branch())
	}

	// Ship: branch=trunk, base=main.
	ship := byTitle["Ship v2.13.0"]
	if ship == nil {
		t.Fatalf("Ship node missing: %v", titles(all))
	}
	if ship.Branch() != "dev/v2.13.0" || ship.Base() != "main" {
		t.Errorf("Ship meta = branch:%q base:%q, want dev/v2.13.0 / main", ship.Branch(), ship.Base())
	}

	// Edges: build a depends_on set keyed by (from→to) task ids.
	deps, err := plans.ListDependencies(ctx, res.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	edge := map[string]bool{}
	for _, e := range deps {
		edge[string(e.FromTaskID)+"->"+string(e.ToTaskID)] = true
	}
	id := func(title string) pm.TaskID {
		n := byTitle[title]
		if n == nil {
			t.Fatalf("no node %q", title)
		}
		return n.ID()
	}
	wantEdges := [][2]string{
		{"F1 规格 · Dev", "S0 开发主分支 — 切 dev/v2.13.0"},
		{"F1 规格 · Review", "F1 规格 · Dev"},
		{"F1 规格 · Integrate", "F1 规格 · Review"},
		{"F9 文档 · Dev", "S0 开发主分支 — 切 dev/v2.13.0"},
		{"集成完成 Gate — PD 关门核对", "F1 规格 · Integrate"},
		{"集成完成 Gate — PD 关门核对", "F9 文档 · Dev"}, // doc-only terminal = its Dev
		{"Accept 验收（集成后主干整体）", "集成完成 Gate — PD 关门核对"},
		{"Ship v2.13.0", "Accept 验收（集成后主干整体）"},
	}
	for _, e := range wantEdges {
		key := string(id(e[0])) + "->" + string(id(e[1]))
		if !edge[key] {
			t.Errorf("missing depends_on edge: %q → %q", e[0], e[1])
		}
	}
	if len(deps) != len(wantEdges) {
		t.Errorf("edge count = %d, want %d", len(deps), len(wantEdges))
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

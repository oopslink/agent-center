package team_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/team"
)

func sampleTemplate(t *testing.T, curated bool) *team.TeamTemplate {
	t.Helper()
	tmpl, err := team.NewTemplate(team.NewTemplateInput{
		ID:    "tmpl-1",
		OrgID: "org-1",
		Name:  "backend squad",
		Roles: []team.RoleSlot{
			{Config: team.RoleConfig{Role: "pd", CLI: "claude-code", Model: "opus"}, Count: 1},
			{Config: team.RoleConfig{Role: "dev", CLI: "claude-code", Model: "sonnet"}, Count: 3},
			{Config: team.RoleConfig{Role: "review", CLI: "claude-code", Model: "opus"}, Count: 1},
		},
		WorkflowTemplateRef: "wf-dev-review-ship",
		Experiences: []team.Experience{
			{Slug: "prefer-tdd", Description: "write tests first", Scope: team.ExpScopeTeam},
			{Slug: "global-style", Description: "gofmt everything", Scope: team.ExpScopeGlobal},
		},
		Curated:   curated,
		CreatedAt: time.Unix(1000, 0),
	})
	if err != nil {
		t.Fatalf("NewTemplate: %v", err)
	}
	return tmpl
}

func TestNewTemplate_ValidatesAndDefaults(t *testing.T) {
	tmpl := sampleTemplate(t, false)
	if tmpl.Version != 1 {
		t.Errorf("version = %d want 1", tmpl.Version)
	}
	// Count default and concurrency default applied.
	for _, sl := range tmpl.Roles {
		if sl.Count < 1 || sl.Config.MaxConcurrency < 1 {
			t.Errorf("role %s: count=%d conc=%d, both must default >=1", sl.Config.Role, sl.Count, sl.Config.MaxConcurrency)
		}
	}

	if _, err := team.NewTemplate(team.NewTemplateInput{ID: "x", Name: ""}); !errors.Is(err, team.ErrInvalidTemplate) {
		t.Errorf("empty name should be ErrInvalidTemplate, got %v", err)
	}
	if _, err := team.NewTemplate(team.NewTemplateInput{
		ID: "x", Name: "n",
		Roles: []team.RoleSlot{{Config: team.RoleConfig{Role: "dev"}}, {Config: team.RoleConfig{Role: "dev"}}},
	}); !errors.Is(err, team.ErrInvalidRole) {
		t.Errorf("duplicate role should be ErrInvalidRole, got %v", err)
	}
}

func TestExtractFromTeam_DropsProjectScopeAndScrubs(t *testing.T) {
	tm, err := team.NewTeam(team.NewTeamInput{
		ID: "team-1", OrgID: "org-1", Name: "squad",
		Roles: []team.RoleConfig{{Role: "dev", CLI: "claude-code"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := team.TeamSnapshot{
		Team:                tm,
		WorkflowTemplateRef: "wf-1",
		Counts:              map[string]int{"dev": 2},
		Experiences: []team.Experience{
			{Slug: "portable", Description: "always write table-driven tests", Scope: team.ExpScopeTeam},
			{Slug: "leaky", Description: "the fix for T950 in internal/team/foo.go", Scope: team.ExpScopeTeam},
			{Slug: "secret", Description: "project-only fact about repo layout", Scope: team.ExpScopeProject},
		},
	}
	res, err := team.ExtractFromTeam(snap, "tmpl-x", nil, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if res.DroppedProject != 1 {
		t.Errorf("DroppedProject = %d want 1", res.DroppedProject)
	}
	if len(res.Draft.Experiences) != 2 {
		t.Errorf("kept experiences = %d want 2", len(res.Draft.Experiences))
	}
	if res.Draft.Curated {
		t.Error("extracted draft must be Curated=false")
	}
	// role count carried from Counts override.
	if len(res.Draft.Roles) != 1 || res.Draft.Roles[0].Count != 2 {
		t.Errorf("role slot = %+v, want dev count 2", res.Draft.Roles)
	}
	// scrub must flag the T950 code name and the path in the "leaky" experience.
	var sawCode, sawPath bool
	for _, f := range res.ScrubFindings {
		if f.Kind == team.ScrubCodeName && f.Token == "T950" {
			sawCode = true
		}
		if f.Kind == team.ScrubPath && strings.Contains(f.Token, "internal/team/foo.go") {
			sawPath = true
		}
	}
	if !sawCode {
		t.Errorf("scrub did not flag T950: %+v", res.ScrubFindings)
	}
	if !sawPath {
		t.Errorf("scrub did not flag the path: %+v", res.ScrubFindings)
	}
}

func TestExportImport_RoundTripCrossOrg(t *testing.T) {
	tmpl := sampleTemplate(t, true)
	data, err := team.ExportTemplate(tmpl)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	imported, err := team.ImportTemplate(data, team.ImportTemplateInput{
		OrgID: "org-2", NewID: "tmpl-new", Now: time.Unix(2000, 0),
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if imported.OrgID != "org-2" {
		t.Errorf("imported org = %q want org-2 (re-homed)", imported.OrgID)
	}
	if imported.ID != "tmpl-new" {
		t.Errorf("imported id = %q want fresh tmpl-new", imported.ID)
	}
	if imported.Curated {
		t.Error("imported template must land Curated=false for re-review")
	}
	if len(imported.Roles) != len(tmpl.Roles) || len(imported.Experiences) != len(tmpl.Experiences) {
		t.Errorf("round trip lost content: roles %d exp %d", len(imported.Roles), len(imported.Experiences))
	}
	if imported.WorkflowTemplateRef != tmpl.WorkflowTemplateRef {
		t.Errorf("workflow ref lost: %q", imported.WorkflowTemplateRef)
	}
}

func TestExportTemplate_RequiresCuration(t *testing.T) {
	tmpl := sampleTemplate(t, false)
	if _, err := team.ExportTemplate(tmpl); !errors.Is(err, team.ErrTemplateNotCurated) {
		t.Fatalf("export of uncurated template must fail with ErrTemplateNotCurated, got %v", err)
	}
}

func TestImportTemplate_RejectsBadFormatAndDropsProjectScope(t *testing.T) {
	if _, err := team.ImportTemplate([]byte(`{"format":"bogus"}`), team.ImportTemplateInput{OrgID: "o", NewID: "i"}); !errors.Is(err, team.ErrInvalidTemplate) {
		t.Errorf("bad format should be ErrInvalidTemplate, got %v", err)
	}
	if _, err := team.ImportTemplate([]byte(`not json`), team.ImportTemplateInput{OrgID: "o", NewID: "i"}); !errors.Is(err, team.ErrInvalidTemplate) {
		t.Errorf("bad json should be ErrInvalidTemplate, got %v", err)
	}
	// craft a curated template with a project-scope experience sneaked in via
	// direct construction, export, then confirm import defensively drops it.
	tmpl := sampleTemplate(t, true)
	tmpl.Experiences = append(tmpl.Experiences, team.Experience{Slug: "leak", Scope: team.ExpScopeProject})
	data, err := team.ExportTemplate(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := team.ImportTemplate(data, team.ImportTemplateInput{OrgID: "o2", NewID: "i2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range imported.Experiences {
		if e.Scope == team.ExpScopeProject {
			t.Errorf("import must drop project-scope experience, found %q", e.Slug)
		}
	}
}

func TestScrubText_Kinds(t *testing.T) {
	findings := team.ScrubText("s", "see T950 and PROJ-42 at https://git.internal/agent-center and /etc/passwd plus internal/team/x.go")
	kinds := map[team.ScrubKind]bool{}
	for _, f := range findings {
		kinds[f.Kind] = true
	}
	for _, want := range []team.ScrubKind{team.ScrubCodeName, team.ScrubURL, team.ScrubPath} {
		if !kinds[want] {
			t.Errorf("missing scrub kind %q in %+v", want, findings)
		}
	}
	// common hyphenated words should not be flagged as repo names.
	clean := team.ScrubText("s", "prefer table-driven tests with round-robin scheduling")
	for _, f := range clean {
		if f.Kind == team.ScrubRepoName {
			t.Errorf("common word wrongly flagged as repo name: %q", f.Token)
		}
	}
}

type fakeMinter struct{ n int }

func (m *fakeMinter) NewEntityID(kind string) string {
	m.n++
	return kind + "-" + string(rune('a'+m.n-1))
}

func TestPlanInstantiation_BuildsIdentitiesConfigMembersAndRuntimeStep(t *testing.T) {
	tmpl := sampleTemplate(t, true)
	inst, rt, err := team.PlanInstantiation(team.InstantiateInput{
		Template:  tmpl,
		OrgID:     "org-1",
		ProjectID: "proj-9",
		TeamName:  "backend @ proj-9",
		Minter:    &fakeMinter{},
		Now:       time.Unix(5, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 1 pd + 3 dev + 1 review = 5 agents/members/enrollments.
	if len(inst.Agents) != 5 || len(inst.Members) != 5 || len(rt.Enrollments) != 5 {
		t.Fatalf("counts: agents=%d members=%d enroll=%d want 5/5/5", len(inst.Agents), len(inst.Members), len(rt.Enrollments))
	}
	if inst.Team == nil || inst.Team.Name() != "backend @ proj-9" {
		t.Errorf("team not built with given name")
	}
	if inst.WorkflowTemplateRef != "wf-dev-review-ship" {
		t.Errorf("workflow not bound: %q", inst.WorkflowTemplateRef)
	}
	// memory seed carries the portable experiences.
	if len(inst.MemorySeed) != 2 {
		t.Errorf("memory seed = %d want 2 portable experiences", len(inst.MemorySeed))
	}
	// every member ref maps to a created agent identity under a declared role.
	for _, m := range inst.Members {
		if !strings.HasPrefix(m.Ref.String(), "agent:") {
			t.Errorf("member ref %q not an agent ref", m.Ref)
		}
		if !inst.Team.HasRole(m.Role) {
			t.Errorf("member role %q not declared on team", m.Role)
		}
	}
	// runtime enrollment carries cli/model for auth provisioning.
	for _, e := range rt.Enrollments {
		if e.CLI == "" {
			t.Errorf("enroll %q missing cli for runtime provisioning", e.AgentID)
		}
	}
}

func TestPlanInstantiation_RequiresProject(t *testing.T) {
	tmpl := sampleTemplate(t, true)
	if _, _, err := team.PlanInstantiation(team.InstantiateInput{Template: tmpl, Minter: &fakeMinter{}}); !errors.Is(err, team.ErrInstantiateNeedsProject) {
		t.Fatalf("missing project should be ErrInstantiateNeedsProject, got %v", err)
	}
}

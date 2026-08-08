package migration

import (
	"context"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	"github.com/oopslink/agent-center/internal/team"
)

func TestPlanLegacyWorkflowTemplateMigrationClaimsOnlyUniqueTeamOwners(t *testing.T) {
	plan := PlanLegacyWorkflowTemplateMigration([]LegacyWorkflowTemplate{
		{ID: "tmpl-1", OrgID: "org-1", Name: "Main workflow", Content: "plan it", CreatedBy: "agent:a"},
		{ID: "tmpl-2", OrgID: "org-1", Name: "Missing owner", Content: "x", CreatedBy: "agent:missing"},
		{ID: "tmpl-3", OrgID: "org-1", Name: "Human owner", Content: "x", CreatedBy: "user:u"},
		{ID: "tmpl-4", OrgID: "org-1", Name: "Ambiguous", Content: "x", CreatedBy: "agent:ambig"},
		{ID: "tmpl-5", OrgID: "org-1", Name: "Builtin", Content: "x", CreatedBy: "agent:a", Builtin: true},
	}, []TeamOwnership{
		{OrgID: "org-1", TeamID: "team-a", AgentRefs: []team.MemberRef{"agent:a"}},
		{OrgID: "org-1", TeamID: "team-b", AgentRefs: []team.MemberRef{"agent:b"}},
		{OrgID: "org-1", TeamID: "team-c", AgentRefs: []team.MemberRef{"agent:ambig"}},
		{OrgID: "org-1", TeamID: "team-d", AgentRefs: []team.MemberRef{"agent:ambig"}},
		{OrgID: "org-2", TeamID: "team-other-org", AgentRefs: []team.MemberRef{"agent:a"}},
	})

	if len(plan.Claims) != 1 {
		t.Fatalf("claims=%+v want exactly one uniquely-owned template", plan.Claims)
	}
	claim := plan.Claims[0]
	if claim.Template.ID != "tmpl-1" || claim.TeamID != "team-a" {
		t.Fatalf("wrong claim: %+v", claim)
	}
	if !claim.Rule.Enabled || len(claim.Rule.AppliesTo) != 1 || claim.Rule.AppliesTo[0] != "plan" {
		t.Fatalf("legacy workflow template should become an enabled plan rule: %+v", claim.Rule)
	}
	if len(plan.Unclaimed) != 4 {
		t.Fatalf("unclaimed=%+v want 4", plan.Unclaimed)
	}
	for _, u := range plan.Unclaimed {
		if strings.TrimSpace(u.Reason) == "" {
			t.Fatalf("unclaimed record missing reason: %+v", u)
		}
	}
}

func TestApplyLegacyWorkflowTemplateMigrationWritesOnlyClaimedTeamRules(t *testing.T) {
	ctx := context.Background()
	host := centergit.NewHost(t.TempDir(), nil)
	plan := PlanLegacyWorkflowTemplateMigration([]LegacyWorkflowTemplate{
		{ID: "tmpl-1", OrgID: "org-1", Name: "Main workflow", Description: "how to plan", Content: "Use a DAG.", CreatedBy: "agent:a"},
	}, []TeamOwnership{
		{OrgID: "org-1", TeamID: "team-a", AgentRefs: []team.MemberRef{"agent:a"}},
		{OrgID: "org-1", TeamID: "team-b", AgentRefs: []team.MemberRef{"agent:b"}},
	})

	applied, err := ApplyLegacyWorkflowTemplateMigration(ctx, host, nil, plan)
	if err != nil {
		t.Fatalf("ApplyLegacyWorkflowTemplateMigration: %v", err)
	}
	if len(applied) != 1 || applied[0].TeamID != "team-a" || applied[0].Path == "" || applied[0].Commit == "" {
		t.Fatalf("applied=%+v want one team-a rule with path+commit", applied)
	}
	if ok, err := host.RepoExists(centergit.TeamRepo("team-b")); err != nil || ok {
		t.Fatalf("unclaimed/unrelated team repo should not be provisioned/broadcast to: ok=%v err=%v", ok, err)
	}

	snap, err := centergit.NewTeamMemoryConsumer(host, nil).ReadTeamRules(ctx, "team-a", "plan")
	if err != nil {
		t.Fatalf("ReadTeamRules: %v", err)
	}
	if len(snap.Rules) != 1 || snap.Rules[0].Body != "Use a DAG." || snap.Rules[0].SourcePath != applied[0].Path {
		t.Fatalf("rules snapshot=%+v applied=%+v", snap.Rules, applied)
	}
	notes := RollbackNotes(applied)
	for _, want := range []string{applied[0].Path, applied[0].Commit, "pm_templates rows were not deleted"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("rollback notes missing %q:\n%s", want, notes)
		}
	}
}

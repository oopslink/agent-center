package api

import (
	"context"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func (s *Server) loadPlanRulesForAgent(ctx context.Context, d HandlerDeps, a *agent.Agent) *pmservice.RuleSnapshot {
	if a == nil {
		return unavailablePlanRules("", "agent unavailable")
	}
	if d.TeamSvc == nil {
		return unavailablePlanRules("", "team service is not wired")
	}
	teamID, hasTeam, err := d.TeamSvc.FindAgentTeam(ctx, operatingAgentMemberRef(a))
	if err != nil {
		return unavailablePlanRules("", err.Error())
	}
	if !hasTeam {
		return emptyPlanRules("", pmservice.PlanRuleSourceTeamMemory)
	}
	t, err := d.TeamSvc.GetTeam(ctx, teamID)
	if err != nil {
		return unavailablePlanRules("", err.Error())
	}
	if t.OrgID() != string(a.OrganizationID()) {
		// Treat a cross-org membership row as no readable team. Do not leak the
		// foreign team id, commit, or rule body into the caller's org.
		return emptyPlanRules("", pmservice.PlanRuleSourceTeamMemory)
	}
	if d.TeamGitHost == nil {
		return emptyPlanRules(teamID.String(), pmservice.PlanRuleSourceTeamMemory)
	}
	snap, err := centergit.NewTeamMemoryConsumer(d.TeamGitHost, nil).ReadTeamRules(ctx, teamID.String(), "plan")
	if err != nil {
		return unavailablePlanRules(teamID.String(), err.Error())
	}
	return planRulesFromTeamSnapshot(snap)
}

func planRulesFromTeamSnapshot(snap centergit.RuleSnapshot) *pmservice.RuleSnapshot {
	out := &pmservice.RuleSnapshot{
		TeamID:           strings.TrimSpace(snap.TeamID),
		Phase:            "plan",
		Commit:           strings.TrimSpace(snap.Commit),
		Source:           pmservice.PlanRuleSourceTeamMemory,
		Skipped:          append([]string(nil), snap.Skipped...),
		RefreshSemantics: pmservice.PlanRuleRefreshSemantics,
	}
	for _, r := range snap.Rules {
		out.Rules = append(out.Rules, pmservice.RuleContext{
			Slug:        strings.TrimSpace(r.Slug),
			Title:       strings.TrimSpace(r.Title),
			Description: strings.TrimSpace(r.Description),
			Body:        r.Body,
			Enabled:     r.Enabled,
			AppliesTo:   append([]string(nil), r.AppliesTo...),
			SourcePath:  strings.TrimSpace(r.SourcePath),
		})
	}
	return pmservice.NormalizePlanRuleSnapshot(out, pmservice.PlanRuleSourceTeamMemory)
}

func unavailablePlanRules(teamID, loadErr string) *pmservice.RuleSnapshot {
	return pmservice.NormalizePlanRuleSnapshot(&pmservice.RuleSnapshot{
		TeamID:           strings.TrimSpace(teamID),
		Phase:            "plan",
		Source:           pmservice.PlanRuleSourceUnavailable,
		LoadError:        strings.TrimSpace(loadErr),
		RefreshSemantics: pmservice.PlanRuleRefreshSemantics,
	}, pmservice.PlanRuleSourceUnavailable)
}

func emptyPlanRules(teamID, source string) *pmservice.RuleSnapshot {
	return pmservice.NormalizePlanRuleSnapshot(&pmservice.RuleSnapshot{
		TeamID:           strings.TrimSpace(teamID),
		Phase:            "plan",
		Source:           strings.TrimSpace(source),
		RefreshSemantics: pmservice.PlanRuleRefreshSemantics,
	}, pmservice.PlanRuleSourceTeamMemory)
}

func planRulesView(snap *pmservice.RuleSnapshot) map[string]any {
	if snap == nil {
		snap = emptyPlanRules("", pmservice.PlanRuleSourceUnavailable)
	}
	return pmservice.PlanRuleSnapshotAudit(snap)
}

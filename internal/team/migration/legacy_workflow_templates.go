package migration

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/cognition/memory/centergit"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/team"
)

// LegacyWorkflowTemplate is the migration-facing projection of the retired
// org-scoped workflow template catalog.
type LegacyWorkflowTemplate struct {
	ID          string
	OrgID       string
	Name        string
	Description string
	Content     string
	CreatedBy   string
	Builtin     bool
	Version     int
}

// LegacyWorkflowTemplateFromPM converts a projectmanager Template without
// exposing the migration planner to its repository implementation.
func LegacyWorkflowTemplateFromPM(t *pm.Template) LegacyWorkflowTemplate {
	if t == nil {
		return LegacyWorkflowTemplate{}
	}
	return LegacyWorkflowTemplate{
		ID:          string(t.ID()),
		OrgID:       t.OrgID(),
		Name:        t.Name(),
		Description: t.Description(),
		Content:     t.Content(),
		CreatedBy:   string(t.CreatedBy()),
		Builtin:     t.IsBuiltin(),
		Version:     t.Version(),
	}
}

// TeamOwnership declares which agent refs belong to a team in an org. The
// planner only migrates a template when CreatedBy resolves through exactly one
// ownership row in the same org.
type TeamOwnership struct {
	OrgID     string
	TeamID    team.TeamID
	AgentRefs []team.MemberRef
}

type MigrationPlan struct {
	Claims    []MigrationClaim
	Unclaimed []UnclaimedTemplate
}

type MigrationClaim struct {
	Template LegacyWorkflowTemplate
	TeamID   team.TeamID
	Rule     centergit.Rule
}

type UnclaimedTemplate struct {
	TemplateID string
	OrgID      string
	Name       string
	CreatedBy  string
	Reason     string
}

type AppliedRule struct {
	TemplateID string
	TeamID     team.TeamID
	Path       string
	Commit     string
}

// PlanLegacyWorkflowTemplateMigration safely maps legacy workflow templates to
// team rules. Ambiguous or unowned templates are returned as Unclaimed instead
// of being copied to every team in the org.
func PlanLegacyWorkflowTemplateMigration(templates []LegacyWorkflowTemplate, ownership []TeamOwnership) MigrationPlan {
	owners := ownershipIndex(ownership)
	plan := MigrationPlan{}
	for _, tmpl := range templates {
		createdBy := strings.TrimSpace(tmpl.CreatedBy)
		if tmpl.Builtin {
			plan.Unclaimed = append(plan.Unclaimed, unclaimed(tmpl, "builtin template has no team owner"))
			continue
		}
		if !strings.HasPrefix(createdBy, "agent:") {
			plan.Unclaimed = append(plan.Unclaimed, unclaimed(tmpl, "created_by is not an agent ref"))
			continue
		}
		key := ownerKey(tmpl.OrgID, createdBy)
		matches := owners[key]
		switch len(matches) {
		case 1:
			plan.Claims = append(plan.Claims, MigrationClaim{
				Template: tmpl,
				TeamID:   matches[0],
				Rule:     ruleFromTemplate(tmpl),
			})
		case 0:
			plan.Unclaimed = append(plan.Unclaimed, unclaimed(tmpl, "created_by agent is not in a team"))
		default:
			plan.Unclaimed = append(plan.Unclaimed, unclaimed(tmpl, "created_by agent maps to multiple teams"))
		}
	}
	sort.Slice(plan.Claims, func(i, j int) bool {
		if plan.Claims[i].TeamID != plan.Claims[j].TeamID {
			return plan.Claims[i].TeamID < plan.Claims[j].TeamID
		}
		return plan.Claims[i].Template.ID < plan.Claims[j].Template.ID
	})
	sort.Slice(plan.Unclaimed, func(i, j int) bool {
		if plan.Unclaimed[i].OrgID != plan.Unclaimed[j].OrgID {
			return plan.Unclaimed[i].OrgID < plan.Unclaimed[j].OrgID
		}
		return plan.Unclaimed[i].TemplateID < plan.Unclaimed[j].TemplateID
	})
	return plan
}

// ApplyLegacyWorkflowTemplateMigration writes planned claims to each team's
// rules/ directory and pushes one commit per affected team. It does not delete
// or mutate the legacy template catalog; callers can do that in a later cleanup
// once the pushed commits have been verified.
func ApplyLegacyWorkflowTemplateMigration(ctx context.Context, host *centergit.Host, runner memory.GitRunner, plan MigrationPlan) ([]AppliedRule, error) {
	if host == nil {
		return nil, fmt.Errorf("%w: host not wired", centergit.ErrGitOpFailed)
	}
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	byTeam := map[team.TeamID][]MigrationClaim{}
	for _, claim := range plan.Claims {
		byTeam[claim.TeamID] = append(byTeam[claim.TeamID], claim)
	}
	teams := make([]team.TeamID, 0, len(byTeam))
	for id := range byTeam {
		teams = append(teams, id)
	}
	sort.Slice(teams, func(i, j int) bool { return teams[i] < teams[j] })

	var applied []AppliedRule
	for _, teamID := range teams {
		claims := byTeam[teamID]
		paths, commit, err := applyTeamClaims(ctx, host, runner, teamID, claims)
		if err != nil {
			return applied, err
		}
		for i, p := range paths {
			applied = append(applied, AppliedRule{
				TemplateID: claims[i].Template.ID,
				TeamID:     teamID,
				Path:       p,
				Commit:     commit,
			})
		}
	}
	return applied, nil
}

func RollbackNotes(applied []AppliedRule) string {
	var b strings.Builder
	b.WriteString("Rollback: for each affected team repo, revert the listed commit or delete the listed rules/<slug>-<uuid>.md files, regenerate MEMORY.md, and push. The legacy pm_templates rows were not deleted by this migration helper, so no database restore is required unless a caller performed a separate cleanup.\n")
	for _, a := range applied {
		fmt.Fprintf(&b, "- team=%s template=%s path=%s commit=%s\n", a.TeamID, a.TemplateID, a.Path, a.Commit)
	}
	return strings.TrimSpace(b.String())
}

func ownershipIndex(ownership []TeamOwnership) map[string][]team.TeamID {
	out := map[string][]team.TeamID{}
	for _, own := range ownership {
		for _, ref := range own.AgentRefs {
			s := strings.TrimSpace(ref.String())
			if !strings.HasPrefix(s, "agent:") {
				continue
			}
			key := ownerKey(own.OrgID, s)
			if !containsTeam(out[key], own.TeamID) {
				out[key] = append(out[key], own.TeamID)
			}
		}
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i] < out[k][j] })
	}
	return out
}

func containsTeam(ids []team.TeamID, id team.TeamID) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

func ownerKey(orgID, agentRef string) string {
	return strings.TrimSpace(orgID) + "\x00" + strings.TrimSpace(agentRef)
}

func unclaimed(t LegacyWorkflowTemplate, reason string) UnclaimedTemplate {
	return UnclaimedTemplate{
		TemplateID: strings.TrimSpace(t.ID),
		OrgID:      strings.TrimSpace(t.OrgID),
		Name:       strings.TrimSpace(t.Name),
		CreatedBy:  strings.TrimSpace(t.CreatedBy),
		Reason:     reason,
	}
}

func ruleFromTemplate(t LegacyWorkflowTemplate) centergit.Rule {
	desc := strings.TrimSpace(t.Description)
	if desc == "" {
		desc = "Migrated legacy workflow template " + strings.TrimSpace(t.ID)
	}
	return centergit.Rule{
		Slug:        ruleSlug(t),
		Title:       strings.TrimSpace(t.Name),
		Description: desc,
		Body:        strings.TrimSpace(t.Content),
		Enabled:     true,
		AppliesTo:   []string{"plan"},
	}
}

func ruleSlug(t LegacyWorkflowTemplate) string {
	base := slugify(t.Name)
	if base == "" {
		base = "workflow-template"
	}
	id := slugify(t.ID)
	if id != "" {
		base += "-" + id
	}
	if len(base) > 120 {
		base = strings.Trim(base[:120], "-")
	}
	if base == "" {
		return "workflow-template"
	}
	return base
}

func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if r <= unicode.MaxASCII && ((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func applyTeamClaims(ctx context.Context, host *centergit.Host, runner memory.GitRunner, teamID team.TeamID, claims []MigrationClaim) ([]string, string, error) {
	repo := centergit.NewTeamMemoryRepository(host, runner)
	rules := make([]centergit.Rule, 0, len(claims))
	for _, claim := range claims {
		rules = append(rules, claim.Rule)
	}
	_, paths, commit, err := repo.BootstrapWithPaths(ctx, teamID.String(), centergit.TrustedBootstrapCommand{
		ActorRef: "system:bootstrap",
		Source:   "legacy-workflow-template-migration",
		Rules:    rules,
	})
	if err != nil {
		return nil, "", err
	}
	return paths, commit, nil
}

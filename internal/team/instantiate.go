package team

import (
	"fmt"
	"strings"
	"time"
)

// instantiate.go plans instantiating a Team template into an ORG (issue-c4dccae0:
// team instantiation is PROJECT-INDEPENDENT — a team is an org-level entity, and
// binding it to one or more projects is a SEPARATE step via associate_project,
// design §3 "team 与 project 是两条正交轴"). Instantiation is split into two
// planning outputs, mirroring the design's explicit two-step model:
//
//   - InstantiationPlan: the IDENTITY + CONFIG + MEMORY-REPO seed + workflow
//     binding — everything the template CARRIES (per-org-independent). Building
//     N new agents (new identities) per the role composition, binding them to
//     the new team under their roles, and the generalizable experiences to seed
//     into the team's center-hosted memory repo.
//
//   - RuntimeProvisioningPlan: the SEPARATE step (design §9) that gives those
//     new agents a runtime home + auth (codex/claude login, MCP token). The
//     template does NOT carry runtime/auth (per-deployment); this reuses the
//     existing enroll / worker-provision flow. Emitted as a distinct plan so the
//     caller runs it as its own step.
//
// This package produces the PLANS (pure, testable); the caller applies them via
// the S1 team service (create team + add members), the agent-identity service,
// and the memory + enroll flows.

// AgentSpec describes one new agent identity to create for an instantiated team.
type AgentSpec struct {
	// AgentID is the freshly minted identity ("agent:<id>" ref is DerivedRef()).
	AgentID string
	Role    string
	CLI     string
	Model   string
	// CapabilityTags carried from the role config.
	CapabilityTags []string
	// Ordinal is the 0-based index of this agent within its role (for naming /
	// display, e.g. "dev #0", "dev #1").
	Ordinal int
}

// DerivedRef returns the MemberRef for this agent ("agent:<id>").
func (a AgentSpec) DerivedRef() MemberRef { return MemberRef("agent:" + a.AgentID) }

// InstantiationPlan is the identity+config+memory step of instantiation.
type InstantiationPlan struct {
	// Team is the new team aggregate (identity + role config), ready to persist
	// via the S1 team service.
	Team *Team
	// Agents are the new agent identities to create (role composition expanded).
	Agents []AgentSpec
	// Members binds each new agent to the team under its role.
	Members []*TeamMember
	// MemorySeed is the portable experience to seed into the team memory repo.
	MemorySeed []Experience
	// WorkflowTemplateRef is the workflow to bind (design §6 "绑 workflow").
	WorkflowTemplateRef string
}

// EnrollSpec is the per-agent runtime-provisioning unit (worker placement + auth
// install), consumed by the existing enroll flow.
type EnrollSpec struct {
	AgentID string
	Role    string
	// CLI/Model tell the enroll flow which runtime + auth to provision.
	CLI   string
	Model string
}

// RuntimeProvisioningPlan is the SEPARATE runtime step (design §9).
type RuntimeProvisioningPlan struct {
	Enrollments []EnrollSpec
}

// IDMinter mints new entity ids (satisfied by idgen.Generator.NewEntityID).
type IDMinter interface {
	NewEntityID(kind string) string
}

// InstantiateInput drives PlanInstantiation.
type InstantiateInput struct {
	Template *TeamTemplate
	OrgID    string
	// TeamName for the instantiated team. Falls back to the template name.
	TeamName string
	Minter   IDMinter
	Now      time.Time
}

// PlanInstantiation expands a template into the two instantiation plans (design
// §6/§9). It does NOT touch any store — the caller applies the plans through the
// existing services. Instantiation is PROJECT-INDEPENDENT (issue-c4dccae0):
// project association, if wanted, is a separate associate_project step. Returns
// ErrInvalidTemplate on a nil/empty template.
func PlanInstantiation(in InstantiateInput) (*InstantiationPlan, *RuntimeProvisioningPlan, error) {
	if in.Template == nil || len(in.Template.Roles) == 0 {
		return nil, nil, ErrInvalidTemplate
	}
	if in.Minter == nil {
		return nil, nil, fmt.Errorf("%w: minter required", ErrInvalidTemplate)
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	orgID := strings.TrimSpace(in.OrgID)
	if orgID == "" {
		orgID = in.Template.OrgID
	}
	name := strings.TrimSpace(in.TeamName)
	if name == "" {
		name = in.Template.Name
	}

	newTeam, err := NewTeam(NewTeamInput{
		ID:          TeamID(in.Minter.NewEntityID("team")),
		OrgID:       orgID,
		Name:        name,
		Description: in.Template.Description,
		Roles:       in.Template.RoleConfigs(),
		CreatedAt:   now,
	})
	if err != nil {
		return nil, nil, err
	}

	var (
		agents   []AgentSpec
		memberRs []*TeamMember
		enrolls  []EnrollSpec
	)
	for _, sl := range in.Template.Roles {
		for i := 0; i < sl.Count; i++ {
			agentID := in.Minter.NewEntityID("agent")
			spec := AgentSpec{
				AgentID:        agentID,
				Role:           sl.Config.Role,
				CLI:            sl.Config.CLI,
				Model:          sl.Config.Model,
				CapabilityTags: append([]string(nil), sl.Config.CapabilityTags...),
				Ordinal:        i,
			}
			agents = append(agents, spec)
			memberRs = append(memberRs, &TeamMember{
				TeamID:    newTeam.ID(),
				Ref:       spec.DerivedRef(),
				Kind:      MemberKindAgent,
				Role:      sl.Config.Role,
				CreatedAt: now,
			})
			enrolls = append(enrolls, EnrollSpec{
				AgentID: agentID,
				Role:    sl.Config.Role,
				CLI:     sl.Config.CLI,
				Model:   sl.Config.Model,
			})
		}
	}

	// Only portable experiences ever reach a template, but guard again so a
	// hand-built template can't seed project facts into the shared team repo.
	seed := make([]Experience, 0, len(in.Template.Experiences))
	for _, e := range in.Template.Experiences {
		if e.Scope.Portable() {
			seed = append(seed, e)
		}
	}

	instPlan := &InstantiationPlan{
		Team:                newTeam,
		Agents:              agents,
		Members:             memberRs,
		MemorySeed:          seed,
		WorkflowTemplateRef: in.Template.WorkflowTemplateRef,
	}
	rtPlan := &RuntimeProvisioningPlan{Enrollments: enrolls}
	return instPlan, rtPlan, nil
}

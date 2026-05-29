package agent

import "context"

// OrgDirectory adapts an agent Repository to the ProjectManager Service's
// optional AgentDirectory dependency (v2.7 D2 b2/d-i, #5a, ADR-0049/0052/OQ6):
// it resolves an agent's owning Organization so AssignTask can cross-org-guard
// before granting an assignee agent ProjectMember. agentID is the bare id (the
// `agent:` prefix is stripped by the pm Service before calling). An unknown
// agent surfaces ErrAgentNotFound, which the pm Service treats as a cross-org
// rejection (org cannot be verified).
type OrgDirectory struct{ repo Repository }

// NewOrgDirectory wraps an agent Repository as an org resolver.
func NewOrgDirectory(repo Repository) *OrgDirectory { return &OrgDirectory{repo: repo} }

// OrgOfAgent returns the agent's OrganizationID.
func (d *OrgDirectory) OrgOfAgent(ctx context.Context, agentID string) (string, error) {
	a, err := d.repo.FindByID(ctx, AgentID(agentID))
	if err != nil {
		return "", err
	}
	return a.OrganizationID(), nil
}

package agent

import (
	"context"
	"errors"
)

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

// OrgOfAgent returns the agent's OrganizationID. agentID may be EITHER the
// execution-entity id OR the identity-member id ("agent-<ulid>", v2.7 #185 — the
// business-layer id the assign path now carries).
func (d *OrgDirectory) OrgOfAgent(ctx context.Context, agentID string) (string, error) {
	a, err := d.resolve(ctx, agentID)
	if err != nil {
		return "", err
	}
	return a.OrganizationID(), nil
}

// resolve accepts either the execution-entity id or the identity-member id
// (#185 member→entity bridge): entity-id first (cheap, no collision — member ids
// are "agent-"-prefixed, entity ids bare ULIDs), then FindByIdentityMemberID.
func (d *OrgDirectory) resolve(ctx context.Context, id string) (*Agent, error) {
	a, err := d.repo.FindByID(ctx, AgentID(id))
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, ErrAgentNotFound) {
		return nil, err
	}
	return d.repo.FindByIdentityMemberID(ctx, id)
}

package identity

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// AgentProvisionForm holds the input for creating an agent identity.
type AgentProvisionForm struct {
	DisplayName string
	Description string
	Role        MemberRole
}

// AgentProvisionResult is the outcome of a successful agent provision.
type AgentProvisionResult struct {
	Identity *Identity
	Member   *Member
}

// AgentIdentityProvisionService implements DS-5: owner/admin provisions an
// agent Identity + Member in one transaction.
type AgentIdentityProvisionService struct {
	db         *sql.DB
	identities IdentityRepository
	members    MemberRepository
	sink       *observability.EventSink
}

// NewAgentIdentityProvisionService constructs the service.
func NewAgentIdentityProvisionService(
	db *sql.DB,
	identities IdentityRepository,
	members MemberRepository,
) *AgentIdentityProvisionService {
	return &AgentIdentityProvisionService{db: db, identities: identities, members: members}
}

// NewAgentIdentityProvisionServiceWithSink constructs the service with an event sink.
func NewAgentIdentityProvisionServiceWithSink(
	db *sql.DB,
	identities IdentityRepository,
	members MemberRepository,
	sink *observability.EventSink,
) *AgentIdentityProvisionService {
	return &AgentIdentityProvisionService{db: db, identities: identities, members: members, sink: sink}
}

// Provision creates an Identity[kind=agent] + Member atomically (DS-5).
// provisionedByIdentityID must be an owner/admin in organizationID.
func (s *AgentIdentityProvisionService) Provision(
	ctx context.Context,
	form AgentProvisionForm,
	organizationID string,
	provisionedByIdentityID string,
) (*AgentProvisionResult, error) {
	if err := validateDisplayName(form.DisplayName); err != nil {
		return nil, err
	}
	if !form.Role.IsValid() {
		return nil, ErrForbidden
	}

	var result AgentProvisionResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// Verify provisioner is owner/admin in this organization.
		provisioner, err := s.members.GetByOrganizationAndIdentity(txCtx, organizationID, provisionedByIdentityID)
		if err != nil {
			return ErrForbidden
		}
		if !provisioner.Role().AtLeast(RoleAdmin) {
			return ErrForbidden
		}

		// Create agent Identity.
		identity, err := IdentityFactory{}.NewAgent(form.DisplayName, form.Description)
		if err != nil {
			return err
		}
		if err := s.identities.Save(txCtx, identity); err != nil {
			return err
		}

		// Create Member.
		invBy := provisionedByIdentityID
		member, err := MemberFactory{}.New(organizationID, identity.ID(), form.Role, &invBy)
		if err != nil {
			return err
		}
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}

		result = AgentProvisionResult{Identity: identity, Member: member}

		// Emit domain events in same TX.
		actor := observability.Actor("user:" + provisionedByIdentityID)
		refs := observability.EventRefs{IdentityID: identity.ID(), OrganizationID: organizationID, MemberID: member.ID()}
		if err := emitEvent(txCtx, s.sink, EvtIdentityCreated, refs, actor, map[string]any{"kind": "agent"}); err != nil {
			return err
		}
		return emitEvent(txCtx, s.sink, EvtMemberAdded, refs, actor, map[string]any{"role": string(member.Role())})
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

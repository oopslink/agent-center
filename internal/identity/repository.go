package identity

import "context"

// IdentityRepository defines persistence operations for the Identity AR.
type IdentityRepository interface {
	Save(ctx context.Context, id *Identity) error
	Update(ctx context.Context, id *Identity) error
	GetByID(ctx context.Context, id string) (*Identity, error)
	GetByDisplayName(ctx context.Context, name string) (*Identity, error)
	List(ctx context.Context) ([]*Identity, error)
	// Delete hard-removes an Identity row by id. v2.7 #197: currently used ONLY by
	// the agent-delete cascade (delete the execution agent + its agent-identity +
	// member atomically, symmetric to #157's atomic create). Generic by design
	// (not kind-limited); callers are responsible for not deleting an identity
	// still strongly referenced (e.g. an org owner). Idempotent: absent id = no-op.
	Delete(ctx context.Context, id string) error
}

// OrganizationRepository defines persistence operations for the Organization AR.
type OrganizationRepository interface {
	Save(ctx context.Context, org *Organization) error
	GetByID(ctx context.Context, id string) (*Organization, error)
	GetBySlug(ctx context.Context, slug string) (*Organization, error)
	ListForIdentity(ctx context.Context, identityID string) ([]*Organization, error)
}

// MemberRepository defines persistence operations for the Member AR.
type MemberRepository interface {
	Save(ctx context.Context, m *Member) error
	GetByID(ctx context.Context, id string) (*Member, error)
	GetByOrganizationAndIdentity(ctx context.Context, organizationID, identityID string) (*Member, error)
	ListByOrganization(ctx context.Context, organizationID string) ([]*Member, error)
	CountActiveOwners(ctx context.Context, organizationID string) (int, error)
	// Delete hard-removes a Member row by id. v2.7 #197: used by the agent-delete
	// cascade (an agent member has no org-owner constraints). Idempotent: absent
	// id = no-op.
	Delete(ctx context.Context, id string) error
}

// InvitationRepository defines persistence operations for the Invitation AR.
type InvitationRepository interface {
	Save(ctx context.Context, inv *Invitation) error
	GetByID(ctx context.Context, id string) (*Invitation, error)
	GetByToken(ctx context.Context, token string) (*Invitation, error)
	ListByOrganization(ctx context.Context, organizationID string) ([]*Invitation, error)
	Delete(ctx context.Context, id string) error
}

package identity

import "context"

// IdentityFilter narrows IdentityRepository.Find.
type IdentityFilter struct {
	Kind   *Kind
	Cursor *IdentityID
	Limit  int
}

// DefaultIdentityLimit caps Find when Limit <= 0.
const DefaultIdentityLimit = 100

// IdentityRepository is the Conversation BC sub-repo for the Identity AR
// (conversation/00 § 5.3, conversation/02 § 1).
type IdentityRepository interface {
	FindByID(ctx context.Context, id IdentityID) (*Identity, error)
	Find(ctx context.Context, filter IdentityFilter) ([]*Identity, error)
	Save(ctx context.Context, i *Identity) error
	Update(ctx context.Context, i *Identity, expectedVersion int) error
}

// ChannelBindingRepository is the Conversation BC sub-repo for the
// ChannelBinding VO (conversation/00 § 5.4).
//
// FindByVendorUserID is a Phase 7 inbound hot-path: vendor sends an event
// with a vendor user id; Bridge resolves it back to the agent-center
// identity. Phase 5 implements it ahead of time per plan-5 § 1.4.
type ChannelBindingRepository interface {
	FindByID(ctx context.Context, id string) (*ChannelBinding, error)
	FindByIdentityID(ctx context.Context, identityID IdentityID) ([]*ChannelBinding, error)
	FindByVendorUserID(ctx context.Context, channel Channel, vendorUserID string) (*ChannelBinding, error)
	FindPreferred(ctx context.Context, identityID IdentityID, channel Channel) (*ChannelBinding, error)
	Save(ctx context.Context, b *ChannelBinding) error
	DeleteByIdentityAndChannel(ctx context.Context, identityID IdentityID, channel Channel) error
}

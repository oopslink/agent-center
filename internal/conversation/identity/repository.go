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
// (v2 per ADR-0033 — ChannelBinding removed).
type IdentityRepository interface {
	FindByID(ctx context.Context, id IdentityID) (*Identity, error)
	Find(ctx context.Context, filter IdentityFilter) ([]*Identity, error)
	Save(ctx context.Context, i *Identity) error
	Update(ctx context.Context, i *Identity, expectedVersion int) error
}

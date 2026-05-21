package identity

import (
	"errors"
	"strings"
	"time"
)

// ChannelBinding maps an Identity to one vendor user id per channel.
// VO under Identity (conversation/02 § 1 sub-从属).
type ChannelBinding struct {
	id           string // ULID
	identityID   IdentityID
	channel      Channel
	vendorUserID string
	preferred    bool
	boundAt      time.Time
	createdAt    time.Time
}

// NewChannelBindingInput captures the constructor args.
type NewChannelBindingInput struct {
	ID           string
	IdentityID   IdentityID
	Channel      Channel
	VendorUserID string
	Preferred    bool
	BoundAt      time.Time
}

// NewChannelBinding validates and constructs a ChannelBinding.
func NewChannelBinding(in NewChannelBindingInput) (*ChannelBinding, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("channel_binding: id required")
	}
	if err := in.IdentityID.Validate(); err != nil {
		return nil, err
	}
	if err := in.Channel.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.VendorUserID) == "" {
		return nil, errors.New("channel_binding: vendor_user_id required")
	}
	if in.BoundAt.IsZero() {
		return nil, errors.New("channel_binding: bound_at required")
	}
	at := in.BoundAt.UTC()
	return &ChannelBinding{
		id:           in.ID,
		identityID:   in.IdentityID,
		channel:      in.Channel,
		vendorUserID: in.VendorUserID,
		preferred:    in.Preferred,
		boundAt:      at,
		createdAt:    at,
	}, nil
}

// RehydrateChannelBindingInput is for repository round-trip.
type RehydrateChannelBindingInput struct {
	ID           string
	IdentityID   IdentityID
	Channel      Channel
	VendorUserID string
	Preferred    bool
	BoundAt      time.Time
	CreatedAt    time.Time
}

// RehydrateChannelBinding reconstructs without invariant validation (repo
// path).
func RehydrateChannelBinding(in RehydrateChannelBindingInput) *ChannelBinding {
	return &ChannelBinding{
		id:           in.ID,
		identityID:   in.IdentityID,
		channel:      in.Channel,
		vendorUserID: in.VendorUserID,
		preferred:    in.Preferred,
		boundAt:      in.BoundAt.UTC(),
		createdAt:    in.CreatedAt.UTC(),
	}
}

// Getters.

// ID returns the ULID id.
func (b *ChannelBinding) ID() string { return b.id }

// IdentityID returns the bound identity id.
func (b *ChannelBinding) IdentityID() IdentityID { return b.identityID }

// Channel returns the vendor channel name.
func (b *ChannelBinding) Channel() Channel { return b.channel }

// VendorUserID returns the vendor side user id.
func (b *ChannelBinding) VendorUserID() string { return b.vendorUserID }

// Preferred reports whether this binding is the default per (identity, channel).
func (b *ChannelBinding) Preferred() bool { return b.preferred }

// BoundAt returns the bind time.
func (b *ChannelBinding) BoundAt() time.Time { return b.boundAt }

// CreatedAt returns the row creation time.
func (b *ChannelBinding) CreatedAt() time.Time { return b.createdAt }

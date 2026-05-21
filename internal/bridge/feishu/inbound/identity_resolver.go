package inbound

import (
	"context"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/observability"
)

// IdentityResolver is the FeishuInboundIdentityResolver (plan-7 § 3.1).
//
// Workflow (per conversation/00 § 3.2):
//  1. Lookup (channel=feishu, vendor_user_id) in ChannelBindingRepository.
//     Hit → return identity_id.
//  2. Miss → enumerate kind=user identities.
//     - exactly 1 → call IdentityRegistration.BindChannel; emit
//       `bridge.identity_auto_bound`; return identity_id.
//     - 0          → emit `bridge.parse_failed
//       reason=no_user_identity`; return ErrNoUserIdentity.
//     - >1         → emit `bridge.parse_failed reason=ambiguous_user`;
//       return ErrAmbiguousUserIdentity (v2 introduces interactive
//       enroll).
//  3. Concurrent first-time binds rely on the
//     `channel_bindings(channel, vendor_user_id) UNIQUE` constraint;
//     the loser receives ErrChannelBindingAlreadyExists and retries
//     step 1 (a tight bound = 1 retry).
type IdentityResolver struct {
	bindings     identity.ChannelBindingRepository
	identities   identity.IdentityRepository
	registration *identity.RegistrationService
	sink         *observability.EventSink
	clock        clock.Clock
	channel      identity.Channel
	actor        observability.Actor
}

// IdentityResolverDeps wires the resolver.
type IdentityResolverDeps struct {
	Bindings     identity.ChannelBindingRepository
	Identities   identity.IdentityRepository
	Registration *identity.RegistrationService
	Sink         *observability.EventSink
	Clock        clock.Clock
	// Channel is the vendor name (e.g. identity.Channel("feishu")).
	Channel identity.Channel
	// Actor is the actor stamp used when this resolver writes the
	// auto-bind row (typically "system").
	Actor observability.Actor
}

// NewIdentityResolver constructs the resolver, validating deps.
func NewIdentityResolver(deps IdentityResolverDeps) (*IdentityResolver, error) {
	if deps.Bindings == nil {
		return nil, errors.New("inbound: bindings repo required")
	}
	if deps.Identities == nil {
		return nil, errors.New("inbound: identities repo required")
	}
	if deps.Registration == nil {
		return nil, errors.New("inbound: registration service required")
	}
	if deps.Sink == nil {
		return nil, errors.New("inbound: event sink required")
	}
	if err := deps.Channel.Validate(); err != nil {
		return nil, fmt.Errorf("inbound: channel: %w", err)
	}
	if err := deps.Actor.Validate(); err != nil {
		return nil, fmt.Errorf("inbound: actor: %w", err)
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	return &IdentityResolver{
		bindings:     deps.Bindings,
		identities:   deps.Identities,
		registration: deps.Registration,
		sink:         deps.Sink,
		clock:        deps.Clock,
		channel:      deps.Channel,
		actor:        deps.Actor,
	}, nil
}

// Resolve maps a vendor user id to a center identity, auto-binding when
// safe. See type-level docstring for the full state machine.
func (r *IdentityResolver) Resolve(ctx context.Context, vendorUserID string) (identity.IdentityID, error) {
	if vendorUserID == "" {
		return "", fmt.Errorf("%w: vendor_user_id required", ErrVendorEventMalformed)
	}
	// Step 1: cache-friendly lookup.
	if b, err := r.bindings.FindByVendorUserID(ctx, r.channel, vendorUserID); err == nil {
		return b.IdentityID(), nil
	} else if !errors.Is(err, identity.ErrChannelBindingNotFound) {
		return "", fmt.Errorf("inbound: lookup binding: %w", err)
	}

	// Step 2: auto-bind based on the user identity population.
	id, err := r.autoBind(ctx, vendorUserID)
	if err == nil {
		return id, nil
	}
	// Step 2.1: a concurrent first-time bind raced us. The other
	// goroutine wrote the row → step-1 retry will hit it.
	if errors.Is(err, identity.ErrChannelBindingAlreadyExists) {
		b, err2 := r.bindings.FindByVendorUserID(ctx, r.channel, vendorUserID)
		if err2 != nil {
			return "", fmt.Errorf("inbound: lookup binding (retry): %w", err2)
		}
		return b.IdentityID(), nil
	}
	return "", err
}

func (r *IdentityResolver) autoBind(ctx context.Context, vendorUserID string) (identity.IdentityID, error) {
	kind := identity.KindUser
	users, err := r.identities.Find(ctx, identity.IdentityFilter{Kind: &kind, Limit: 2})
	if err != nil {
		return "", fmt.Errorf("inbound: list user identities: %w", err)
	}
	switch len(users) {
	case 0:
		r.emitParseFailed(ctx, "no_user_identity",
			"cannot auto-bind feishu vendor_user_id="+vendorUserID+
				": no user identity exists; run `agent-center identity add user:<name>` first")
		return "", ErrNoUserIdentity
	case 1:
		// happy path: register the binding via the Identity BC service so
		// `identity.channel_bound` lands inside the binding transaction
		// (ADR-0014 § 2 same-tx).
		res, err := r.registration.BindChannel(ctx, identity.BindChannelCommand{
			IdentityID:   users[0].ID(),
			Channel:      r.channel,
			VendorUserID: vendorUserID,
			Preferred:    true,
			Actor:        r.actor,
		})
		if err != nil {
			return "", err
		}
		// Emit Bridge-side audit. Identity BC already emitted
		// identity.channel_bound (in the same tx); we only add the
		// Bridge audit trail.
		if _, err := r.sink.Emit(ctx, observability.EmitCommand{
			EventType: "bridge.identity_auto_bound",
			Actor:     r.actor,
			Payload: map[string]any{
				"channel":        string(r.channel),
				"vendor_user_id": vendorUserID,
				"identity_id":    string(users[0].ID()),
				"binding_id":     res.Binding.ID(),
			},
		}); err != nil {
			// Logging-only is forbidden (§ 17); surface emit failures
			// up the stack.
			return "", fmt.Errorf("inbound: emit auto_bound: %w", err)
		}
		return users[0].ID(), nil
	default:
		r.emitParseFailed(ctx, "ambiguous_user",
			fmt.Sprintf("cannot auto-bind feishu vendor_user_id=%s: %d user identities found; bind manually with `agent-center identity bind`", vendorUserID, len(users)))
		return "", ErrAmbiguousUserIdentity
	}
}

func (r *IdentityResolver) emitParseFailed(ctx context.Context, reason, message string) {
	// best-effort but not log-only: failure surfaces only when the sink
	// itself is broken (and we cannot meaningfully recover here without
	// turning every inbound into an error).
	_, _ = r.sink.Emit(ctx, observability.EmitCommand{
		EventType: "bridge.parse_failed",
		Actor:     r.actor,
		Payload: map[string]any{
			"reason":         reason,
			"message":        message,
			"vendor_kind":    "identity_resolution",
			"channel":        string(r.channel),
		},
	})
}

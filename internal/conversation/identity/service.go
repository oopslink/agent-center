package identity

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// RegistrationService is the Conversation BC Identity registration service
// (conversation/00 § 3.2). Manual paths land in Phase 5; auto-registration
// (Bridge inbound) is reserved for Phase 7 — method signatures are present
// here so Phase 7 only adds callers.
type RegistrationService struct {
	db          *sql.DB
	identities  IdentityRepository
	bindings    ChannelBindingRepository
	sink        *observability.EventSink
	idgen       idgen.Generator
	clock       clock.Clock
}

// NewRegistrationService constructs the service. clk defaults to system
// clock when nil.
func NewRegistrationService(
	db *sql.DB,
	identities IdentityRepository,
	bindings ChannelBindingRepository,
	sink *observability.EventSink,
	gen idgen.Generator,
	clk clock.Clock,
) *RegistrationService {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &RegistrationService{
		db:         db,
		identities: identities,
		bindings:   bindings,
		sink:       sink,
		idgen:      gen,
		clock:      clk,
	}
}

// RegisterIdentityCommand is the input for RegisterIdentity.
type RegisterIdentityCommand struct {
	ID          IdentityID
	Kind        Kind
	DisplayName string
	Actor       observability.Actor
}

// RegisterIdentityResult tracks the created identity + event id.
type RegisterIdentityResult struct {
	Identity *Identity
	EventID  observability.EventID
}

// RegisterIdentity creates an Identity and emits identity.registered in the
// same tx (ADR-0014 § 2). Returns ErrIdentityAlreadyExists if the id is taken.
func (s *RegistrationService) RegisterIdentity(ctx context.Context, cmd RegisterIdentityCommand) (RegisterIdentityResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return RegisterIdentityResult{}, err
	}
	if err := cmd.ID.Validate(); err != nil {
		return RegisterIdentityResult{}, err
	}
	derived, err := KindFromID(cmd.ID)
	if err != nil {
		return RegisterIdentityResult{}, err
	}
	// If kind is provided, it must match derived; if omitted (empty), use derived.
	kind := cmd.Kind
	if kind == "" {
		kind = derived
	}
	if kind != derived {
		return RegisterIdentityResult{}, errors.New("identity: kind does not match id prefix")
	}
	if strings.TrimSpace(cmd.DisplayName) == "" {
		return RegisterIdentityResult{}, errors.New("identity: display_name required")
	}
	id, err := NewIdentity(NewIdentityInput{
		ID:          cmd.ID,
		Kind:        kind,
		DisplayName: cmd.DisplayName,
		CreatedAt:   s.clock.Now(),
	})
	if err != nil {
		return RegisterIdentityResult{}, err
	}
	var evID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.identities.Save(txCtx, id); err != nil {
			return err
		}
		emitted, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "identity.registered",
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"identity_id":  string(id.ID()),
				"kind":         string(id.Kind()),
				"display_name": id.DisplayName(),
			},
		})
		if err != nil {
			return err
		}
		evID = emitted
		return nil
	})
	if err != nil {
		return RegisterIdentityResult{}, err
	}
	return RegisterIdentityResult{Identity: id, EventID: evID}, nil
}

// BindChannelCommand is the input for BindChannel.
type BindChannelCommand struct {
	IdentityID   IdentityID
	Channel      Channel
	VendorUserID string
	Preferred    bool
	Actor        observability.Actor
}

// BindChannelResult tracks the created binding + event id.
type BindChannelResult struct {
	Binding *ChannelBinding
	EventID observability.EventID
}

// BindChannel inserts a ChannelBinding for an existing Identity and emits
// identity.channel_bound in the same tx.
//
// Returns:
//   - ErrIdentityNotFound when the identity_id has no row (app-layer
//     integrity per conventions § 9.w; we do not rely on FK).
//   - ErrChannelBindingAlreadyExists when (channel, vendor_user_id) is taken.
//   - ErrChannelBindingPreferredConflict when preferred=true conflicts.
func (s *RegistrationService) BindChannel(ctx context.Context, cmd BindChannelCommand) (BindChannelResult, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return BindChannelResult{}, err
	}
	if err := cmd.IdentityID.Validate(); err != nil {
		return BindChannelResult{}, err
	}
	if err := cmd.Channel.Validate(); err != nil {
		return BindChannelResult{}, err
	}
	if strings.TrimSpace(cmd.VendorUserID) == "" {
		return BindChannelResult{}, errors.New("identity: vendor_user_id required")
	}
	b, err := NewChannelBinding(NewChannelBindingInput{
		ID:           s.idgen.NewULID(),
		IdentityID:   cmd.IdentityID,
		Channel:      cmd.Channel,
		VendorUserID: cmd.VendorUserID,
		Preferred:    cmd.Preferred,
		BoundAt:      s.clock.Now(),
	})
	if err != nil {
		return BindChannelResult{}, err
	}
	var evID observability.EventID
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// App-layer integrity: confirm identity exists before binding
		// (conventions § 9.w no-FK; conversation/02 § 4 invariant 5).
		if _, err := s.identities.FindByID(txCtx, cmd.IdentityID); err != nil {
			return err
		}
		if err := s.bindings.Save(txCtx, b); err != nil {
			return err
		}
		emitted, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "identity.channel_bound",
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"identity_id":    string(b.IdentityID()),
				"channel":        string(b.Channel()),
				"vendor_user_id": b.VendorUserID(),
				"preferred":      b.Preferred(),
			},
		})
		if err != nil {
			return err
		}
		evID = emitted
		return nil
	})
	if err != nil {
		return BindChannelResult{}, err
	}
	return BindChannelResult{Binding: b, EventID: evID}, nil
}

// UnbindChannelCommand is the input for UnbindChannel.
type UnbindChannelCommand struct {
	IdentityID IdentityID
	Channel    Channel
	Actor      observability.Actor
}

// UnbindChannel removes all bindings matching (identity, channel) and emits
// identity.channel_unbound. Returns ErrChannelBindingNotFound when no row
// existed.
func (s *RegistrationService) UnbindChannel(ctx context.Context, cmd UnbindChannelCommand) (observability.EventID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	if err := cmd.IdentityID.Validate(); err != nil {
		return "", err
	}
	if err := cmd.Channel.Validate(); err != nil {
		return "", err
	}
	var evID observability.EventID
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.bindings.DeleteByIdentityAndChannel(txCtx, cmd.IdentityID, cmd.Channel); err != nil {
			return err
		}
		emitted, err := s.sink.Emit(txCtx, observability.EmitCommand{
			EventType: "identity.channel_unbound",
			Actor:     cmd.Actor,
			Payload: map[string]any{
				"identity_id": string(cmd.IdentityID),
				"channel":     string(cmd.Channel),
			},
		})
		if err != nil {
			return err
		}
		evID = emitted
		return nil
	})
	return evID, err
}

// AutoRegisterFromVendor is the Phase 7 inbound auto-registration hook.
// Phase 5 leaves it unimplemented but in place so callers compile.
//
// Returns an error in Phase 5; Phase 7 wires inbound caller logic here.
func (s *RegistrationService) AutoRegisterFromVendor(ctx context.Context, channel Channel, vendorUserID, displayName string, actor observability.Actor) (RegisterIdentityResult, error) {
	return RegisterIdentityResult{}, errors.New("identity: AutoRegisterFromVendor reserved for Phase 7 inbound")
}

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
// (v2 per ADR-0033 — ChannelBinding removed; v2 simplified flow per
// plan-10 § 3.8: `agent-center identity add --name=<n>` init once; later
// AgentInstance create auto-registers Identity[kind=agent]).
type RegistrationService struct {
	db         *sql.DB
	identities IdentityRepository
	sink       *observability.EventSink
	idgen      idgen.Generator
	clock      clock.Clock
}

// NewRegistrationService constructs the service. clk defaults to system
// clock when nil.
func NewRegistrationService(
	db *sql.DB,
	identities IdentityRepository,
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

// RegisterAgentIdentityInTx implements the workforce IdentityRegistrar
// port: writes Identity[kind=agent].id = "agent:<instanceID>" inside the
// caller's tx (cross-aggregate invariant per ADR-0033 § 4).
func (s *RegistrationService) RegisterAgentIdentityInTx(ctx context.Context, agentInstanceID string, displayName string, actor observability.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	id := IdentityID("agent:" + agentInstanceID)
	if err := id.Validate(); err != nil {
		return err
	}
	identity, err := NewIdentity(NewIdentityInput{
		ID: id, Kind: KindAgent, DisplayName: displayName, CreatedAt: s.clock.Now(),
	})
	if err != nil {
		return err
	}
	if err := s.identities.Save(ctx, identity); err != nil {
		return err
	}
	_, err = s.sink.Emit(ctx, observability.EmitCommand{
		EventType: "identity.registered",
		Actor:     actor,
		Payload: map[string]any{
			"identity_id":  string(identity.ID()),
			"kind":         string(identity.Kind()),
			"display_name": identity.DisplayName(),
			"source":       "agent_instance_create_auto",
		},
	})
	return err
}

// EnsureSystemIdentity is the center startup auto-provision per plan
// § 3.8. Returns nil if the system identity already exists.
func (s *RegistrationService) EnsureSystemIdentity(ctx context.Context, actor observability.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if _, err := s.identities.FindByID(ctx, IdentityID("system")); err == nil {
		return nil
	} else if !errors.Is(err, ErrIdentityNotFound) {
		return err
	}
	_, err := s.RegisterIdentity(ctx, RegisterIdentityCommand{
		ID: IdentityID("system"), Kind: KindSystem,
		DisplayName: "agent-center system", Actor: actor,
	})
	return err
}

// RegisterIdentity creates an Identity and emits identity.registered in
// the same tx (ADR-0014 § 2). Returns ErrIdentityAlreadyExists if the id
// is taken.
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

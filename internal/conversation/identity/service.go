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

package identity

import (
	"context"
	"database/sql"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ============================================================
// SignupService — DS-1: Identity + Organization + Member in one TX
// ============================================================

// SignupForm holds validated user input for the signup flow.
type SignupForm struct {
	DisplayName      string
	PasscodePlain    string
	OrganizationName string
	OrganizationSlug string
	// Email (v2.7.1 #214) is required for new signups (NOT verified). Uniqueness is
	// enforced at the DB → mapped to ErrIdentityEmailTaken (409).
	Email string
}

// Validate checks all field constraints before execution.
func (f SignupForm) Validate() error {
	if err := validateDisplayName(f.DisplayName); err != nil {
		return err
	}
	// v2.7.1 #214: email is OPTIONAL at the service layer (only its shape is checked
	// when present) — "required for new signups" is a v2.7.1 API policy enforced at
	// the HTTP handler, not baked into the signup domain flow (which other callers /
	// tests use without an email). Mirrors the worker_id-required-at-handler choice.
	if f.Email != "" {
		if err := validateEmail(f.Email); err != nil {
			return err
		}
	}
	if err := ValidatePasscodePlain(f.PasscodePlain); err != nil {
		return err
	}
	if f.OrganizationName == "" || len(f.OrganizationName) > 80 {
		return ErrOrganizationNotFound
	}
	return ValidateSlug(f.OrganizationSlug)
}

// SignupResult contains the outcome of a successful signup.
type SignupResult struct {
	Identity     *Identity
	Organization *Organization
	Member       *Member
}

// SignupService executes the signup flow (DS-1).
type SignupService struct {
	db         *sql.DB
	identities IdentityRepository
	orgs       OrganizationRepository
	members    MemberRepository
	sink       *observability.EventSink
}

// NewSignupService constructs the service without an event sink.
func NewSignupService(
	db *sql.DB,
	identities IdentityRepository,
	orgs OrganizationRepository,
	members MemberRepository,
) *SignupService {
	return &SignupService{db: db, identities: identities, orgs: orgs, members: members}
}

// NewSignupServiceWithSink constructs the service with an event sink.
func NewSignupServiceWithSink(
	db *sql.DB,
	identities IdentityRepository,
	orgs OrganizationRepository,
	members MemberRepository,
	sink *observability.EventSink,
) *SignupService {
	return &SignupService{db: db, identities: identities, orgs: orgs, members: members, sink: sink}
}

// Execute validates the form and atomically creates Identity + Organization + Member.
func (s *SignupService) Execute(ctx context.Context, form SignupForm) (*SignupResult, error) {
	if err := form.Validate(); err != nil {
		return nil, err
	}
	var result SignupResult
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		// 1. Check display_name uniqueness.
		if existing, _ := s.identities.GetByDisplayName(txCtx, form.DisplayName); existing != nil {
			return ErrIdentityDisplayNameTaken
		}
		// 2. Check slug uniqueness.
		if existing, _ := s.orgs.GetBySlug(txCtx, form.OrganizationSlug); existing != nil {
			return ErrOrganizationSlugTaken
		}
		// 3. Hash passcode + create Identity.
		hash, err := HashPasscode(form.PasscodePlain)
		if err != nil {
			return err
		}
		identity, err := IdentityFactory{}.NewUser(form.DisplayName, hash)
		if err != nil {
			return err
		}
		// v2.7.1 #214: attach the (validated) email before Save so it is in the
		// INSERT; the DB unique index → ErrIdentityEmailTaken on a dup. Optional at
		// this layer (handler enforces required for new signups).
		if form.Email != "" {
			if err := identity.SetEmail(form.Email); err != nil {
				return err
			}
		}
		if err := s.identities.Save(txCtx, identity); err != nil {
			return err
		}
		// 4. Create Organization.
		org, err := OrganizationFactory{}.New(form.OrganizationSlug, form.OrganizationName, identity.ID())
		if err != nil {
			return err
		}
		if err := s.orgs.Save(txCtx, org); err != nil {
			return err
		}
		// 5. Create owner Member.
		member, err := MemberFactory{}.New(org.ID(), identity.ID(), RoleOwner, nil)
		if err != nil {
			return err
		}
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}
		result = SignupResult{Identity: identity, Organization: org, Member: member}

		// 6. Emit domain events in same TX.
		actor := observability.Actor("user:" + identity.ID())
		refs := observability.EventRefs{IdentityID: identity.ID(), OrganizationID: org.ID(), MemberID: member.ID()}
		if err := emitEvent(txCtx, s.sink, EvtIdentityCreated, refs, actor, map[string]any{"kind": "user"}); err != nil {
			return err
		}
		if err := emitEvent(txCtx, s.sink, EvtOrganizationCreated, refs, actor, map[string]any{"slug": org.Slug()}); err != nil {
			return err
		}
		return emitEvent(txCtx, s.sink, EvtMemberAdded, refs, actor, map[string]any{"role": string(member.Role())})
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ============================================================
// SigninService
// ============================================================

// SigninResult holds the session JWT on successful signin.
type SigninResult struct {
	IdentityID string
	JWT        string
	Jti        string
}

// SigninService validates credentials and mints a JWT.
type SigninService struct {
	identities IdentityRepository
	signingKey []byte
	sink       *observability.EventSink
}

// NewSigninService constructs the service without an event sink.
func NewSigninService(identities IdentityRepository, signingKey []byte) *SigninService {
	return &SigninService{identities: identities, signingKey: signingKey}
}

// NewSigninServiceWithSink constructs the service with an event sink.
func NewSigninServiceWithSink(identities IdentityRepository, signingKey []byte, sink *observability.EventSink) *SigninService {
	return &SigninService{identities: identities, signingKey: signingKey, sink: sink}
}

// Execute authenticates and returns a JWT. Returns ErrPasscodeInvalid on any
// auth failure (no enumeration leakage).
func (s *SigninService) Execute(ctx context.Context, displayName, passcodePlain string) (*SigninResult, error) {
	identity, err := s.identities.GetByDisplayName(ctx, displayName)
	if err != nil || identity == nil {
		_ = emitEvent(ctx, s.sink, EvtAuthSigninFailed, observability.EventRefs{}, observability.Actor("system"), map[string]any{"reason": "not_found", "message": "identity not found"})
		return nil, ErrPasscodeInvalid
	}
	if identity.AccountStatus() == AccountDisabled {
		_ = emitEvent(ctx, s.sink, EvtAuthSigninFailed, observability.EventRefs{IdentityID: identity.ID()}, observability.Actor("system"), map[string]any{"reason": "disabled", "message": "account disabled"})
		return nil, ErrPasscodeInvalid
	}
	if !VerifyPasscode(identity.PasscodeHash(), passcodePlain) {
		_ = emitEvent(ctx, s.sink, EvtAuthSigninFailed, observability.EventRefs{IdentityID: identity.ID()}, observability.Actor("system"), map[string]any{"reason": "bad_passcode", "message": "passcode mismatch"})
		return nil, ErrPasscodeInvalid
	}
	token, err := MintJWT(identity.ID(), s.signingKey)
	if err != nil {
		return nil, err
	}
	claims, _ := VerifyJWT(token, s.signingKey)
	jti := ""
	if claims != nil {
		jti = claims.Jti
	}
	actor := observability.Actor("user:" + identity.ID())
	_ = emitEvent(ctx, s.sink, EvtAuthSignedIn, observability.EventRefs{IdentityID: identity.ID()}, actor, map[string]any{"jti": jti})
	// v2.7.1 #214: stamp the last successful-signin time (powers Humans list +
	// UserDetail last_session_at). Best-effort — a stamp write failure must NOT
	// fail an otherwise-valid signin.
	identity.RecordSession(time.Now())
	_ = s.identities.Update(ctx, identity)
	return &SigninResult{IdentityID: identity.ID(), JWT: token, Jti: jti}, nil
}

// ============================================================
// SignoutService — emit auth.signed_out (stateless JWT; cookie cleared by HTTP layer)
// ============================================================

// SignoutService records signout events. JWT invalidation is out-of-scope for
// v2.6 (stateless tokens; cookie cleared by the HTTP handler).
type SignoutService struct {
	sink *observability.EventSink
}

// NewSignoutService constructs the service.
func NewSignoutService(sink *observability.EventSink) *SignoutService {
	return &SignoutService{sink: sink}
}

// Execute emits auth.signed_out for the given identity.
func (s *SignoutService) Execute(ctx context.Context, identityID, jti string) error {
	actor := observability.Actor("user:" + identityID)
	return emitEvent(ctx, s.sink, EvtAuthSignedOut, observability.EventRefs{IdentityID: identityID}, actor, map[string]any{"jti": jti})
}

// ============================================================
// AuthService — JWT verify + DS-4 identity status check
// ============================================================

// AuthService verifies a JWT and hydrates the current Identity (DS-4).
// Used by HTTP middleware to authenticate requests.
type AuthService struct {
	identities IdentityRepository
	signingKey []byte
}

// NewAuthService constructs the service.
func NewAuthService(identities IdentityRepository, signingKey []byte) *AuthService {
	return &AuthService{identities: identities, signingKey: signingKey}
}

// AuthenticateToken verifies the JWT and returns the active Identity.
// Returns ErrUnauthenticated on any failure (bad token, expired, disabled).
func (s *AuthService) AuthenticateToken(ctx context.Context, jwtToken string) (*Identity, error) {
	claims, err := VerifyJWT(jwtToken, s.signingKey)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	identity, err := s.identities.GetByID(ctx, claims.Sub)
	if err != nil || identity == nil {
		return nil, ErrUnauthenticated
	}
	// DS-4: every request checks account_status.
	if identity.AccountStatus() == AccountDisabled {
		return nil, ErrUnauthenticated
	}
	return identity, nil
}

// ============================================================
// IdentityBCFacade — read-only queries for downstream BCs
// ============================================================

// IdentityBCFacade provides read-only access to Identity BC data for
// downstream BCs (AS-1/AS-2/AS-3 in v2.6-design § 4.8.3).
type IdentityBCFacade struct {
	identities IdentityRepository
	orgs       OrganizationRepository
	members    MemberRepository
}

// NewIdentityBCFacade constructs the facade.
func NewIdentityBCFacade(
	identities IdentityRepository,
	orgs OrganizationRepository,
	members MemberRepository,
) *IdentityBCFacade {
	return &IdentityBCFacade{identities: identities, orgs: orgs, members: members}
}

// IdentityExists returns true if an Identity with the given id exists.
func (f *IdentityBCFacade) IdentityExists(ctx context.Context, identityID string) bool {
	id, _ := f.identities.GetByID(ctx, identityID)
	return id != nil
}

// GetActiveOrganization returns an active (non-deleted) Organization or nil.
func (f *IdentityBCFacade) GetActiveOrganization(ctx context.Context, orgID string) (*Organization, error) {
	org, err := f.orgs.GetByID(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if org.IsDeleted() {
		return nil, ErrOrganizationDeleted
	}
	return org, nil
}

// GetMemberForOrganization returns the joined Member for (org, identity) or nil.
func (f *IdentityBCFacade) GetMemberForOrganization(ctx context.Context, orgID, identityID string) (*Member, error) {
	return f.members.GetByOrganizationAndIdentity(ctx, orgID, identityID)
}

// ============================================================
// emitEvent — nil-safe helper used by all identity services
// ============================================================

// emitEvent emits an event via sink. No-ops if sink is nil.
func emitEvent(ctx context.Context, sink *observability.EventSink, evtType observability.EventType, refs observability.EventRefs, actor observability.Actor, payload map[string]any) error {
	if sink == nil {
		return nil
	}
	_, err := sink.Emit(ctx, observability.EmitCommand{
		EventType: evtType,
		Refs:      refs,
		Actor:     actor,
		Payload:   payload,
	})
	return err
}

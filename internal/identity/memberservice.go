package identity

import (
	"context"
	cryptoRand "crypto/rand"
	"database/sql"
	"errors"
	"fmt"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ============================================================
// MemberRoleChangeService — DS-2 (last-owner guard)
// ============================================================

// ErrLastOwnerCannotChangeRole is returned when demoting the last owner.
var ErrLastOwnerCannotChangeRole = errors.New("member: cannot demote last owner of organization")

// MemberRoleChangeService changes a member's role with last-owner protection.
type MemberRoleChangeService struct {
	db      *sql.DB
	members MemberRepository
	lock    *OrganizationLockManager
	sink    *observability.EventSink
}

// NewMemberRoleChangeService constructs the service.
func NewMemberRoleChangeService(db *sql.DB, members MemberRepository, lock *OrganizationLockManager) *MemberRoleChangeService {
	return &MemberRoleChangeService{db: db, members: members, lock: lock}
}

// NewMemberRoleChangeServiceWithSink constructs the service with an event sink.
func NewMemberRoleChangeServiceWithSink(db *sql.DB, members MemberRepository, lock *OrganizationLockManager, sink *observability.EventSink) *MemberRoleChangeService {
	return &MemberRoleChangeService{db: db, members: members, lock: lock, sink: sink}
}

// Change updates the member's role. Rejects if the change would leave an
// organization with no active owner (DS-2, M3 invariant).
func (s *MemberRoleChangeService) Change(ctx context.Context, memberID string, newRole MemberRole, changedBy string) error {
	var orgID string
	m, err := s.members.GetByID(ctx, memberID)
	if err != nil {
		return err
	}
	orgID = m.OrganizationID()

	return s.lock.WithLock(orgID, func() error {
		return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			member, err := s.members.GetByID(txCtx, memberID)
			if err != nil {
				return err
			}
			oldRole := member.Role()
			if oldRole == RoleOwner && newRole != RoleOwner {
				count, err := s.members.CountActiveOwners(txCtx, member.OrganizationID())
				if err != nil {
					return err
				}
				if count <= 1 {
					return ErrLastOwnerCannotChangeRole
				}
			}
			member.ChangeRole(newRole)
			if err := s.members.Save(txCtx, member); err != nil {
				return err
			}
			actor := observability.Actor("user:" + changedBy)
			refs := observability.EventRefs{MemberID: memberID, OrganizationID: member.OrganizationID(), IdentityID: member.IdentityID()}
			return emitEvent(txCtx, s.sink, EvtMemberRoleChanged, refs, actor, map[string]any{
				"old_role": string(oldRole),
				"new_role": string(newRole),
			})
		})
	})
}

// ============================================================
// MemberDisableService — DS-2 (last-owner guard) + DS-5 (idempotent)
// ============================================================

// ErrLastOwnerCannotDisable is returned when trying to disable the last owner.
var ErrLastOwnerCannotDisable = errors.New("member: cannot disable last owner of organization")

// MemberDisableService disables or re-enables member access.
type MemberDisableService struct {
	db      *sql.DB
	members MemberRepository
	lock    *OrganizationLockManager
	sink    *observability.EventSink
}

// NewMemberDisableService constructs the service.
func NewMemberDisableService(db *sql.DB, members MemberRepository, lock *OrganizationLockManager) *MemberDisableService {
	return &MemberDisableService{db: db, members: members, lock: lock}
}

// NewMemberDisableServiceWithSink constructs the service with an event sink.
func NewMemberDisableServiceWithSink(db *sql.DB, members MemberRepository, lock *OrganizationLockManager, sink *observability.EventSink) *MemberDisableService {
	return &MemberDisableService{db: db, members: members, lock: lock, sink: sink}
}

// Disable disables a member (idempotent if already disabled).
func (s *MemberDisableService) Disable(ctx context.Context, memberID, reason string) error {
	m, err := s.members.GetByID(ctx, memberID)
	if err != nil {
		return err
	}
	orgID := m.OrganizationID()

	return s.lock.WithLock(orgID, func() error {
		return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			member, err := s.members.GetByID(txCtx, memberID)
			if err != nil {
				return err
			}
			if member.Status() == MemberDisabled {
				return nil // idempotent
			}
			if member.Role() == RoleOwner {
				count, err := s.members.CountActiveOwners(txCtx, member.OrganizationID())
				if err != nil {
					return err
				}
				if count <= 1 {
					return ErrLastOwnerCannotDisable
				}
			}
			member.Disable(reason)
			if err := s.members.Save(txCtx, member); err != nil {
				return err
			}
			refs := observability.EventRefs{MemberID: memberID, OrganizationID: member.OrganizationID(), IdentityID: member.IdentityID()}
			return emitEvent(txCtx, s.sink, EvtMemberDisabled, refs, observability.Actor("system"), map[string]any{"reason": reason, "message": "member disabled"})
		})
	})
}

// ReEnable re-enables a disabled member.
func (s *MemberDisableService) ReEnable(ctx context.Context, memberID string) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		member, err := s.members.GetByID(txCtx, memberID)
		if err != nil {
			return err
		}
		if member.Status() == MemberJoined {
			return nil // already joined; idempotent
		}
		member.ReEnable()
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}
		refs := observability.EventRefs{MemberID: memberID, OrganizationID: member.OrganizationID(), IdentityID: member.IdentityID()}
		return emitEvent(txCtx, s.sink, EvtMemberReEnabled, refs, observability.Actor("system"), nil)
	})
}

// ============================================================
// OrganizationCreateService — DS-4: create Org + owner Member in one TX
// ============================================================

// OrganizationCreateService creates an organization for an existing identity.
type OrganizationCreateService struct {
	db      *sql.DB
	orgs    OrganizationRepository
	members MemberRepository
	sink    *observability.EventSink
}

// NewOrganizationCreateService constructs the service.
func NewOrganizationCreateService(db *sql.DB, orgs OrganizationRepository, members MemberRepository) *OrganizationCreateService {
	return &OrganizationCreateService{db: db, orgs: orgs, members: members}
}

// NewOrganizationCreateServiceWithSink constructs the service with an event sink.
func NewOrganizationCreateServiceWithSink(db *sql.DB, orgs OrganizationRepository, members MemberRepository, sink *observability.EventSink) *OrganizationCreateService {
	return &OrganizationCreateService{db: db, orgs: orgs, members: members, sink: sink}
}

// Create atomically creates an Organization + owner Member for identityID.
func (s *OrganizationCreateService) Create(ctx context.Context, slug, name, identityID string) (*Organization, *Member, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, nil, err
	}
	var (
		org    *Organization
		member *Member
	)
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if existing, _ := s.orgs.GetBySlug(txCtx, slug); existing != nil {
			return ErrOrganizationSlugTaken
		}
		var err error
		org, err = OrganizationFactory{}.New(slug, name, identityID)
		if err != nil {
			return err
		}
		if err := s.orgs.Save(txCtx, org); err != nil {
			return err
		}
		member, err = MemberFactory{}.New(org.ID(), identityID, RoleOwner, nil)
		if err != nil {
			return err
		}
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}
		actor := observability.Actor("user:" + identityID)
		refs := observability.EventRefs{OrganizationID: org.ID(), MemberID: member.ID(), IdentityID: identityID}
		if err := emitEvent(txCtx, s.sink, EvtOrganizationCreated, refs, actor, map[string]any{"slug": org.Slug()}); err != nil {
			return err
		}
		return emitEvent(txCtx, s.sink, EvtMemberAdded, refs, actor, map[string]any{"role": string(RoleOwner)})
	})
	if err != nil {
		return nil, nil, err
	}
	return org, member, nil
}

// ============================================================
// OrganizationLifecycleService — DS-3: soft-delete org + cascade members
// ============================================================

// OrganizationLifecycleService manages organization soft-deletion with
// member cascade (DS-3).
type OrganizationLifecycleService struct {
	db      *sql.DB
	orgs    OrganizationRepository
	members MemberRepository
	lock    *OrganizationLockManager
	sink    *observability.EventSink
}

// NewOrganizationLifecycleService constructs the service.
func NewOrganizationLifecycleService(
	db *sql.DB,
	orgs OrganizationRepository,
	members MemberRepository,
	lock *OrganizationLockManager,
) *OrganizationLifecycleService {
	return &OrganizationLifecycleService{db: db, orgs: orgs, members: members, lock: lock}
}

// NewOrganizationLifecycleServiceWithSink constructs the service with an event sink.
func NewOrganizationLifecycleServiceWithSink(
	db *sql.DB,
	orgs OrganizationRepository,
	members MemberRepository,
	lock *OrganizationLockManager,
	sink *observability.EventSink,
) *OrganizationLifecycleService {
	return &OrganizationLifecycleService{db: db, orgs: orgs, members: members, lock: lock, sink: sink}
}

// Delete soft-deletes an organization and all its joined members (DS-3).
// Idempotent if already deleted.
func (s *OrganizationLifecycleService) Delete(ctx context.Context, organizationID string) error {
	return s.lock.WithLock(organizationID, func() error {
		return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			org, err := s.orgs.GetByID(txCtx, organizationID)
			if err != nil {
				return err
			}
			if org.IsDeleted() {
				return nil // idempotent
			}
			// DS-3: cascade disable all joined members in same TX.
			members, err := s.members.ListByOrganization(txCtx, organizationID)
			if err != nil {
				return err
			}
			for _, m := range members {
				if m.Status() == MemberJoined {
					m.Disable("organization-deleted")
					if err := s.members.Save(txCtx, m); err != nil {
						return err
					}
					refs := observability.EventRefs{MemberID: m.ID(), OrganizationID: organizationID, IdentityID: m.IdentityID()}
					if err := emitEvent(txCtx, s.sink, EvtMemberDisabled, refs, observability.Actor("system"), map[string]any{"reason": "organization-deleted", "message": "member disabled due to org deletion"}); err != nil {
						return err
					}
				}
			}
			org.SoftDelete()
			if err := s.orgs.Save(txCtx, org); err != nil {
				return err
			}
			return emitEvent(txCtx, s.sink, EvtOrganizationDeleted, observability.EventRefs{OrganizationID: organizationID}, observability.Actor("system"), nil)
		})
	})
}

// ============================================================
// OrganizationUpdateService — update org name/slug
// ============================================================

// OrganizationUpdateService updates mutable organization fields.
type OrganizationUpdateService struct {
	db   *sql.DB
	orgs OrganizationRepository
	sink *observability.EventSink
}

// NewOrganizationUpdateService constructs the service.
func NewOrganizationUpdateService(db *sql.DB, orgs OrganizationRepository) *OrganizationUpdateService {
	return &OrganizationUpdateService{db: db, orgs: orgs}
}

// NewOrganizationUpdateServiceWithSink constructs the service with an event sink.
func NewOrganizationUpdateServiceWithSink(db *sql.DB, orgs OrganizationRepository, sink *observability.EventSink) *OrganizationUpdateService {
	return &OrganizationUpdateService{db: db, orgs: orgs, sink: sink}
}

// UpdateName updates the organization display name.
func (s *OrganizationUpdateService) UpdateName(ctx context.Context, orgID, name, updatedByIdentityID string) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		org, err := s.orgs.GetByID(txCtx, orgID)
		if err != nil {
			return err
		}
		if org.IsDeleted() {
			return ErrOrganizationDeleted
		}
		org.UpdateName(name)
		if err := s.orgs.Save(txCtx, org); err != nil {
			return err
		}
		actor := observability.Actor("user:" + updatedByIdentityID)
		return emitEvent(txCtx, s.sink, EvtOrganizationUpdated, observability.EventRefs{OrganizationID: orgID}, actor, map[string]any{"field": "name"})
	})
}

// UpdateSlug changes the organization slug after validating format + uniqueness.
// Slug changes affect URL routing; the caller (UI) must redirect afterward.
func (s *OrganizationUpdateService) UpdateSlug(ctx context.Context, orgID, slug, updatedByIdentityID string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		org, err := s.orgs.GetByID(txCtx, orgID)
		if err != nil {
			return err
		}
		if org.IsDeleted() {
			return ErrOrganizationDeleted
		}
		// Uniqueness: reject if another org already holds the slug.
		if existing, _ := s.orgs.GetBySlug(txCtx, slug); existing != nil && existing.ID() != orgID {
			return ErrOrganizationSlugTaken
		}
		if err := org.UpdateSlug(slug); err != nil {
			return err
		}
		if err := s.orgs.Save(txCtx, org); err != nil {
			return err
		}
		actor := observability.Actor("user:" + updatedByIdentityID)
		return emitEvent(txCtx, s.sink, EvtOrganizationUpdated, observability.EventRefs{OrganizationID: orgID}, actor, map[string]any{"field": "slug"})
	})
}

// UpdateDescription updates the organization description.
func (s *OrganizationUpdateService) UpdateDescription(ctx context.Context, orgID, description, updatedByIdentityID string) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		org, err := s.orgs.GetByID(txCtx, orgID)
		if err != nil {
			return err
		}
		if org.IsDeleted() {
			return ErrOrganizationDeleted
		}
		org.UpdateDescription(description)
		if err := s.orgs.Save(txCtx, org); err != nil {
			return err
		}
		actor := observability.Actor("user:" + updatedByIdentityID)
		return emitEvent(txCtx, s.sink, EvtOrganizationUpdated, observability.EventRefs{OrganizationID: orgID}, actor, map[string]any{"field": "description"})
	})
}

// ============================================================
// MemberRemoveService — hard-remove a member record
// ============================================================

// ErrCannotRemoveLastOwner is returned when removing the last owner.
var ErrCannotRemoveLastOwner = errors.New("member: cannot remove last owner of organization")

// MemberRemoveService removes a member from an organization.
type MemberRemoveService struct {
	db      *sql.DB
	members MemberRepository
	lock    *OrganizationLockManager
	sink    *observability.EventSink
}

// NewMemberRemoveService constructs the service.
func NewMemberRemoveService(db *sql.DB, members MemberRepository, lock *OrganizationLockManager) *MemberRemoveService {
	return &MemberRemoveService{db: db, members: members, lock: lock}
}

// NewMemberRemoveServiceWithSink constructs the service with an event sink.
func NewMemberRemoveServiceWithSink(db *sql.DB, members MemberRepository, lock *OrganizationLockManager, sink *observability.EventSink) *MemberRemoveService {
	return &MemberRemoveService{db: db, members: members, lock: lock, sink: sink}
}

// Remove removes a member from an organization. Guards last-owner invariant.
func (s *MemberRemoveService) Remove(ctx context.Context, memberID, removedByIdentityID string) error {
	m, err := s.members.GetByID(ctx, memberID)
	if err != nil {
		return err
	}
	orgID := m.OrganizationID()

	return s.lock.WithLock(orgID, func() error {
		return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
			member, err := s.members.GetByID(txCtx, memberID)
			if err != nil {
				return err
			}
			if member.Role() == RoleOwner {
				count, err := s.members.CountActiveOwners(txCtx, member.OrganizationID())
				if err != nil {
					return err
				}
				if count <= 1 {
					return ErrCannotRemoveLastOwner
				}
			}
			// Disable as removal (v2.6 uses soft-disable; full DELETE is v2.7+).
			member.Disable("removed")
			if err := s.members.Save(txCtx, member); err != nil {
				return err
			}
			actor := observability.Actor("user:" + removedByIdentityID)
			refs := observability.EventRefs{MemberID: memberID, OrganizationID: member.OrganizationID(), IdentityID: member.IdentityID()}
			return emitEvent(txCtx, s.sink, EvtMemberRemoved, refs, actor, nil)
		})
	})
}

// ============================================================
// IdentityAccountService — disable/re-enable identity account
// ============================================================

// IdentityAccountService manages account-level enable/disable for identities.
type IdentityAccountService struct {
	db         *sql.DB
	identities IdentityRepository
	sink       *observability.EventSink
}

// NewIdentityAccountService constructs the service.
func NewIdentityAccountService(db *sql.DB, identities IdentityRepository) *IdentityAccountService {
	return &IdentityAccountService{db: db, identities: identities}
}

// NewIdentityAccountServiceWithSink constructs the service with an event sink.
func NewIdentityAccountServiceWithSink(db *sql.DB, identities IdentityRepository, sink *observability.EventSink) *IdentityAccountService {
	return &IdentityAccountService{db: db, identities: identities, sink: sink}
}

// Disable disables an identity's account (idempotent).
func (s *IdentityAccountService) Disable(ctx context.Context, identityID, reason, disabledByIdentityID string) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		identity, err := s.identities.GetByID(txCtx, identityID)
		if err != nil {
			return err
		}
		if identity.AccountStatus() == AccountDisabled {
			return nil // idempotent
		}
		identity.Disable()
		if err := s.identities.Update(txCtx, identity); err != nil {
			return err
		}
		actor := observability.Actor("user:" + disabledByIdentityID)
		return emitEvent(txCtx, s.sink, EvtIdentityAccountDisabled, observability.EventRefs{IdentityID: identityID}, actor, map[string]any{"reason": reason, "message": "account disabled"})
	})
}

// ReEnable re-enables a disabled identity account (idempotent).
func (s *IdentityAccountService) ReEnable(ctx context.Context, identityID, reEnabledByIdentityID string) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		identity, err := s.identities.GetByID(txCtx, identityID)
		if err != nil {
			return err
		}
		if identity.AccountStatus() == AccountActive {
			return nil // idempotent
		}
		identity.ReEnable()
		if err := s.identities.Update(txCtx, identity); err != nil {
			return err
		}
		actor := observability.Actor("user:" + reEnabledByIdentityID)
		return emitEvent(txCtx, s.sink, EvtIdentityAccountReEnabled, observability.EventRefs{IdentityID: identityID}, actor, nil)
	})
}

// ============================================================
// MemberCreateUserService — create a new user identity + add to org
// ============================================================

// MemberCreateUserResult holds the new identity, member, and temp passcode.
type MemberCreateUserResult struct {
	Identity      *Identity
	Member        *Member
	TempPasscode  string // plaintext temp passcode (returned ONCE; user must change on first signin)
}

// MemberCreateUserService creates a new user Identity and adds it as a member
// of the target organization in a single transaction. Returns a temporary
// passcode that the new user must use to sign in (and should change).
type MemberCreateUserService struct {
	db         *sql.DB
	identities IdentityRepository
	members    MemberRepository
	sink       *observability.EventSink
}

func NewMemberCreateUserService(db *sql.DB, identities IdentityRepository, members MemberRepository) *MemberCreateUserService {
	return &MemberCreateUserService{db: db, identities: identities, members: members}
}

func NewMemberCreateUserServiceWithSink(db *sql.DB, identities IdentityRepository, members MemberRepository, sink *observability.EventSink) *MemberCreateUserService {
	return &MemberCreateUserService{db: db, identities: identities, members: members, sink: sink}
}

// Create generates a temp passcode, creates the Identity + Member, and emits
// `identity.registered` + `member.added` events.
func (s *MemberCreateUserService) Create(ctx context.Context, orgID, displayName, role, addedByIdentityID string) (*MemberCreateUserResult, error) {
	if err := validateDisplayName(displayName); err != nil {
		return nil, err
	}
	mRole := MemberRole(role)
	if !mRole.IsValid() {
		return nil, ErrForbidden
	}
	tempPlain, err := generateTempPasscode()
	if err != nil {
		return nil, err
	}
	hash, err := HashPasscode(tempPlain)
	if err != nil {
		return nil, err
	}
	var result MemberCreateUserResult
	result.TempPasscode = tempPlain
	err = persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if existing, _ := s.identities.GetByDisplayName(txCtx, displayName); existing != nil {
			return ErrIdentityDisplayNameTaken
		}
		ident, err := IdentityFactory{}.NewUser(displayName, hash)
		if err != nil {
			return err
		}
		if err := s.identities.Save(txCtx, ident); err != nil {
			return err
		}
		invitedBy := addedByIdentityID
		member, err := MemberFactory{}.New(orgID, ident.ID(), mRole, &invitedBy)
		if err != nil {
			return err
		}
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}
		result.Identity = ident
		result.Member = member
		actor := observability.Actor("user:" + addedByIdentityID)
		_ = emitEvent(txCtx, s.sink, EvtIdentityCreated, observability.EventRefs{IdentityID: ident.ID()}, actor, map[string]any{"kind": "user", "display_name": displayName})
		return emitEvent(txCtx, s.sink, EvtMemberAdded, observability.EventRefs{IdentityID: ident.ID(), OrganizationID: orgID}, actor, map[string]any{"role": role})
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Character sets used to compose a compliant temp passcode. Each set is
// non-empty so a generated passcode is guaranteed at least one letter, one
// digit, and one symbol — satisfying ValidatePasscodePlain.
const (
	tempPasscodeLetters = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ"
	tempPasscodeDigits  = "23456789"
	tempPasscodeSymbols = "!@#$%^&*-_+=?"
	tempPasscodeLength  = 12
)

// generateTempPasscode returns a random passcode that satisfies
// ValidatePasscodePlain: length 12, with at least one letter, one digit, and
// one symbol. Uses crypto/rand for all selections.
func generateTempPasscode() (string, error) {
	all := tempPasscodeLetters + tempPasscodeDigits + tempPasscodeSymbols
	out := make([]byte, tempPasscodeLength)
	// Guarantee at least one of each required class up front.
	if c, err := randChar(tempPasscodeLetters); err != nil {
		return "", err
	} else {
		out[0] = c
	}
	if c, err := randChar(tempPasscodeDigits); err != nil {
		return "", err
	} else {
		out[1] = c
	}
	if c, err := randChar(tempPasscodeSymbols); err != nil {
		return "", err
	} else {
		out[2] = c
	}
	// Fill the remainder from the full alphabet.
	for i := 3; i < tempPasscodeLength; i++ {
		c, err := randChar(all)
		if err != nil {
			return "", err
		}
		out[i] = c
	}
	// Shuffle so the guaranteed classes are not always in fixed positions.
	if err := cryptoShuffle(out); err != nil {
		return "", err
	}
	return string(out), nil
}

// randChar returns a uniformly random byte from set using crypto/rand.
func randChar(set string) (byte, error) {
	n, err := cryptoRandInt(len(set))
	if err != nil {
		return 0, err
	}
	return set[n], nil
}

// cryptoShuffle performs an in-place Fisher–Yates shuffle using crypto/rand.
func cryptoShuffle(b []byte) error {
	for i := len(b) - 1; i > 0; i-- {
		j, err := cryptoRandInt(i + 1)
		if err != nil {
			return err
		}
		b[i], b[j] = b[j], b[i]
	}
	return nil
}

// cryptoRandInt returns a uniform random int in [0, n) using crypto/rand,
// rejecting values that would introduce modulo bias.
func cryptoRandInt(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("passcode: invalid random bound %d", n)
	}
	// Largest multiple of n that fits in a uint32, to reject bias.
	limit := ^uint32(0) - (^uint32(0) % uint32(n))
	var buf [4]byte
	for {
		if _, err := cryptoRand.Read(buf[:]); err != nil {
			return 0, err
		}
		v := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
		if v < limit {
			return int(v % uint32(n)), nil
		}
	}
}

// ============================================================
// MemberAddService — add an existing identity to an organization
// ============================================================

// MemberAddService adds an existing Identity to an Organization as a member.
type MemberAddService struct {
	db         *sql.DB
	identities IdentityRepository
	members    MemberRepository
	sink       *observability.EventSink
}

// NewMemberAddService constructs the service.
func NewMemberAddService(db *sql.DB, identities IdentityRepository, members MemberRepository) *MemberAddService {
	return &MemberAddService{db: db, identities: identities, members: members}
}

// NewMemberAddServiceWithSink constructs the service with an event sink.
func NewMemberAddServiceWithSink(db *sql.DB, identities IdentityRepository, members MemberRepository, sink *observability.EventSink) *MemberAddService {
	return &MemberAddService{db: db, identities: identities, members: members, sink: sink}
}

// Add finds the identity by display_name and creates a Member in the given org.
// Returns the new Member. Idempotent: returns ErrMemberAlreadyExists if already joined.
func (s *MemberAddService) Add(ctx context.Context, orgID, displayName, role, addedByIdentityID string) (*Member, error) {
	mRole := MemberRole(role)
	if !mRole.IsValid() {
		return nil, ErrForbidden
	}
	var result *Member
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		identity, err := s.identities.GetByDisplayName(txCtx, displayName)
		if err != nil || identity == nil {
			return ErrIdentityNotFound
		}
		// Idempotency: check existing membership.
		existing, _ := s.members.GetByOrganizationAndIdentity(txCtx, orgID, identity.ID())
		if existing != nil {
			return ErrMemberAlreadyExists
		}
		invitedBy := addedByIdentityID
		member, err := MemberFactory{}.New(orgID, identity.ID(), mRole, &invitedBy)
		if err != nil {
			return err
		}
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}
		result = member
		refs := observability.EventRefs{IdentityID: identity.ID(), OrganizationID: orgID}
		actor := observability.Actor("user:" + addedByIdentityID)
		return emitEvent(txCtx, s.sink, EvtMemberAdded, refs, actor, map[string]any{"role": role})
	})
	return result, err
}

// ============================================================
// PasscodeChangeService — change identity passcode
// ============================================================

// PasscodeChangeService changes a user identity's passcode.
type PasscodeChangeService struct {
	db         *sql.DB
	identities IdentityRepository
	sink       *observability.EventSink
}

// NewPasscodeChangeService constructs the service.
func NewPasscodeChangeService(db *sql.DB, identities IdentityRepository) *PasscodeChangeService {
	return &PasscodeChangeService{db: db, identities: identities}
}

// NewPasscodeChangeServiceWithSink constructs the service with an event sink.
func NewPasscodeChangeServiceWithSink(db *sql.DB, identities IdentityRepository, sink *observability.EventSink) *PasscodeChangeService {
	return &PasscodeChangeService{db: db, identities: identities, sink: sink}
}

// Change verifies the current passcode and replaces it with a new one.
func (s *PasscodeChangeService) Change(ctx context.Context, identityID, currentPlain, newPlain string) error {
	if err := ValidatePasscodePlain(newPlain); err != nil {
		return err
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		identity, err := s.identities.GetByID(txCtx, identityID)
		if err != nil {
			return err
		}
		if !VerifyPasscode(identity.PasscodeHash(), currentPlain) {
			return ErrPasscodeInvalid
		}
		newHash, err := HashPasscode(newPlain)
		if err != nil {
			return err
		}
		identity.SetPasscode(newHash)
		if err := s.identities.Update(txCtx, identity); err != nil {
			return err
		}
		actor := observability.Actor("user:" + identityID)
		return emitEvent(txCtx, s.sink, EvtIdentityPasscodeChanged, observability.EventRefs{IdentityID: identityID}, actor, nil)
	})
}

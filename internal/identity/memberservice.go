package identity

import (
	"context"
	"database/sql"
	"errors"

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
}

// NewMemberRoleChangeService constructs the service.
func NewMemberRoleChangeService(db *sql.DB, members MemberRepository, lock *OrganizationLockManager) *MemberRoleChangeService {
	return &MemberRoleChangeService{db: db, members: members, lock: lock}
}

// Change updates the member's role. Rejects if the change would leave an
// organization with no active owner (DS-2, M3 invariant).
func (s *MemberRoleChangeService) Change(ctx context.Context, memberID string, newRole MemberRole, changedBy string) error {
	var orgID string
	// Pre-fetch org ID outside the lock to get the correct mutex key.
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
			return s.members.Save(txCtx, member)
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
}

// NewMemberDisableService constructs the service.
func NewMemberDisableService(db *sql.DB, members MemberRepository, lock *OrganizationLockManager) *MemberDisableService {
	return &MemberDisableService{db: db, members: members, lock: lock}
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
			return s.members.Save(txCtx, member)
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
		return s.members.Save(txCtx, member)
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
}

// NewOrganizationCreateService constructs the service.
func NewOrganizationCreateService(db *sql.DB, orgs OrganizationRepository, members MemberRepository) *OrganizationCreateService {
	return &OrganizationCreateService{db: db, orgs: orgs, members: members}
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
		return s.members.Save(txCtx, member)
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
				}
			}
			org.SoftDelete()
			return s.orgs.Save(txCtx, org)
		})
	})
}

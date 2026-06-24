package identity

import (
	"strings"
	"time"
)

// Organization is the BC9 Organization AR (v2.6, ADR-0041).
//
// Invariants:
//
//	O1: slug must match regex ^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])?$
//	O2: slug unique among non-deleted organizations (enforced by DB + repo)
//	O3: soft-delete cascades members (enforced by OrganizationLifecycleService)
//	O4: creation also creates creator's owner Member (enforced by SignupService / OrganizationCreateService)
type Organization struct {
	id                  string
	slug                string
	name                string
	description         string
	createdByIdentityID string
	createdAt           time.Time
	updatedAt           time.Time
	deletedAt           *time.Time
	// disabledAt is the I41 (T470) org-login gate. A DISABLED org is distinct from
	// a DELETED one: it is reversible (Enable) and stays resolvable by slug, so the
	// owner can still enter it (full access) to manage / re-enable. The login gate
	// (requireOrgMember) blocks NON-owner members of a disabled org; the owner is
	// never affected. nil = enabled.
	disabledAt *time.Time
}

// OrganizationFactory creates Organization instances.
type OrganizationFactory struct{}

// New creates a new Organization. slug is validated (O1).
func (OrganizationFactory) New(slug, name, createdByIdentityID string) (*Organization, error) {
	if err := ValidateSlug(slug); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrOrganizationNotFound // repurpose to "name required"
	}
	if len(name) > 80 {
		return nil, ErrOrganizationNotFound
	}
	now := time.Now().UTC()
	return &Organization{
		id:                  NewOrganizationID(),
		slug:                slug,
		name:                name,
		createdByIdentityID: createdByIdentityID,
		createdAt:           now,
		updatedAt:           now,
	}, nil
}

// RehydrateOrganization reconstructs from DB data. Used by repositories.
func RehydrateOrganization(
	id, slug, name, description, createdByIdentityID string,
	createdAt, updatedAt time.Time,
	deletedAt, disabledAt *time.Time,
) *Organization {
	return &Organization{
		id:                  id,
		slug:                slug,
		name:                name,
		description:         description,
		createdByIdentityID: createdByIdentityID,
		createdAt:           createdAt.UTC(),
		updatedAt:           updatedAt.UTC(),
		deletedAt:           deletedAt,
		disabledAt:          disabledAt,
	}
}

// Getters.

func (o *Organization) ID() string                  { return o.id }
func (o *Organization) Slug() string                { return o.slug }
func (o *Organization) Name() string                { return o.name }
func (o *Organization) Description() string         { return o.description }
func (o *Organization) CreatedByIdentityID() string { return o.createdByIdentityID }
func (o *Organization) CreatedAt() time.Time        { return o.createdAt }
func (o *Organization) UpdatedAt() time.Time        { return o.updatedAt }
func (o *Organization) DeletedAt() *time.Time       { return o.deletedAt }
func (o *Organization) IsDeleted() bool             { return o.deletedAt != nil }
func (o *Organization) DisabledAt() *time.Time      { return o.disabledAt }
func (o *Organization) IsDisabled() bool            { return o.disabledAt != nil }

// UpdateName sets the organization name (admin+).
func (o *Organization) UpdateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return ErrOrganizationNotFound
	}
	o.name = name
	o.updatedAt = time.Now().UTC()
	return nil
}

// UpdateDescription sets the description.
func (o *Organization) UpdateDescription(desc string) {
	o.description = desc
	o.updatedAt = time.Now().UTC()
}

// UpdateSlug changes the slug (owner only). New slug is validated.
func (o *Organization) UpdateSlug(slug string) error {
	if err := ValidateSlug(slug); err != nil {
		return err
	}
	o.slug = slug
	o.updatedAt = time.Now().UTC()
	return nil
}

// SoftDelete marks the organization as deleted.
func (o *Organization) SoftDelete() {
	now := time.Now().UTC()
	o.deletedAt = &now
	o.updatedAt = now
}

// Disable marks the organization disabled (I41 / T470). Reversible via Enable;
// idempotent. A disabled org stays resolvable by slug (unlike SoftDelete) so the
// owner can still enter it — the non-owner login gate lives in requireOrgMember.
func (o *Organization) Disable() {
	if o.disabledAt != nil {
		return
	}
	now := time.Now().UTC()
	o.disabledAt = &now
	o.updatedAt = now
}

// Enable clears the disabled state (I41 / T470). Idempotent.
func (o *Organization) Enable() {
	if o.disabledAt == nil {
		return
	}
	o.disabledAt = nil
	o.updatedAt = time.Now().UTC()
}

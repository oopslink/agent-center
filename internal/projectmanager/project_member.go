package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// ProjectMember is who is in a Project. In v1 it is the delivery scope AND the
// minimum write-gate (plan §10 OQ6): being a member is the precondition for
// any write in that Project. There is NO intra-project role subdivision in
// v2.7 — the Role field exists for the roadmap permission model but is not
// enforced. Org membership is a prerequisite (checked by the AppService).
type ProjectMember struct {
	id         MemberID
	projectID  ProjectID
	identityID IdentityRef
	role       ProjectMemberRole
	addedBy    IdentityRef
	createdAt  time.Time
}

// NewProjectMemberInput captures constructor args.
type NewProjectMemberInput struct {
	ID         MemberID
	ProjectID  ProjectID
	IdentityID IdentityRef
	Role       ProjectMemberRole
	AddedBy    IdentityRef
	CreatedAt  time.Time
}

// NewProjectMember constructs a member record.
func NewProjectMember(in NewProjectMemberInput) (*ProjectMember, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: member id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if err := in.IdentityID.Validate(); err != nil {
		return nil, err
	}
	role := in.Role
	if role == "" {
		role = RoleMember
	}
	if !role.IsValid() {
		return nil, errors.New("projectmanager: invalid project member role")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	return &ProjectMember{
		id:         in.ID,
		projectID:  in.ProjectID,
		identityID: in.IdentityID,
		role:       role,
		addedBy:    in.AddedBy,
		createdAt:  in.CreatedAt.UTC(),
	}, nil
}

// RehydrateProjectMember reconstructs from storage.
func RehydrateProjectMember(in NewProjectMemberInput) (*ProjectMember, error) {
	return NewProjectMember(in)
}

// Getters.
func (m *ProjectMember) ID() MemberID            { return m.id }
func (m *ProjectMember) ProjectID() ProjectID    { return m.projectID }
func (m *ProjectMember) IdentityID() IdentityRef { return m.identityID }
func (m *ProjectMember) Role() ProjectMemberRole { return m.role }
func (m *ProjectMember) AddedBy() IdentityRef    { return m.addedBy }
func (m *ProjectMember) CreatedAt() time.Time    { return m.createdAt }

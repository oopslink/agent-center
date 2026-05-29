// Package projectmanager is the ProjectManager bounded context (v2.7,
// ADR-0046): the single work-management truth for Projects, ProjectMembers,
// Issues, Tasks, their subscriber truth, and their state transitions.
//
// Boundaries (plan §1, ADR-0046):
//   - A Task/Issue belongs to exactly one Project; there are no global or
//     cross-Project work items.
//   - State NEVER changes by inference from Conversation messages — only
//     through this BC's explicit AppServices.
//   - Conversation participants mirror effective subscribers (ADR-0052), but
//     the subscriber truth lives HERE, not in Conversation.
//
// B1 (task #96) ships the aggregates + repositories + state machines. The
// AppServices + outbox-driven participant projection land in B2 (#97).
package projectmanager

import (
	"errors"
	"strings"
)

// Typed identifiers (conventions § 0.3).
type (
	ProjectID string
	IssueID   string
	TaskID    string
	MemberID  string
	// IdentityRef mirrors the kind-prefixed identity vocabulary (ADR-0033):
	// `user:<id>` / `agent:<id>` / `system`.
	IdentityRef string
)

func (id ProjectID) String() string  { return string(id) }
func (id IssueID) String() string    { return string(id) }
func (id TaskID) String() string     { return string(id) }
func (id MemberID) String() string   { return string(id) }
func (r IdentityRef) String() string { return string(r) }

// Validate enforces the kind-prefixed identity vocabulary (ADR-0033).
func (r IdentityRef) Validate() error {
	s := string(r)
	if s == "" {
		return errors.New("projectmanager: identity ref required")
	}
	if s == "system" {
		return nil
	}
	for _, p := range []string{"user:", "agent:"} {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return nil
		}
	}
	return errors.New("projectmanager: identity ref must be 'system' or 'user:<id>' / 'agent:<id>' (ADR-0033)")
}

// ProjectMemberRole — v1 has domain isolation, NOT role permissions
// (plan §10 OQ6): membership is the minimum write-gate; all members have equal
// capability. The role field exists for the roadmap permission model but is
// not enforced in v2.7.
type ProjectMemberRole string

const (
	RoleMember ProjectMemberRole = "member"
	RoleOwner  ProjectMemberRole = "owner"
)

// IsValid reports enum membership.
func (r ProjectMemberRole) IsValid() bool {
	return r == RoleMember || r == RoleOwner
}

// Sentinel errors.
var (
	ErrProjectNotFound     = errors.New("projectmanager: project not found")
	ErrProjectExists       = errors.New("projectmanager: project already exists")
	ErrMemberNotFound      = errors.New("projectmanager: project member not found")
	ErrMemberExists        = errors.New("projectmanager: project member already exists")
	ErrIssueNotFound       = errors.New("projectmanager: issue not found")
	ErrIssueExists         = errors.New("projectmanager: issue already exists")
	ErrTaskNotFound        = errors.New("projectmanager: task not found")
	ErrTaskExists          = errors.New("projectmanager: task already exists")
	ErrSubscriberNotFound  = errors.New("projectmanager: subscriber not found")
	ErrCodeRepoRefNotFound = errors.New("projectmanager: code repo ref not found")
	ErrCrossProject        = errors.New("projectmanager: cross-project operation rejected (scope invariant)")
	ErrInvalidStatus       = errors.New("projectmanager: invalid status")
	ErrIllegalTransition   = errors.New("projectmanager: illegal status transition")
	ErrBlockReasonRequired = errors.New("projectmanager: blocked requires a reason (plan §2.2)")
	ErrSelfVerify          = errors.New("projectmanager: an identity cannot verify a task it completed (plan §2.2/OQ4)")
	ErrVersionConflict     = errors.New("projectmanager: version conflict (optimistic lock)")
	ErrEmptyProjectScope   = errors.New("projectmanager: project_id required (no global work items)")
)

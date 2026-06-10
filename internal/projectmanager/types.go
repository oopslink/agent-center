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
	PlanID    string
	// IdentityRef mirrors the kind-prefixed identity vocabulary (ADR-0033):
	// `user:<id>` / `agent:<id>` / `system`.
	IdentityRef string
)

func (id ProjectID) String() string  { return string(id) }
func (id IssueID) String() string    { return string(id) }
func (id TaskID) String() string     { return string(id) }
func (id MemberID) String() string   { return string(id) }
func (id PlanID) String() string     { return string(id) }
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
	ErrCrossOrgAssignee    = errors.New("projectmanager: assignee agent is not in the project's organization (OQ6: org membership is the prerequisite for project membership)")
	// ErrAgentDirectoryUnavailable is returned (fail-closed) when an agent is
	// assigned but no AgentDirectory is wired to verify the agent's org — a
	// missing dependency must not silently bypass the cross-org guard.
	ErrAgentDirectoryUnavailable = errors.New("projectmanager: agent directory unavailable — cannot verify assignee agent's organization")
	// Plan orchestration (v2.9 #283).
	ErrEmptyPlanName         = errors.New("projectmanager: plan name required")
	ErrPlanCycle             = errors.New("projectmanager: dependency would create a cycle")
	ErrSelfDependency        = errors.New("projectmanager: a task cannot depend on itself")
	ErrIllegalPlanTransition = errors.New("projectmanager: illegal plan status transition")
	ErrInvalidPlanStatus     = errors.New("projectmanager: invalid plan status")
	ErrPlanNotDraft          = errors.New("projectmanager: plan dependencies/tasks editable only in draft")
	ErrPlanNotFound          = errors.New("projectmanager: plan not found")
	ErrPlanExists            = errors.New("projectmanager: plan already exists")
	// ErrTaskInOtherPlan rejects selecting a task into a Plan when it already
	// belongs to a DIFFERENT Plan (Task ↔ Plan = 0..1, design §2). Re-selecting
	// into the SAME plan is a no-op (not an error).
	ErrTaskInOtherPlan = errors.New("projectmanager: task already belongs to another plan")
	// ErrPlanProjectMismatch rejects selecting a task whose project differs from
	// the Plan's project (a Plan selects only its own project's backlog, §2/§9.6d).
	ErrPlanProjectMismatch = errors.New("projectmanager: task and plan belong to different projects")
)

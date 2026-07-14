// Package team hosts the Team BC tactical types (Team S1 data layer, design
// §2/§4/§9):
//
//   - Aggregate Root: Team (with its declared RoleConfigs).
//   - Value Objects: TeamID, MemberRef, MemberKind, RoleConfig.
//   - Association records: TeamMember, TeamProject.
//   - Repository interface + sentinel errors (see repository.go / errors.go).
//
// Roles are NOT hardcoded: each team template declares its own set of roles
// (design §9), so a role's cli / model / capability_tags / concurrency live
// per (team, role) in RoleConfig.
package team

import (
	"strings"
	"time"
)

// TeamID is the user-facing team identifier ("team-<8hex>", idgen §187 shape).
type TeamID string

// String returns the typed value as a plain string.
func (id TeamID) String() string { return string(id) }

// MemberKind classifies a team member. It drives the agent-exclusivity rule:
// an agent belongs to at most one team, a human may belong to many.
type MemberKind string

const (
	// MemberKindAgent is an execution agent — subject to the exclusivity rule.
	MemberKindAgent MemberKind = "agent"
	// MemberKindHuman is a human user — may join multiple teams.
	MemberKindHuman MemberKind = "human"
)

// IsValid reports whether k is a known kind.
func (k MemberKind) IsValid() bool {
	switch k {
	case MemberKindAgent, MemberKindHuman:
		return true
	}
	return false
}

// String returns the underlying kind string.
func (k MemberKind) String() string { return string(k) }

// MemberRef is an identity reference to a team member. It follows the same
// scheme the rest of the platform uses for assignees: "agent:<id>" for an
// execution agent and "user:<id>" for a human. The kind is derived from the
// prefix — see Kind.
type MemberRef string

// String returns the raw ref.
func (r MemberRef) String() string { return string(r) }

// BareID returns the identity id portion of the ref (everything after the first
// ":"), i.e. "agent:agent-123" → "agent-123". A prefix-less ref returns itself
// unchanged. This is the id used to look the identity up in the directory.
func (r MemberRef) BareID() string {
	s := string(r)
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Kind derives the MemberKind from the ref prefix. It returns ErrInvalidMemberRef
// when the ref is empty, prefix-less, or carries an empty id.
func (r MemberRef) Kind() (MemberKind, error) {
	ref := string(r)
	switch {
	case strings.HasPrefix(ref, "agent:"):
		if strings.TrimSpace(ref[len("agent:"):]) == "" {
			return "", ErrInvalidMemberRef
		}
		return MemberKindAgent, nil
	case strings.HasPrefix(ref, "user:"):
		if strings.TrimSpace(ref[len("user:"):]) == "" {
			return "", ErrInvalidMemberRef
		}
		return MemberKindHuman, nil
	default:
		return "", ErrInvalidMemberRef
	}
}

// RoleConfig is a per-team role declaration (design §9). Roles are template-
// defined, not hardcoded, so each carries its own execution config.
type RoleConfig struct {
	// Role is the template-defined role name (e.g. "reviewer", "impl"). Unique
	// within a team.
	Role string
	// CLI is the agent CLI this role runs on (e.g. "claude-code").
	CLI string
	// Model is the model id this role uses (e.g. "claude-opus-4-8").
	Model string
	// CapabilityTags are free-form capability requirements for the role.
	CapabilityTags []string
	// MaxConcurrency caps how many members of this role may run at once.
	MaxConcurrency int
}

// TeamMember is a membership record binding a MemberRef to a team under a role.
type TeamMember struct {
	TeamID    TeamID
	Ref       MemberRef
	Kind      MemberKind
	Role      string
	CreatedAt time.Time
}

// TeamProject records a Team ↔ Project association.
type TeamProject struct {
	TeamID    TeamID
	ProjectID string
	CreatedAt time.Time
}

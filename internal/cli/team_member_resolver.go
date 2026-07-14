package cli

import (
	"context"
	"errors"

	"github.com/oopslink/agent-center/internal/identity"
	"github.com/oopslink/agent-center/internal/team"
	teamservice "github.com/oopslink/agent-center/internal/team/service"
	teamsql "github.com/oopslink/agent-center/internal/team/sqlite"
)

// newHardenedTeamService is the ONE constructor for the Team service. Every wiring
// site (admin API + both webconsole facade paths) MUST go through it so the
// add-member identity hardening (MemberResolver) can never be forgotten on a path
// — the exact "one consumer patched, another path missed" gap that first left the
// web facade unhardened while admin/MCP was covered. Nil identity repos degrade to
// a nil resolver (pass-through) rather than failing closed.
func newHardenedTeamService(a *App) *teamservice.Service {
	return teamservice.New(teamsql.NewRepo(a.DB), a.DB, a.IDGen, a.Clock).
		WithMemberResolver(newIdentityMemberResolver(a.IdentityRepo, a.IdentityMemberRepo))
}

// identityMemberResolver bridges the Team BC's teamservice.MemberResolver seam to
// the identity BC: it answers whether a team member ref points at a real identity
// of the matching kind that is a joined member of the org. Wired into the team
// service (admin_wiring) so AddMember rejects dangling / cross-org / kind-
// mismatched refs from ANY client — the web facade and the MCP add_member tool
// both funnel through AddMember, so this one seam hardens both.
type identityMemberResolver struct {
	identities identity.IdentityRepository
	members    identity.MemberRepository
}

// newIdentityMemberResolver returns nil when either repo is unwired, so the team
// service degrades (skips the check) rather than failing every add-member.
func newIdentityMemberResolver(ids identity.IdentityRepository, members identity.MemberRepository) teamservice.MemberResolver {
	if ids == nil || members == nil {
		return nil
	}
	return identityMemberResolver{identities: ids, members: members}
}

// MemberExists implements teamservice.MemberResolver. It enforces three
// invariants and returns (false, nil) if any fails (well-formed but unusable ref);
// only a real infrastructure error propagates:
//  1. the ref's identity is a JOINED member of orgID (existence + org scope, one
//     query — GetByOrganizationAndIdentity filters status='joined');
//  2. the identity actually exists (defensive: a member row with no identity);
//  3. the identity's kind matches the ref prefix (agent: ↔ agent, user: ↔ user).
func (r identityMemberResolver) MemberExists(ctx context.Context, orgID string, ref team.MemberRef) (bool, error) {
	kind, err := ref.Kind()
	if err != nil {
		// Malformed ref — caller (MemberRef.Kind in AddMember) already rejected it;
		// treat as absent here too rather than erroring.
		return false, nil
	}
	bareID := ref.BareID()

	// (1) existence within the org (joined member).
	if _, err := r.members.GetByOrganizationAndIdentity(ctx, orgID, bareID); err != nil {
		if errors.Is(err, identity.ErrMemberNotFound) {
			return false, nil
		}
		return false, err
	}

	// (2)+(3) the identity exists and its kind matches the ref prefix.
	ident, err := r.identities.GetByID(ctx, bareID)
	if err != nil {
		if errors.Is(err, identity.ErrIdentityNotFound) {
			return false, nil
		}
		return false, err
	}
	return kindMatches(kind, ident.Kind()), nil
}

// kindMatches maps the team member kind (from the ref prefix) onto the identity
// kind and reports whether they agree.
func kindMatches(mk team.MemberKind, ik identity.IdentityKind) bool {
	switch mk {
	case team.MemberKindAgent:
		return ik == identity.KindAgent
	case team.MemberKindHuman:
		return ik == identity.KindUser
	default:
		return false
	}
}

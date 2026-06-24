package identity

import "github.com/oopslink/agent-center/internal/observability"

// Identity BC domain event types (v2.6-design § 4.4).
const (
	EvtIdentityCreated          observability.EventType = "identity.created"
	EvtIdentityPasscodeChanged  observability.EventType = "identity.passcode_changed"
	EvtIdentityAccountDisabled  observability.EventType = "identity.account_disabled"
	EvtIdentityAccountReEnabled observability.EventType = "identity.account_re_enabled"

	EvtOrganizationCreated observability.EventType = "organization.created"
	EvtOrganizationUpdated observability.EventType = "organization.updated"
	EvtOrganizationDeleted observability.EventType = "organization.deleted"

	EvtMemberAdded       observability.EventType = "member.added"
	EvtMemberRoleChanged observability.EventType = "member.role_changed"
	EvtMemberDisabled    observability.EventType = "member.disabled"
	EvtMemberReEnabled   observability.EventType = "member.re_enabled"
	EvtMemberRemoved     observability.EventType = "member.removed"

	EvtAuthSignedIn     observability.EventType = "auth.signed_in"
	EvtAuthSignedOut    observability.EventType = "auth.signed_out"
	EvtAuthSigninFailed observability.EventType = "auth.signin_failed"
)

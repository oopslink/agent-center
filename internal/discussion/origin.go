package discussion

import "errors"

// Origin classifies the entry point that produced the Issue. Per
// discussion/00-overview § 4.1: 5 callers, mapped to {sync-build vs
// lazy-create} for the Issue↔Conversation linkage.
type Origin string

const (
	// OriginCLI ⇒ lazy-create path (issue.conversation_id starts null).
	OriginCLI Origin = "cli"
	// OriginWebConsole ⇒ sync-build (web posts directly into a known
	// conversation, so the binding is set at create time).
	OriginWebConsole Origin = "web_console"
	// OriginSupervisor ⇒ sync-build (supervisor knows the caller channel).
	OriginSupervisor Origin = "supervisor"
	// OriginAgentOpenIssue ⇒ lazy-create (worker agent triggers via worker
	// daemon → center; same shape as CLI path).
	OriginAgentOpenIssue Origin = "agent_open_issue"
	// OriginDerivedFromConversation ⇒ sync-build (CV4 派生入口 per
	// ADR-0036: `issue open --from-conversation=<c> --select-messages=...`
	// creates the issue + its conversation in one tx; carry-over refs
	// attach to the new conversation).
	OriginDerivedFromConversation Origin = "derived_from_conversation"
)

// IsValid checks enum membership.
func (o Origin) IsValid() bool {
	switch o {
	case OriginCLI, OriginWebConsole, OriginSupervisor,
		OriginAgentOpenIssue, OriginDerivedFromConversation:
		return true
	}
	return false
}

// String returns the enum value.
func (o Origin) String() string { return string(o) }

// NeedsSyncConversationBuild reports whether the origin uses the sync-build
// path (issue + Conversation built in the same tx + issue.conversation_id
// set immediately). The remaining origins use the lazy-create path
// (issue.conversation_id starts null; bind-conversation is a separate step).
func (o Origin) NeedsSyncConversationBuild() bool {
	switch o {
	case OriginWebConsole, OriginSupervisor, OriginDerivedFromConversation:
		return true
	}
	return false
}

// ErrInvalidOrigin is returned by ParseOrigin on unknown input.
var ErrInvalidOrigin = errors.New("discussion: invalid issue origin")

// ParseOrigin returns the Origin enum or ErrInvalidOrigin.
func ParseOrigin(s string) (Origin, error) {
	o := Origin(s)
	if !o.IsValid() {
		return "", ErrInvalidOrigin
	}
	return o, nil
}

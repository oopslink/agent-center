package discussion

import "errors"

// Origin classifies the entry point that produced the Issue. Per
// discussion/00-overview § 4.1: 5 callers, mapped to {sync-build vs
// lazy-create} for the Issue↔Conversation linkage.
type Origin string

const (
	// OriginCLI ⇒ lazy-create path (issue.conversation_id starts null).
	OriginCLI Origin = "cli"
	// OriginWebConsole ⇒ sync-build (web is a conversation channel binding).
	OriginWebConsole Origin = "web_console"
	// OriginFeishuAt ⇒ sync-build (Bridge inbound free-text + supervisor
	// intent → issue open carries the user's current feishu channel).
	OriginFeishuAt Origin = "feishu_at"
	// OriginSupervisor ⇒ sync-build (supervisor knows the caller channel).
	OriginSupervisor Origin = "supervisor"
	// OriginAgentOpenIssue ⇒ lazy-create (worker agent triggers via worker
	// daemon → center; same shape as CLI path).
	OriginAgentOpenIssue Origin = "agent_open_issue"
)

// IsValid checks enum membership.
func (o Origin) IsValid() bool {
	switch o {
	case OriginCLI, OriginWebConsole, OriginFeishuAt, OriginSupervisor, OriginAgentOpenIssue:
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
	case OriginWebConsole, OriginFeishuAt, OriginSupervisor:
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

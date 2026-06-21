package conversation

import "strings"

// OwnerKind identifies the owning-object family behind a Conversation.owner_ref.
// Its string value is used verbatim in wake briefs ("this {kind}"), so keep the
// values lowercase and human-readable.
type OwnerKind string

const (
	OwnerKindChannel OwnerKind = "channel" // id://organizations/{org_id}
	OwnerKindProject OwnerKind = "project" // pm://projects/{project_id}
	OwnerKindIssue   OwnerKind = "issue"   // pm://issues/{issue_id}
	OwnerKindTask    OwnerKind = "task"    // pm://tasks/{task_id}
	OwnerKindPlan    OwnerKind = "plan"    // pm://plans/{plan_id}
)

// OwnerContext is the resolved identity of the object a wake/converse brief is
// "about", derived from a SINGLE owner_ref → context table (T254, I19). It
// replaces the per-scheme branching that previously only understood plans, so
// plan / issue / task / project chats (and their threads) can all tell the agent
// WHICH object id "this {kind}" refers to — not just plans.
type OwnerContext struct {
	// Kind is the owning-object family (plan/issue/task/project/channel).
	Kind OwnerKind
	// Label is the header noun ("Plan", "Issue", "Task", "Project", "Channel").
	Label string
	// IDField is the brief's id key for this kind ("plan_id", "issue_id",
	// "task_id", "project_id"); empty for channel (which is not id-anchored).
	IDField string
	// ID is the object id parsed out of owner_ref (the suffix after the scheme).
	ID string
	// Anchored reports whether the brief should render the id-anchored header
	// ([{Label} chat — "{Name}" ({IDField}=ID)]) and the "this {kind}" anchor
	// note. True for the pm:// owner objects; false for channel, which keeps its
	// own [Channel #name] framing untouched (DM/channel briefs stay byte-stable).
	Anchored bool
	// Name is the resolved human name/title of the object (plan name, issue/task
	// title, project name). ResolveOwnerContext does NOT populate it — name/title
	// resolution is a db/network lookup owned by the env wake projector (T255),
	// which threads it through converseCommandPayload.ConvName before the brief is
	// rendered. An empty Name makes the brief fall back to id-only framing; a
	// name miss must never block a wake.
	Name string
}

// ownerContextRegistry is the single owner_ref → OwnerContext resolution table.
// It is keyed by the scheme prefix constants and MUST stay in lockstep with
// knownOwnerSchemes (context_refs.go): the OQ4 guard test iterates the latter
// and asserts every scheme has an entry here, so a newly-recognised scheme that
// forgets to register trips a red test instead of silently mis-rendering.
var ownerContextRegistry = map[string]OwnerContext{
	ownerRefPlans:    {Kind: OwnerKindPlan, Label: "Plan", IDField: "plan_id", Anchored: true},
	ownerRefIssues:   {Kind: OwnerKindIssue, Label: "Issue", IDField: "issue_id", Anchored: true},
	ownerRefTasks:    {Kind: OwnerKindTask, Label: "Task", IDField: "task_id", Anchored: true},
	ownerRefProjects: {Kind: OwnerKindProject, Label: "Project", IDField: "project_id", Anchored: true},
	ownerRefOrgs:     {Kind: OwnerKindChannel, Label: "Channel", IDField: "", Anchored: false},
}

// ResolveOwnerContext maps a conversation's owner_ref to its OwnerContext. It
// returns ok=false for the empty (dm) ref and for any unrecognised scheme, so
// callers fall back to their generic framing. The returned ID is the suffix
// after the scheme; Name is left empty for the caller to fill from the
// env-resolved title (see OwnerContext.Name).
func ResolveOwnerContext(ownerRef string) (OwnerContext, bool) {
	s := strings.TrimSpace(ownerRef)
	if s == "" {
		return OwnerContext{}, false
	}
	for _, prefix := range knownOwnerSchemes {
		if strings.HasPrefix(s, prefix) && len(s) > len(prefix) {
			oc := ownerContextRegistry[prefix]
			oc.ID = s[len(prefix):]
			return oc, true
		}
	}
	return OwnerContext{}, false
}

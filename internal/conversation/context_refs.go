package conversation

import (
	"encoding/json"
	"strings"
)

// v2.7 A0 value objects (ADR-0047): Conversation.owner_ref URI, Message
// context_refs, and the unified MessageAttachment. Kept here to keep types.go
// focused on the pre-existing enums.

// OwnerRef is a Conversation's URI reference back to its owning object
// (ADR-0047 §2, finalized plan §10 OQ10):
//
//	channel:      id://organizations/{org_id}   (generic Org-level group chat)
//	issue:        pm://issues/{issue_id}
//	task:         pm://tasks/{task_id}
//	dm:           (empty — no owner_ref)
//
// channel owner_ref pins it to exactly one Org (no cross-org); issue/task pin
// to their ProjectManager object. It is a weak cross-BC reference (soft
// constraint). A channel may additionally carry a nullable project_ref soft
// label (see Conversation.ProjectRef) — that is grouping-only, NOT ownership.
type OwnerRef string

const (
	ownerRefProjects = "pm://projects/"
	ownerRefIssues   = "pm://issues/"
	ownerRefTasks    = "pm://tasks/"
	ownerRefPlans    = "pm://plans/"
	ownerRefOrgs     = "id://organizations/"
)

// knownOwnerSchemes is the canonical list of owner_ref scheme prefixes the
// domain recognises. WellFormed accepts exactly these (plus the empty dm ref),
// and it is the iteration source for the OQ4 exhaustiveness guard, which asserts
// every scheme here is also registered in the OwnerContext resolution table
// (see owner_context.go). Keep this list and ownerContextRegistry in lockstep —
// adding a sixth scheme here without registering it trips that guard test.
var knownOwnerSchemes = []string{
	ownerRefOrgs, ownerRefProjects, ownerRefIssues, ownerRefTasks, ownerRefPlans,
}

func (r OwnerRef) String() string { return string(r) }

// IsEmpty reports whether no owner is set (dm / placeholder project_channel).
func (r OwnerRef) IsEmpty() bool { return strings.TrimSpace(string(r)) == "" }

// NewOrgOwnerRef builds a channel's owner_ref (id://organizations/{org_id}).
// NewProjectOwnerRef / NewIssueOwnerRef / NewTaskOwnerRef build the pm:// URIs
// used by issue/task conversations (phase B wiring).
func NewOrgOwnerRef(orgID string) OwnerRef         { return OwnerRef(ownerRefOrgs + orgID) }
func NewProjectOwnerRef(projectID string) OwnerRef { return OwnerRef(ownerRefProjects + projectID) }
func NewIssueOwnerRef(issueID string) OwnerRef     { return OwnerRef(ownerRefIssues + issueID) }
func NewTaskOwnerRef(taskID string) OwnerRef       { return OwnerRef(ownerRefTasks + taskID) }

// NewPlanOwnerRef builds a Plan conversation's owner_ref (pm://plans/{plan_id},
// v2.9 plan orchestration §2).
func NewPlanOwnerRef(planID string) OwnerRef { return OwnerRef(ownerRefPlans + planID) }

// WellFormed reports whether a non-empty owner_ref uses a known scheme
// (id://organizations for channel, pm:// for issue/task). Advisory.
func (r OwnerRef) WellFormed() bool {
	s := string(r)
	if s == "" {
		return true // empty is allowed (dm)
	}
	for _, p := range knownOwnerSchemes {
		if strings.HasPrefix(s, p) && len(s) > len(p) {
			return true
		}
	}
	return false
}

// ContextRefs travels on a Message to identify the source of a message
// segment inside a (possibly long-lived, reassigned) Task Conversation
// (ADR-0047 §3, plan §2.4). All fields optional; the UI groups by
// WorkItemRef. interaction_ref is intentionally NOT here — interaction-level
// detail lives in AgentActivityEvent (plan §2.6).
type ContextRefs struct {
	WorkItemRef string `json:"work_item_ref,omitempty"`
	TaskRef     string `json:"task_ref,omitempty"`
	AgentRef    string `json:"agent_ref,omitempty"`
}

// IsEmpty reports whether no context refs are set.
func (c ContextRefs) IsEmpty() bool {
	return c.WorkItemRef == "" && c.TaskRef == "" && c.AgentRef == ""
}

// MarshalContextRefsJSON encodes context refs for the messages.context_refs
// column. An empty value encodes as "{}".
func MarshalContextRefsJSON(c ContextRefs) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "{}", err
	}
	return string(b), nil
}

// UnmarshalContextRefsJSON decodes the column; empty/whitespace yields a
// zero-value ContextRefs.
func UnmarshalContextRefsJSON(s string) (ContextRefs, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return ContextRefs{}, nil
	}
	var c ContextRefs
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return ContextRefs{}, err
	}
	return c, nil
}

// MessageAttachment is the unified attachment structure (ADR-0047 §4,
// ADR-0048). There is NO file/image split in the domain model — the display
// type is derived from MimeType by the UI. URI is an `ac://files/{ulid}`
// reference (the files BC owns the blob); filename/mime/size are placement
// metadata carried with the message.
type MessageAttachment struct {
	URI      string `json:"uri"` // ac://files/{ulid}
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// MarshalAttachmentsJSON encodes the attachments slice for the
// messages.attachments column. nil/empty encodes as "[]".
func MarshalAttachmentsJSON(atts []MessageAttachment) (string, error) {
	if len(atts) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(atts)
	if err != nil {
		return "[]", err
	}
	return string(b), nil
}

// UnmarshalAttachmentsJSON decodes the column; empty/whitespace yields nil.
func UnmarshalAttachmentsJSON(s string) ([]MessageAttachment, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil, nil
	}
	var atts []MessageAttachment
	if err := json.Unmarshal([]byte(s), &atts); err != nil {
		return nil, err
	}
	return atts, nil
}

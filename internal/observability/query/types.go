// Package query implements Observability BC's QueryService — the unified
// dispatch behind the 5 CLI verbs (inspect / query / ps / stats / logs)
// plus peek-trace. Per plan-4 § 3.5 + observability/00 § 7.1.
package query

import (
	"errors"
	"time"
)

// InspectKind enumerates the 10 inspect kinds (plan-4 § 3.5 list).
type InspectKind string

const (
	InspectTask         InspectKind = "task"
	InspectExecution    InspectKind = "execution"
	InspectWorker       InspectKind = "worker"
	InspectIssue        InspectKind = "issue"
	InspectSupervisor   InspectKind = "supervisor"
	InspectConversation InspectKind = "conversation"
	InspectInputRequest InspectKind = "input_request"
	InspectProject      InspectKind = "project"
	InspectWorktree     InspectKind = "worktree"
	InspectDecision     InspectKind = "decision"
)

// AllInspectKinds is the closed-enum list used by `--help` and validation.
var AllInspectKinds = []InspectKind{
	InspectTask, InspectExecution, InspectWorker, InspectIssue,
	InspectSupervisor, InspectConversation, InspectInputRequest,
	InspectProject, InspectWorktree, InspectDecision,
}

// ValidInspectKind reports whether kind is a recognised inspect kind.
func ValidInspectKind(kind string) bool {
	for _, k := range AllInspectKinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// QueryResource enumerates the 8 query resources (plan-4 § 3.5 list).
type QueryResource string

const (
	QueryTasks         QueryResource = "tasks"
	QueryExecutions    QueryResource = "executions"
	QueryWorkers       QueryResource = "workers"
	QueryIssues        QueryResource = "issues"
	QueryInputRequests QueryResource = "input_requests"
	QueryProposals     QueryResource = "proposals"
	QueryEvents        QueryResource = "events"
	QueryDecisions     QueryResource = "decisions"
)

// AllQueryResources lists every supported `query <resource>` value.
var AllQueryResources = []QueryResource{
	QueryTasks, QueryExecutions, QueryWorkers, QueryIssues,
	QueryInputRequests, QueryProposals, QueryEvents, QueryDecisions,
}

// ValidQueryResource reports whether the resource is recognised.
func ValidQueryResource(resource string) bool {
	for _, r := range AllQueryResources {
		if string(r) == resource {
			return true
		}
	}
	return false
}

// QueryFilter is the unified flag bag for the `query` verb.
type QueryFilter struct {
	Status        string
	ProjectID     string
	Priority      string
	BlockedBy     string
	WorkerID      string
	TaskID        string
	ExecutionID   string
	IssueID       string
	Opener        string
	FailedReason  string
	HasMapping    *bool
	NotDispatch   *bool
	InvocationID  string
	Kind          string
	Outcome       string
	Actor         string
	CorrelationID string
	DecisionID    string
	EventType     string // exact match or prefix when ending in '.'
	Since         *time.Time
	Until         *time.Time
	Limit         int
	Cursor        string
}

// QueryResult is the unified result envelope returned by Query.
type QueryResult struct {
	Resource   QueryResource `json:"resource"`
	Items      []any         `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// InspectResult wraps a kind-specific result struct with a tag so JSON
// consumers can dispatch.
type InspectResult struct {
	Kind InspectKind `json:"kind"`
	ID   string      `json:"id"`
	Data any         `json:"data"`
}

// Sentinel errors for the QueryService. Use errors.Is to test.
var (
	ErrInspectKindUnknown    = errors.New("query: unknown inspect kind")
	ErrQueryResourceUnknown  = errors.New("query: unknown resource")
	ErrInspectIDRequired     = errors.New("query: inspect id required")
	ErrInspectNotFound       = errors.New("query: inspect target not found")
	ErrInspectUnimplemented  = errors.New("query: inspect kind not yet provisioned with data")
)

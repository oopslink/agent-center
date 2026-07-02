package orchestration

import "errors"

// Typed identifiers.
type (
	GraphID string
	NodeID  string
)

// NodeCategory distinguishes business nodes from control nodes.
type NodeCategory string

const (
	NodeCategoryBusiness NodeCategory = "business"
	NodeCategoryControl  NodeCategory = "control"
)

func (c NodeCategory) IsValid() bool {
	switch c {
	case NodeCategoryBusiness, NodeCategoryControl:
		return true
	}
	return false
}

// ControlKind sub-classifies control nodes.
type ControlKind string

const (
	ControlKindStart     ControlKind = "start"
	ControlKindEnd       ControlKind = "end"
	ControlKindCondition ControlKind = "condition"
)

func (k ControlKind) IsValid() bool {
	switch k {
	case ControlKindStart, ControlKindEnd, ControlKindCondition:
		return true
	}
	return false
}

// NodeStatus enum + state machine.
type NodeStatus string

const (
	NodeOpen      NodeStatus = "open"
	NodeRunning   NodeStatus = "running"
	NodeCompleted NodeStatus = "completed"
	NodeReopen    NodeStatus = "reopen"
	NodeDiscarded NodeStatus = "discarded"
)

func (s NodeStatus) IsValid() bool {
	switch s {
	case NodeOpen, NodeRunning, NodeCompleted, NodeReopen, NodeDiscarded:
		return true
	}
	return false
}

var nodeTransitions = map[NodeStatus][]NodeStatus{
	NodeOpen:      {NodeRunning, NodeDiscarded},
	NodeRunning:   {NodeCompleted, NodeDiscarded},
	NodeCompleted: {NodeReopen},
	NodeReopen:    {NodeRunning, NodeDiscarded},
	NodeDiscarded: {}, // terminal
}

func (s NodeStatus) CanTransitionTo(to NodeStatus) bool {
	for _, n := range nodeTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

func (s NodeStatus) IsTerminal() bool {
	return s == NodeDiscarded
}

// GraphStatus enum.
type GraphStatus string

const (
	GraphDraft    GraphStatus = "draft"
	GraphRunning  GraphStatus = "running"
	GraphDone     GraphStatus = "done"
	GraphArchived GraphStatus = "archived"
)

func (s GraphStatus) IsValid() bool {
	switch s {
	case GraphDraft, GraphRunning, GraphDone, GraphArchived:
		return true
	}
	return false
}

var graphTransitions = map[GraphStatus][]GraphStatus{
	GraphDraft:    {GraphRunning, GraphArchived},
	GraphRunning:  {GraphDraft, GraphDone},
	GraphDone:     {GraphArchived},
	GraphArchived: {}, // terminal
}

func (s GraphStatus) CanTransitionTo(to GraphStatus) bool {
	for _, n := range graphTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

// Sentinel errors.
var (
	ErrGraphNotFound        = errors.New("orchestration: graph not found")
	ErrGraphExists          = errors.New("orchestration: graph already exists")
	ErrNodeNotFound         = errors.New("orchestration: node not found")
	ErrNodeExists           = errors.New("orchestration: node already exists")
	ErrEdgeExists           = errors.New("orchestration: edge already exists")
	ErrIllegalTransition    = errors.New("orchestration: illegal status transition")
	ErrSelfEdge             = errors.New("orchestration: self-referencing edge not allowed")
	ErrCycleDetected        = errors.New("orchestration: adding edge would create a cycle")
	ErrNodeNotRemovable     = errors.New("orchestration: node in running/completed status cannot be removed")
	ErrMissingRequiredField = errors.New("orchestration: missing required field")
	ErrInvalidCategory      = errors.New("orchestration: invalid node category")
	ErrMissingControlKind   = errors.New("orchestration: control node requires a controlKind")
	ErrInvalidControlKind   = errors.New("orchestration: invalid controlKind")
)

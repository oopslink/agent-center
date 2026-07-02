package orchestration

import (
	"encoding/json"
	"maps"
	"strings"
	"time"
)

// ActionLog records a node lifecycle event.
type ActionLog struct {
	OccurredAt time.Time
	Action     string
	Detail     string
}

// Node is a business or control node within a Graph.
type Node struct {
	id          NodeID
	graphID     GraphID
	category    NodeCategory
	controlKind ControlKind
	title       string
	status      NodeStatus
	outcome     string
	metadata    map[string]any
	actionLogs  []ActionLog
	createdAt   time.Time
	updatedAt   time.Time
	version     int
}

// NewNodeInput captures constructor args.
type NewNodeInput struct {
	ID          NodeID
	GraphID     GraphID
	Category    NodeCategory
	ControlKind ControlKind // required for control nodes, empty for business
	Title       string
	Metadata    map[string]any
	CreatedAt   time.Time // zero → time.Now()
}

func NewNode(in NewNodeInput) (*Node, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, ErrMissingRequiredField
	}
	if strings.TrimSpace(string(in.GraphID)) == "" {
		return nil, ErrMissingRequiredField
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, ErrMissingRequiredField
	}
	if !in.Category.IsValid() {
		return nil, ErrInvalidCategory
	}
	if in.Category == NodeCategoryControl {
		if in.ControlKind == "" {
			return nil, ErrMissingControlKind
		}
		if !in.ControlKind.IsValid() {
			return nil, ErrInvalidControlKind
		}
	}
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	meta := in.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	return &Node{
		id:          in.ID,
		graphID:     in.GraphID,
		category:    in.Category,
		controlKind: in.ControlKind,
		title:       in.Title,
		status:      NodeOpen,
		metadata:    meta,
		actionLogs:  nil,
		createdAt:   at.UTC(),
		updatedAt:   at.UTC(),
		version:     1,
	}, nil
}

// RehydrateNodeInput for persistence round-trip.
type RehydrateNodeInput struct {
	ID          NodeID
	GraphID     GraphID
	Category    NodeCategory
	ControlKind ControlKind
	Title       string
	Status      NodeStatus
	Outcome     string
	Metadata    map[string]any
	ActionLogs  []ActionLog
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Version     int
}

func RehydrateNode(in RehydrateNodeInput) (*Node, error) {
	if !in.Status.IsValid() {
		return nil, ErrIllegalTransition
	}
	if in.Version < 1 {
		return nil, ErrMissingRequiredField
	}
	meta := in.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	return &Node{
		id:          in.ID,
		graphID:     in.GraphID,
		category:    in.Category,
		controlKind: in.ControlKind,
		title:       in.Title,
		status:      in.Status,
		outcome:     in.Outcome,
		metadata:    meta,
		actionLogs:  in.ActionLogs,
		createdAt:   in.CreatedAt.UTC(),
		updatedAt:   in.UpdatedAt.UTC(),
		version:     in.Version,
	}, nil
}

// Getters.
func (n *Node) ID() NodeID               { return n.id }
func (n *Node) GraphID() GraphID         { return n.graphID }
func (n *Node) Category() NodeCategory   { return n.category }
func (n *Node) ControlKind() ControlKind { return n.controlKind }
func (n *Node) Title() string            { return n.title }
func (n *Node) Status() NodeStatus       { return n.status }
func (n *Node) Outcome() string          { return n.outcome }
func (n *Node) CreatedAt() time.Time     { return n.createdAt }
func (n *Node) UpdatedAt() time.Time     { return n.updatedAt }
func (n *Node) Version() int             { return n.version }

func (n *Node) Metadata() map[string]any {
	cp := make(map[string]any, len(n.metadata))
	maps.Copy(cp, n.metadata)
	return cp
}

func (n *Node) ActionLogs() []ActionLog {
	cp := make([]ActionLog, len(n.actionLogs))
	copy(cp, n.actionLogs)
	return cp
}

// MetadataJSON returns metadata as a JSON string for persistence.
func (n *Node) MetadataJSON() string {
	if len(n.metadata) == 0 {
		return "{}"
	}
	b, err := json.Marshal(n.metadata)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ActionLogsJSON returns action logs as a JSON string for persistence.
func (n *Node) ActionLogsJSON() string {
	if len(n.actionLogs) == 0 {
		return "[]"
	}
	b, err := json.Marshal(n.actionLogs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// Mutations.

func (n *Node) Start(at time.Time) error {
	return n.transition(NodeRunning, "started", "", at)
}

func (n *Node) Complete(outcome string, at time.Time) error {
	if err := n.transition(NodeCompleted, "completed", outcome, at); err != nil {
		return err
	}
	n.outcome = outcome
	return nil
}

func (n *Node) Reopen(reason string, at time.Time) error {
	if err := n.transition(NodeReopen, "reopened", reason, at); err != nil {
		return err
	}
	n.outcome = "" // clear outcome on reopen
	return nil
}

func (n *Node) Discard(at time.Time) error {
	return n.transition(NodeDiscarded, "discarded", "", at)
}

// SetTitle updates the node title.
func (n *Node) SetTitle(title string, at time.Time) error {
	if strings.TrimSpace(title) == "" {
		return ErrMissingRequiredField
	}
	n.title = title
	n.touch(at)
	return nil
}

// SetMetadata replaces the metadata map.
func (n *Node) SetMetadata(meta map[string]any, at time.Time) {
	if meta == nil {
		meta = map[string]any{}
	}
	n.metadata = meta
	n.touch(at)
}

func (n *Node) transition(to NodeStatus, action, detail string, at time.Time) error {
	if !n.status.CanTransitionTo(to) {
		return ErrIllegalTransition
	}
	n.status = to
	n.appendLog(action, detail, at)
	n.touch(at)
	return nil
}

func (n *Node) appendLog(action, detail string, at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	n.actionLogs = append(n.actionLogs, ActionLog{
		OccurredAt: at.UTC(),
		Action:     action,
		Detail:     detail,
	})
}

func (n *Node) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	n.updatedAt = at.UTC()
	n.version++
}

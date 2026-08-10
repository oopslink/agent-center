package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// EvolutionNodeAction describes how an Evolution commit treats an existing
// already-authored node. It is stored in the immutable generation diff so review
// can distinguish preserved history from intentionally superseded unstarted work.
type EvolutionNodeAction string

const (
	EvolutionPreserve   EvolutionNodeAction = "preserve"
	EvolutionHoldAtGate EvolutionNodeAction = "hold_at_gate"
	EvolutionSupersede  EvolutionNodeAction = "supersede"
)

func (a EvolutionNodeAction) IsValid() bool {
	switch a {
	case EvolutionPreserve, EvolutionHoldAtGate, EvolutionSupersede:
		return true
	}
	return false
}

// PlanGenerationNodeDecision is the per-existing-node intent carried by a
// generation diff.
type PlanGenerationNodeDecision struct {
	TaskID TaskID              `json:"task_id"`
	Action EvolutionNodeAction `json:"action"`
	Reason string              `json:"reason,omitempty"`
}

// PlanGenerationTaskDraft is a new task node to create as part of an Evolution
// commit. Ref is local to the diff and can be used by PlanGenerationEdgeDraft.
type PlanGenerationTaskDraft struct {
	Ref              string           `json:"ref"`
	Title            string           `json:"title"`
	Description      string           `json:"description,omitempty"`
	AssigneeRef      IdentityRef      `json:"assignee_ref"`
	DispatchMode     DispatchMode     `json:"dispatch_mode,omitempty"`
	DeliveryContract DeliveryContract `json:"delivery_contract,omitempty"`
	StageID          StageID          `json:"stage_id,omitempty"`
	FollowsTaskID    TaskID           `json:"follows_task_id,omitempty"`
}

// PlanGenerationEdgeDraft adds a dependency edge. From/To may be new task refs
// or existing task ids; the service resolves them at commit time.
type PlanGenerationEdgeDraft struct {
	From      string   `json:"from"`
	To        string   `json:"to"`
	Kind      EdgeKind `json:"kind,omitempty"`
	When      string   `json:"when,omitempty"`
	MaxRounds int      `json:"max_rounds,omitempty"`
}

// PlanGenerationDiff is the immutable input-level diff attached to a generation.
type PlanGenerationDiff struct {
	NodeDecisions []PlanGenerationNodeDecision `json:"node_decisions,omitempty"`
	Tasks         []PlanGenerationTaskDraft    `json:"tasks,omitempty"`
	Edges         []PlanGenerationEdgeDraft    `json:"edges,omitempty"`
}

// Snapshot types intentionally copy descriptive fields instead of referencing
// live aggregates. Once saved, a generation snapshot must not drift when tasks,
// edges, or dispatch records later change.
type PlanGenerationTaskSnapshot struct {
	TaskID           TaskID           `json:"task_id"`
	StageID          StageID          `json:"stage_id,omitempty"`
	NodeID           string           `json:"node_id,omitempty"`
	Title            string           `json:"title"`
	Description      string           `json:"description,omitempty"`
	AssigneeRef      IdentityRef      `json:"assignee_ref,omitempty"`
	Status           TaskStatus       `json:"status"`
	DispatchMode     DispatchMode     `json:"dispatch_mode,omitempty"`
	DeliveryContract DeliveryContract `json:"delivery_contract,omitempty"`
	FollowsTaskID    TaskID           `json:"follows_task_id,omitempty"`
	OriginVerdictID  GateVerdictID    `json:"origin_verdict_id,omitempty"`
}

type PlanGenerationEdgeSnapshot struct {
	FromTaskID TaskID   `json:"from_task_id"`
	ToTaskID   TaskID   `json:"to_task_id"`
	Kind       EdgeKind `json:"kind,omitempty"`
	When       string   `json:"when,omitempty"`
	MaxRounds  int      `json:"max_rounds,omitempty"`
}

type PlanGenerationDispatchSnapshot struct {
	TaskID            TaskID    `json:"task_id"`
	DispatchedAt      time.Time `json:"dispatched_at"`
	DispatchMessageID string    `json:"dispatch_message_id,omitempty"`
}

type PlanGenerationSnapshot struct {
	PlanID             PlanID                           `json:"plan_id"`
	PlanVersion        int                              `json:"plan_version"`
	ActiveGenerationID PlanGenerationID                 `json:"active_generation_id,omitempty"`
	Tasks              []PlanGenerationTaskSnapshot     `json:"tasks"`
	Edges              []PlanGenerationEdgeSnapshot     `json:"edges"`
	DispatchRecords    []PlanGenerationDispatchSnapshot `json:"dispatch_records"`
}

// PlanGeneration is an immutable plan topology snapshot plus the Evolution
// command metadata that produced it.
type PlanGeneration struct {
	ID                 PlanGenerationID
	PlanID             PlanID
	ParentGenerationID PlanGenerationID
	Reason             string
	Evidence           string
	CreatorRef         IdentityRef
	Diff               PlanGenerationDiff
	Snapshot           PlanGenerationSnapshot
	IdempotencyKey     string
	RequestFingerprint string
	DispatchedTaskIDs  []TaskID
	CreatedAt          time.Time
}

func NewPlanGeneration(g PlanGeneration) (*PlanGeneration, error) {
	if g.ID == "" || g.PlanID == "" {
		return nil, errors.New("projectmanager: plan generation id and plan_id required")
	}
	g.Reason = strings.TrimSpace(g.Reason)
	g.Evidence = strings.TrimSpace(g.Evidence)
	g.IdempotencyKey = strings.TrimSpace(g.IdempotencyKey)
	g.RequestFingerprint = strings.TrimSpace(g.RequestFingerprint)
	if g.Reason == "" || g.Evidence == "" {
		return nil, errors.New("projectmanager: plan generation reason and evidence required")
	}
	if err := g.CreatorRef.Validate(); err != nil {
		return nil, err
	}
	if g.IdempotencyKey == "" || g.RequestFingerprint == "" {
		return nil, errors.New("projectmanager: plan generation idempotency key and request fingerprint required")
	}
	if g.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: plan generation created_at required")
	}
	g.CreatedAt = g.CreatedAt.UTC()
	g.Snapshot.PlanID = g.PlanID
	return &g, nil
}

// Package collaborationeffect implements the replayable collaboration-effect
// read model owned by the Observability bounded context.
package collaborationeffect

import (
	"context"
	"errors"
	"time"
)

const RuleVersionV1 = "collaboration-effect.mvp.v1"

type RelationType string
type Polarity string

const (
	RelationAssign            RelationType = "assign"
	RelationReassign          RelationType = "reassign"
	RelationBlock             RelationType = "block"
	RelationUnblock           RelationType = "unblock"
	RelationComplete          RelationType = "complete"
	RelationDependencyRelease RelationType = "dependency_release"
	RelationReviewAccept      RelationType = "review_accept"
	RelationReviewReject      RelationType = "review_reject"

	PolarityPositive Polarity = "positive"
	PolarityNegative Polarity = "negative"
	PolarityNeutral  Polarity = "neutral"
	PolarityMixed    Polarity = "mixed"
)

type Fact struct {
	EventID       string
	EventType     string
	OccurredAt    time.Time
	ActorRef      string
	ProjectID     string
	TaskID        string
	CorrelationID string
	DecisionID    string
	Payload       map[string]any
}

type Effect struct {
	EffectID         string         `json:"effect_id"`
	ProjectID        string         `json:"project_id"`
	TargetTaskID     string         `json:"target_task_id"`
	SourceAgentRef   string         `json:"source_agent_ref"`
	TargetAgentRef   string         `json:"target_agent_ref"`
	RelationType     RelationType   `json:"relation_type"`
	Polarity         Polarity       `json:"polarity"`
	Magnitude        int            `json:"magnitude"`
	Confidence       string         `json:"confidence"`
	OccurredAt       time.Time      `json:"occurred_at"`
	RuleVersion      string         `json:"rule_version"`
	EvidenceEventIDs []string       `json:"evidence_event_ids"`
	BeforeState      map[string]any `json:"before_state"`
	AfterState       map[string]any `json:"after_state"`
	ExplanationKey   string         `json:"explanation_key"`
}

type Diagnostic struct {
	SourceEventID string
	RuleVersion   string
	Reason        string
	OccurredAt    time.Time
}

type Dependency struct {
	ProjectID, PlanID, UpstreamTaskID, DownstreamTaskID, SourceEventID string
	OccurredAt                                                         time.Time
}

type Filter struct {
	ProjectID    string       `json:"project_id"`
	TaskID       string       `json:"task_id,omitempty"`
	AgentRef     string       `json:"agent_ref,omitempty"`
	RelationType RelationType `json:"relation_type,omitempty"`
	Polarity     Polarity     `json:"polarity,omitempty"`
	Since        *time.Time   `json:"since,omitempty"`
	Until        *time.Time   `json:"until,omitempty"`
	Cursor       string       `json:"cursor,omitempty"`
	Limit        int          `json:"limit,omitempty"`
	RuleVersion  string       `json:"rule_version,omitempty"`
}

type Repository interface {
	Apply(context.Context, Fact, string, []Effect, []Dependency, []Diagnostic) error
	DependenciesForUpstream(context.Context, string, string, string) ([]Dependency, error)
	List(context.Context, Filter) ([]Effect, string, error)
	FindByID(context.Context, string) (Effect, error)
	ReplaceVersion(context.Context, string, string) error
	ActiveVersion(context.Context) (string, error)
	DeleteVersion(context.Context, string) error
}

var ErrInvalidFact = errors.New("collaboration effect: invalid fact")
var ErrEffectNotFound = errors.New("collaboration effect: effect not found")

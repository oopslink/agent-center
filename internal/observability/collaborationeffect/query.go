package collaborationeffect

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
)

const MaxQueryLimit = 500

var (
	ErrInvalidQuery  = errors.New("collaboration insight: invalid query")
	ErrInvalidCursor = errors.New("collaboration insight: invalid cursor")
)

type QueryRepository interface {
	List(context.Context, Filter) ([]Effect, string, error)
	FindByID(context.Context, string) (Effect, error)
	ActiveVersion(context.Context) (string, error)
}

type QueryService struct {
	effects QueryRepository
	events  observability.EventRepository
	now     func() time.Time
}

func NewQueryService(effects QueryRepository, events observability.EventRepository) (*QueryService, error) {
	if effects == nil || events == nil {
		return nil, errors.New("collaboration insight: repositories required")
	}
	return &QueryService{effects: effects, events: events, now: time.Now}, nil
}

type GraphNode struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	TaskID string `json:"task_id,omitempty"`
}
type GraphEdge struct {
	ID           string       `json:"id"`
	Source       string       `json:"source"`
	Target       string       `json:"target"`
	RelationType RelationType `json:"relation_type"`
	Polarity     Polarity     `json:"polarity"`
	Magnitude    int          `json:"magnitude"`
	EffectID     string       `json:"effect_id"`
}
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}
type Summary struct {
	PositiveCount     int `json:"positive_count"`
	NegativeCount     int `json:"negative_count"`
	NeutralCount      int `json:"neutral_count"`
	MixedCount        int `json:"mixed_count"`
	AffectedTaskCount int `json:"affected_task_count"`
}
type QueryResult struct {
	Graph       Graph     `json:"graph"`
	Effects     []Effect  `json:"effects"`
	Summary     Summary   `json:"summary"`
	AsOf        time.Time `json:"as_of"`
	RuleVersion string    `json:"rule_version"`
	Truncated   bool      `json:"truncated"`
	NextCursor  string    `json:"next_cursor"`
}

func (s *QueryService) Query(ctx context.Context, f Filter) (QueryResult, error) {
	if f.Limit < 0 || f.Limit > MaxQueryLimit {
		return QueryResult{}, fmt.Errorf("%w: limit must be 1..%d", ErrInvalidQuery, MaxQueryLimit)
	}
	if f.Cursor != "" && !strings.HasPrefix(f.Cursor, "ce_") {
		return QueryResult{}, ErrInvalidCursor
	}
	if f.Since != nil && f.Until != nil && !f.Since.Before(*f.Until) {
		return QueryResult{}, fmt.Errorf("%w: since must be before until", ErrInvalidQuery)
	}
	effects, next, err := s.effects.List(ctx, f)
	if err != nil {
		return QueryResult{}, err
	}
	version := f.RuleVersion
	if version == "" {
		version, err = s.effects.ActiveVersion(ctx)
		if err != nil {
			return QueryResult{}, err
		}
	}
	res := QueryResult{Effects: effects, AsOf: s.now().UTC(), RuleVersion: version, NextCursor: next, Truncated: next != "", Graph: Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}}
	nodes := map[string]GraphNode{}
	tasks := map[string]struct{}{}
	for _, e := range effects {
		addNode(nodes, GraphNode{ID: e.SourceAgentRef, Kind: "agent", Label: e.SourceAgentRef})
		if e.TargetAgentRef != "" {
			addNode(nodes, GraphNode{ID: e.TargetAgentRef, Kind: "agent", Label: e.TargetAgentRef})
		}
		taskNode := "task:" + e.TargetTaskID
		addNode(nodes, GraphNode{ID: taskNode, Kind: "task", Label: e.TargetTaskID, TaskID: e.TargetTaskID})
		target := taskNode
		if e.TargetAgentRef != "" {
			target = e.TargetAgentRef
		}
		res.Graph.Edges = append(res.Graph.Edges, GraphEdge{ID: e.EffectID, Source: e.SourceAgentRef, Target: target, RelationType: e.RelationType, Polarity: e.Polarity, Magnitude: e.Magnitude, EffectID: e.EffectID})
		tasks[e.TargetTaskID] = struct{}{}
		switch e.Polarity {
		case PolarityPositive:
			res.Summary.PositiveCount++
		case PolarityNegative:
			res.Summary.NegativeCount++
		case PolarityNeutral:
			res.Summary.NeutralCount++
		case PolarityMixed:
			res.Summary.MixedCount++
		}
	}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		res.Graph.Nodes = append(res.Graph.Nodes, nodes[k])
	}
	res.Summary.AffectedTaskCount = len(tasks)
	return res, nil
}

func addNode(m map[string]GraphNode, n GraphNode) {
	if n.ID != "" {
		m[n.ID] = n
	}
}

type EvidenceEvent struct {
	EventID    string                  `json:"event_id"`
	EventType  string                  `json:"event_type"`
	OccurredAt time.Time               `json:"occurred_at"`
	ActorRef   string                  `json:"actor_ref"`
	Refs       observability.EventRefs `json:"refs"`
	Payload    map[string]any          `json:"payload"`
}
type EvidenceResult struct {
	EffectID       string          `json:"effect_id"`
	ProjectID      string          `json:"project_id"`
	RuleVersion    string          `json:"rule_version"`
	BeforeState    map[string]any  `json:"before_state"`
	AfterState     map[string]any  `json:"after_state"`
	ExplanationKey string          `json:"explanation_key"`
	Evidence       []EvidenceEvent `json:"evidence"`
}

func (s *QueryService) Evidence(ctx context.Context, effectID, projectID string) (EvidenceResult, error) {
	e, err := s.effects.FindByID(ctx, effectID)
	if err != nil {
		return EvidenceResult{}, err
	}
	if projectID == "" || e.ProjectID != projectID {
		return EvidenceResult{}, ErrEffectNotFound
	}
	res := EvidenceResult{EffectID: e.EffectID, ProjectID: e.ProjectID, RuleVersion: e.RuleVersion, BeforeState: e.BeforeState, AfterState: e.AfterState, ExplanationKey: e.ExplanationKey, Evidence: []EvidenceEvent{}}
	for _, id := range e.EvidenceEventIDs {
		ev, findErr := s.events.FindByID(ctx, observability.EventID(id))
		if findErr != nil {
			return EvidenceResult{}, fmt.Errorf("evidence %s: %w", id, findErr)
		}
		res.Evidence = append(res.Evidence, EvidenceEvent{EventID: ev.ID().String(), EventType: ev.Type().String(), OccurredAt: ev.OccurredAt().UTC(), ActorRef: ev.Actor().String(), Refs: ev.Refs(), Payload: ev.Payload()})
	}
	return res, nil
}

package collaborationeffect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	ProjectID string `json:"project_id,omitempty"`
	PlanID    string `json:"plan_id,omitempty"`
	StageID   string `json:"stage_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
}
type GraphEdge struct {
	ID               string       `json:"id"`
	SemanticKey      string       `json:"semantic_key"`
	Source           string       `json:"source"`
	Target           string       `json:"target"`
	RelationType     RelationType `json:"relation_type"`
	Polarity         Polarity     `json:"polarity"`
	Magnitude        int          `json:"magnitude"`
	EffectID         string       `json:"effect_id,omitempty"`
	InteractionCount int          `json:"interaction_count"`
	FirstOccurredAt  time.Time    `json:"first_occurred_at,omitempty"`
	LastOccurredAt   time.Time    `json:"last_occurred_at,omitempty"`
	EvidenceCount    int          `json:"evidence_count"`
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
	Graph        Graph     `json:"graph"`
	Effects      []Effect  `json:"effects"`
	Summary      Summary   `json:"summary"`
	AsOf         time.Time `json:"as_of"`
	RuleVersion  string    `json:"rule_version"`
	GraphVersion string    `json:"graph_version"`
	Truncated    bool      `json:"truncated"`
	NextCursor   string    `json:"next_cursor"`
}

type GraphPlan struct {
	ID        string
	Label     string
	ProjectID string
}

type GraphStage struct {
	ID        string
	Label     string
	ProjectID string
	PlanID    string
}

type GraphTask struct {
	ID        string
	Label     string
	ProjectID string
	PlanID    string
	StageID   string
}

type GraphTopology struct {
	Plans  map[string]GraphPlan
	Stages map[string]GraphStage
	Tasks  map[string]GraphTask
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
	graph, summary := BuildGraph(effects, version, GraphTopology{})
	res := QueryResult{Effects: effects, Summary: summary, AsOf: s.now().UTC(), RuleVersion: version, GraphVersion: version, NextCursor: next, Truncated: next != "", Graph: graph}
	return res, nil
}

func BuildGraph(effects []Effect, version string, topology GraphTopology) (Graph, Summary) {
	graph := Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	if version == "" {
		version = RuleVersionV1
	}
	nodes := map[string]GraphNode{}
	tasks := map[string]struct{}{}
	edges := map[string]*GraphEdge{}
	plans := topology.Plans
	if plans == nil {
		plans = map[string]GraphPlan{}
	}
	stages := topology.Stages
	if stages == nil {
		stages = map[string]GraphStage{}
	}
	taskMeta := topology.Tasks
	if taskMeta == nil {
		taskMeta = map[string]GraphTask{}
	}
	for _, p := range plans {
		addNode(nodes, GraphNode{ID: planNodeID(p.ID), Kind: "plan", Label: labelOrID(p.Label, p.ID), ProjectID: p.ProjectID, PlanID: p.ID})
	}
	for _, st := range stages {
		addNode(nodes, GraphNode{ID: stageNodeID(st.ID), Kind: "stage", Label: labelOrID(st.Label, st.ID), ProjectID: st.ProjectID, PlanID: st.PlanID, StageID: st.ID})
		addAggregateEdge(edges, version, planNodeID(st.PlanID), stageNodeID(st.ID), RelationPlanStage, PolarityNeutral, 1, "", nil)
	}
	for _, task := range taskMeta {
		addNode(nodes, GraphNode{ID: taskNodeID(task.ID), Kind: "task", Label: labelOrID(task.Label, task.ID), ProjectID: task.ProjectID, PlanID: task.PlanID, StageID: task.StageID, TaskID: task.ID})
		if task.StageID != "" {
			addAggregateEdge(edges, version, stageNodeID(task.StageID), taskNodeID(task.ID), RelationStageTask, PolarityNeutral, 1, "", nil)
		} else if task.PlanID != "" {
			addAggregateEdge(edges, version, planNodeID(task.PlanID), taskNodeID(task.ID), RelationPlanTask, PolarityNeutral, 1, "", nil)
		}
	}
	var summary Summary
	for _, e := range effects {
		addNode(nodes, GraphNode{ID: e.SourceAgentRef, Kind: "agent", Label: e.SourceAgentRef})
		if e.TargetAgentRef != "" {
			addNode(nodes, GraphNode{ID: e.TargetAgentRef, Kind: "agent", Label: e.TargetAgentRef})
		}
		task := taskMeta[e.TargetTaskID]
		if task.ID == "" {
			task = GraphTask{ID: e.TargetTaskID, Label: e.TargetTaskID, ProjectID: e.ProjectID}
		}
		taskNode := taskNodeID(e.TargetTaskID)
		addNode(nodes, GraphNode{ID: taskNode, Kind: "task", Label: labelOrID(task.Label, e.TargetTaskID), ProjectID: e.ProjectID, PlanID: task.PlanID, StageID: task.StageID, TaskID: e.TargetTaskID})
		target := taskNode
		if e.TargetAgentRef != "" {
			target = e.TargetAgentRef
		}
		addAggregateEdge(edges, version, e.SourceAgentRef, target, e.RelationType, e.Polarity, e.Magnitude, e.EffectID, &e)
		if task.PlanID != "" {
			addAggregateEdge(edges, version, e.SourceAgentRef, planNodeID(task.PlanID), RelationAgentPlan, e.Polarity, e.Magnitude, e.EffectID, &e)
			if task.StageID != "" {
				addAggregateEdge(edges, version, stageNodeID(task.StageID), taskNode, RelationStageTask, PolarityNeutral, 1, "", nil)
			} else {
				addAggregateEdge(edges, version, planNodeID(task.PlanID), taskNode, RelationPlanTask, PolarityNeutral, 1, "", nil)
			}
		}
		tasks[e.TargetTaskID] = struct{}{}
		switch e.Polarity {
		case PolarityPositive:
			summary.PositiveCount++
		case PolarityNegative:
			summary.NegativeCount++
		case PolarityNeutral:
			summary.NeutralCount++
		case PolarityMixed:
			summary.MixedCount++
		}
	}
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		graph.Nodes = append(graph.Nodes, nodes[k])
	}
	edgeKeys := make([]string, 0, len(edges))
	for k := range edges {
		edgeKeys = append(edgeKeys, k)
	}
	sort.Strings(edgeKeys)
	for _, k := range edgeKeys {
		graph.Edges = append(graph.Edges, *edges[k])
	}
	summary.AffectedTaskCount = len(tasks)
	return graph, summary
}

func addNode(m map[string]GraphNode, n GraphNode) {
	if n.ID != "" {
		m[n.ID] = n
	}
}

func addAggregateEdge(edges map[string]*GraphEdge, version, source, target string, relation RelationType, polarity Polarity, magnitude int, effectID string, effect *Effect) {
	if source == "" || target == "" {
		return
	}
	if polarity == "" {
		polarity = PolarityNeutral
	}
	semanticKey := strings.Join([]string{source, target, string(relation), string(polarity)}, "|")
	edge := edges[semanticKey]
	if edge == nil {
		idHash := sha256.Sum256([]byte(version + "\x00" + semanticKey))
		edge = &GraphEdge{ID: "edge_" + hex.EncodeToString(idHash[:10]), SemanticKey: semanticKey, Source: source, Target: target, RelationType: relation, Polarity: polarity, Magnitude: magnitude, EffectID: effectID}
		edges[semanticKey] = edge
	}
	if edge.Magnitude < magnitude {
		edge.Magnitude = magnitude
	}
	if edge.EffectID == "" {
		edge.EffectID = effectID
	}
	if effect == nil {
		return
	}
	edge.InteractionCount++
	if edge.FirstOccurredAt.IsZero() || effect.OccurredAt.Before(edge.FirstOccurredAt) {
		edge.FirstOccurredAt = effect.OccurredAt.UTC()
	}
	if edge.LastOccurredAt.IsZero() || effect.OccurredAt.After(edge.LastOccurredAt) {
		edge.LastOccurredAt = effect.OccurredAt.UTC()
	}
	edge.EvidenceCount += len(effect.EvidenceEventIDs)
}

func taskNodeID(id string) string  { return "task:" + id }
func planNodeID(id string) string  { return "plan:" + id }
func stageNodeID(id string) string { return "stage:" + id }
func labelOrID(label, id string) string {
	if strings.TrimSpace(label) != "" {
		return label
	}
	return id
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

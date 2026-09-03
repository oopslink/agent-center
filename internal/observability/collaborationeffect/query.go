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
	graphs  graphScopeReader
	now     func() time.Time
}

func NewQueryService(effects QueryRepository, events observability.EventRepository) (*QueryService, error) {
	if effects == nil || events == nil {
		return nil, errors.New("collaboration insight: repositories required")
	}
	return &QueryService{effects: effects, events: events, now: time.Now}, nil
}

func NewQueryServiceWithGraph(effects QueryRepository, events observability.EventRepository, graphs graphScopeReader) (*QueryService, error) {
	svc, err := NewQueryService(effects, events)
	if err != nil {
		return nil, err
	}
	svc.graphs = graphs
	return svc, nil
}

type GraphNode struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	ProjectID string `json:"project_id,omitempty"`
	PlanID    string `json:"plan_id,omitempty"`
	StageID   string `json:"stage_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	Status    string `json:"status,omitempty"`
}
type GraphEdge struct {
	ID               string       `json:"id"`
	Source           string       `json:"source"`
	Target           string       `json:"target"`
	RelationType     RelationType `json:"relation_type"`
	Polarity         Polarity     `json:"polarity"`
	Magnitude        int          `json:"magnitude"`
	EffectID         string       `json:"effect_id,omitempty"`
	InteractionCount int          `json:"interaction_count"`
	FirstOccurredAt  *time.Time   `json:"first_occurred_at,omitempty"`
	LastOccurredAt   *time.Time   `json:"last_occurred_at,omitempty"`
	EvidenceCount    int          `json:"evidence_count"`
	Clustered        bool         `json:"clustered,omitempty"`
}
type Graph struct {
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	LOD       string      `json:"lod"`
	Clusters  []GraphNode `json:"clusters,omitempty"`
	Truncated bool        `json:"truncated"`
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
	Unchanged    bool      `json:"unchanged,omitempty"`
	Truncated    bool      `json:"truncated"`
	NextCursor   string    `json:"next_cursor"`
}

func (s *QueryService) Query(ctx context.Context, f Filter) (QueryResult, error) {
	f.OrganizationID = strings.TrimSpace(f.OrganizationID)
	f.ProjectID = strings.TrimSpace(f.ProjectID)
	f.PlanID = strings.TrimSpace(f.PlanID)
	f.TaskID = strings.TrimSpace(f.TaskID)
	f.StageID = strings.TrimSpace(f.StageID)
	f.AgentRef = strings.TrimSpace(f.AgentRef)
	f.LOD = strings.TrimSpace(f.LOD)
	if f.OrganizationID == "" && f.ProjectID == "" {
		return QueryResult{}, fmt.Errorf("%w: project_id required", ErrInvalidQuery)
	}
	if f.Limit < 0 || f.Limit > MaxQueryLimit {
		return QueryResult{}, fmt.Errorf("%w: limit must be 1..%d", ErrInvalidQuery, MaxQueryLimit)
	}
	if f.Cursor != "" && !strings.HasPrefix(f.Cursor, "ce_") && !strings.HasPrefix(f.Cursor, "cg_") {
		return QueryResult{}, ErrInvalidCursor
	}
	if f.LOD != "" && f.LOD != "full" && f.LOD != "cluster" {
		return QueryResult{}, fmt.Errorf("%w: lod must be full or cluster", ErrInvalidQuery)
	}
	if f.Since != nil && f.Until != nil && !f.Since.Before(*f.Until) {
		return QueryResult{}, fmt.Errorf("%w: since must be before until", ErrInvalidQuery)
	}
	version := f.RuleVersion
	var err error
	if version == "" {
		version, err = s.effects.ActiveVersion(ctx)
		if err != nil {
			return QueryResult{}, err
		}
	}
	if s.graphs != nil {
		scope, err := s.graphs.ReadGraphScope(ctx, f, version)
		if err != nil {
			return QueryResult{}, err
		}
		return s.assembleGraphResult(f, version, scope), nil
	}
	if f.ProjectID == "" {
		return QueryResult{}, fmt.Errorf("%w: organization query requires graph repository", ErrInvalidQuery)
	}
	effects, next, err := s.effects.List(ctx, f)
	if err != nil {
		return QueryResult{}, err
	}
	return s.assembleGraphResult(f, version, graphScope{Effects: effects, NextCursor: next}), nil
}

func (s *QueryService) assembleGraphResult(f Filter, version string, scope graphScope) QueryResult {
	graphVersion := computeGraphVersion(version, scope)
	res := QueryResult{Effects: scope.Effects, AsOf: s.now().UTC(), RuleVersion: version, GraphVersion: graphVersion, NextCursor: scope.NextCursor, Truncated: scope.NextCursor != "", Graph: Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}, LOD: "full"}}
	if f.LOD == "cluster" {
		res.Graph.LOD = "cluster"
	}
	if f.GraphVersion != "" && f.GraphVersion == graphVersion {
		res.Effects = []Effect{}
		res.Truncated = false
		res.NextCursor = ""
		res.Unchanged = true
		return res
	}
	nodes := map[string]GraphNode{}
	tasks := map[string]struct{}{}
	for _, p := range scope.Projects {
		addNode(nodes, GraphNode{ID: "project:" + p.ID, Kind: "project", Label: p.Name, ProjectID: p.ID})
	}
	for _, p := range scope.Plans {
		addNode(nodes, GraphNode{ID: "plan:" + p.ID, Kind: "plan", Label: p.Name, ProjectID: p.ProjectID, PlanID: p.ID, Status: p.Status})
		addStaticEdge(&res.Graph.Edges, "project:"+p.ProjectID, "plan:"+p.ID, "project_plan")
	}
	for _, st := range scope.Stages {
		addNode(nodes, GraphNode{ID: "stage:" + st.ID, Kind: "stage", Label: st.Name, PlanID: st.PlanID, StageID: st.ID})
		addStaticEdge(&res.Graph.Edges, "plan:"+st.PlanID, "stage:"+st.ID, "plan_stage")
	}
	for _, t := range scope.Tasks {
		addNode(nodes, GraphNode{ID: "task:" + t.ID, Kind: "task", Label: t.Title, ProjectID: t.ProjectID, PlanID: t.PlanID, StageID: t.StageID, TaskID: t.ID, Status: t.Status})
		if t.PlanID != "" {
			addStaticEdge(&res.Graph.Edges, "plan:"+t.PlanID, "task:"+t.ID, "plan_task")
		} else {
			addStaticEdge(&res.Graph.Edges, "project:"+t.ProjectID, "task:"+t.ID, "project_task")
		}
		if t.StageID != "" {
			addStaticEdge(&res.Graph.Edges, "stage:"+t.StageID, "task:"+t.ID, "stage_task")
		}
		if t.Assignee != "" {
			addNode(nodes, GraphNode{ID: t.Assignee, Kind: "agent", Label: t.Assignee})
			addStaticEdge(&res.Graph.Edges, t.Assignee, "task:"+t.ID, "agent_task")
			if t.PlanID != "" {
				addStaticEdge(&res.Graph.Edges, t.Assignee, "plan:"+t.PlanID, "agent_plan")
			}
		}
	}
	for _, d := range scope.Deps {
		addStaticEdge(&res.Graph.Edges, "task:"+d.ToTaskID, "task:"+d.FromTaskID, "task_dependency")
	}
	aggregated := map[string]*GraphEdge{}
	evidenceByEdge := map[string]map[string]struct{}{}
	for _, e := range scope.Effects {
		addNode(nodes, GraphNode{ID: e.SourceAgentRef, Kind: "agent", Label: e.SourceAgentRef})
		if e.TargetAgentRef != "" {
			addNode(nodes, GraphNode{ID: e.TargetAgentRef, Kind: "agent", Label: e.TargetAgentRef})
		}
		taskNode := "task:" + e.TargetTaskID
		addNode(nodes, GraphNode{ID: taskNode, Kind: "task", Label: e.TargetTaskID, ProjectID: e.ProjectID, TaskID: e.TargetTaskID})
		target := taskNode
		if e.TargetAgentRef != "" {
			target = e.TargetAgentRef
		}
		key := semanticEdgeKey(e.SourceAgentRef, target, e.RelationType, e.Polarity)
		edge := aggregated[key]
		if edge == nil {
			at := e.OccurredAt.UTC()
			edge = &GraphEdge{ID: semanticEdgeID(key), Source: e.SourceAgentRef, Target: target, RelationType: e.RelationType, Polarity: e.Polarity, Magnitude: e.Magnitude, EffectID: e.EffectID, InteractionCount: 0, FirstOccurredAt: &at, LastOccurredAt: &at}
			aggregated[key] = edge
		}
		edge.InteractionCount++
		if evidenceByEdge[key] == nil {
			evidenceByEdge[key] = map[string]struct{}{}
		}
		for _, id := range e.EvidenceEventIDs {
			if id != "" {
				evidenceByEdge[key][id] = struct{}{}
			}
		}
		edge.EvidenceCount = len(evidenceByEdge[key])
		if e.Magnitude > edge.Magnitude {
			edge.Magnitude = e.Magnitude
		}
		at := e.OccurredAt.UTC()
		if edge.FirstOccurredAt == nil || at.Before(*edge.FirstOccurredAt) {
			edge.FirstOccurredAt = &at
		}
		if edge.LastOccurredAt == nil || at.After(*edge.LastOccurredAt) {
			edge.LastOccurredAt = &at
		}
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
	aggKeys := make([]string, 0, len(aggregated))
	for k := range aggregated {
		aggKeys = append(aggKeys, k)
	}
	sort.Strings(aggKeys)
	for _, k := range aggKeys {
		res.Graph.Edges = append(res.Graph.Edges, *aggregated[k])
	}
	res.Graph.Edges = dedupeGraphEdges(res.Graph.Edges)
	keys := make([]string, 0, len(nodes))
	for k := range nodes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		res.Graph.Nodes = append(res.Graph.Nodes, nodes[k])
	}
	res.Summary.AffectedTaskCount = len(tasks)
	if f.MaxNodes > 0 && len(res.Graph.Nodes) > f.MaxNodes {
		res.Graph.Nodes = res.Graph.Nodes[:f.MaxNodes]
		res.Graph.Truncated = true
		res.Truncated = true
	}
	if res.Graph.LOD == "cluster" {
		res.Graph.Clusters = clusterNodes(res.Graph.Nodes)
	}
	return res
}

func addNode(m map[string]GraphNode, n GraphNode) {
	if n.ID != "" {
		m[n.ID] = n
	}
}

func addStaticEdge(edges *[]GraphEdge, source, target, rel string) {
	if source == "" || target == "" {
		return
	}
	key := semanticEdgeKey(source, target, RelationType(rel), PolarityNeutral)
	*edges = append(*edges, GraphEdge{ID: semanticEdgeID(key), Source: source, Target: target, RelationType: RelationType(rel), Polarity: PolarityNeutral, Magnitude: 1})
}

func dedupeGraphEdges(edges []GraphEdge) []GraphEdge {
	seen := make(map[string]struct{}, len(edges))
	out := edges[:0]
	for _, e := range edges {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		out = append(out, e)
	}
	return out
}

func semanticEdgeID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "cge_" + hex.EncodeToString(sum[:12])
}

func semanticEdgeKey(source, target string, relation RelationType, polarity Polarity) string {
	return strings.Join([]string{"edge", source, target, string(relation), string(polarity)}, "\x00")
}

func computeGraphVersion(ruleVersion string, scope graphScope) string {
	h := sha256.New()
	_, _ = h.Write([]byte(ruleVersion))
	for _, e := range scope.Effects {
		_, _ = h.Write([]byte("\x00" + e.EffectID + "\x00" + e.OccurredAt.UTC().Format(time.RFC3339Nano)))
	}
	for _, p := range scope.Projects {
		_, _ = h.Write([]byte("\x00p:" + p.ID))
	}
	for _, p := range scope.Plans {
		_, _ = h.Write([]byte("\x00pl:" + p.ID))
	}
	for _, s := range scope.Stages {
		_, _ = h.Write([]byte("\x00s:" + s.ID))
	}
	for _, t := range scope.Tasks {
		_, _ = h.Write([]byte("\x00t:" + t.ID))
	}
	return "cgv_" + hex.EncodeToString(h.Sum(nil)[:12])
}

func clusterNodes(nodes []GraphNode) []GraphNode {
	seen := map[string]GraphNode{}
	for _, n := range nodes {
		if n.ProjectID == "" {
			continue
		}
		id := "cluster:project:" + n.ProjectID
		seen[id] = GraphNode{ID: id, Kind: "cluster", Label: n.ProjectID, ProjectID: n.ProjectID}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]GraphNode, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
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

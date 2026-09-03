package collaborationeffect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obssql "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestQueryServiceGraphPaginationSummaryAndEvidence(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(t.TempDir() + "/query.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	repo, _ := NewSQLiteRepository(db)
	events, err := obssql.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := NewQueryService(repo, events)
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	eventID := idgen.MustNewULID()
	ev, err := observability.NewEvent(observability.NewEventInput{ID: observability.EventID(eventID), Seq: 1, OccurredAt: at, CreatedAt: at, EventType: "pm.task.state_changed", Actor: "agent:a", Refs: observability.EventRefs{ProjectID: "P1", TaskID: "T1"}, Payload: map[string]any{"prev_status": "running", "status": "completed"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = events.Append(ctx, ev); err != nil {
		t.Fatal(err)
	}
	effects := []Effect{{EffectID: "ce_01", ProjectID: "P1", TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: RelationComplete, Polarity: PolarityPositive, Magnitude: 2, OccurredAt: at, RuleVersion: RuleVersionV1, EvidenceEventIDs: []string{eventID}, BeforeState: map[string]any{"task_status": "running"}, AfterState: map[string]any{"task_status": "completed"}, ExplanationKey: "collaboration.effect.complete"}, {EffectID: "ce_02", ProjectID: "P1", TargetTaskID: "T2", SourceAgentRef: "agent:b", RelationType: RelationBlock, Polarity: PolarityNegative, Magnitude: 2, OccurredAt: at, RuleVersion: RuleVersionV1}}
	for i, e := range effects {
		if err = repo.Apply(ctx, Fact{EventID: string(rune('a' + i)), OccurredAt: at}, RuleVersionV1, []Effect{e}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err := svc.Query(ctx, Filter{ProjectID: "P1", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Effects) != 1 || !page.Truncated || page.NextCursor != "ce_01" || page.Summary.PositiveCount != 1 || len(page.Graph.Nodes) != 2 {
		t.Fatalf("bad first page: %+v", page)
	}
	page2, err := svc.Query(ctx, Filter{ProjectID: "P1", Cursor: page.NextCursor, Limit: 1})
	if err != nil || len(page2.Effects) != 1 || page2.Effects[0].EffectID != "ce_02" || page2.Truncated {
		t.Fatalf("bad second page: %+v err=%v", page2, err)
	}
	detail, err := svc.Evidence(ctx, "ce_01", "P1")
	if err != nil {
		t.Fatal(err)
	}
	if detail.RuleVersion != RuleVersionV1 || detail.ExplanationKey == "" || len(detail.Evidence) != 1 || detail.Evidence[0].EventID != eventID {
		t.Fatalf("bad evidence: %+v", detail)
	}
	if _, err = svc.Evidence(ctx, "ce_01", "P2"); !errors.Is(err, ErrEffectNotFound) {
		t.Fatalf("cross-project err=%v", err)
	}
}

func TestQueryServiceStableValidationErrors(t *testing.T) {
	ctx := context.Background()
	db, _ := persistence.Open(t.TempDir() + "/validation.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(ctx)
	repo, _ := NewSQLiteRepository(db)
	events, _ := obssql.NewEventRepo(ctx, db)
	svc, _ := NewQueryService(repo, events)
	if _, err := svc.Query(ctx, Filter{}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("missing project err=%v", err)
	}
	if _, err := svc.Query(ctx, Filter{ProjectID: "P1", Limit: 501}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatalf("large limit err=%v", err)
	}
	if _, err := svc.Query(ctx, Filter{ProjectID: "P1", Cursor: "not-opaque"}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cursor err=%v", err)
	}
}

func TestQueryServiceGraphUsesStableSemanticEdgesAcrossCursorPages(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(t.TempDir() + "/semantic-pages.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	repo, _ := NewSQLiteRepository(db)
	events, err := obssql.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := NewQueryService(repo, events)
	at := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	effects := []Effect{
		{EffectID: "ce_01", ProjectID: "P1", TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: RelationComplete, Polarity: PolarityPositive, Magnitude: 1, OccurredAt: at, RuleVersion: RuleVersionV1, EvidenceEventIDs: []string{"evt-1"}},
		{EffectID: "ce_02", ProjectID: "P1", TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: RelationComplete, Polarity: PolarityPositive, Magnitude: 3, OccurredAt: at.Add(time.Hour), RuleVersion: RuleVersionV1, EvidenceEventIDs: []string{"evt-2"}},
		{EffectID: "ce_03", ProjectID: "P1", TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: RelationComplete, Polarity: PolarityNegative, Magnitude: 2, OccurredAt: at.Add(2 * time.Hour), RuleVersion: RuleVersionV1, EvidenceEventIDs: []string{"evt-3"}},
		{EffectID: "ce_04", ProjectID: "P1", TargetTaskID: "T1", SourceAgentRef: "agent:a", RelationType: RelationBlock, Polarity: PolarityPositive, Magnitude: 2, OccurredAt: at.Add(3 * time.Hour), RuleVersion: RuleVersionV1, EvidenceEventIDs: []string{"evt-4"}},
	}
	for i, effect := range effects {
		if err = repo.Apply(ctx, Fact{EventID: string(rune('a' + i)), OccurredAt: effect.OccurredAt}, RuleVersionV1, []Effect{effect}, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := svc.Query(ctx, Filter{ProjectID: "P1", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	page2, err := svc.Query(ctx, Filter{ProjectID: "P1", Cursor: page1.NextCursor, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Graph.Edges) != 1 || len(page2.Graph.Edges) != 1 {
		t.Fatalf("paged graph edges = %d/%d, want 1/1", len(page1.Graph.Edges), len(page2.Graph.Edges))
	}
	if page1.Graph.Edges[0].ID == "" || page1.Graph.Edges[0].ID != page2.Graph.Edges[0].ID {
		t.Fatalf("semantic edge ids not stable across pages: %q vs %q", page1.Graph.Edges[0].ID, page2.Graph.Edges[0].ID)
	}

	full, err := svc.Query(ctx, Filter{ProjectID: "P1", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Graph.Edges) != 3 {
		t.Fatalf("full graph edges = %d, want 3 distinct semantic edges: %+v", len(full.Graph.Edges), full.Graph.Edges)
	}
	merged := findGraphEdge(full.Graph.Edges, RelationComplete, PolarityPositive)
	if merged == nil {
		t.Fatalf("missing merged complete/positive edge: %+v", full.Graph.Edges)
	}
	if merged.InteractionCount != 2 || merged.EvidenceCount != 2 || merged.Magnitude != 3 || !merged.Clustered {
		t.Fatalf("merged edge counters = %+v, want count=2 evidence=2 magnitude=max(3) clustered", *merged)
	}
	if !merged.FirstOccurredAt.Equal(at) || !merged.LastOccurredAt.Equal(at.Add(time.Hour)) {
		t.Fatalf("merged time bounds = %s/%s", merged.FirstOccurredAt, merged.LastOccurredAt)
	}
	if findGraphEdge(full.Graph.Edges, RelationComplete, PolarityNegative) == nil || findGraphEdge(full.Graph.Edges, RelationBlock, PolarityPositive) == nil {
		t.Fatalf("different polarity/relation were incorrectly merged away: %+v", full.Graph.Edges)
	}
}

func findGraphEdge(edges []GraphEdge, relation RelationType, polarity Polarity) *GraphEdge {
	for i := range edges {
		if edges[i].RelationType == relation && edges[i].Polarity == polarity {
			return &edges[i]
		}
	}
	return nil
}

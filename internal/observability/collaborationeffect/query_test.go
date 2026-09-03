package collaborationeffect

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
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

func TestQueryServiceOrgGraphAggregatesRealProjectPlanStageTaskFixture(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(t.TempDir() + "/org-graph.db")
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
	graphs, err := NewSQLiteGraphReader(db)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := NewQueryServiceWithGraph(repo, events, graphs)
	base := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	seedOrgGraphFixture(t, ctx, db, repo, events, base)

	first, err := svc.Query(ctx, Filter{OrganizationID: "org-g", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if first.RuleVersion != RuleVersionV1 || first.GraphVersion == "" || len(first.Effects) != 9 {
		t.Fatalf("bad org graph envelope: %+v", first)
	}
	if !hasNode(first.Graph.Nodes, "project:proj-a", "project") || !hasNode(first.Graph.Nodes, "project:proj-b", "project") ||
		!hasNode(first.Graph.Nodes, "plan:plan-a", "plan") || !hasNode(first.Graph.Nodes, "stage:stage-a", "stage") ||
		!hasNode(first.Graph.Nodes, "task:task-a1", "task") || !hasNode(first.Graph.Nodes, "agent:alpha", "agent") {
		t.Fatalf("missing structure/agent nodes: %+v", first.Graph.Nodes)
	}
	for _, want := range []string{"project_plan", "plan_task", "plan_stage", "stage_task", "agent_task", "agent_plan", "task_dependency"} {
		if !hasRelation(first.Graph.Edges, RelationType(want)) {
			t.Fatalf("missing structural relation %s in %+v", want, first.Graph.Edges)
		}
	}
	complete := findEdge(first.Graph.Edges, "agent:alpha", "task:task-a1", RelationComplete, PolarityPositive)
	if complete == nil || complete.InteractionCount != 2 || complete.EvidenceCount != 2 ||
		complete.FirstOccurredAt == nil || !complete.FirstOccurredAt.Equal(base.Add(time.Minute)) ||
		complete.LastOccurredAt == nil || !complete.LastOccurredAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("complete edge not semantically aggregated: %+v", complete)
	}
	if agentAgent := findEdge(first.Graph.Edges, "agent:alpha", "agent:beta", RelationReassign, PolarityMixed); agentAgent == nil || agentAgent.InteractionCount != 1 {
		t.Fatalf("missing real agent-agent edge: %+v", first.Graph.Edges)
	}
	again, err := svc.Query(ctx, Filter{OrganizationID: "org-g", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if first.GraphVersion != again.GraphVersion || findEdge(again.Graph.Edges, "agent:alpha", "task:task-a1", RelationComplete, PolarityPositive).ID != complete.ID {
		t.Fatalf("graph replay not deterministic: %s/%s", first.GraphVersion, again.GraphVersion)
	}
	unchanged, err := svc.Query(ctx, Filter{OrganizationID: "org-g", Limit: 10, GraphVersion: first.GraphVersion})
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.Unchanged || unchanged.GraphVersion != first.GraphVersion || len(unchanged.Effects) != 0 || len(unchanged.Graph.Nodes) != 0 || len(unchanged.Graph.Edges) != 0 {
		t.Fatalf("graph_version dedupe failed: %+v", unchanged)
	}

	planOnly, err := svc.Query(ctx, Filter{OrganizationID: "org-g", PlanID: "plan-a", RelationType: RelationComplete, Polarity: PolarityPositive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(planOnly.Effects) != 5 || !hasNode(planOnly.Graph.Nodes, "plan:plan-a", "plan") || hasNode(planOnly.Graph.Nodes, "plan:plan-b", "plan") {
		t.Fatalf("plan/relation/polarity filter failed: %+v", planOnly)
	}
	stageOnly, err := svc.Query(ctx, Filter{OrganizationID: "org-g", StageID: "stage-a", AgentRef: "agent:alpha", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(stageOnly.Effects) != 3 || !hasNode(stageOnly.Graph.Nodes, "stage:stage-a", "stage") || hasNode(stageOnly.Graph.Nodes, "stage:stage-b", "stage") {
		t.Fatalf("stage/agent filter failed: %+v", stageOnly)
	}

	page1, err := svc.Query(ctx, Filter{OrganizationID: "org-g", AgentRef: "agent:alpha", Limit: 2})
	if err != nil || !page1.Truncated || page1.NextCursor == "" || !strings.HasPrefix(page1.NextCursor, "cg_") {
		t.Fatalf("bad first page: %+v err=%v", page1, err)
	}
	page2, err := svc.Query(ctx, Filter{OrganizationID: "org-g", AgentRef: "agent:alpha", Cursor: page1.NextCursor, Limit: 2})
	if err != nil || page2.Truncated || len(page2.Effects) != 2 || page2.Effects[0].EffectID == page1.Effects[0].EffectID {
		t.Fatalf("bad second page: %+v err=%v", page2, err)
	}
	sameEdgePage1, err := svc.Query(ctx, Filter{OrganizationID: "org-g", Limit: 1})
	if err != nil || !sameEdgePage1.Truncated || sameEdgePage1.NextCursor == "" {
		t.Fatalf("bad same-edge first cursor page: %+v err=%v", sameEdgePage1, err)
	}
	sameEdgePage2, err := svc.Query(ctx, Filter{OrganizationID: "org-g", Cursor: sameEdgePage1.NextCursor, Limit: 1})
	if err != nil || len(sameEdgePage2.Effects) != 1 {
		t.Fatalf("bad same-edge second cursor page: %+v err=%v", sameEdgePage2, err)
	}
	firstComplete := findEdge(sameEdgePage1.Graph.Edges, "agent:alpha", "task:task-a1", RelationComplete, PolarityPositive)
	secondComplete := findEdge(sameEdgePage2.Graph.Edges, "agent:alpha", "task:task-a1", RelationComplete, PolarityPositive)
	if firstComplete == nil || secondComplete == nil || firstComplete.ID != secondComplete.ID {
		t.Fatalf("semantic edge id not stable across cursor pages: first=%+v second=%+v", firstComplete, secondComplete)
	}
	splitByRelation, err := svc.Query(ctx, Filter{OrganizationID: "org-g", AgentRef: "agent:beta", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if findEdge(splitByRelation.Graph.Edges, "agent:beta", "task:task-a2", RelationComplete, PolarityPositive) == nil ||
		findEdge(splitByRelation.Graph.Edges, "agent:beta", "task:task-a2", RelationComplete, PolarityNegative) == nil ||
		findEdge(splitByRelation.Graph.Edges, "agent:beta", "task:task-a2", RelationBlock, PolarityNegative) == nil {
		t.Fatalf("relation/polarity edges were incorrectly merged: %+v", splitByRelation.Graph.Edges)
	}
	dupEvidence, err := svc.Query(ctx, Filter{OrganizationID: "org-g", AgentRef: "agent:gamma", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	gammaComplete := findEdge(dupEvidence.Graph.Edges, "agent:gamma", "task:task-a2", RelationComplete, PolarityPositive)
	if gammaComplete == nil || gammaComplete.InteractionCount != 2 || gammaComplete.EvidenceCount != 2 {
		t.Fatalf("duplicate evidence not deduped on semantic edge: %+v", gammaComplete)
	}
	clustered, err := svc.Query(ctx, Filter{OrganizationID: "org-g", LOD: "cluster", MaxNodes: 7, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if clustered.Graph.LOD != "cluster" || len(clustered.Graph.Clusters) == 0 || !clustered.Graph.Truncated || !clustered.Truncated {
		t.Fatalf("LOD/truncated missing: %+v", clustered.Graph)
	}
}

func seedOrgGraphFixture(t *testing.T, ctx context.Context, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, repo *SQLiteRepository, events observability.EventRepository, base time.Time) {
	t.Helper()
	ts := func(offset time.Duration) string { return base.Add(offset).Format(time.RFC3339Nano) }
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'active', 'user:test', ?, ?)`, []any{"proj-a", "org-g", "Project A", ts(0), ts(0)}},
		{`INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'active', 'user:test', ?, ?)`, []any{"proj-b", "org-g", "Project B", ts(0), ts(0)}},
		{`INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'active', 'user:test', ?, ?)`, []any{"proj-c", "org-other", "Project C", ts(0), ts(0)}},
		{`INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, created_at, updated_at) VALUES (?, ?, ?, '', 'running', 'user:test', ?, ?)`, []any{"plan-a", "proj-a", "Plan A", ts(0), ts(0)}},
		{`INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, created_at, updated_at) VALUES (?, ?, ?, '', 'running', 'user:test', ?, ?)`, []any{"plan-b", "proj-b", "Plan B", ts(0), ts(0)}},
		{`INSERT INTO pm_stages (id, plan_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, []any{"stage-a", "plan-a", "Stage A", ts(0), ts(0)}},
		{`INSERT INTO pm_stages (id, plan_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, []any{"stage-b", "plan-b", "Stage B", ts(0), ts(0)}},
		{`INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, plan_id, stage_id, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'running', ?, ?, ?, 'user:test', ?, ?)`, []any{"task-a1", "proj-a", "Task A1", "agent:alpha", "plan-a", "stage-a", ts(0), ts(0)}},
		{`INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, plan_id, stage_id, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'running', ?, ?, ?, 'user:test', ?, ?)`, []any{"task-a2", "proj-a", "Task A2", "agent:beta", "plan-a", "stage-a", ts(0), ts(0)}},
		{`INSERT INTO pm_tasks (id, project_id, title, description, status, assignee, plan_id, stage_id, created_by, created_at, updated_at) VALUES (?, ?, ?, '', 'running', ?, ?, ?, 'user:test', ?, ?)`, []any{"task-b1", "proj-b", "Task B1", "agent:alpha", "plan-b", "stage-b", ts(0), ts(0)}},
		{`INSERT INTO pm_task_dependencies (plan_id, from_task_id, to_task_id, kind, "when", max_rounds) VALUES ('plan-a', 'task-a2', 'task-a1', 'seq', '', 0)`, nil},
	} {
		if _, err := db.ExecContext(ctx, stmt.q, stmt.args...); err != nil {
			t.Fatalf("seed SQL: %v\n%s", err, stmt.q)
		}
	}
	eventIDs := idgen.NewGenerator(clock.NewFakeClock(base))
	var seq int64
	seedEffect := func(id, task, source, target string, rel RelationType, pol Polarity, at time.Time, reuseEventID ...string) string {
		t.Helper()
		eventID := eventIDs.NewULID()
		if len(reuseEventID) > 0 {
			eventID = reuseEventID[0]
		}
		seq++
		if len(reuseEventID) == 0 {
			ev, err := observability.NewEvent(observability.NewEventInput{ID: observability.EventID(eventID), Seq: seq, OccurredAt: at, CreatedAt: at, EventType: "pm.task.state_changed", Actor: observability.Actor(source), Refs: observability.EventRefs{ProjectID: "proj-a", TaskID: task}, Payload: map[string]any{"status": "completed"}})
			if err != nil {
				t.Fatal(err)
			}
			if err = events.Append(ctx, ev); err != nil {
				t.Fatal(err)
			}
		}
		evidenceIDs := []string{eventID}
		if len(reuseEventID) > 0 {
			evidenceIDs = append([]string(nil), reuseEventID...)
		}
		e := Effect{EffectID: id, ProjectID: "proj-a", TargetTaskID: task, SourceAgentRef: source, TargetAgentRef: target, RelationType: rel, Polarity: pol, Magnitude: 2, Confidence: "high", OccurredAt: at, RuleVersion: RuleVersionV1, EvidenceEventIDs: evidenceIDs, BeforeState: map[string]any{"task_status": "running"}, AfterState: map[string]any{"task_status": "completed"}, ExplanationKey: "collaboration.effect." + string(rel)}
		if task == "task-b1" {
			e.ProjectID = "proj-b"
		}
		if err := repo.Apply(ctx, Fact{EventID: id, OccurredAt: at}, RuleVersionV1, []Effect{e}, nil, nil); err != nil {
			t.Fatal(err)
		}
		return eventID
	}
	seedEffect("ce_0001", "task-a1", "agent:alpha", "", RelationComplete, PolarityPositive, base.Add(time.Minute))
	seedEffect("ce_0002", "task-a1", "agent:alpha", "", RelationComplete, PolarityPositive, base.Add(2*time.Minute))
	seedEffect("ce_0003", "task-a2", "agent:alpha", "agent:beta", RelationReassign, PolarityMixed, base.Add(3*time.Minute))
	seedEffect("ce_0004", "task-b1", "agent:alpha", "", RelationBlock, PolarityNegative, base.Add(4*time.Minute))
	seedEffect("ce_0005", "task-a2", "agent:beta", "", RelationComplete, PolarityPositive, base.Add(5*time.Minute))
	seedEffect("ce_0006", "task-a2", "agent:beta", "", RelationComplete, PolarityNegative, base.Add(6*time.Minute))
	seedEffect("ce_0007", "task-a2", "agent:beta", "", RelationBlock, PolarityNegative, base.Add(7*time.Minute))
	reused := seedEffect("ce_0008", "task-a2", "agent:gamma", "", RelationComplete, PolarityPositive, base.Add(8*time.Minute))
	seedEffect("ce_0009", "task-a2", "agent:gamma", "", RelationComplete, PolarityPositive, base.Add(9*time.Minute), reused, "evt-extra")
}

func hasNode(nodes []GraphNode, id, kind string) bool {
	for _, n := range nodes {
		if n.ID == id && n.Kind == kind {
			return true
		}
	}
	return false
}

func hasRelation(edges []GraphEdge, rel RelationType) bool {
	for _, e := range edges {
		if e.RelationType == rel {
			return true
		}
	}
	return false
}

func findEdge(edges []GraphEdge, source, target string, rel RelationType, pol Polarity) *GraphEdge {
	for i := range edges {
		e := &edges[i]
		if e.Source == source && e.Target == target && e.RelationType == rel && e.Polarity == pol {
			return e
		}
	}
	return nil
}

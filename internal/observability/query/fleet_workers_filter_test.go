package query_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/observability/query"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestFleetSnapshot_WorkerProjectFilter_NoMappingsForFilteredProject(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	// Worker has NO mapping for proj-a, so should be filtered out.
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{ProjectID: "proj-a"})
	if len(snap.Workers) != 0 {
		t.Fatalf("expected 0 workers w/o matching mapping, got %d", len(snap.Workers))
	}
}

func TestFleetSnapshot_WorkerProjectFilter_WithMapping(t *testing.T) {
	env := newQEnv(t)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedProject(t, "proj-a", "A")
	// Add a mapping
	m, err := workforce.NewWorkerProjectMapping(workforce.NewMappingInput{
		ID: "M-1", WorkerID: "W-1", ProjectID: "proj-a", BasePath: "/tmp",
		AddedAt: env.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.Mappings.Save(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{ProjectID: "proj-a"})
	if len(snap.Workers) != 1 {
		t.Fatalf("expected 1 worker w/ matching mapping, got %d", len(snap.Workers))
	}
}

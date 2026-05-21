package query_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/observability/query"
)

// Tests for low-coverage branches that just need exercise.

func TestQuery_RepoNotWiredErrors(t *testing.T) {
	svc := query.NewService(query.Deps{})
	for _, r := range []string{"tasks", "executions", "workers", "issues", "input_requests", "proposals", "events"} {
		_, err := svc.Query(context.Background(), r, query.QueryFilter{})
		if err == nil {
			t.Errorf("%s should error when repo missing", r)
		}
	}
}

func TestInspect_RepoNotWiredErrors(t *testing.T) {
	svc := query.NewService(query.Deps{})
	for _, k := range []string{"task", "execution", "worker", "issue", "conversation", "input_request", "project", "worktree"} {
		_, err := svc.Inspect(context.Background(), k, "X")
		if err == nil {
			t.Errorf("%s should error when repo missing", k)
		}
	}
}

func TestStats_RepoNotWiredErrors(t *testing.T) {
	svc := query.NewStatsService(query.Deps{})
	for _, sc := range []string{"tasks", "executions", "workers", "events", "issues"} {
		_, err := svc.Aggregate(context.Background(), sc, nil)
		if err == nil {
			t.Errorf("%s should error when repo missing", sc)
		}
	}
}

func TestLogs_RepoNotWiredErrors(t *testing.T) {
	svc := query.NewLogsService(query.Deps{}, nil)
	_, _, err := svc.Open(context.Background(), query.LogsRequest{Kind: query.LogsTask, ID: "T-1"})
	if err == nil {
		t.Fatal("expected err: blob store not wired")
	}
}

func TestQuery_Proposals_StatusButNoWorker(t *testing.T) {
	env := newQEnv(t)
	res, err := env.svc.Query(context.Background(), "proposals", query.QueryFilter{Status: "accepted"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 {
		t.Fatalf("expected 0, got %d", len(res.Items))
	}
}

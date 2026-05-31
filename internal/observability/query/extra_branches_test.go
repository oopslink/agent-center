package query_test

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/observability/query"
)

// Tests for low-coverage branches that just need exercise.

func TestQuery_RepoNotWiredErrors(t *testing.T) {
	svc := query.NewService(query.Deps{})
	for _, r := range []string{"tasks", "executions", "workers", "issues", "events"} {
		_, err := svc.Query(context.Background(), r, query.QueryFilter{})
		if err == nil {
			t.Errorf("%s should error when repo missing", r)
		}
	}
}

func TestInspect_RepoNotWiredErrors(t *testing.T) {
	svc := query.NewService(query.Deps{})
	for _, k := range []string{"task", "execution", "worker", "issue", "conversation", "project", "worktree"} {
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

// TestQuery_Proposals_StatusButNoWorker removed — the `query proposals` verb is
// deleted in v2.7 #131 (workforce WorkerProjectProposal model retired; no
// new-model equivalent). "proposals" now resolves to ErrQueryResourceUnknown.

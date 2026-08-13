package sqlite

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition/ruleregistry"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestAuditRepoAppendLoadedIdempotentAndListsSorted(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewAuditRepo(db)
	base := ruleregistry.LoadAudit{
		ExecutionID:      "exec-1",
		TeamID:           "team-1",
		TeamMemoryCommit: "0123456789012345678901234567890123456789",
		RuleSlug:         "z-rule",
		Phase:            "execute",
		AgentID:          "agent-1",
		LoadedAt:         time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC),
	}
	inserted, err := repo.AppendLoaded(ctx, base)
	if err != nil || !inserted {
		t.Fatalf("first append = (%v,%v), want inserted", inserted, err)
	}
	inserted, err = repo.AppendLoaded(ctx, base)
	if err != nil || inserted {
		t.Fatalf("duplicate append = (%v,%v), want idempotent no-op", inserted, err)
	}
	base.RuleSlug = "a-rule"
	base.LoadedAt = base.LoadedAt.Add(time.Second)
	if inserted, err = repo.AppendLoaded(ctx, base); err != nil || !inserted {
		t.Fatalf("second slug append = (%v,%v), want inserted", inserted, err)
	}

	got, err := repo.ListByExecutionIDs(ctx, []string{"exec-1"})
	if err != nil {
		t.Fatal(err)
	}
	rows := got["exec-1"]
	if len(rows) != 2 || rows[0].RuleSlug != "a-rule" || rows[1].RuleSlug != "z-rule" {
		t.Fatalf("rows = %+v, want sorted unique slugs", rows)
	}
}

func TestAuditRepoAppendLoadedPlanningSessionIdempotentAndListsSorted(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewAuditRepo(db)
	base := ruleregistry.LoadAudit{
		PlanningSessionID: "agent:agent-1/generation:7",
		TeamID:            "team-1",
		TeamMemoryCommit:  "0123456789012345678901234567890123456789",
		RuleSlug:          "z-rule",
		Phase:             "plan",
		AgentID:           "agent-1",
		LoadedAt:          time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC),
	}
	inserted, err := repo.AppendLoaded(ctx, base)
	if err != nil || !inserted {
		t.Fatalf("first planning append = (%v,%v), want inserted", inserted, err)
	}
	inserted, err = repo.AppendLoaded(ctx, base)
	if err != nil || inserted {
		t.Fatalf("duplicate planning append = (%v,%v), want idempotent no-op", inserted, err)
	}
	base.RuleSlug = "a-rule"
	base.LoadedAt = base.LoadedAt.Add(time.Second)
	if inserted, err = repo.AppendLoaded(ctx, base); err != nil || !inserted {
		t.Fatalf("second planning slug append = (%v,%v), want inserted", inserted, err)
	}

	got, err := repo.ListByPlanningSessionIDs(ctx, []string{" agent:agent-1/generation:7 ", "agent:agent-1/generation:7"})
	if err != nil {
		t.Fatal(err)
	}
	rows := got["agent:agent-1/generation:7"]
	if len(rows) != 2 || rows[0].RuleSlug != "a-rule" || rows[1].RuleSlug != "z-rule" {
		t.Fatalf("planning rows = %+v, want sorted unique slugs", rows)
	}
	if rows[0].ExecutionID != "" || rows[0].PlanningSessionID != "agent:agent-1/generation:7" {
		t.Fatalf("planning scope not preserved: %+v", rows[0])
	}
	byExec, err := repo.ListByExecutionIDs(ctx, []string{"agent:agent-1/generation:7"})
	if err != nil {
		t.Fatal(err)
	}
	if len(byExec) != 0 {
		t.Fatalf("planning audit leaked into execution lookup: %+v", byExec)
	}
}

func TestAuditRepoAppendLoadedConcurrentDuplicateReadIsSingleFact(t *testing.T) {
	for _, tc := range []struct {
		name  string
		audit ruleregistry.LoadAudit
		list  func(context.Context, *AuditRepo) (map[string][]ruleregistry.LoadAudit, string, error)
	}{
		{
			name: "execution",
			audit: ruleregistry.LoadAudit{
				ExecutionID:      "exec-concurrent",
				TeamID:           "team-1",
				TeamMemoryCommit: "0123456789012345678901234567890123456789",
				RuleSlug:         "same-rule",
				Phase:            "execute",
				AgentID:          "agent-1",
			},
			list: func(ctx context.Context, repo *AuditRepo) (map[string][]ruleregistry.LoadAudit, string, error) {
				rows, err := repo.ListByExecutionIDs(ctx, []string{"exec-concurrent"})
				return rows, "exec-concurrent", err
			},
		},
		{
			name: "planning",
			audit: ruleregistry.LoadAudit{
				PlanningSessionID: "agent:agent-1/generation:9",
				TeamID:            "team-1",
				TeamMemoryCommit:  "0123456789012345678901234567890123456789",
				RuleSlug:          "same-rule",
				Phase:             "plan",
				AgentID:           "agent-1",
			},
			list: func(ctx context.Context, repo *AuditRepo) (map[string][]ruleregistry.LoadAudit, string, error) {
				rows, err := repo.ListByPlanningSessionIDs(ctx, []string{"agent:agent-1/generation:9"})
				return rows, "agent:agent-1/generation:9", err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := persistence.Open(persistence.MemoryDSN())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err := persistence.NewMigrator(db).Up(ctx); err != nil {
				t.Fatal(err)
			}
			repo := NewAuditRepo(db)

			var inserted atomic.Int32
			errs := make(chan error, 32)
			var wg sync.WaitGroup
			for i := 0; i < cap(errs); i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					ok, err := repo.AppendLoaded(ctx, tc.audit)
					if err != nil {
						errs <- err
						return
					}
					if ok {
						inserted.Add(1)
					}
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				t.Fatalf("concurrent append error: %v", err)
			}
			if got := inserted.Load(); got != 1 {
				t.Fatalf("inserted count = %d, want exactly one durable fact", got)
			}
			got, key, err := tc.list(ctx, repo)
			if err != nil {
				t.Fatal(err)
			}
			if rows := got[key]; len(rows) != 1 || rows[0].RuleSlug != "same-rule" {
				t.Fatalf("rows = %+v, want one idempotent audit fact", rows)
			}
		})
	}
}

func TestAuditRepoSchemaDoesNotPersistRuleBody(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'team_rule_load_audits'`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(schema), "body") {
		t.Fatalf("team_rule_load_audits must not persist rule body, schema=%s", schema)
	}
}

func TestAuditRepoRequiresExecutionOrPlanningScope(t *testing.T) {
	ctx := context.Background()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewAuditRepo(db)
	_, err = repo.AppendLoaded(ctx, ruleregistry.LoadAudit{
		TeamID: "team-1", TeamMemoryCommit: "c", RuleSlug: "rule", Phase: "execute",
	})
	if err != ErrAuditScopeRequired {
		t.Fatalf("scope error = %v, want ErrAuditScopeRequired", err)
	}
}

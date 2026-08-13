package sqlite

import (
	"context"
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

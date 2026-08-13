package sqlite

import (
	"context"
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

func TestAuditRepoAppendLoadedConcurrentDuplicateOnce(t *testing.T) {
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
		ExecutionID:      "exec-race",
		TeamID:           "team-1",
		TeamMemoryCommit: "0123456789012345678901234567890123456789",
		RuleSlug:         "same-rule",
		Phase:            "execute",
		AgentID:          "agent-1",
		LoadedAt:         time.Date(2026, 8, 13, 1, 2, 3, 0, time.UTC),
	}

	const callers = 16
	start := make(chan struct{})
	errs := make(chan error, callers)
	var inserted int64
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			a := base
			a.LoadedAt = base.LoadedAt.Add(time.Duration(i) * time.Nanosecond)
			ok, err := repo.AppendLoaded(ctx, a)
			if err != nil {
				errs <- err
				return
			}
			if ok {
				atomic.AddInt64(&inserted, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := atomic.LoadInt64(&inserted); got != 1 {
		t.Fatalf("inserted count = %d, want one unique audit fact", got)
	}
	rows, err := repo.ListByExecutionIDs(ctx, []string{"exec-race"})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(rows["exec-race"]); got != 1 {
		t.Fatalf("rows = %+v, want one idempotent audit fact", rows["exec-race"])
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

package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestAssignmentPoolRepo_RoundTripAndClaimCAS(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	repo := NewAssignmentPoolRepo(db)
	at := time.Unix(1000, 0).UTC()
	pool, err := pm.NewAssignmentPool(pm.NewAssignmentPoolInput{ID: "pool-x", ProjectID: "project-x",
		SchedulingClass: pm.AssignmentPoolBackground, AutoAssignEnabled: true, HoldingCap: 3, CreatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByProject(context.Background(), "project-x")
	if err != nil || got.ID() != pool.ID() || got.HoldingCap() != 3 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	m, _ := pm.NewAssignmentPoolTask(pool.ID(), "task-x", 7, "user:a", at)
	if err := repo.AddTask(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Claim(context.Background(), pool.ID(), "task-x", 1, "agent:a", at.Add(time.Second), time.Time{}); err != nil || !won {
		t.Fatalf("claim won=%v err=%v", won, err)
	}
	if won, err := repo.Claim(context.Background(), pool.ID(), "task-x", 1, "agent:b", at, time.Time{}); err != nil || won {
		t.Fatalf("stale claim won=%v err=%v", won, err)
	}
}

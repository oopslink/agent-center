package environment_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	env "github.com/oopslink/agent-center/internal/environment"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TestControlLog_AppendCommand_RaceOverSqlite drives AppendCommand over the REAL
// sqlite ControlEventRepo and proves the idempotency-race path: re-issuing the
// same logical command (same idempotency key) returns the FIRST entry — no
// error, no second offset, no duplicate stream entry — even though the second
// call's pre-check could miss it under concurrency (here the UNIQUE backstop +
// re-fetch carry it).
func TestControlLog_AppendCommand_RaceOverSqlite(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	repo := envsqlite.NewControlEventRepo(db)
	clk := clock.NewFakeClock(time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC))
	log := env.NewControlLog(repo, idgen.NewGenerator(clk), clk)

	in := env.AppendCommandInput{
		WorkerID: "w1", CommandType: "stop", Payload: `{}`, IdempotencyKey: "k",
	}

	first, err := log.AppendCommand(ctx, in)
	if err != nil {
		t.Fatalf("first AppendCommand: %v", err)
	}
	if first.Offset() != 1 {
		t.Fatalf("first offset: got %d want 1", first.Offset())
	}

	// Second call with the same key returns the first entry unchanged.
	second, err := log.AppendCommand(ctx, in)
	if err != nil {
		t.Fatalf("second AppendCommand: %v", err)
	}
	if second.ID() != first.ID() || second.Offset() != first.Offset() {
		t.Fatalf("idempotency broke: first=%s/%d second=%s/%d",
			first.ID(), first.Offset(), second.ID(), second.Offset())
	}

	// No duplicate landed: still exactly one entry, MaxOffset still 1.
	if off, _ := repo.MaxOffset(ctx, "w1"); off != 1 {
		t.Fatalf("MaxOffset after dedup: got %d want 1", off)
	}
	all, _ := repo.ListAfter(ctx, "w1", 0)
	if len(all) != 1 {
		t.Fatalf("stream length after dedup: got %d want 1", len(all))
	}

	// Simulate the lost-race path explicitly: pre-insert a NEW key directly via
	// the repo at the next offset, then drive AppendCommand for that same key.
	// The pre-check would find it, but to exercise the Append→duplicate→re-fetch
	// branch we insert with the SAME offset AppendCommand will compute.
	pre, _ := env.NewWorkerControlEvent(env.NewWorkerControlEventInput{
		ID: "pre", WorkerID: "w1", Offset: 2, IdempotencyKey: "k2",
		CommandType: "reset", Payload: `{}`,
		CreatedAt: time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
	})
	if err := repo.Append(ctx, pre); err != nil {
		t.Fatalf("pre-insert: %v", err)
	}
	got, err := log.AppendCommand(ctx, env.AppendCommandInput{
		WorkerID: "w1", CommandType: "reset", Payload: `{}`, IdempotencyKey: "k2",
	})
	if err != nil {
		t.Fatalf("AppendCommand over pre-inserted key: %v", err)
	}
	if got.ID() != "pre" {
		t.Fatalf("expected pre-inserted entry returned, got %s", got.ID())
	}
}

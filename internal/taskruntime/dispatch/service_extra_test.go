package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	trsqlite "github.com/oopslink/agent-center/internal/taskruntime/sqlite"
)

func TestNewService_DefaultsApplied(t *testing.T) {
	db, _ := persistence.Open(persistence.MemoryDSN())
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	clk := clock.NewFakeClock(time.Now())
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	taskRepo := trsqlite.NewTaskRepo(db)
	execRepo := trsqlite.NewTaskExecutionRepo(db)
	// nil sender + nil clock + zero cfg should not panic and defaults
	// should be applied.
	svc := NewService(db, taskRepo, execRepo, sink, nil, nil, gen, DispatchConfig{})
	if svc.cfg.MaxExecutionsPerTask != 3 {
		t.Fatalf("expected default max=3: %d", svc.cfg.MaxExecutionsPerTask)
	}
	if svc.cfg.DispatchAckTimeout != 30*time.Second {
		t.Fatalf("expected default 30s: %v", svc.cfg.DispatchAckTimeout)
	}
}

func TestDefaultConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxExecutionsPerTask != 3 || cfg.DispatchAckTimeout != 30*time.Second {
		t.Fatalf("defaults: %+v", cfg)
	}
}

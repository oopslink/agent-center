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
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
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

// TestHandleNack_DoubleNackRejected: the second NACK on the same execution
// should be rejected as already-terminated.
func TestHandleNack_DoubleNackRejected(t *testing.T) {
	h := setup(t)
	seedTask(t, h, "T-1")
	res, _ := h.svc.Dispatch(context.Background(), DispatchInput{
		TaskID: "T-1", WorkerID: "W-1", AgentCLI: "claude-code", Actor: "user:hayang",
	})
	nack := DispatchNack{
		ExecutionID: res.ExecutionID, Accepted: false,
		Reason: execution.NackWorkerAtCapacity, Message: "boom",
		AckedAt: h.clk.Now(),
	}
	if err := h.svc.HandleNack(context.Background(), nack, "worker:W-1"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.HandleNack(context.Background(), nack, "worker:W-1"); err == nil {
		t.Fatal("expected error on double-nack")
	}
}

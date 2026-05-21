package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
)

func TestTimeoutHandler_Run_StopsOnCtxCancel(t *testing.T) {
	db := openSchedulerTestDB(t)
	repo := cognitiondb.NewInvocationRepo(db)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Now().UTC())
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	th, _ := scheduler.NewTimeoutHandler(scheduler.TimeoutConfig{
		TickInterval: 5 * time.Millisecond, Grace: time.Second,
	}, db, repo, nil, sink, clk)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- th.Run(ctx, nil) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("timed out waiting for Run to exit")
	}
}

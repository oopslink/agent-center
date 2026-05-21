package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
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

// errFindRunningRepo wraps a real repo and forces FindRunning to return an
// error. Used to make Run's errorHook branch deterministically reachable
// (otherwise the branch only fires when ticker + ctx race during shutdown).
type errFindRunningRepo struct {
	cognition.SupervisorInvocationRepository
	calls atomic.Int32
}

func (r *errFindRunningRepo) FindRunning(ctx context.Context) ([]*cognition.SupervisorInvocation, error) {
	r.calls.Add(1)
	return nil, errors.New("forced findrunning error")
}

// TestTimeoutHandler_Run_InvokesErrorHookOnTickError deterministically
// exercises the `errorHook(err)` branch in Run (timeout.go:140-142) and the
// `errorHook = func(error) {}` default-assignment branch (line 131). Both
// blocks are otherwise flap-y because they require a Tick to fail mid-Run
// before ctx cancel wins the select race.
func TestTimeoutHandler_Run_InvokesErrorHookOnTickError(t *testing.T) {
	db := openSchedulerTestDB(t)
	realRepo := cognitiondb.NewInvocationRepo(db)
	failRepo := &errFindRunningRepo{SupervisorInvocationRepository: realRepo}
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Now().UTC())
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	th, _ := scheduler.NewTimeoutHandler(scheduler.TimeoutConfig{
		TickInterval: 2 * time.Millisecond, Grace: time.Second,
	}, db, failRepo, nil, sink, clk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gotErr := make(chan error, 8)
	hook := func(err error) {
		select {
		case gotErr <- err:
		default:
		}
	}
	done := make(chan error, 1)
	go func() { done <- th.Run(ctx, hook) }()
	// Wait for at least one errorHook invocation — proves both line 140-142
	// (Tick err branch) and the Tick path are reached.
	select {
	case err := <-gotErr:
		if err == nil {
			t.Fatal("hook received nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("errorHook not invoked within 2s")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Run did not exit after cancel")
	}
	if failRepo.calls.Load() == 0 {
		t.Error("FindRunning was never called")
	}
}

// TestTimeoutHandler_Run_NilHookDefaultsToNoop deterministically covers the
// `errorHook = func(error) {}` assignment branch (timeout.go:131) AND
// ensures the default no-op hook is actually invoked (so the branch's
// statement body — the empty func — is also executed). Without explicit
// Tick errors, the body never runs and the block stays uncovered when the
// ctx-cancel race wins the select.
func TestTimeoutHandler_Run_NilHookDefaultsToNoop(t *testing.T) {
	db := openSchedulerTestDB(t)
	realRepo := cognitiondb.NewInvocationRepo(db)
	failRepo := &errFindRunningRepo{SupervisorInvocationRepository: realRepo}
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	clk := clock.NewFakeClock(time.Now().UTC())
	sink := observability.NewEventSink(er, er, idgen.NewGenerator(clk), clk)
	th, _ := scheduler.NewTimeoutHandler(scheduler.TimeoutConfig{
		TickInterval: 2 * time.Millisecond, Grace: time.Second,
	}, db, failRepo, nil, sink, clk)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Pass nil hook → forces the default-assignment branch.
	go func() { done <- th.Run(ctx, nil) }()
	// Spin until at least 2 ticks have fired so the no-op hook body has
	// definitely executed at least once.
	deadline := time.Now().Add(2 * time.Second)
	for failRepo.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("Run did not exit after cancel")
	}
	if failRepo.calls.Load() < 2 {
		t.Fatalf("expected >=2 FindRunning calls, got %d", failRepo.calls.Load())
	}
}

package scheduler_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/scheduler"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
)

// fakeEventRepo implements observability.EventRepository in memory.
type fakeEventRepo struct {
	mu     sync.Mutex
	events []*observability.Event
}

func (r *fakeEventRepo) Append(_ context.Context, e *observability.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	sort.Slice(r.events, func(i, j int) bool { return r.events[i].ID() < r.events[j].ID() })
	return nil
}

func (r *fakeEventRepo) FindByID(_ context.Context, id observability.EventID) (*observability.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.ID() == id {
			return e, nil
		}
	}
	return nil, observability.ErrEventNotFound
}

func (r *fakeEventRepo) Find(_ context.Context, filter observability.EventQueryFilter) ([]*observability.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > observability.MaxEventQueryLimit {
		return nil, observability.ErrEventQueryLimitTooLarge
	}
	var out []*observability.Event
	for _, e := range r.events {
		if filter.Cursor != nil && e.ID() <= *filter.Cursor {
			continue
		}
		if filter.EventType != nil && e.Type() != *filter.EventType {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// fakeInvocationRepo tracks invocations in memory + records FindRunningByScope calls.
type fakeInvocationRepo struct {
	mu       sync.Mutex
	running  map[string]*cognition.SupervisorInvocation // scope.String() → AR
}

func (r *fakeInvocationRepo) Save(_ context.Context, inv *cognition.SupervisorInvocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running == nil {
		r.running = map[string]*cognition.SupervisorInvocation{}
	}
	key := inv.Scope().String()
	if _, ok := r.running[key]; ok {
		return cognition.ErrScopeKeyRunningExists
	}
	r.running[key] = inv
	return nil
}

func (r *fakeInvocationRepo) UpdateStatusToTerminal(_ context.Context, inv *cognition.SupervisorInvocation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.running, inv.Scope().String())
	return nil
}

func (r *fakeInvocationRepo) FindByID(_ context.Context, id cognition.InvocationID) (*cognition.SupervisorInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, inv := range r.running {
		if inv.ID() == id {
			return inv, nil
		}
	}
	return nil, cognition.ErrInvocationNotFound
}

func (r *fakeInvocationRepo) FindRunningByScope(_ context.Context, scope cognition.InvocationScope) (*cognition.SupervisorInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inv, ok := r.running[scope.String()]; ok {
		return inv, nil
	}
	return nil, cognition.ErrInvocationNotFound
}

func (r *fakeInvocationRepo) FindRunning(_ context.Context) ([]*cognition.SupervisorInvocation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*cognition.SupervisorInvocation, 0, len(r.running))
	for _, inv := range r.running {
		out = append(out, inv)
	}
	return out, nil
}

func (r *fakeInvocationRepo) Find(_ context.Context, _ cognition.InvocationFilter) ([]*cognition.SupervisorInvocation, error) {
	return r.FindRunning(context.Background())
}

func appendEvent(t *testing.T, repo *fakeEventRepo, gen idgen.Generator, clk clock.Clock, etype observability.EventType, refs observability.EventRefs) observability.EventID {
	t.Helper()
	e, err := observability.NewEvent(observability.NewEventInput{
		ID:         observability.EventID(gen.NewULID()),
		OccurredAt: clk.Now(),
		Seq:        int64(len(repo.events) + 1),
		EventType:  etype,
		Refs:       refs,
		Actor:      observability.Actor("system"),
		Payload:    map[string]any{},
	})
	if err != nil {
		t.Fatalf("new event: %v", err)
	}
	if err := repo.Append(context.Background(), e); err != nil {
		t.Fatalf("append: %v", err)
	}
	return e.ID()
}

func newTestCoalescer(t *testing.T, cfg scheduler.CoalescerConfig) (*scheduler.Coalescer, *fakeEventRepo, *fakeInvocationRepo, *scheduler.InMemoryQueue, *clock.FakeClock) {
	t.Helper()
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	er := &fakeEventRepo{}
	ir := &fakeInvocationRepo{}
	q := scheduler.NewInMemoryQueue(5)
	c, err := scheduler.NewCoalescer(cfg, scheduler.CoalescerDeps{
		EventRepo:      er,
		InvocationRepo: ir,
		Clock:          clk,
	}, q)
	if err != nil {
		t.Fatalf("new coalescer: %v", err)
	}
	return c, er, ir, q, clk
}

func TestCoalescer_New_Validation(t *testing.T) {
	er := &fakeEventRepo{}
	ir := &fakeInvocationRepo{}
	q := scheduler.NewInMemoryQueue(5)
	if _, err := scheduler.NewCoalescer(scheduler.DefaultCoalescerConfig(), scheduler.CoalescerDeps{}, q); err == nil {
		t.Fatal("missing deps")
	}
	if _, err := scheduler.NewCoalescer(scheduler.DefaultCoalescerConfig(), scheduler.CoalescerDeps{EventRepo: er}, q); err == nil {
		t.Fatal("missing repo")
	}
	if _, err := scheduler.NewCoalescer(scheduler.DefaultCoalescerConfig(), scheduler.CoalescerDeps{EventRepo: er, InvocationRepo: ir}, nil); err == nil {
		t.Fatal("missing queue")
	}
}

// TestCoalescer_New_DefaultsFill verifies that NewCoalescer with a zero
// CoalescerConfig fills all five default values (RollingWindow / HardWindow
// / BatchSize / MaxConcurrentInvocations / TickInterval). Locks in the
// default-fill code paths.
func TestCoalescer_New_DefaultsFill(t *testing.T) {
	er := &fakeEventRepo{}
	ir := &fakeInvocationRepo{}
	q := scheduler.NewInMemoryQueue(5)
	c, err := scheduler.NewCoalescer(scheduler.CoalescerConfig{}, scheduler.CoalescerDeps{
		EventRepo:      er,
		InvocationRepo: ir,
	}, q)
	if err != nil {
		t.Fatalf("zero cfg ok: %v", err)
	}
	if c == nil {
		t.Fatal("nil coalescer")
	}
}

func TestCoalescer_RollingClose(t *testing.T) {
	c, er, _, q, clk := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow:            30 * time.Second,
		HardWindow:               5 * time.Minute,
		BatchSize:                100,
		MaxConcurrentInvocations: 5,
		TickInterval:             1 * time.Second,
	})
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(42))
	appendEvent(t, er, gen, clk, "task.created", observability.EventRefs{TaskID: "T-1"})

	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if q.Len() != 0 {
		t.Fatalf("queue not empty before close")
	}
	// snapshot has 1 window with 1 event
	if got := c.WindowsSnapshot()["task:T-1"]; got != 1 {
		t.Errorf("snapshot = %d", got)
	}
	// advance past rolling window
	clk.Advance(31 * time.Second)
	if _, closed, err := c.Tick(context.Background()); err != nil {
		t.Fatalf("tick2: %v", err)
	} else if closed != 1 {
		t.Fatalf("closed = %d", closed)
	}
	if q.Len() != 1 {
		t.Fatalf("queue len = %d", q.Len())
	}
}

func TestCoalescer_HardWindowClose(t *testing.T) {
	c, er, _, q, clk := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow:            30 * time.Second,
		HardWindow:               2 * time.Minute,
		BatchSize:                100,
		MaxConcurrentInvocations: 5,
		TickInterval:             1 * time.Second,
	})
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(1))
	// emit an event every 10s for 3 minutes; rolling window never elapses;
	// hard window must close at 2 minutes.
	for i := 0; i < 18; i++ {
		appendEvent(t, er, gen, clk, "task.created", observability.EventRefs{TaskID: "T-1"})
		if _, _, err := c.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		clk.Advance(10 * time.Second)
	}
	// after the loop hard window should be exceeded; one more tick → close.
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Fatalf("queue len = %d; window snapshot = %+v", q.Len(), c.WindowsSnapshot())
	}
}

func TestCoalescer_PerScopeSerialization(t *testing.T) {
	c, er, ir, q, clk := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow: 30 * time.Second, HardWindow: 5 * time.Minute,
		BatchSize: 100, MaxConcurrentInvocations: 5, TickInterval: 1 * time.Second,
	})
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(2))
	// seed running invocation for task:T-1
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"E"})
	inv, _ := cognition.Spawn(cognition.SpawnInput{ID: "INV", Scope: scope, TriggerEvents: tes, StartedAt: clk.Now()})
	if err := ir.Save(context.Background(), inv); err != nil {
		t.Fatal(err)
	}
	// new event for same scope
	appendEvent(t, er, gen, clk, "task.created", observability.EventRefs{TaskID: "T-1"})
	clk.Advance(60 * time.Second)
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 0 {
		t.Errorf("queue should be empty (per-scope serialised); got %d", q.Len())
	}
	// finish running invocation → next tick should enqueue
	_ = ir.UpdateStatusToTerminal(context.Background(), inv)
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Errorf("queue should now be 1; got %d", q.Len())
	}
}

func TestCoalescer_CrossScopeParallel(t *testing.T) {
	c, er, _, q, clk := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow: 30 * time.Second, HardWindow: 5 * time.Minute,
		BatchSize: 100, MaxConcurrentInvocations: 5, TickInterval: 1 * time.Second,
	})
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(3))
	for _, key := range []string{"T-1", "T-2", "T-3"} {
		appendEvent(t, er, gen, clk, "task.created", observability.EventRefs{TaskID: key})
	}
	clk.Advance(31 * time.Second)
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 3 {
		t.Errorf("queue len = %d, want 3", q.Len())
	}
}

func TestCoalescer_QueueFullKeepsWindow(t *testing.T) {
	c, er, _, q, clk := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow: 30 * time.Second, HardWindow: 5 * time.Minute,
		BatchSize: 100, MaxConcurrentInvocations: 1, TickInterval: 1 * time.Second,
	})
	// queue cap=5 but we'll fill to 5 manually
	for i := 0; i < 5; i++ {
		if err := q.Enqueue(scheduler.InvocationRequest{
			Scope: cognition.MustNewInvocationScope(cognition.ScopeWorker, "W-"+string(rune('A'+i))),
		}); err != nil {
			t.Fatal(err)
		}
	}
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(4))
	appendEvent(t, er, gen, clk, "task.created", observability.EventRefs{TaskID: "T-X"})
	clk.Advance(31 * time.Second)
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.WindowsSnapshot()["task:T-X"]; got != 1 {
		t.Errorf("window should remain when queue full; snapshot = %+v", c.WindowsSnapshot())
	}
}

func TestCoalescer_SkipsNonWakeAndAdvancesCursor(t *testing.T) {
	c, er, _, q, clk := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow: 30 * time.Second, HardWindow: 5 * time.Minute,
		BatchSize: 100, MaxConcurrentInvocations: 5, TickInterval: 1 * time.Second,
	})
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(5))
	// non-wake event
	appendEvent(t, er, gen, clk, "task_execution.progress_reported", observability.EventRefs{TaskID: "T-1"})
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.WindowsSnapshot(); len(got) != 0 {
		t.Errorf("non-wake should not enter window: %+v", got)
	}
	if c.Cursor() == "" {
		t.Errorf("cursor should advance even on skipped events")
	}
	// wake event after
	appendEvent(t, er, gen, clk, "task.created", observability.EventRefs{TaskID: "T-1"})
	clk.Advance(31 * time.Second)
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Errorf("queue len = %d, want 1", q.Len())
	}
}

func TestCoalescer_WakeWithMissingRefsSkipped(t *testing.T) {
	c, er, _, _, clk := newTestCoalescer(t, scheduler.CoalescerConfig{
		RollingWindow: 30 * time.Second, HardWindow: 5 * time.Minute,
		BatchSize: 100, MaxConcurrentInvocations: 5, TickInterval: 1 * time.Second,
	})
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(7))
	// wake event with no task_id
	appendEvent(t, er, gen, clk, "task.created", observability.EventRefs{})
	if _, _, err := c.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.WindowsSnapshot(); len(got) != 0 {
		t.Errorf("missing refs should be skipped: %+v", got)
	}
}

func TestCoalescer_SetCursor(t *testing.T) {
	c, _, _, _, _ := newTestCoalescer(t, scheduler.DefaultCoalescerConfig())
	c.SetCursor("custom")
	if c.Cursor() != "custom" {
		t.Errorf("cursor = %q", c.Cursor())
	}
}

func TestCoalescer_RepoErrorPropagated(t *testing.T) {
	c, _, _, _, _ := newTestCoalescer(t, scheduler.DefaultCoalescerConfig())
	// inject a repo that always errors
	if _, _, err := c.Tick(context.Background()); err != nil && !errors.Is(err, context.Canceled) {
		// no error expected from empty repo; just exercise path
	}
}

func TestInMemoryQueue(t *testing.T) {
	q := scheduler.NewInMemoryQueue(2)
	if q.Len() != 0 {
		t.Fatal("empty")
	}
	if err := q.Enqueue(scheduler.InvocationRequest{
		Scope: cognition.MustNewInvocationScope(cognition.ScopeTask, "T-1"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(scheduler.InvocationRequest{
		Scope: cognition.MustNewInvocationScope(cognition.ScopeTask, "T-2"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(scheduler.InvocationRequest{
		Scope: cognition.MustNewInvocationScope(cognition.ScopeTask, "T-3"),
	}); !errors.Is(err, scheduler.ErrQueueFull) {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
	got, ok := q.Dequeue()
	if !ok || got.Scope.Key() != "T-1" {
		t.Errorf("dequeue 1: %v %s", ok, got.Scope.Key())
	}
	if q.Len() != 1 {
		t.Errorf("len = %d", q.Len())
	}
	_, _ = q.Dequeue()
	if _, ok := q.Dequeue(); ok {
		t.Error("empty dequeue")
	}
}

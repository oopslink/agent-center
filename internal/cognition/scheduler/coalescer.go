package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

// CoalescerConfig captures the tuning knobs (ADR-0013 § 3).
type CoalescerConfig struct {
	// RollingWindow is the "no event for X seconds" close trigger
	// (default 30s).
	RollingWindow time.Duration
	// HardWindow is the "first event happened more than X ago" forced
	// close (default 5m).
	HardWindow time.Duration
	// BatchSize caps the number of events pulled per scan.
	BatchSize int
	// MaxConcurrentInvocations caps the global queue width.
	MaxConcurrentInvocations int
	// TickInterval is the coalescer poll cadence.
	TickInterval time.Duration
}

// DefaultCoalescerConfig returns the v1 defaults.
func DefaultCoalescerConfig() CoalescerConfig {
	return CoalescerConfig{
		RollingWindow:            30 * time.Second,
		HardWindow:               5 * time.Minute,
		BatchSize:                100,
		MaxConcurrentInvocations: 5,
		TickInterval:             1 * time.Second,
	}
}

// CoalescerDeps captures the collaborators the Coalescer needs.
type CoalescerDeps struct {
	EventRepo        observability.EventRepository
	InvocationRepo   cognition.SupervisorInvocationRepository
	Clock            clock.Clock
}

// InvocationRequest is the scheduled spawn handed to the SpawnQueue when a
// scope window closes.
type InvocationRequest struct {
	Scope         cognition.InvocationScope
	TriggerEvents cognition.TriggerEventSet
	EnqueuedAt    time.Time
}

// SpawnQueue is the port the Coalescer uses to hand off ready scopes to
// the Spawner. Implementations decide FIFO vs priority + concurrency
// limiting.
type SpawnQueue interface {
	Enqueue(req InvocationRequest) error
}

// windowState is the per-scope accumulator.
type windowState struct {
	scope    cognition.InvocationScope
	firstAt  time.Time
	lastAt   time.Time
	eventIDs []observability.EventID
}

// Coalescer is the SupervisorTriggerCoalescer domain service. It scans
// the events table from a cursor, routes wake events to per-scope windows,
// closes windows on the rolling / hard timer, and enqueues
// InvocationRequests when (a) the window is ripe AND (b) no invocation
// is currently running for that scope.
//
// Concurrency model: single goroutine — Run owns the loop. State (cursor,
// windows) is plain struct fields, no locks needed.
type Coalescer struct {
	cfg      CoalescerConfig
	deps     CoalescerDeps
	queue    SpawnQueue
	cursor   observability.EventID
	windows  map[string]*windowState
	cursorMu sync.Mutex // guards cursor for tests that call Snapshot()
}

// NewCoalescer wires a Coalescer. queue must be non-nil; deps must have
// the EventRepo + InvocationRepo set.
func NewCoalescer(cfg CoalescerConfig, deps CoalescerDeps, queue SpawnQueue) (*Coalescer, error) {
	if queue == nil {
		return nil, errors.New("coalescer: queue required")
	}
	if deps.EventRepo == nil {
		return nil, errors.New("coalescer: event_repo required")
	}
	if deps.InvocationRepo == nil {
		return nil, errors.New("coalescer: invocation_repo required")
	}
	if deps.Clock == nil {
		deps.Clock = clock.SystemClock{}
	}
	if cfg.RollingWindow == 0 {
		cfg.RollingWindow = 30 * time.Second
	}
	if cfg.HardWindow == 0 {
		cfg.HardWindow = 5 * time.Minute
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if cfg.MaxConcurrentInvocations == 0 {
		cfg.MaxConcurrentInvocations = 5
	}
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 1 * time.Second
	}
	return &Coalescer{
		cfg:     cfg,
		deps:    deps,
		queue:   queue,
		windows: map[string]*windowState{},
	}, nil
}

// SetCursor seeds the cursor (used by CrashRecovery to replay missed
// events).
func (c *Coalescer) SetCursor(id observability.EventID) {
	c.cursorMu.Lock()
	defer c.cursorMu.Unlock()
	c.cursor = id
}

// Cursor returns the current cursor (testing).
func (c *Coalescer) Cursor() observability.EventID {
	c.cursorMu.Lock()
	defer c.cursorMu.Unlock()
	return c.cursor
}

// Tick performs one cycle: pull events from cursor → push into windows →
// close ripe windows → enqueue. Returns the number of events processed
// and number of windows closed. Tests call Tick directly with a FakeClock
// advanced between calls.
func (c *Coalescer) Tick(ctx context.Context) (events int, closed int, err error) {
	cursor := c.Cursor()
	filter := observability.EventQueryFilter{Limit: c.cfg.BatchSize}
	if cursor != "" {
		filter.Cursor = &cursor
	}
	rows, err := c.deps.EventRepo.Find(ctx, filter)
	if err != nil {
		return 0, 0, err
	}
	now := c.deps.Clock.Now()
	for _, e := range rows {
		// Always advance the cursor — even non-wake events count
		// (cognition/00 § 3.2: cursor monotonic, skip non-wake).
		c.SetCursor(e.ID())
		if !IsWakeEvent(e.Type()) {
			continue
		}
		scope, ok := RouteToScope(e.Type(), e.Refs())
		if !ok {
			continue
		}
		// Use the event's occurred_at as the window timestamp so that
		// FakeClock-driven tests can advance time between Tick calls
		// deterministically. (The wall-clock arrival is approximately
		// occurred_at in production single-host deployments.)
		c.pushIntoWindow(scope, e.ID(), e.OccurredAt())
	}
	closed = c.closeRipeWindows(ctx, now)
	return len(rows), closed, nil
}

// Run blocks until ctx done, calling Tick on TickInterval. Errors are
// surfaced via the optional ErrorHook callback (defaults to no-op so
// that callers can plug an observability emit).
func (c *Coalescer) Run(ctx context.Context, errorHook func(error)) error {
	if errorHook == nil {
		errorHook = func(error) {}
	}
	t := time.NewTicker(c.cfg.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if _, _, err := c.Tick(ctx); err != nil {
				errorHook(err)
			}
		}
	}
}

// pushIntoWindow extends the per-scope window with eventID at time now.
func (c *Coalescer) pushIntoWindow(scope cognition.InvocationScope, id observability.EventID, now time.Time) {
	key := scope.String()
	w, ok := c.windows[key]
	if !ok {
		w = &windowState{scope: scope, firstAt: now}
		c.windows[key] = w
	}
	w.lastAt = now
	w.eventIDs = append(w.eventIDs, id)
}

// closeRipeWindows enqueues windows that have crossed the rolling /
// hard threshold AND have no currently-running invocation in their
// scope. Returns the number of scopes successfully enqueued.
func (c *Coalescer) closeRipeWindows(ctx context.Context, now time.Time) int {
	closed := 0
	for key, w := range c.windows {
		if !c.windowReady(w, now) {
			continue
		}
		// per-scope serialization — ADR-0013 § 1.
		if c.scopeHasRunning(ctx, w.scope) {
			continue
		}
		tes, err := cognition.NewTriggerEventSet(w.eventIDs)
		if err != nil {
			// drop the window — malformed (shouldn't happen because we
			// validated input above, but a defensive path).
			delete(c.windows, key)
			continue
		}
		if err := c.queue.Enqueue(InvocationRequest{
			Scope:         w.scope,
			TriggerEvents: tes,
			EnqueuedAt:    now,
		}); err != nil {
			// Queue is full — keep the window and retry next Tick.
			continue
		}
		delete(c.windows, key)
		closed++
	}
	return closed
}

func (c *Coalescer) windowReady(w *windowState, now time.Time) bool {
	if w == nil || len(w.eventIDs) == 0 {
		return false
	}
	if now.Sub(w.lastAt) >= c.cfg.RollingWindow {
		return true
	}
	if now.Sub(w.firstAt) >= c.cfg.HardWindow {
		return true
	}
	return false
}

func (c *Coalescer) scopeHasRunning(ctx context.Context, scope cognition.InvocationScope) bool {
	_, err := c.deps.InvocationRepo.FindRunningByScope(ctx, scope)
	if err == nil {
		return true
	}
	if errors.Is(err, cognition.ErrInvocationNotFound) {
		return false
	}
	// On unrelated repo errors we deliberately err on the side of caution
	// (treat as running) — the Coalescer will retry next Tick.
	return true
}

// WindowsSnapshot returns a copy of the in-memory window state. Used by
// tests and inspect.
func (c *Coalescer) WindowsSnapshot() map[string]int {
	out := map[string]int{}
	for k, w := range c.windows {
		out[k] = len(w.eventIDs)
	}
	return out
}

package service

import (
	"context"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/clock"
)

// WorkItemReconcile tunables. The default stale-age of 30 min (@oopslink 2026-06-07)
// gives an agent a generous window for thinking / long tool calls before its active
// item is judged stuck, while still detecting a true hang in a reasonable time.
// Tick 5 min → a hang is caught within (staleAge + tick).
const (
	WorkItemReconcileDefaultStaleAge = 30 * time.Minute
	WorkItemReconcileDefaultTick     = 5 * time.Minute
)

// WorkItemReconciler periodically releases STUCK active WorkItems — an agent that
// marked a WorkItem active but has produced no activity for staleAge (hung, wedged,
// or crashed without the #111 process-death signal). Without it the stuck item
// holds the agent's single-active slot forever, so the agent can never pull new
// work. Release = FailFromAgentDeath (active→failed, version++) under version-CAS:
// it loses cleanly to a concurrent agent complete (the v2.8.1 #278 7a-iii true
// race — the reconciler is the "releaser" that makes that race real, which is why
// complete/fail/pause/resume are CAS-guarded). v2.8.1 #278 D PR5.
type WorkItemReconciler struct {
	wiRepo       agent.WorkItemRepository
	activityRepo agent.ActivityEventRepository
	clk          clock.Clock
	staleAge     time.Duration
	tick         time.Duration
	log          func(string, ...any)
}

// NewWorkItemReconciler wires the reconciler. Zero staleAge/tick → package
// defaults; nil clk → system clock; nil log → no-op.
func NewWorkItemReconciler(
	wiRepo agent.WorkItemRepository,
	activityRepo agent.ActivityEventRepository,
	clk clock.Clock,
	staleAge, tick time.Duration,
	log func(string, ...any),
) *WorkItemReconciler {
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if staleAge <= 0 {
		staleAge = WorkItemReconcileDefaultStaleAge
	}
	if tick <= 0 {
		tick = WorkItemReconcileDefaultTick
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &WorkItemReconciler{
		wiRepo:       wiRepo,
		activityRepo: activityRepo,
		clk:          clk,
		staleAge:     staleAge,
		tick:         tick,
		log:          log,
	}
}

// Run blocks until ctx is done, calling Tick on the configured cadence. A failed
// tick is logged, never fatal (a transient scan error must not kill the loop).
func (r *WorkItemReconciler) Run(ctx context.Context) error {
	t := time.NewTicker(r.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if n, err := r.Tick(ctx); err != nil {
				r.log("work_item_reconcile: tick failed: %v", err)
			} else if n > 0 {
				r.log("work_item_reconcile: released %d stuck work item(s)", n)
			}
		}
	}
}

// Tick scans active WorkItems and releases those whose owning agent has been
// inactive for staleAge. Returns the count released. Per-item errors — including
// a CAS conflict (the agent completed the item concurrently, the 7a-iii race) —
// are skipped, not fatal.
func (r *WorkItemReconciler) Tick(ctx context.Context) (int, error) {
	active, err := r.wiRepo.ListByStatus(ctx, agent.WorkItemActive)
	if err != nil {
		return 0, err
	}
	now := r.clk.Now()
	released := 0
	for _, wi := range active {
		stale, err := r.isStale(ctx, wi, now)
		if err != nil {
			r.log("work_item_reconcile: staleness check wi=%s: %v", wi.ID(), err)
			continue
		}
		if !stale {
			continue
		}
		preV := wi.Version()
		if err := wi.FailFromAgentDeath(now); err != nil {
			r.log("work_item_reconcile: fail-edge wi=%s: %v", wi.ID(), err)
			continue
		}
		if err := r.wiRepo.UpdateCAS(ctx, wi, preV); err != nil {
			// ErrWorkItemReassigned = the agent completed/changed it first (7a-iii
			// race) → the agent won, leave it. Any other error → log + skip.
			if !errors.Is(err, agent.ErrWorkItemReassigned) {
				r.log("work_item_reconcile: release wi=%s: %v", wi.ID(), err)
			}
			continue
		}
		released++
		r.log("work_item_reconcile: released stuck wi=%s agent=%s (no activity for >=%s)", wi.ID(), wi.AgentID(), r.staleAge)
	}
	return released, nil
}

// isStale reports whether wi's owning agent has been inactive for >= staleAge.
// The baseline is max(wi activation time, latest activity event) — so a
// freshly-activated item that has not yet produced an event is never wrongly
// released (mirrors the heartbeat reconciler's first-heartbeat-window guard).
func (r *WorkItemReconciler) isStale(ctx context.Context, wi *agent.AgentWorkItem, now time.Time) (bool, error) {
	lastSeen := wi.UpdatedAt()
	events, err := r.activityRepo.ListByAgent(ctx, wi.AgentID(), 1, "")
	if err != nil {
		return false, err
	}
	if len(events) > 0 && events[0].OccurredAt().After(lastSeen) {
		lastSeen = events[0].OccurredAt()
	}
	return now.Sub(lastSeen) >= r.staleAge, nil
}

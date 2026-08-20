package service

import (
	"context"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/background"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanReconcileLoop runs Service.ReconcileRunningPlans as a background job — the
// v2.9 P2-3 RECONCILIATION SWEEP (the auto-orchestration safety net). It mirrors
// the WakeReconcileLoop / outbox Pump / files GCLoop pattern: a SINGLE goroutine
// runs once on boot, then on a ticker, until ctx is canceled. Single-threaded by
// construction so a sweep never overlaps itself.
//
// Safety net, not the primary path: the orchestrator projector (P2-1/P2-2) is the
// low-latency push path that dispatches ready nodes on events. This sweep catches
// the GAP — a missed event / a crash between an upstream completing and its
// downstream being dispatched — by periodically re-running the IDEMPOTENT dispatch
// core over every running plan. Already-dispatched nodes are skipped
// (INSERT-OR-IGNORE record + derived NodeDispatched), so the sweep is harmless on
// the happy path (it dispatches nothing when everything is already dispatched).
//
// Slow cadence: the dispatch core is idempotent and crash-recovery is not
// latency-critical, so the tick interval is non-critical (60s default).
//
// I103 §3: each sweep ALSO materializes the旁路 BlockedOn snapshots
// (ReconcileRunningPlans → materializeBlockedOn) — a pure-observation classification
// of why each non-terminal node is not progressing, refreshed/cleared per node. That
// materialization is BEST-EFFORT and runs in its own tx, so it can never break the
// dispatch safety net above (see ReconcileRunningPlans).
type PlanReconcileLoop struct {
	svc      *Service
	interval time.Duration
	logger   func(string)
	auth     *authz.Service
}

// NewPlanReconcileLoop builds the loop. interval<=0 defaults to 60s.
func NewPlanReconcileLoop(svc *Service, interval time.Duration, logger func(string), authorizer ...*authz.Service) *PlanReconcileLoop {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	var auth *authz.Service
	if len(authorizer) > 0 {
		auth = authorizer[0]
	}
	return &PlanReconcileLoop{svc: svc, interval: interval, logger: logger, auth: auth}
}

// Run executes one reconcile sweep immediately, then on each tick, until ctx is
// canceled. Blocks; call as `go loop.Run(ctx)`. Never panics — a sweep / per-plan
// error is logged and the loop keeps running (the failed work retries next tick).
func (l *PlanReconcileLoop) Run(ctx context.Context) {
	if passCtx, cancel, ok := background.OperationContext(ctx, 0); ok {
		_ = l.Tick(passCtx)
		cancel()
	}
	t := time.NewTicker(l.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			passCtx, cancel, ok := background.OperationContext(ctx, 0)
			if !ok {
				return
			}
			_ = l.Tick(passCtx)
			cancel()
		}
	}
}

func (l *PlanReconcileLoop) Tick(ctx context.Context) error {
	if l.svc == nil {
		return nil
	}
	if err := requireBackgroundAuthorization(ctx, l.auth, "plan_reconcile"); err != nil {
		l.log("plan reconcile loop: authorization failed: " + err.Error())
		return err
	}
	// Per-plan errors are logged so one bad plan never silences the whole sweep;
	// the returned (first) error is also logged for the list/setup failures.
	errFn := func(planID pm.PlanID, err error) {
		l.log("plan reconcile loop: plan " + string(planID) + ": " + err.Error())
	}
	if err := l.svc.ReconcileRunningPlans(ctx, errFn); err != nil {
		l.log("plan reconcile loop: " + err.Error())
		return err
	}
	return nil
}

func (l *PlanReconcileLoop) log(msg string) {
	if l.logger != nil {
		l.logger(msg)
	}
}

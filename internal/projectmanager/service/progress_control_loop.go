package service

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/background"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ProgressWatchdogLoop is intentionally separate from PlanReconcileLoop: a
// wedged dispatcher/reconciler cannot prevent detection of its own silence.
type ProgressWatchdogLoop struct {
	svc               *Service
	interval, silence time.Duration
	logger            func(string)
}

func NewProgressWatchdogLoop(svc *Service, interval, silence time.Duration, logger func(string)) *ProgressWatchdogLoop {
	if interval <= 0 {
		interval = time.Minute
	}
	if silence <= 0 {
		silence = 3 * time.Minute
	}
	return &ProgressWatchdogLoop{svc: svc, interval: interval, silence: silence, logger: logger}
}

func (l *ProgressWatchdogLoop) Run(ctx context.Context) {
	l.run(ctx)
	t := time.NewTicker(l.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.run(ctx)
		}
	}
}

func (l *ProgressWatchdogLoop) run(ctx context.Context) {
	if l == nil || l.svc == nil {
		return
	}
	opCtx, cancel, ok := background.OperationContext(ctx, 0)
	if !ok {
		return
	}
	defer cancel()
	if err := l.svc.requireBackgroundAuthorization(opCtx, "progress_watchdog"); err != nil {
		l.log(err.Error())
		return
	}
	if err := l.svc.ProgressWatchdogTick(opCtx, l.silence); err != nil {
		l.log(err.Error())
	}
}

func (l *ProgressWatchdogLoop) log(msg string) {
	if l.logger != nil {
		l.logger(msg)
	}
}

// ProgressWakeDrainLoop retries durable suppressed lanes independently of both
// the push producer and the Plan reconciliation loop.
type ProgressWakeDrainLoop struct {
	svc      *Service
	interval time.Duration
	deliver  func(context.Context, pm.ProgressSuppressedWake) error
	logger   func(string)
}

func NewProgressWakeDrainLoop(svc *Service, interval time.Duration, deliver func(context.Context, pm.ProgressSuppressedWake) error, logger func(string)) *ProgressWakeDrainLoop {
	if interval <= 0 {
		interval = time.Minute
	}
	return &ProgressWakeDrainLoop{svc: svc, interval: interval, deliver: deliver, logger: logger}
}

func (l *ProgressWakeDrainLoop) Run(ctx context.Context) {
	l.run(ctx)
	t := time.NewTicker(l.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.run(ctx)
		}
	}
}

func (l *ProgressWakeDrainLoop) run(ctx context.Context) {
	if l == nil || l.svc == nil {
		return
	}
	opCtx, cancel, ok := background.OperationContext(ctx, 0)
	if !ok {
		return
	}
	defer cancel()
	if err := l.svc.requireBackgroundAuthorization(opCtx, "progress_wake_drain"); err != nil {
		l.log(err.Error())
		return
	}
	if err := l.svc.DrainProgressSuppressedWakes(opCtx, 100, l.deliver); err != nil {
		l.log(err.Error())
	}
}

func (l *ProgressWakeDrainLoop) log(msg string) {
	if l.logger != nil {
		l.logger(msg)
	}
}

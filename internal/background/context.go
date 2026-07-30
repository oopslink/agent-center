package background

import (
	"context"
	"time"
)

const DefaultOperationTimeout = 30 * time.Second

// OperationContext returns a fresh, bounded context for one background pass.
// The parent context controls the loop lifetime; each DB pass gets its own
// server-owned deadline so a canceled/expired pass context is never reused on
// the next tick.
func OperationContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc, bool) {
	if parent == nil {
		parent = context.Background()
	}
	select {
	case <-parent.Done():
		return parent, func() {}, false
	default:
	}
	if timeout <= 0 {
		timeout = DefaultOperationTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	return ctx, cancel, true
}

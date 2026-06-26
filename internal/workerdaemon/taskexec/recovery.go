package taskexec

import "fmt"

// ContextRecovery implements the dual-track context recovery (design §10).
type ContextRecovery struct {
	MaxRetries int // design §10.2: retry upper limit, default 3
}

// RecoverTask attempts to recover a task's LLM context.
// Fast path (§10.1): if cacheValid is true, returns immediately (zero-cost).
// Slow path (§10.2): reads events.current.jsonl, replays events, returns them.
func (r *ContextRecovery) RecoverTask(taskDir string, cacheValid bool) ([]RawEvent, error) {
	if cacheValid {
		return nil, nil // fast path: cache is valid, no replay needed
	}
	// Slow path: replay from local event stream
	w := NewEventStreamWriter()
	events, err := w.ReadAll(taskDir)
	if err != nil {
		return nil, fmt.Errorf("taskexec: context recovery read events: %w", err)
	}
	return events, nil
}

// DefaultContextRecovery returns a ContextRecovery with design defaults.
func DefaultContextRecovery() *ContextRecovery {
	return &ContextRecovery{MaxRetries: 3}
}

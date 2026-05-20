package dispatch

import (
	"context"
)

// EnvelopeSender abstracts the worker-side transport that delivers a
// DispatchEnvelope to the chosen worker daemon. The DispatchService
// invokes Send after the (Task + TaskExecution + events) tx has committed.
//
// Send must NOT block on agent-side phases; it returns once the envelope
// is handed off to transport.
type EnvelopeSender interface {
	Send(ctx context.Context, env DispatchEnvelope) error
}

// NoopSender is a sender that records what was sent. Useful as a default
// when there's no worker daemon (test / unit setup) — tests typically wire
// a fake sender instead.
type NoopSender struct{}

// Send is a no-op.
func (NoopSender) Send(context.Context, DispatchEnvelope) error { return nil }

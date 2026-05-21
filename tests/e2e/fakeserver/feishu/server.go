// Package feishu provides a minimal HTTP + event-injection fake feishu
// server used by Phase 7 e2e scenarios (plan-7 § 3.8). The Phase 5
// `internal/bridge/feishu/client.FakeServer` covers outbound HTTP; this
// fake adds the inbound side: a channel into the inbound Router so
// tests can drive `im.message.receive_v1` / `card.action.trigger`
// events end-to-end.
//
// Two complementary fakes are intentional:
//
//   - client.FakeServer (Phase 5)  — adapter-level HTTP stub used by
//     OAPIAdapter integration tests
//   - feishu.Server   (Phase 7)   — full e2e double exposing inbound
//     injection + outbound assertions over an in-process channel
package feishu

import (
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/inbound"
)

// OutboundRecord captures one outbound message the bridge dispatched.
type OutboundRecord struct {
	ThreadKey    string
	VendorUserID string
	Content      string
	IsCard       bool
	AtUTC        time.Time
}

// Server is the in-process fake.
type Server struct {
	mu     sync.Mutex
	out    []OutboundRecord
	in     chan inbound.VendorEvent
	closed bool
}

// New constructs a fresh in-process fake.
func New() *Server {
	return &Server{
		in: make(chan inbound.VendorEvent, 64),
	}
}

// Close shuts down the inbound channel.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.in)
}

// Inject pushes a vendor event into the inbound stream. The
// Driver(...) goroutine drains this channel into the Router.
func (s *Server) Inject(ev inbound.VendorEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.in <- ev
}

// Inbox returns the receive channel so the harness can wire the
// router goroutine.
func (s *Server) Inbox() <-chan inbound.VendorEvent { return s.in }

// RecordOutbound captures a send. Production bridge dispatchers do
// not touch this directly; tests proxy through it.
func (s *Server) RecordOutbound(r OutboundRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out = append(s.out, r)
}

// Outbound returns a snapshot of recorded outbound messages.
func (s *Server) Outbound() []OutboundRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutboundRecord, len(s.out))
	copy(out, s.out)
	return out
}

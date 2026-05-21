// Package client hosts the Bridge BC vendor port for Feishu (bridge/01 §
// 3-5) plus its real-world adapter and an in-process fake for tests.
//
// Layering rule:
//   - This file (`client.go`) is the domain port: it MUST NOT import any
//     feishu vendor SDK type. All other agent-center packages (`internal/
//     conversation/...`, `internal/taskruntime/...`, `internal/discussion/...`,
//     `internal/cognition/...`) depend on this port and remain vendor-clean
//     per conventions § 9.y.
//   - `oapi_adapter.go` is the SOLE source file that imports
//     `github.com/larksuite/oapi-sdk-go/v3`. The e2e import-graph test
//     (tests/e2e/phase5_test.go) verifies this is the only leaf.
//   - `fake_server.go` is a test-only HTTP stub used by integration + e2e
//     tests; it does not import the vendor SDK.
package client

import (
	"context"
	"errors"
)

// Target is the routing address an outbound message points at.
//   - Channel: e.g. "feishu"
//   - ThreadKey: vendor thread / chat id. Empty when the dispatcher is
//     creating a fresh thread root (the first Task / Issue root card).
//   - VendorUserID: optional fallback when no thread exists yet (e.g. DM
//     case).
type Target struct {
	Channel      string
	ThreadKey    string
	VendorUserID string
}

// SendResult carries the vendor-side ids returned by the API after a
// successful send.
type SendResult struct {
	VendorMsgRef  string // feishu message id
	CardMessageID string // feishu interactive card msg id (empty when text)
	ThreadKey     string // assigned thread key when fresh; echoed otherwise
}

// ConnectionStatus is the long-poll state enum.
type ConnectionStatus string

// ConnectionStatus values.
const (
	StatusConnected    ConnectionStatus = "connected"
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusReconnecting ConnectionStatus = "reconnecting"
)

// VendorEvent is the inbound event envelope passed to OnEvent handlers.
// Phase 5 registers a no-op handler; Phase 7 wires inbound parsing here.
type VendorEvent struct {
	Kind    string // e.g. "im.message.receive_v1" / "card.action.trigger"
	RawJSON string
}

// Client is the domain port. Adapters fulfill it (oapi_adapter.go) and
// tests fake it.
type Client interface {
	// Connect establishes the WebSocket / long-poll connection.
	Connect(ctx context.Context) error
	// SendTextMessage sends a markdown / plain text message.
	SendTextMessage(ctx context.Context, target Target, markdown string) (SendResult, error)
	// SendInteractiveCard sends a raw card JSON payload.
	SendInteractiveCard(ctx context.Context, target Target, cardJSON string) (SendResult, error)
	// UpdateCard is v2+ feature; Phase 5 returns ErrUpdateCardNotSupported.
	UpdateCard(ctx context.Context, cardMessageID string, cardJSON string) error
	// Close terminates the connection.
	Close() error
	// ConnectionStatus reports the current status snapshot.
	ConnectionStatus() ConnectionStatus
	// OnEvent registers a callback for inbound vendor events; Phase 5
	// registers a no-op handler. Multiple calls overwrite the previous
	// handler (single-handler model).
	OnEvent(handler func(VendorEvent))
}

// Sentinel errors used by all adapters + fakes.
var (
	ErrNotConnected           = errors.New("feishu client: not connected")
	ErrUpdateCardNotSupported = errors.New("feishu client: UpdateCard reserved for v2+")
	ErrPermanentFailure       = errors.New("feishu client: permanent failure (4xx)")
	ErrTransientFailure       = errors.New("feishu client: transient failure (5xx / network)")
	ErrAuthFailed             = errors.New("feishu client: auth failed")
)

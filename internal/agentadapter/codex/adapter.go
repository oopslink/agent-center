// Package codex is a skeleton Codex CLI adapter (05-agent-adapters § 8.2).
//
// Phase 2 ships a not-implemented stub. Phase 3+ may fill in the actual
// adapter once Codex CLI behaviour is verified.
package codex

import (
	"github.com/oopslink/agent-center/internal/agentadapter"
)

// AdapterName is the well-known name.
const AdapterName = "codex"

// Adapter is the Codex stub.
type Adapter struct{}

// New constructs the stub.
func New() *Adapter { return &Adapter{} }

// Name returns the adapter name.
func (a *Adapter) Name() string { return AdapterName }

// SupportsSession returns false (unknown).
func (a *Adapter) SupportsSession() bool { return false }

// BuildCommand returns ErrNotImplemented.
func (a *Adapter) BuildCommand(_ agentadapter.SpawnRequest) (agentadapter.CmdSpec, error) {
	return agentadapter.CmdSpec{}, agentadapter.ErrNotImplemented
}

// ParseEvent returns ErrNotImplemented.
func (a *Adapter) ParseEvent(_ []byte) (agentadapter.AgentTraceEvent, error) {
	return agentadapter.AgentTraceEvent{}, agentadapter.ErrNotImplemented
}

// NOTE: no init(). Callers must explicitly Register if they want this
// adapter exposed; Phase 2 keeps it off the default registry to avoid
// accidental use.

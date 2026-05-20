// Package opencode is a skeleton OpenCode CLI adapter (05-agent-adapters
// § 8.3). Phase 2 stub.
package opencode

import (
	"github.com/oopslink/agent-center/internal/agentadapter"
)

// AdapterName is the well-known name.
const AdapterName = "opencode"

// Adapter is the OpenCode stub.
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

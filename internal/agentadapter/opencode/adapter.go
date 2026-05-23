// Package opencode is the OpenCode CLI adapter (SST OpenCode; per ADR-0030
// § 1). v2-phase stub mirroring the codex pattern: contract-complete but
// behaviour returns ErrNotImplemented until per-CLI flag mapping is
// verified.
package opencode

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// AdapterName is the well-known name.
const AdapterName = "opencode"

// Adapter is the OpenCode adapter. binary defaults to "opencode".
type Adapter struct {
	binary string
}

// New constructs the adapter; binary="" defaults to "opencode".
func New(binary string) *Adapter {
	if binary == "" {
		binary = "opencode"
	}
	return &Adapter{binary: binary}
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return AdapterName }

// SupportsSession returns false (unknown).
func (a *Adapter) SupportsSession() bool { return false }

// BuildCommand returns ErrNotImplemented (stub).
func (a *Adapter) BuildCommand(_ agentadapter.SpawnRequest) (agentadapter.CmdSpec, error) {
	return agentadapter.CmdSpec{}, agentadapter.ErrNotImplemented
}

// ParseEvent returns ErrNotImplemented (stub).
func (a *Adapter) ParseEvent(_ []byte) (agentadapter.AgentTraceEvent, error) {
	return agentadapter.AgentTraceEvent{}, agentadapter.ErrNotImplemented
}

// Probe checks whether the opencode binary is on PATH and reports version.
func (a *Adapter) Probe(ctx context.Context) (bool, string, error) {
	if _, err := exec.LookPath(a.binary); err != nil {
		return false, "", nil
	}
	cmd := exec.CommandContext(ctx, a.binary, "--version")
	out, err := cmd.Output()
	if err != nil {
		return false, "", nil
	}
	return true, strings.TrimSpace(string(out)), nil
}

// SupportedFeatures — conservative defaults; opencode MCP / skill / session
// support is unverified. Revisit after CLI behaviour probe.
func (a *Adapter) SupportedFeatures() agentadapter.FeatureSet {
	return agentadapter.FeatureSet{
		SupportsMCP:     false,
		SupportsSkills:  false,
		SupportsSession: false,
	}
}

// BuildMCPConfigArg returns error (not yet supported).
func (a *Adapter) BuildMCPConfigArg(_ string) (agentadapter.MCPSetup, error) {
	return agentadapter.MCPSetup{}, errors.New("opencode: MCP injection not yet supported")
}

// BuildSkillMountSetup returns error (not yet supported).
func (a *Adapter) BuildSkillMountSetup(_, _ string) (agentadapter.SkillMountSetup, error) {
	return agentadapter.SkillMountSetup{}, errors.New("opencode: skill mount not yet supported")
}

// init self-registers the adapter.
func init() {
	agentadapter.Register(New(""))
}

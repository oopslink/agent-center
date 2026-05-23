// Package codex is the Codex CLI adapter (OpenAI Codex CLI; per ADR-0030
// § 1). v2-phase stub: implements the v2 Adapter interface contract but
// most methods return zero-value / ErrNotImplemented until the real CLI
// behaviour is verified at integration time.
package codex

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/oopslink/agent-center/internal/agentadapter"
)

// AdapterName is the well-known name.
const AdapterName = "codex"

// Adapter is the Codex adapter. binary defaults to "codex" looked up via
// $PATH at exec time.
type Adapter struct {
	binary string
}

// New constructs the adapter; binary="" defaults to "codex".
func New(binary string) *Adapter {
	if binary == "" {
		binary = "codex"
	}
	return &Adapter{binary: binary}
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return AdapterName }

// SupportsSession reports session-id support. v2 default: false (most codex
// CLI variants do not support session continuation; revisit when probing).
func (a *Adapter) SupportsSession() bool { return false }

// BuildCommand assembles the codex invocation. Stub: returns
// ErrNotImplemented until per-CLI flag mapping is verified.
func (a *Adapter) BuildCommand(_ agentadapter.SpawnRequest) (agentadapter.CmdSpec, error) {
	return agentadapter.CmdSpec{}, agentadapter.ErrNotImplemented
}

// ParseEvent maps one codex JSONL line to AgentTraceEvent. Stub.
func (a *Adapter) ParseEvent(_ []byte) (agentadapter.AgentTraceEvent, error) {
	return agentadapter.AgentTraceEvent{}, agentadapter.ErrNotImplemented
}

// Probe checks whether the codex binary is on PATH and reports its version
// (per ADR-0030 § 2).
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

// SupportedFeatures — v2 conservative defaults. Codex MCP support is
// uncertain; mark false until probed. Session continuation likewise false.
// Skills support is also unknown; default false. Revisit after CLI probe.
func (a *Adapter) SupportedFeatures() agentadapter.FeatureSet {
	return agentadapter.FeatureSet{
		SupportsMCP:     false,
		SupportsSkills:  false,
		SupportsSession: false,
	}
}

// BuildMCPConfigArg returns zero MCPSetup (codex MCP path unverified).
// DispatchService.FeatureCheck rejects MCP-bearing agents on codex workers
// per SupportedFeatures().SupportsMCP=false before this is called.
func (a *Adapter) BuildMCPConfigArg(_ string) (agentadapter.MCPSetup, error) {
	return agentadapter.MCPSetup{}, errors.New("codex: MCP injection not yet supported")
}

// BuildSkillMountSetup returns zero SkillMountSetup (codex skill path
// unverified). DispatchService rejects skill-bearing agents on codex
// workers per SupportedFeatures().SupportsSkills=false.
func (a *Adapter) BuildSkillMountSetup(_, _ string) (agentadapter.SkillMountSetup, error) {
	return agentadapter.SkillMountSetup{}, errors.New("codex: skill mount not yet supported")
}

// init self-registers the adapter on import.
func init() {
	agentadapter.Register(New(""))
}

package workerdaemon

import (
	"context"
	"sort"
	"time"

	"github.com/oopslink/agent-center/internal/agentadapter"
	"github.com/oopslink/agent-center/internal/workforce"
)

// DefaultProbeTimeout caps each adapter.Probe call so a single hung CLI
// can't block the whole online sequence.
const DefaultProbeTimeout = 3 * time.Second

// ProbeAllAdapters iterates the supplied registry, calling Probe +
// SupportedFeatures on each adapter, and returns the resulting list of
// workforce.Capability ready to upload to center via
// WorkerRepository.UpdateCapabilities (per ADR-0023 § 4 + ADR-0030 § 4).
//
// Behaviour:
//   - Each Probe call is wrapped in a per-adapter ctx with DefaultProbeTimeout
//     (callers may override via the wrapping ctx)
//   - Probe error or `available=false` records Detected=false; the entry is
//     still emitted so center can know the CLI was checked
//   - Enabled is set to Detected by default; user choice is preserved on the
//     center side (Worker.ApplyCapabilities merges Enabled from prior
//     entries)
//   - Output is sorted by AgentCLI for deterministic event payloads
func ProbeAllAdapters(ctx context.Context, registry *agentadapter.Registry) []workforce.Capability {
	if registry == nil {
		registry = agentadapter.DefaultRegistry
	}
	names := registry.Names()
	out := make([]workforce.Capability, 0, len(names))
	for _, name := range names {
		adapter, ok := registry.Get(name)
		if !ok {
			continue
		}
		out = append(out, probeOne(ctx, adapter))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentCLI < out[j].AgentCLI })
	return out
}

// probeOne runs Probe + SupportedFeatures for one adapter and returns the
// matching Capability.
func probeOne(ctx context.Context, adapter agentadapter.Adapter) workforce.Capability {
	cap := workforce.Capability{
		AgentCLI: adapter.Name(),
	}
	probeCtx, cancel := context.WithTimeout(ctx, DefaultProbeTimeout)
	defer cancel()
	avail, version, err := adapter.Probe(probeCtx)
	if err != nil {
		// Probe error: treat as not-detected (defensive). Center sees the
		// CLI was checked but is unavailable.
		return cap
	}
	cap.Detected = avail
	cap.Enabled = avail // first-probe default; user can disable later
	cap.Version = version
	feat := adapter.SupportedFeatures()
	cap.SupportsMCP = feat.SupportsMCP
	cap.SupportsSkills = feat.SupportsSkills
	cap.SupportsSession = feat.SupportsSession
	return cap
}

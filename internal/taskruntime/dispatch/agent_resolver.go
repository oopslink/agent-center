package dispatch

import (
	"context"
	"errors"
)

// AgentResolution carries the information DispatchService needs from the
// Workforce BC to build a V2 DispatchEnvelope and run the per-ADR-0030 § 5
// feature-check.
//
// Fields:
//   - AgentInstanceID / AgentCLI / WorkerID / HomeDir come from
//     AgentInstance + its bound Worker
//   - FeatureCheck is the pre-computed result of
//     workforce.CheckDispatchFeatures(agent_instance, worker_capability);
//     OK=true means dispatch can proceed; OK=false means caller emits NACK
//     with Reason+Message verbatim.
type AgentResolution struct {
	AgentInstanceID string
	WorkerID        string
	AgentCLI        string
	HomeDir         string

	FeatureOK      bool
	FeatureReason  string
	FeatureMessage string
}

// AgentResolver is the v2 dispatch-time lookup callback. Implementations
// (typically in internal/workforce/service) materialise the AgentInstance
// + Worker.Capability join and run the feature-check.
//
// DispatchService.Dispatch uses this only when DispatchInput.AgentInstanceID
// is non-empty (v2 path); v1 dispatch (worker_id+agent_cli direct) bypasses
// the resolver entirely.
//
// Implementations should return ErrAgentResolverNotConfigured if Resolve is
// called but the service was wired with a nil resolver.
type AgentResolver interface {
	Resolve(ctx context.Context, agentInstanceID string) (AgentResolution, error)
}

// ErrAgentResolverNotConfigured signals the DispatchService was asked to
// resolve an agent_instance_id but no resolver was wired in. Callers should
// surface this as a misconfiguration, not a runtime NACK.
var ErrAgentResolverNotConfigured = errors.New("dispatch: agent_instance_id supplied but no AgentResolver wired")

// ErrAgentResolutionUnknownAgent signals the resolver could not find the
// supplied agent_instance_id. Callers map this to a NACK with
// reason=agent_unavailable per ADR-0011.
var ErrAgentResolutionUnknownAgent = errors.New("dispatch: agent_instance_id not found")

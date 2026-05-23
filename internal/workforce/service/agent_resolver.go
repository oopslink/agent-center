package service

import (
	"context"
	"errors"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
	"github.com/oopslink/agent-center/internal/workforce"
)

// AgentResolver implements dispatch.AgentResolver against the Workforce BC
// AgentInstanceRepository + WorkerRepository (per ADR-0030 § 5).
//
// Resolve:
//   1. Look up AgentInstance by id
//   2. Look up the bound Worker (worker_id is required for non-builtin)
//   3. Find the Worker.Capability for AgentInstance.AgentCLI
//   4. Run workforce.CheckDispatchFeatures
//   5. Return dispatch.AgentResolution (FeatureOK / FeatureReason /
//      FeatureMessage carry the verdict to the caller)
type AgentResolver struct {
	aiRepo     workforce.AgentInstanceRepository
	workerRepo workforce.WorkerRepository
}

// NewAgentResolver wires the resolver.
func NewAgentResolver(aiRepo workforce.AgentInstanceRepository, workerRepo workforce.WorkerRepository) *AgentResolver {
	return &AgentResolver{aiRepo: aiRepo, workerRepo: workerRepo}
}

// Resolve implements dispatch.AgentResolver.
func (r *AgentResolver) Resolve(ctx context.Context, agentInstanceID string) (dispatch.AgentResolution, error) {
	if r == nil || r.aiRepo == nil || r.workerRepo == nil {
		return dispatch.AgentResolution{}, errors.New("workforce: AgentResolver misconfigured (nil deps)")
	}
	ai, err := r.aiRepo.FindByID(ctx, workforce.AgentInstanceID(agentInstanceID))
	if err != nil {
		if errors.Is(err, workforce.ErrAgentInstanceNotFound) {
			return dispatch.AgentResolution{}, dispatch.ErrAgentResolutionUnknownAgent
		}
		return dispatch.AgentResolution{}, err
	}
	res := dispatch.AgentResolution{
		AgentInstanceID: agentInstanceID,
		AgentCLI:        ai.AgentCLI(),
		HomeDir:         ai.HomeDirPath(),
	}
	if ai.WorkerID() == nil {
		// Built-in supervisor — not a valid dispatch target (per ADR-0029).
		res.FeatureOK = false
		res.FeatureReason = "agent_unavailable"
		res.FeatureMessage = "built-in agent instance cannot receive dispatch (per ADR-0029)"
		return res, nil
	}
	res.WorkerID = string(*ai.WorkerID())
	w, err := r.workerRepo.FindByID(ctx, *ai.WorkerID())
	if err != nil {
		if errors.Is(err, workforce.ErrWorkerNotFound) {
			res.FeatureOK = false
			res.FeatureReason = "agent_unavailable"
			res.FeatureMessage = "agent_instance.worker_id refers to unknown worker"
			return res, nil
		}
		return dispatch.AgentResolution{}, err
	}
	cap, ok := w.CapabilityForCLI(ai.AgentCLI())
	if !ok {
		res.FeatureOK = false
		res.FeatureReason = "capability_missing"
		res.FeatureMessage = "worker has no capability entry for agent_cli=" + ai.AgentCLI()
		return res, nil
	}
	if !cap.Detected || !cap.Enabled {
		res.FeatureOK = false
		res.FeatureReason = "capability_missing"
		res.FeatureMessage = "worker capability for agent_cli=" + ai.AgentCLI() + " is not detected/enabled"
		return res, nil
	}
	check := workforce.CheckDispatchFeatures(ai, cap)
	res.FeatureOK = check.OK
	res.FeatureReason = check.Reason
	res.FeatureMessage = check.Message
	return res, nil
}

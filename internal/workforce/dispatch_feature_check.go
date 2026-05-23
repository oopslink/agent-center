package workforce

import (
	"errors"
	"fmt"
)

// FeatureCheckResult describes a single failed feature constraint, returned
// by CheckDispatchFeatures so DispatchService can emit NACK reason +
// message verbatim (per ADR-0030 § 5).
type FeatureCheckResult struct {
	OK      bool
	Reason  string
	Message string
}

// CheckDispatchFeatures verifies the worker's capability for the given
// agent_cli supports the v2 features the AgentInstance requires
// (per ADR-0030 § 5).
//
//   - capability ∈ worker.capabilities where AgentCLI = ai.AgentCLI is
//     detected AND enabled (caller resolves this beforehand and passes via
//     the supplied capability; this helper only checks the feature flags)
//   - if ai.HasMCPConfig() && !cap.SupportsMCP → fail (reason=feature_unsupported)
//   - if ai.HasSkillsHint() && !cap.SupportsSkills → fail (soft warning;
//     surfaced as failed but caller may downgrade)
//
// Returns OK=true when no constraint fires.
func CheckDispatchFeatures(ai *AgentInstance, cap Capability) FeatureCheckResult {
	if ai == nil {
		return FeatureCheckResult{
			OK:      false,
			Reason:  "feature_unsupported",
			Message: "dispatch feature-check called with nil AgentInstance",
		}
	}
	if cap.AgentCLI == "" {
		return FeatureCheckResult{
			OK:      false,
			Reason:  "feature_unsupported",
			Message: "dispatch feature-check called with empty Capability.AgentCLI",
		}
	}
	if cap.AgentCLI != ai.AgentCLI() {
		return FeatureCheckResult{
			OK:      false,
			Reason:  "capability_missing",
			Message: fmt.Sprintf("capability mismatch: agent expects %s but capability is for %s",
				ai.AgentCLI(), cap.AgentCLI),
		}
	}
	if ai.HasMCPConfig() && !cap.SupportsMCP {
		return FeatureCheckResult{
			OK:      false,
			Reason:  "feature_unsupported",
			Message: fmt.Sprintf("adapter %q does not support MCP but agent_instance %q has mcp_config",
				cap.AgentCLI, ai.Name()),
		}
	}
	if ai.HasSkillsHint() && !cap.SupportsSkills {
		return FeatureCheckResult{
			OK:      false,
			Reason:  "feature_unsupported",
			Message: fmt.Sprintf("adapter %q does not support skills but agent_instance %q references skills",
				cap.AgentCLI, ai.Name()),
		}
	}
	return FeatureCheckResult{OK: true}
}

// Sentinel errors for callers that prefer typed checks.
var (
	ErrDispatchFeatureUnsupported = errors.New("workforce: dispatch feature unsupported (per ADR-0030 § 5)")
	ErrDispatchCapabilityMissing  = errors.New("workforce: dispatch capability missing (agent_cli mismatch)")
)

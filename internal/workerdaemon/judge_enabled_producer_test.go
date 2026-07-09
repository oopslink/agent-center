package workerdaemon

import (
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// TestExecConfig_JudgeEnabledPassthrough proves the LAST leg of the T950 ② producer
// chain: BOTH ExecutorConfig builders (the resume path and the live-reconcile path)
// carry the per-agent judge opt-in through to ExecutorConfig.JudgeEnabled, which
// BuildExecutorEngine reads to wire the judge. Without this the switch would be inert
// (the review-loopback bug). Asserts the true value flows, not merely that the field
// exists, and that false stays false (OFF byte-identical).
func TestExecConfig_JudgeEnabledPassthrough(t *testing.T) {
	pool := []agent.ExecutorProfile{{CLI: "claude-code", Model: "opus"}}

	// Resume path: ResumeAgent.JudgeEnabled → ExecutorConfig.JudgeEnabled.
	for _, on := range []bool{true, false} {
		ec, _, err := execConfigFromResumeAgent(ResumeAgent{
			AgentID: "agent-1", JudgeEnabled: on, AllowedExecutors: pool,
		})
		if err != nil {
			t.Fatalf("execConfigFromResumeAgent(on=%v): %v", on, err)
		}
		if ec.JudgeEnabled != on {
			t.Errorf("resume path: ExecutorConfig.JudgeEnabled=%v want %v", ec.JudgeEnabled, on)
		}
	}

	// Reconcile path: reconcilePayload.JudgeEnabled → ExecutorConfig.JudgeEnabled.
	for _, on := range []bool{true, false} {
		ec := execConfigOf(reconcilePayload{AgentID: "agent-1", JudgeEnabled: on, AllowedExecutors: pool})
		if ec.JudgeEnabled != on {
			t.Errorf("reconcile path: ExecutorConfig.JudgeEnabled=%v want %v", ec.JudgeEnabled, on)
		}
	}
}

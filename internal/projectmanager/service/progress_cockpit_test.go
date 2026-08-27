package service

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestDeriveProgressRequiredActions_AuthoritativeFactsOnly(t *testing.T) {
	deadline := time.Date(2026, 8, 27, 13, 0, 0, 0, time.UTC)
	actions := deriveProgressRequiredActions(pm.ProgressControlSnapshot{
		OpenObligations: []pm.ProgressObligation{
			{ID: "human-1", Kind: "human_decision", OwnerRef: "user:owner", DeadlineAt: deadline, SourceFactRefs: []string{"decision-required:1"}},
			{ID: "source-1", Kind: pm.ObligationSourceRecovery, OwnerRef: "service:pm", DeadlineAt: deadline, SourceFactRefs: []string{"upstream:done"}},
		},
	})
	if len(actions) != 2 {
		t.Fatalf("actions=%+v", actions)
	}
	if actions[0].Category != "owner_action" || len(actions[0].Options) != 3 {
		t.Fatalf("human decision=%+v", actions[0])
	}
	if actions[1].Category != "prerequisite_wait" || actions[1].TriggerFactRefs[0] != "upstream:done" {
		t.Fatalf("prerequisite=%+v", actions[1])
	}
	for _, action := range actions {
		if action.SourceID == "" || len(action.TriggerFactRefs) == 0 {
			t.Fatalf("action lacks authoritative source: %+v", action)
		}
	}
}

func TestDeriveProgressRequiredActions_DoesNotInferFromNodeOrExecutorStatus(t *testing.T) {
	// With no durable Obligation/Incident/Hold there is no required action. A
	// running/ready task or executor state is intentionally not an input here.
	if got := deriveProgressRequiredActions(pm.ProgressControlSnapshot{}); len(got) != 0 {
		t.Fatalf("status-only facts produced actions: %+v", got)
	}
}

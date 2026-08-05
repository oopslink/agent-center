package projectmanager

import "testing"

func TestCompileRemediationProposalDefaultsEvidenceSupplementOnly(t *testing.T) {
	payload := RemediationProposalPayload{
		Name: "remediate", Rationale: "rejected",
		Tasks: []RemediationTaskSpec{
			{Ref: "proof", Title: "补验收证据", AssigneeRef: "user:a", DispatchMode: DispatchExecutorFork},
			{Ref: "fix", Title: "Fix implementation", AssigneeRef: "user:a", DispatchMode: DispatchExecutorFork},
		},
		Gate: RemediationGateSpec{AssigneeRef: "user:a", AcceptanceContract: "run tests; missing proof"},
	}
	got, diagnostics := CompileRemediationProposal(payload, "run tests", "missing proof")
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics=%v", diagnostics)
	}
	if got.Tasks[0].DeliveryContract != DeliveryEvidenceOnly {
		t.Fatalf("evidence task contract=%q", got.Tasks[0].DeliveryContract)
	}
	if got.Tasks[1].DeliveryContract != "" || got.Tasks[1].DeliveryContract.Effective() != DeliveryCodeChange {
		t.Fatalf("ordinary fix contract=%q", got.Tasks[1].DeliveryContract)
	}

	payload.Tasks[0].DeliveryContract = "future"
	_, diagnostics = CompileRemediationProposal(payload, "run tests", "missing proof")
	if len(diagnostics) == 0 {
		t.Fatal("unknown delivery contract must fail closed")
	}
}

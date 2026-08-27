package service

import (
	"context"
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

type fakeDeliveryVerifier struct {
	bySHA map[string]DeliveryVerification
}

func (f *fakeDeliveryVerifier) VerifyDeliverySubject(_ context.Context, req DeliveryVerificationRequest) (DeliveryVerification, error) {
	if f.bySHA == nil {
		return DeliveryVerification{}, nil
	}
	return f.bySHA[req.CandidateSHA], nil
}

const (
	s2bBase  = "1111111111111111111111111111111111111111"
	s2bGood  = "2222222222222222222222222222222222222222"
	s2bOther = "3333333333333333333333333333333333333333"
)

func seedS2BStage(t *testing.T, verifier *fakeDeliveryVerifier) (*planAdvanceHarness, pm.PlanID, pm.StageID, pm.TaskID, pm.TaskID) {
	t.Helper()
	h, _ := planGraphSetup(t)
	h.svc.acceptances = pmsql.NewDeliveryAcceptanceRepo(h.db)
	h.svc.deliveryVerifier = verifier
	ctx := h.ctx
	pid, _ := h.svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	ref, err := pm.NewCodeRepoRef(pm.NewCodeRepoRefInput{
		ID: "repo-ref-1", ProjectID: pid, URL: "https://example.invalid/repo.git", AddedBy: "user:a", CreatedAt: h.clk.Now(), IsPrimary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.codeRepoRefs.Save(ctx, ref); err != nil {
		t.Fatal(err)
	}
	planID, _ := h.svc.CreatePlan(ctx, CreatePlanCommand{ProjectID: pid, Name: "s2b", CreatedBy: "user:a"})
	stageID, err := h.svc.CreateStage(ctx, CreateStageCommand{PlanID: planID, Name: "A", Actor: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	work := h.seedAssignedTask(t, pid, planID, "work", "user:dev")
	if err := h.svc.AssignTaskToStage(ctx, planID, work, stageID, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(ctx, planID, "user:a"); err != nil {
		t.Fatal(err)
	}
	stage, _ := h.svc.stages.FindByID(ctx, stageID)
	if err := h.svc.RecordDelivery(ctx, work, "user:dev", &pm.Delivery{
		Probed: true, Pushed: true, Branch: "feat/s2b", HeadSHA: s2bGood,
		BaseRef: s2bBase, BaseKnown: true, AheadOfBase: 1,
	}); err != nil {
		t.Fatal(err)
	}
	h.setTaskStatus(t, stage.GateTaskID(), pm.TaskCompleted)
	return h, planID, stageID, work, stage.GateTaskID()
}

func TestS2B_RecordStageGateVerdict_FailClosedOnRemoteAndAuthority(t *testing.T) {
	verifier := &fakeDeliveryVerifier{bySHA: map[string]DeliveryVerification{
		s2bGood: {CandidateExists: true, RefMatches: true, Pushed: true, BaseIsAncestor: true, RemoteSHA: s2bGood},
	}}
	h, _, _, _, gate := seedS2BStage(t, verifier)

	cases := []struct {
		name        string
		reviewedSHA string
		verify      DeliveryVerification
	}{
		{
			name:        "wrong-reviewed-sha",
			reviewedSHA: s2bOther,
			verify:      DeliveryVerification{CandidateExists: true, RefMatches: true, Pushed: true, BaseIsAncestor: true, RemoteSHA: s2bGood},
		},
		{
			name:        "missing-candidate",
			reviewedSHA: s2bGood,
			verify:      DeliveryVerification{CandidateExists: false, RefMatches: true, Pushed: true, BaseIsAncestor: true, RemoteSHA: s2bGood},
		},
		{
			name:        "missing-push",
			reviewedSHA: s2bGood,
			verify:      DeliveryVerification{CandidateExists: true, RefMatches: true, Pushed: false, BaseIsAncestor: true, RemoteSHA: s2bGood},
		},
		{
			name:        "moving-or-mismatched-ref",
			reviewedSHA: s2bGood,
			verify:      DeliveryVerification{CandidateExists: true, RefMatches: false, Pushed: true, BaseIsAncestor: true, RemoteSHA: s2bOther},
		},
		{
			name:        "base-not-ancestor",
			reviewedSHA: s2bGood,
			verify:      DeliveryVerification{CandidateExists: true, RefMatches: true, Pushed: true, BaseIsAncestor: false, RemoteSHA: s2bGood},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verifier.bySHA[s2bGood] = tc.verify
			if _, err := h.svc.RecordStageGateVerdict(h.ctx, RecordStageGateVerdictCommand{
				GateTaskID: gate, Outcome: pm.GateVerdictPass, Evidence: "ok", ReviewedSHA: tc.reviewedSHA, IdempotencyKey: tc.name, Actor: "user:a",
			}); !errors.Is(err, ErrDeliveryVerificationFailed) {
				t.Fatalf("err=%v, want ErrDeliveryVerificationFailed", err)
			}
		})
	}

	verifier.bySHA[s2bGood] = DeliveryVerification{CandidateExists: true, RefMatches: true, Pushed: true, BaseIsAncestor: true, RemoteSHA: s2bGood}
	if _, err := h.svc.RecordStageGateVerdict(h.ctx, RecordStageGateVerdictCommand{
		GateTaskID: gate, Outcome: pm.GateVerdictPass, Evidence: "ok", ReviewedSHA: s2bGood, IdempotencyKey: "unauth", Actor: "user:stranger",
	}); !errors.Is(err, ErrAcceptanceUnauthorized) && !errors.Is(err, ErrNotMember) {
		t.Fatalf("unauthorized err=%v, want authority or membership denial", err)
	}
	if _, err := h.svc.RecordStageGateVerdict(h.ctx, RecordStageGateVerdictCommand{
		GateTaskID: gate, Outcome: pm.GateVerdictPass, Evidence: "ok", ReviewedSHA: s2bGood, IdempotencyKey: "pass", Actor: "user:a",
	}); err != nil {
		t.Fatalf("authorized verified pass: %v", err)
	}
}

func TestS2B_AcceptanceLedgerHigherAuthorityOverride(t *testing.T) {
	h, _ := planGraphSetup(t)
	ctx := h.ctx
	da := pmsql.NewDeliveryAcceptanceRepo(h.db)
	subject, err := pm.NewDeliverySubject(pm.DeliverySubject{
		ID: "subject-1", SubjectType: pm.DeliverySubjectCommit, PlanID: "plan-1", TaskID: "task-1",
		Remote: "origin", Branch: "feat", BaseSHA: s2bBase, CandidateSHA: s2bGood,
		CandidateRef: "refs/heads/feat", PushedRemote: "origin",
		DeliveryContractHash: pm.ContractHash("code_change"), AcceptanceContractHash: pm.ContractHash("contract"),
		CreatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := da.SaveDeliverySubject(ctx, subject); err != nil {
		t.Fatal(err)
	}
	low, _ := pm.NewAcceptance(pm.Acceptance{ID: "acc-low", SubjectID: subject.ID, SubjectDigest: subject.Digest(), PlanID: subject.PlanID, TaskID: subject.TaskID, ContractHash: subject.AcceptanceContractHash, Verdict: pm.AcceptancePassed, ActorRef: "user:member", AuthorityRank: 10, AuthoritySource: "project_member", EvidenceSHA: s2bGood, CreatedAt: h.clk.Now()}, subject)
	high, _ := pm.NewAcceptance(pm.Acceptance{ID: "acc-high", SubjectID: subject.ID, SubjectDigest: subject.Digest(), PlanID: subject.PlanID, TaskID: subject.TaskID, ContractHash: subject.AcceptanceContractHash, Verdict: pm.AcceptanceRejected, ActorRef: "user:owner", AuthorityRank: 100, AuthoritySource: "project_owner", EvidenceSHA: s2bGood, CreatedAt: h.clk.Now().Add(1)}, subject)
	if err := da.SaveAcceptance(ctx, low); err != nil {
		t.Fatal(err)
	}
	if err := da.SaveAcceptance(ctx, high); err != nil {
		t.Fatal(err)
	}
	eff, found, err := da.FindEffectiveAcceptance(ctx, subject.ID, subject.AcceptanceContractHash)
	if err != nil || !found {
		t.Fatalf("effective acceptance found=%v err=%v", found, err)
	}
	if eff.ID != "acc-high" || eff.Verdict != pm.AcceptanceRejected {
		t.Fatalf("effective=%+v, want higher-authority reject", eff)
	}
}

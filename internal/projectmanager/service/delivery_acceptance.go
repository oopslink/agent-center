package service

import (
	"context"
	"errors"
	"strings"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

var (
	ErrDeliveryAcceptanceUnavailable = errors.New("projectmanager: delivery acceptance ledger unavailable")
	ErrDeliveryVerificationFailed    = errors.New("projectmanager: delivery subject verification failed")
	ErrAcceptanceUnauthorized        = errors.New("projectmanager: actor lacks trusted acceptance authority")
)

func (s *Service) recordDeliverySubjectForTask(ctx context.Context, t *pm.Task, d *pm.Delivery) (pm.DeliverySubject, bool, error) {
	if s.acceptances == nil || s.deliveryVerifier == nil || t == nil || d == nil || !d.HasValidDelivery() || t.PlanID() == "" {
		return pm.DeliverySubject{}, false, nil
	}
	base := strings.TrimSpace(d.BaseRef)
	candidate := strings.TrimSpace(d.HeadSHA)
	if base == "" || candidate == "" {
		return pm.DeliverySubject{}, false, nil
	}
	ref, err := s.primaryCodeRepoRef(ctx, t.ProjectID())
	if err != nil {
		return pm.DeliverySubject{}, false, err
	}
	if ref == nil {
		return pm.DeliverySubject{}, false, nil
	}
	remote := "origin"
	branch := strings.TrimSpace(d.Branch)
	subject, err := pm.NewDeliverySubject(pm.DeliverySubject{
		ID:                     s.idgen.NewEntityID("subject"),
		SubjectType:            pm.DeliverySubjectCommit,
		PlanID:                 t.PlanID(),
		TaskID:                 t.ID(),
		NodeID:                 t.NodeID(),
		ExecutionID:            d.ExecutorID,
		RepoID:                 ref.RepoID(),
		Remote:                 remote,
		Branch:                 branch,
		BaseSHA:                base,
		CandidateSHA:           candidate,
		CandidateRef:           "refs/heads/" + branch,
		PushedRemote:           remote,
		DeliveryContractHash:   pm.ContractHash(string(t.DeliveryContract().Effective())),
		AcceptanceContractHash: pm.ContractHash(""),
		CreatedAt:              s.clock.Now(),
	})
	if err != nil {
		return pm.DeliverySubject{}, false, nil
	}
	v, err := s.deliveryVerifier.VerifyDeliverySubject(ctx, DeliveryVerificationRequest{
		RepoID: subject.RepoID, Remote: subject.Remote, Branch: subject.Branch, CandidateRef: subject.CandidateRef,
		CandidateSHA: subject.CandidateSHA, BaseSHA: subject.BaseSHA,
	})
	if err != nil {
		return pm.DeliverySubject{}, false, err
	}
	if !v.CandidateExists || !v.RefMatches || !v.Pushed || !v.BaseIsAncestor {
		return pm.DeliverySubject{}, false, nil
	}
	if err := s.acceptances.SaveDeliverySubject(ctx, subject); err != nil {
		return pm.DeliverySubject{}, false, err
	}
	return subject, true, nil
}

func (s *Service) primaryCodeRepoRef(ctx context.Context, projectID pm.ProjectID) (*pm.CodeRepoRef, error) {
	if s.codeRepoRefs == nil {
		return nil, nil
	}
	refs, err := s.codeRepoRefs.ListByProject(ctx, projectID)
	if err != nil || len(refs) == 0 {
		return nil, err
	}
	chosen := refs[0]
	for _, ref := range refs {
		if ref.IsPrimary() {
			chosen = ref
			break
		}
	}
	return chosen, nil
}

func (s *Service) acceptanceAuthority(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef, gateAssignee pm.IdentityRef) (int, string, error) {
	if actor == "" {
		return 0, "", ErrAcceptanceUnauthorized
	}
	member, err := s.members.FindByProjectAndIdentity(ctx, projectID, actor)
	if err != nil {
		if errors.Is(err, pm.ErrMemberNotFound) {
			return 0, "", ErrAcceptanceUnauthorized
		}
		return 0, "", err
	}
	if member.Role() == pm.RoleOwner {
		return 100, "project_owner", nil
	}
	if actor == gateAssignee {
		return 50, "gate_assignee", nil
	}
	return 10, "project_member", nil
}

func acceptanceVerdictForGateOutcome(outcome pm.GateVerdictOutcome) pm.AcceptanceVerdict {
	if outcome == pm.GateVerdictPass {
		return pm.AcceptancePassed
	}
	return pm.AcceptanceRejected
}

func (s *Service) recordAcceptanceForGate(ctx context.Context, stage *pm.Stage, gateTask *pm.Task, plan *pm.Plan, cmd RecordStageGateVerdictCommand) (pm.Acceptance, error) {
	if s.acceptances == nil || s.deliveryVerifier == nil {
		return pm.Acceptance{}, nil
	}
	rank, source, err := s.acceptanceAuthority(ctx, plan.ProjectID(), cmd.Actor, gateTask.Assignee())
	if err != nil {
		return pm.Acceptance{}, err
	}
	subject, found, err := s.findSubjectForGateSHA(ctx, stage, cmd.ReviewedSHA)
	if err != nil {
		return pm.Acceptance{}, err
	}
	if !found {
		return pm.Acceptance{}, ErrDeliveryVerificationFailed
	}
	contractHash := pm.ContractHash(stage.GateSpec().AcceptanceContract)
	if subject.AcceptanceContractHash != contractHash {
		subject.ID = s.idgen.NewEntityID("subject")
		subject.AcceptanceContractHash = contractHash
		subject.CreatedAt = s.clock.Now()
		subject, err = pm.NewDeliverySubject(subject)
		if err != nil {
			return pm.Acceptance{}, err
		}
		if err := s.acceptances.SaveDeliverySubject(ctx, subject); err != nil {
			return pm.Acceptance{}, err
		}
	}
	v, err := s.deliveryVerifier.VerifyDeliverySubject(ctx, DeliveryVerificationRequest{
		RepoID: subject.RepoID, Remote: subject.Remote, Branch: subject.Branch, CandidateRef: subject.CandidateRef,
		CandidateSHA: subject.CandidateSHA, BaseSHA: subject.BaseSHA,
	})
	if err != nil {
		return pm.Acceptance{}, err
	}
	if !v.CandidateExists || !v.RefMatches || !v.Pushed || !v.BaseIsAncestor {
		return pm.Acceptance{}, ErrDeliveryVerificationFailed
	}
	a, err := pm.NewAcceptance(pm.Acceptance{
		ID:              s.idgen.NewEntityID("acceptance"),
		SubjectID:       subject.ID,
		SubjectDigest:   subject.Digest(),
		PlanID:          subject.PlanID,
		TaskID:          subject.TaskID,
		GateTaskID:      gateTask.ID(),
		ContractHash:    contractHash,
		Verdict:         acceptanceVerdictForGateOutcome(cmd.Outcome),
		ActorRef:        cmd.Actor,
		AuthorityRank:   rank,
		AuthoritySource: source,
		EvidenceRef:     cmd.Evidence,
		EvidenceSHA:     cmd.ReviewedSHA,
		FindingsJSON:    "[]",
		CreatedAt:       s.clock.Now(),
	}, subject)
	if err != nil {
		return pm.Acceptance{}, err
	}
	if err := s.acceptances.SaveAcceptance(ctx, a); err != nil {
		return pm.Acceptance{}, err
	}
	return a, nil
}

func (s *Service) findSubjectForGateSHA(ctx context.Context, stage *pm.Stage, reviewedSHA string) (pm.DeliverySubject, bool, error) {
	tasks, err := s.tasks.ListByPlan(ctx, stage.PlanID())
	if err != nil {
		return pm.DeliverySubject{}, false, err
	}
	want := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(reviewedSHA)), "sha256:")
	for _, task := range tasks {
		if task.StageID() != stage.ID() || task.ID() == stage.GateTaskID() {
			continue
		}
		subject, found, err := s.acceptances.FindLatestDeliverySubjectByTask(ctx, stage.PlanID(), task.ID())
		if err != nil || !found {
			return pm.DeliverySubject{}, false, err
		}
		if strings.TrimPrefix(strings.ToLower(subject.CandidateSHA), "sha256:") == want {
			return subject, true, nil
		}
	}
	return pm.DeliverySubject{}, false, nil
}

func (s *Service) acceptancePassesGate(ctx context.Context, stage *pm.Stage, gateTaskID pm.TaskID) (bool, error) {
	if s.acceptances == nil {
		return false, nil
	}
	tasks, err := s.tasks.ListByPlan(ctx, stage.PlanID())
	if err != nil {
		return false, err
	}
	contractHash := pm.ContractHash(stage.GateSpec().AcceptanceContract)
	for _, task := range tasks {
		if task.StageID() != stage.ID() || task.ID() == gateTaskID {
			continue
		}
		subject, found, err := s.acceptances.FindLatestDeliverySubjectByTask(ctx, stage.PlanID(), task.ID())
		if err != nil || !found {
			return false, err
		}
		if subject.AcceptanceContractHash != contractHash {
			return false, nil
		}
		acc, found, err := s.acceptances.FindEffectiveAcceptance(ctx, subject.ID, contractHash)
		if err != nil || !found {
			return false, err
		}
		if acc.SubjectDigest != subject.Digest() || acc.Verdict != pm.AcceptancePassed {
			return false, nil
		}
	}
	return true, nil
}

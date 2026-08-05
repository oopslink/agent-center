package projectmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type GateVerdictOutcome string

const (
	GateVerdictPass   GateVerdictOutcome = "pass"
	GateVerdictReject GateVerdictOutcome = "reject"
)

var (
	ErrGateAlreadyVerdicted        = errors.New("projectmanager: gate already has an immutable verdict")
	ErrGateVerdictNotFound         = errors.New("projectmanager: gate verdict not found")
	ErrContinuationNotFound        = errors.New("projectmanager: continuation not found")
	ErrContinuationClosed          = errors.New("projectmanager: continuation is closed")
	ErrRemediationBudgetExhausted  = errors.New("projectmanager: remediation budget exhausted")
	ErrRemediationProposalInvalid  = errors.New("projectmanager: invalid remediation proposal")
	ErrRemediationProposalStale    = errors.New("projectmanager: remediation proposal is stale")
	ErrRemediationProposalNotFound = errors.New("projectmanager: remediation proposal not found")
	ErrRemediationProposalExists   = errors.New("projectmanager: remediation proposal already exists")
	ErrRemediationUnavailable      = errors.New("projectmanager: remediation ledger unavailable")
	ErrIdempotencyConflict         = errors.New("projectmanager: idempotency key reused with different content")
)

type GateVerdict struct {
	ID             GateVerdictID      `json:"id"`
	ProjectID      ProjectID          `json:"project_id"`
	PlanID         PlanID             `json:"plan_id"`
	StageID        StageID            `json:"stage_id"`
	GateTaskID     TaskID             `json:"gate_task_id"`
	Outcome        GateVerdictOutcome `json:"outcome"`
	Evidence       string             `json:"evidence"`
	ReviewedSHA    string             `json:"reviewed_sha"`
	ActorRef       IdentityRef        `json:"actor_ref"`
	IdempotencyKey string             `json:"idempotency_key"`
	CreatedAt      time.Time          `json:"created_at"`
}

func NewGateVerdict(v GateVerdict) (GateVerdict, error) {
	if v.ID == "" || v.ProjectID == "" || v.PlanID == "" || v.StageID == "" || v.GateTaskID == "" {
		return GateVerdict{}, errors.New("projectmanager: verdict scope required")
	}
	if v.Outcome != GateVerdictPass && v.Outcome != GateVerdictReject {
		return GateVerdict{}, errors.New("projectmanager: verdict outcome must be pass or reject")
	}
	if strings.TrimSpace(v.Evidence) == "" || strings.TrimSpace(v.ReviewedSHA) == "" || strings.TrimSpace(v.IdempotencyKey) == "" {
		return GateVerdict{}, errors.New("projectmanager: verdict evidence, reviewed_sha and idempotency_key required")
	}
	if err := v.ActorRef.Validate(); err != nil {
		return GateVerdict{}, err
	}
	if v.CreatedAt.IsZero() {
		return GateVerdict{}, errors.New("projectmanager: verdict created_at required")
	}
	v.Evidence = strings.TrimSpace(v.Evidence)
	v.ReviewedSHA = strings.TrimSpace(v.ReviewedSHA)
	v.IdempotencyKey = strings.TrimSpace(v.IdempotencyKey)
	v.CreatedAt = v.CreatedAt.UTC()
	return v, nil
}

type ContinuationStatus string

const (
	ContinuationAwaitingRemediation ContinuationStatus = "awaiting_remediation"
	ContinuationExecuting           ContinuationStatus = "executing"
	ContinuationBudgetExhausted     ContinuationStatus = "budget_exhausted"
	ContinuationClosed              ContinuationStatus = "closed"
)

type PlanContinuation struct {
	ID                  ContinuationID        `json:"id"`
	ProjectID           ProjectID             `json:"project_id"`
	PlanID              PlanID                `json:"plan_id"`
	RootStageID         StageID               `json:"root_stage_id"`
	CurrentStageID      StageID               `json:"current_stage_id"`
	TriggerVerdictID    GateVerdictID         `json:"trigger_verdict_id"`
	Status              ContinuationStatus    `json:"status"`
	Generation          int                   `json:"generation"`
	RemainingBudget     int                   `json:"remaining_budget"`
	BoundaryFingerprint string                `json:"boundary_fingerprint"`
	PendingProposalID   RemediationProposalID `json:"pending_proposal_id,omitempty"`
	ClosedByVerdictID   GateVerdictID         `json:"closed_by_verdict_id,omitempty"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	Version             int                   `json:"version"`
}

func NewPlanContinuation(id ContinuationID, verdict GateVerdict, boundary string, budget int, at time.Time) (*PlanContinuation, error) {
	if id == "" || verdict.Outcome != GateVerdictReject || strings.TrimSpace(boundary) == "" {
		return nil, errors.New("projectmanager: reject verdict and continuation boundary required")
	}
	if budget <= 0 {
		budget = DefaultStageMaxRounds
	}
	at = at.UTC()
	return &PlanContinuation{ID: id, ProjectID: verdict.ProjectID, PlanID: verdict.PlanID,
		RootStageID: verdict.StageID, CurrentStageID: verdict.StageID, TriggerVerdictID: verdict.ID,
		Status: ContinuationAwaitingRemediation, RemainingBudget: budget,
		BoundaryFingerprint: boundary, CreatedAt: at, UpdatedAt: at, Version: 1}, nil
}

func (c *PlanContinuation) AttachStage(stageID StageID, proposalID RemediationProposalID, boundary string, at time.Time) error {
	if c.Status == ContinuationClosed {
		return ErrContinuationClosed
	}
	if c.RemainingBudget <= 0 {
		c.Status = ContinuationBudgetExhausted
		return ErrRemediationBudgetExhausted
	}
	if stageID == "" || proposalID == "" || boundary == "" {
		return ErrRemediationProposalInvalid
	}
	c.CurrentStageID = stageID
	c.PendingProposalID = proposalID
	c.Generation++
	c.RemainingBudget--
	c.BoundaryFingerprint = boundary
	c.Status = ContinuationExecuting
	c.UpdatedAt = at.UTC()
	c.Version++
	return nil
}

func (c *PlanContinuation) AwaitNext(verdict GateVerdict, boundary string, at time.Time) error {
	if c.Status == ContinuationClosed {
		return ErrContinuationClosed
	}
	c.TriggerVerdictID = verdict.ID
	c.PendingProposalID = ""
	c.BoundaryFingerprint = boundary
	if c.RemainingBudget <= 0 {
		c.Status = ContinuationBudgetExhausted
	} else {
		c.Status = ContinuationAwaitingRemediation
	}
	c.UpdatedAt = at.UTC()
	c.Version++
	return nil
}

// RefreshAwaitingBoundary rebases an uncompiled continuation after a pure Plan
// control-state change (for example paused → running). No topology has been
// committed yet, so adopting the current Plan version is safe; once a proposal
// exists, its own version/fingerprint checks remain the stale-write guard.
func (c *PlanContinuation) RefreshAwaitingBoundary(boundary string, at time.Time) error {
	if c.Status != ContinuationAwaitingRemediation || c.PendingProposalID != "" {
		return ErrRemediationProposalStale
	}
	boundary = strings.TrimSpace(boundary)
	if boundary == "" {
		return ErrRemediationProposalInvalid
	}
	if c.BoundaryFingerprint == boundary {
		return nil
	}
	c.BoundaryFingerprint = boundary
	c.UpdatedAt = at.UTC()
	c.Version++
	return nil
}

func (c *PlanContinuation) Close(verdictID GateVerdictID, at time.Time) error {
	if c.Status == ContinuationClosed {
		return ErrContinuationClosed
	}
	c.Status = ContinuationClosed
	c.ClosedByVerdictID = verdictID
	c.PendingProposalID = ""
	c.UpdatedAt = at.UTC()
	c.Version++
	return nil
}

type RemediationTaskSpec struct {
	Ref              string           `json:"ref"`
	Title            string           `json:"title"`
	Description      string           `json:"description,omitempty"`
	AssigneeRef      IdentityRef      `json:"assignee_ref"`
	DispatchMode     DispatchMode     `json:"dispatch_mode,omitempty"`
	DeliveryContract DeliveryContract `json:"delivery_contract,omitempty"`
	FollowsTaskID    TaskID           `json:"follows_task_id,omitempty"`
}

type RemediationEdgeSpec struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type RemediationGateSpec struct {
	AssigneeRef        IdentityRef `json:"assignee_ref"`
	AcceptanceContract string      `json:"acceptance_contract"`
}

type RemediationProposalPayload struct {
	Name      string                `json:"name"`
	Rationale string                `json:"rationale"`
	Tasks     []RemediationTaskSpec `json:"tasks"`
	Edges     []RemediationEdgeSpec `json:"edges,omitempty"`
	Gate      RemediationGateSpec   `json:"gate"`
}

type RemediationProposal struct {
	ID                  RemediationProposalID
	ProjectID           ProjectID
	PlanID              PlanID
	ContinuationID      ContinuationID
	TriggerVerdictID    GateVerdictID
	IdempotencyKey      string
	BasedOnPlanVersion  int
	BoundaryFingerprint string
	Payload             RemediationProposalPayload
	Status              string
	Diagnostics         []string
	CreatedBy           IdentityRef
	CreatedAt           time.Time
	CommittedAt         time.Time
}

// CompileRemediationProposal is the pure topology validator. It canonicalizes
// refs and rejects cycles/bad references before any repository write.
func CompileRemediationProposal(payload RemediationProposalPayload, baseContract, rejectEvidence string) (RemediationProposalPayload, []string) {
	var diagnostics []string
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Rationale = strings.TrimSpace(payload.Rationale)
	if payload.Name == "" {
		diagnostics = append(diagnostics, "name_required")
	}
	if len(payload.Tasks) == 0 {
		diagnostics = append(diagnostics, "task_required")
	}
	refs := map[string]bool{}
	for i := range payload.Tasks {
		t := &payload.Tasks[i]
		t.Ref = strings.TrimSpace(t.Ref)
		t.Title = strings.TrimSpace(t.Title)
		if t.Ref == "" || t.Title == "" {
			diagnostics = append(diagnostics, fmt.Sprintf("task_%d_ref_title_required", i))
			continue
		}
		if refs[t.Ref] {
			diagnostics = append(diagnostics, "duplicate_task_ref:"+t.Ref)
		}
		refs[t.Ref] = true
		if err := t.AssigneeRef.Validate(); err != nil {
			diagnostics = append(diagnostics, "invalid_assignee:"+t.Ref)
		}
		if !t.DispatchMode.IsValid() {
			diagnostics = append(diagnostics, "invalid_dispatch_mode:"+t.Ref)
		}
		// Evidence-supplement remediation nodes default to evidence_only; ordinary repair
		// nodes remain the legacy code_change contract.
		if t.DeliveryContract == "" {
			text := strings.ToLower(t.Title + "\n" + t.Description)
			if strings.Contains(text, "evidence") || strings.Contains(text, "verification") || strings.Contains(text, "验收证据") || strings.Contains(text, "补证据") {
				t.DeliveryContract = DeliveryEvidenceOnly
			}
		}
		if !t.DeliveryContract.IsValid() {
			diagnostics = append(diagnostics, "invalid_delivery_contract:"+t.Ref)
		}
	}
	adj, indegree := map[string][]string{}, map[string]int{}
	for ref := range refs {
		indegree[ref] = 0
	}
	for _, edge := range payload.Edges {
		if !refs[edge.From] || !refs[edge.To] || edge.From == edge.To {
			diagnostics = append(diagnostics, "invalid_edge:"+edge.From+"->"+edge.To)
			continue
		}
		adj[edge.From] = append(adj[edge.From], edge.To)
		indegree[edge.To]++
	}
	queue := make([]string, 0)
	for ref, degree := range indegree {
		if degree == 0 {
			queue = append(queue, ref)
		}
	}
	sort.Strings(queue)
	seen := 0
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		seen++
		for _, next := range adj[ref] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
				sort.Strings(queue)
			}
		}
	}
	if seen != len(refs) {
		diagnostics = append(diagnostics, "cycle")
	}
	if err := payload.Gate.AssigneeRef.Validate(); err != nil {
		diagnostics = append(diagnostics, "gate_assignee_required")
	}
	contract := strings.TrimSpace(payload.Gate.AcceptanceContract)
	if base := strings.TrimSpace(baseContract); base != "" && !strings.Contains(contract, base) {
		diagnostics = append(diagnostics, "base_acceptance_contract_missing")
	}
	if evidence := strings.TrimSpace(rejectEvidence); evidence != "" && !strings.Contains(contract, evidence) {
		diagnostics = append(diagnostics, "reject_evidence_missing")
	}
	if contract == "" {
		diagnostics = append(diagnostics, "acceptance_contract_required")
	}
	sort.Strings(diagnostics)
	return payload, diagnostics
}

func ContinuationBoundaryFingerprint(planID PlanID, stageID StageID, downstreamNodeIDs []string, version int) string {
	ids := append([]string(nil), downstreamNodeIDs...)
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%d\n%s", planID, stageID, version, strings.Join(ids, "\n"))))
	return "sha256:" + hex.EncodeToString(sum[:])
}

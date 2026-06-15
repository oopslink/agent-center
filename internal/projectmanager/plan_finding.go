package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// PlanFindingKind tags a finding by the DeLM taxonomy (arXiv:2606.10662 §4.2.1):
//
//	fact          — a verified discovery (e.g. "the real bug is on path X")
//	failure       — a falsified hypothesis / dead end (so siblings don't re-explore)
//	constraint    — a binding rule later work must respect (kept explicit, not softened)
//	patch_summary — a compact summary of a completed change/result for hand-off
type PlanFindingKind string

const (
	FindingFact         PlanFindingKind = "fact"
	FindingFailure      PlanFindingKind = "failure"
	FindingConstraint   PlanFindingKind = "constraint"
	FindingPatchSummary PlanFindingKind = "patch_summary"
)

// IsValid reports enum membership.
func (k PlanFindingKind) IsValid() bool {
	switch k {
	case FindingFact, FindingFailure, FindingConstraint, FindingPatchSummary:
		return true
	}
	return false
}

// MaxFindingContentLen caps a finding gist so it stays COMPACT (DeLM gists are
// ~100 tokens; we allow generous slack) and well under the [§8] 10KB BlobStore
// threshold, so a finding always lives in the DB TEXT column (never BlobStore).
const MaxFindingContentLen = 4000

// PlanFinding is a project-scoped, plan-scoped knowledge gist that an agent
// records back to its Plan's SHARED CONTEXT (ADR-0053, the DeLM "verified shared
// context" minimal slice). It is bound to the SOURCE Task that produced it
// (taskID) and attributed to its author. A finding is IMMUTABLE (append-only):
// there is no Update — to change it, retract and re-record (hence no updatedAt;
// version is fixed at 1, kept only to mirror the AR round-trip shape). Downstream
// / sibling agents read findings (injected into dispatch, or via list_findings)
// to build on prior progress instead of re-discovering it.
type PlanFinding struct {
	id        PlanFindingID
	planID    PlanID
	taskID    TaskID
	projectID ProjectID
	authorRef IdentityRef
	kind      PlanFindingKind
	content   string
	createdAt time.Time
	version   int
}

// PlanFindingID is the finding's typed identifier (ULID).
type PlanFindingID string

// String returns the raw id.
func (id PlanFindingID) String() string { return string(id) }

// NewPlanFindingInput captures constructor args.
type NewPlanFindingInput struct {
	ID        PlanFindingID
	PlanID    PlanID
	TaskID    TaskID
	ProjectID ProjectID
	AuthorRef IdentityRef
	Kind      PlanFindingKind
	Content   string
	CreatedAt time.Time
}

// NewPlanFinding constructs a fresh finding. It validates the structural
// invariants (ids present, author ref well-formed, kind in the taxonomy, content
// non-empty and compact). The ADMISSION rules (author == source task's assignee,
// task belongs to the plan, actor is a project member, project not archived) are
// cross-aggregate and enforced by the AppService, not here.
func NewPlanFinding(in NewPlanFindingInput) (*PlanFinding, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: plan finding id required")
	}
	if strings.TrimSpace(string(in.PlanID)) == "" {
		return nil, ErrPlanFindingNoPlan
	}
	if strings.TrimSpace(string(in.TaskID)) == "" {
		return nil, ErrPlanFindingNoTask
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if err := in.AuthorRef.Validate(); err != nil {
		return nil, err
	}
	if !in.Kind.IsValid() {
		return nil, ErrInvalidFindingKind
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return nil, ErrEmptyFindingContent
	}
	if len(content) > MaxFindingContentLen {
		return nil, ErrFindingContentTooLong
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	return &PlanFinding{
		id:        in.ID,
		planID:    in.PlanID,
		taskID:    in.TaskID,
		projectID: in.ProjectID,
		authorRef: in.AuthorRef,
		kind:      in.Kind,
		content:   content,
		createdAt: in.CreatedAt.UTC(),
		version:   1,
	}, nil
}

// RehydratePlanFindingInput is for repository round-trip.
type RehydratePlanFindingInput struct {
	ID        PlanFindingID
	PlanID    PlanID
	TaskID    TaskID
	ProjectID ProjectID
	AuthorRef IdentityRef
	Kind      PlanFindingKind
	Content   string
	CreatedAt time.Time
	Version   int
}

// RehydratePlanFinding reconstructs without business validation (only enum +
// version, mirroring RehydratePlan).
func RehydratePlanFinding(in RehydratePlanFindingInput) (*PlanFinding, error) {
	if !in.Kind.IsValid() {
		return nil, ErrInvalidFindingKind
	}
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	return &PlanFinding{
		id:        in.ID,
		planID:    in.PlanID,
		taskID:    in.TaskID,
		projectID: in.ProjectID,
		authorRef: in.AuthorRef,
		kind:      in.Kind,
		content:   in.Content,
		createdAt: in.CreatedAt.UTC(),
		version:   in.Version,
	}, nil
}

// Getters.
func (f *PlanFinding) ID() PlanFindingID      { return f.id }
func (f *PlanFinding) PlanID() PlanID         { return f.planID }
func (f *PlanFinding) TaskID() TaskID         { return f.taskID }
func (f *PlanFinding) ProjectID() ProjectID   { return f.projectID }
func (f *PlanFinding) AuthorRef() IdentityRef { return f.authorRef }
func (f *PlanFinding) Kind() PlanFindingKind  { return f.kind }
func (f *PlanFinding) Content() string        { return f.content }
func (f *PlanFinding) CreatedAt() time.Time   { return f.createdAt }
func (f *PlanFinding) Version() int           { return f.version }

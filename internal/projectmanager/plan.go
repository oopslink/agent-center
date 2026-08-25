package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// PlanStatus is the ADR-0055 Plan lifecycle. Pause is an execution latch, not a
// return to pending authoring. Discarded is permanent; done may reopen to paused
// only to append a follow-up immutable generation.
type PlanStatus string

const (
	PlanPending   PlanStatus = "pending"
	PlanRunning   PlanStatus = "running"
	PlanPaused    PlanStatus = "paused"
	PlanDone      PlanStatus = "done"
	PlanDiscarded PlanStatus = "discarded"
)

// IsValid reports enum membership.
func (s PlanStatus) IsValid() bool {
	switch s {
	case PlanPending, PlanRunning, PlanPaused, PlanDone, PlanDiscarded:
		return true
	}
	return false
}

// planTransitions is the allowed-transition adjacency.
var planTransitions = map[PlanStatus][]PlanStatus{
	PlanPending:   {PlanRunning, PlanDiscarded},
	PlanRunning:   {PlanPaused, PlanDone, PlanDiscarded},
	PlanPaused:    {PlanRunning, PlanDone, PlanDiscarded},
	PlanDone:      {},
	PlanDiscarded: {},
}

// CanTransitionTo reports whether from→to is a legal Plan transition.
func (s PlanStatus) CanTransitionTo(to PlanStatus) bool {
	for _, n := range planTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

// Plan is a project-scoped, parallel-capable orchestration unit (design §2). It
// selects a subset of the project's backlog tasks and owns exactly one execution
// DAG over them (§9.8). Node status is DERIVED by the orchestrator, never stored
// here (§9.2): a Plan holds no node_status/node_state. The 1:1 conversation is
// wired in #284 (conversationID is "" until then).
type Plan struct {
	id             PlanID
	projectID      ProjectID
	name           string
	description    string
	status         PlanStatus
	creatorRef     IdentityRef
	ownerRef       IdentityRef
	backupOwnerRef IdentityRef
	conversationID string
	targetDate     *time.Time
	// builtin marks the per-project default "assignment pool" plan (ADR-0047): one
	// per project, auto-created + always-started, FLAT (no dependency edges), a
	// "pull, no-wake" dispatch pool. It cannot be stopped / archived / deleted on its
	// own (it is archived WITH its project).
	builtin bool
	// orgNumber is the per-org monotonic display/reference number (v2.10.1 [T99],
	// rendered "P<n>"). Allocated at create by the org sequence (entity_type
	// "plan", INDEPENDENT of tasks/issues); 0 for the builtin pool + rows
	// predating the allocator / not yet backfilled (DTO omits org_ref then).
	orgNumber int
	// graphID is the orchestration engine graph ID that this plan maps to (v2.2.8).
	// "" when not wired to an orchestration graph.
	graphID string
	// activeGenerationID points at the immutable PlanGeneration snapshot the Plan is
	// currently executing/authoring against. The generation row is append-only; this
	// pointer is the mutable Plan aggregate state switched by Evolution CAS commits.
	activeGenerationID   PlanGenerationID
	attentionStatus      PlanAttentionStatus
	attentionSince       time.Time
	lastAttentionEventID PlanBlockEventID
	recoveryPolicy       PlanRecoveryPolicy
	archivedAt           *time.Time
	archivedBy           IdentityRef
	createdAt            time.Time
	updatedAt            time.Time
	version              int
}

// NewPlanInput captures constructor args.
type NewPlanInput struct {
	ID             PlanID
	ProjectID      ProjectID
	Name           string
	Description    string
	CreatorRef     IdentityRef
	OwnerRef       IdentityRef
	BackupOwnerRef IdentityRef
	RecoveryPolicy PlanRecoveryPolicy
	TargetDate     *time.Time
	Builtin        bool // ADR-0047: the per-project default assignment pool
	// OrgNumber is the allocated per-org plan number (v2.10.1 [T99]), supplied by
	// the service from the org sequence within the create tx. 0 ⇒ no org_ref.
	OrgNumber int
	// GraphID is the orchestration engine graph ID (v2.2.8); "" when not wired.
	GraphID            string
	ActiveGenerationID PlanGenerationID
	CreatedAt          time.Time
}

// NewPlan constructs a fresh pending Plan. A Plan must belong to a Project.
func NewPlan(in NewPlanInput) (*Plan, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: plan id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrEmptyPlanName
	}
	if err := in.CreatorRef.Validate(); err != nil {
		return nil, err
	}
	if in.OwnerRef == "" {
		if in.Builtin {
			in.OwnerRef = in.CreatorRef
		} else {
			return nil, ErrPlanOwnerRequired
		}
	}
	if err := in.OwnerRef.Validate(); err != nil {
		return nil, err
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Plan{
		id:                 in.ID,
		projectID:          in.ProjectID,
		name:               in.Name,
		description:        in.Description,
		status:             PlanPending,
		creatorRef:         in.CreatorRef,
		ownerRef:           in.OwnerRef,
		backupOwnerRef:     in.BackupOwnerRef,
		targetDate:         normalizeTargetDate(in.TargetDate),
		builtin:            in.Builtin,
		orgNumber:          in.OrgNumber,
		graphID:            in.GraphID,
		activeGenerationID: in.ActiveGenerationID,
		attentionStatus:    PlanAttentionNone,
		recoveryPolicy:     normalizeRecoveryPolicy(in.RecoveryPolicy),
		createdAt:          at,
		updatedAt:          at,
		version:            1,
	}, nil
}

// RehydratePlanInput is for repository round-trip.
type RehydratePlanInput struct {
	ID             PlanID
	ProjectID      ProjectID
	Name           string
	Description    string
	Status         PlanStatus
	CreatorRef     IdentityRef
	OwnerRef       IdentityRef
	BackupOwnerRef IdentityRef
	ConversationID string
	TargetDate     *time.Time
	Builtin        bool
	OrgNumber      int
	// GraphID is the orchestration engine graph ID (v2.2.8); "" when not wired.
	GraphID              string
	ActiveGenerationID   PlanGenerationID
	AttentionStatus      PlanAttentionStatus
	AttentionSince       time.Time
	LastAttentionEventID PlanBlockEventID
	RecoveryPolicy       PlanRecoveryPolicy
	ArchivedAt           *time.Time
	ArchivedBy           IdentityRef
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Version              int
}

// RehydratePlan reconstructs without invariant checks (only enum + version).
func RehydratePlan(in RehydratePlanInput) (*Plan, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidPlanStatus
	}
	if in.OwnerRef == "" {
		in.OwnerRef = in.CreatorRef
	}
	if in.AttentionStatus == "" {
		in.AttentionStatus = PlanAttentionNone
	}
	if !in.AttentionStatus.IsValid() {
		return nil, errors.New("projectmanager: invalid plan attention status")
	}
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	return &Plan{
		id:                   in.ID,
		projectID:            in.ProjectID,
		name:                 in.Name,
		description:          in.Description,
		status:               in.Status,
		creatorRef:           in.CreatorRef,
		ownerRef:             in.OwnerRef,
		backupOwnerRef:       in.BackupOwnerRef,
		conversationID:       in.ConversationID,
		targetDate:           normalizeTargetDate(in.TargetDate),
		builtin:              in.Builtin,
		orgNumber:            in.OrgNumber,
		graphID:              in.GraphID,
		activeGenerationID:   in.ActiveGenerationID,
		attentionStatus:      in.AttentionStatus,
		attentionSince:       in.AttentionSince.UTC(),
		lastAttentionEventID: in.LastAttentionEventID,
		recoveryPolicy:       normalizeRecoveryPolicy(in.RecoveryPolicy),
		archivedAt:           normalizeTargetDate(in.ArchivedAt),
		archivedBy:           in.ArchivedBy,
		createdAt:            in.CreatedAt.UTC(),
		updatedAt:            in.UpdatedAt.UTC(),
		version:              in.Version,
	}, nil
}

func normalizeRecoveryPolicy(p PlanRecoveryPolicy) PlanRecoveryPolicy {
	d := DefaultPlanRecoveryPolicy()
	if p.NotifyAfterSeconds > 0 {
		d.NotifyAfterSeconds = p.NotifyAfterSeconds
	}
	if p.RemindAfterSeconds > 0 {
		d.RemindAfterSeconds = p.RemindAfterSeconds
	}
	if p.EscalateAfterSeconds > 0 {
		d.EscalateAfterSeconds = p.EscalateAfterSeconds
	}
	return d
}

// normalizeTargetDate UTC-normalizes a non-nil target date.
func normalizeTargetDate(d *time.Time) *time.Time {
	if d == nil || d.IsZero() {
		return nil
	}
	u := d.UTC()
	return &u
}

// Getters.
func (p *Plan) ID() PlanID                  { return p.id }
func (p *Plan) ProjectID() ProjectID        { return p.projectID }
func (p *Plan) Name() string                { return p.name }
func (p *Plan) Description() string         { return p.description }
func (p *Plan) Status() PlanStatus          { return p.status }
func (p *Plan) CreatorRef() IdentityRef     { return p.creatorRef }
func (p *Plan) OwnerRef() IdentityRef       { return p.ownerRef }
func (p *Plan) BackupOwnerRef() IdentityRef { return p.backupOwnerRef }
func (p *Plan) ConversationID() string      { return p.conversationID }
func (p *Plan) TargetDate() *time.Time      { return p.targetDate }
func (p *Plan) CreatedAt() time.Time        { return p.createdAt }
func (p *Plan) UpdatedAt() time.Time        { return p.updatedAt }
func (p *Plan) Version() int                { return p.version }
func (p *Plan) IsBuiltin() bool             { return p.builtin }
func (p *Plan) OrgNumber() int              { return p.orgNumber }
func (p *Plan) GraphID() string             { return p.graphID }
func (p *Plan) ActiveGenerationID() PlanGenerationID {
	return p.activeGenerationID
}
func (p *Plan) AttentionStatus() PlanAttentionStatus   { return p.attentionStatus }
func (p *Plan) AttentionSince() time.Time              { return p.attentionSince }
func (p *Plan) LastAttentionEventID() PlanBlockEventID { return p.lastAttentionEventID }
func (p *Plan) RecoveryPolicy() PlanRecoveryPolicy     { return p.recoveryPolicy }
func (p *Plan) ArchivedAt() *time.Time                 { return normalizeTargetDate(p.archivedAt) }
func (p *Plan) ArchivedBy() IdentityRef                { return p.archivedBy }
func (p *Plan) IsArchived() bool                       { return p.archivedAt != nil }

// SetGraphID wires this plan to an orchestration engine graph (v2.2.8).
func (p *Plan) SetGraphID(id string, at time.Time) {
	p.graphID = id
	p.touch(at)
}

// SetActiveGenerationID switches the Plan to an immutable generation snapshot.
func (p *Plan) SetActiveGenerationID(id PlanGenerationID, at time.Time) {
	p.activeGenerationID = id
	p.touch(at)
}

func (p *Plan) SetOwner(owner, backup IdentityRef, at time.Time) error {
	if owner == "" {
		return ErrPlanOwnerRequired
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	if backup != "" {
		if err := backup.Validate(); err != nil {
			return err
		}
	}
	p.ownerRef = owner
	p.backupOwnerRef = backup
	p.touch(at)
	return nil
}

func (p *Plan) RequireAttention(eventID PlanBlockEventID, at time.Time) {
	if p.attentionStatus != PlanAttentionEscalated {
		p.attentionStatus = PlanAttentionRequired
	}
	if p.attentionSince.IsZero() {
		p.attentionSince = at.UTC()
	}
	p.lastAttentionEventID = eventID
	p.touch(at)
}

func (p *Plan) ClearAttention(at time.Time) {
	p.attentionStatus = PlanAttentionNone
	p.attentionSince = time.Time{}
	p.lastAttentionEventID = ""
	p.touch(at)
}

// SetVersion overwrites the plan version + updatedAt. It exists for the live-topology
// commit (edit_plan_topology, 2026-07-05 design §4): a whole ops batch is ONE commit
// that advances the plan to exactly base_version+1, regardless of how many internal
// touch()es happen inside the same tx (e.g. a running-plan graph rebuild's SetGraphID).
// The optimistic-concurrency CAS is the caller's (compare the loaded version to
// base_version) under SQLite's single-writer serialization; this stamps the agreed
// next version deterministically so the commit is a single, well-defined increment.
func (p *Plan) SetVersion(v int, at time.Time) {
	p.version = v
	if at.IsZero() {
		at = time.Now()
	}
	p.updatedAt = at.UTC()
}

// Rename updates the display name.
func (p *Plan) Rename(name string, at time.Time) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmptyPlanName
	}
	p.name = name
	p.touch(at)
	return nil
}

// SetDescription updates the description/goal.
func (p *Plan) SetDescription(desc string, at time.Time) {
	p.description = desc
	p.touch(at)
}

// SetTargetDate sets or clears (nil) the optional target date.
func (p *Plan) SetTargetDate(d *time.Time, at time.Time) {
	p.targetDate = normalizeTargetDate(d)
	p.touch(at)
}

// SetConversationID binds the auto-created Plan conversation (#284 wires this;
// "" until then).
func (p *Plan) SetConversationID(id string, at time.Time) {
	p.conversationID = id
	p.touch(at)
}

// Start moves pending→running. Start
// VALIDATION (§9.6: acyclic, ≥1 task, resolvable assignees) is enforced by the
// AppService in #285, not here.
func (p *Plan) Start(at time.Time) error { return p.transition(PlanRunning, at) }

// Pause closes new dispatch without making executed topology editable.
func (p *Plan) Pause(at time.Time) error {
	if p.builtin {
		return ErrBuiltinPlanImmutable
	}
	return p.transition(PlanPaused, at)
}

// Resume reopens dispatch from the same immutable history/frontier.
func (p *Plan) Resume(at time.Time) error {
	if p.builtin {
		return ErrBuiltinPlanImmutable
	}
	return p.transition(PlanRunning, at)
}

// Reopen moves a completed Plan back to paused so a follow-up immutable
// generation can be appended without the auto-advance sweep immediately closing it
// again. Completed task history remains immutable.
func (p *Plan) Reopen(at time.Time) error {
	if p.builtin {
		return ErrBuiltinPlanImmutable
	}
	if p.IsArchived() {
		return ErrPlanArchived
	}
	if p.status != PlanDone {
		return ErrIllegalPlanTransition
	}
	p.status = PlanPaused
	p.touch(at)
	return nil
}

// Stop is the compatibility name for Pause; it never returns a Plan to pending.
func (p *Plan) Stop(at time.Time) error {
	return p.Pause(at)
}

// MarkDone moves running→done (§9.1: every node terminal/done). ADR-0047: the
// built-in pool is a resident pool — it never "completes".
func (p *Plan) MarkDone(at time.Time) error {
	if p.builtin {
		return ErrBuiltinPlanImmutable
	}
	return p.transition(PlanDone, at)
}

// Discard explicitly abandons a pending/running/paused Plan. It is permanent.
func (p *Plan) Discard(at time.Time) error {
	if p.builtin {
		return ErrBuiltinPlanImmutable
	}
	return p.transition(PlanDiscarded, at)
}

// Archive is an orthogonal terminal-only marker; it never changes lifecycle.
func (p *Plan) Archive(at time.Time, actors ...IdentityRef) error {
	if p.IsArchived() {
		return ErrPlanArchived
	}
	if p.status != PlanDone && p.status != PlanDiscarded {
		return ErrPlanNotTerminal
	}
	actor := IdentityRef("system")
	if len(actors) > 0 {
		actor = actors[0]
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	u := at.UTC()
	p.archivedAt = &u
	p.archivedBy = actor
	p.touch(at)
	return nil
}

// ArchiveWithProject archives a Plan as part of its PROJECT being archived
// (ADR-0047): unlike Archive it accepts ANY non-archived status — including a
// running (or built-in, always-running) plan — since the project archive is the
// one legitimate path that retires the resident built-in pool. Idempotent:
// re-archiving returns ErrPlanArchived. Used ONLY by the project-archive cascade.
func (p *Plan) ArchiveWithProject(at time.Time) error {
	if p.IsArchived() {
		return ErrPlanArchived
	}
	if p.status != PlanDone && p.status != PlanDiscarded {
		p.status = PlanDiscarded
	}
	u := at.UTC()
	p.archivedAt = &u
	p.archivedBy = IdentityRef("system")
	p.touch(at)
	return nil
}

// transition applies a status move guarded by the state machine.
func (p *Plan) transition(to PlanStatus, at time.Time) error {
	if !to.IsValid() {
		return ErrInvalidPlanStatus
	}
	if !p.status.CanTransitionTo(to) {
		return ErrIllegalPlanTransition
	}
	p.status = to
	p.touch(at)
	return nil
}

func (p *Plan) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	p.updatedAt = at.UTC()
	p.version++
}

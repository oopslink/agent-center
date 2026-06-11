package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// PlanStatus is the Plan lifecycle enum (v2.9 plan orchestration, design §2/§3).
//
//	draft → running   (Start: orchestrator becomes active, §3.4)
//	running → draft   (Stop: orchestration halted to edit the DAG, §9.4)
//	running → done    (MarkDone: every node terminal, §9.1)
//	done              (terminal)
//
// v1 has NO approval gates and NO failed/paused status (design §2: "v1 has no
// gates — fully autonomous"). A failed node keeps the Plan running (§9.1); the
// Plan never auto-enters a terminal failed state.
type PlanStatus string

const (
	PlanDraft   PlanStatus = "draft"
	PlanRunning PlanStatus = "running"
	PlanDone    PlanStatus = "done"
	// PlanArchived is the v2.9 P3 terminal, IRREVERSIBLE archive state. A
	// non-running Plan (draft or done) can be archived; archiving cascade-archives
	// the Plan's tasks (orthogonal Task.archived, status preserved). A running Plan
	// can NOT be archived — it must be stopped/finished first (ArchivePlan rejects
	// running with ErrPlanRunning). Nothing transitions OUT of archived.
	PlanArchived PlanStatus = "archived"
)

// IsValid reports enum membership.
func (s PlanStatus) IsValid() bool {
	switch s {
	case PlanDraft, PlanRunning, PlanDone, PlanArchived:
		return true
	}
	return false
}

// planTransitions is the allowed-transition adjacency.
var planTransitions = map[PlanStatus][]PlanStatus{
	PlanDraft:    {PlanRunning, PlanArchived}, // archived = v2.9 P3 archive (draft is non-running)
	PlanRunning:  {PlanDraft, PlanDone},       // draft = stop (§9.4); done = §9.1; NOT →archived (stop/finish first)
	PlanDone:     {PlanArchived},              // archived = v2.9 P3 archive (done is non-running)
	PlanArchived: {},                          // terminal, IRREVERSIBLE
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
	conversationID string
	targetDate     *time.Time
	createdAt      time.Time
	updatedAt      time.Time
	version        int
}

// NewPlanInput captures constructor args.
type NewPlanInput struct {
	ID          PlanID
	ProjectID   ProjectID
	Name        string
	Description string
	CreatorRef  IdentityRef
	TargetDate  *time.Time
	CreatedAt   time.Time
}

// NewPlan constructs a fresh draft Plan. A Plan must belong to a Project.
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
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &Plan{
		id:          in.ID,
		projectID:   in.ProjectID,
		name:        in.Name,
		description: in.Description,
		status:      PlanDraft,
		creatorRef:  in.CreatorRef,
		targetDate:  normalizeTargetDate(in.TargetDate),
		createdAt:   at,
		updatedAt:   at,
		version:     1,
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
	ConversationID string
	TargetDate     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
}

// RehydratePlan reconstructs without invariant checks (only enum + version).
func RehydratePlan(in RehydratePlanInput) (*Plan, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidPlanStatus
	}
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	return &Plan{
		id:             in.ID,
		projectID:      in.ProjectID,
		name:           in.Name,
		description:    in.Description,
		status:         in.Status,
		creatorRef:     in.CreatorRef,
		conversationID: in.ConversationID,
		targetDate:     normalizeTargetDate(in.TargetDate),
		createdAt:      in.CreatedAt.UTC(),
		updatedAt:      in.UpdatedAt.UTC(),
		version:        in.Version,
	}, nil
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
func (p *Plan) ID() PlanID              { return p.id }
func (p *Plan) ProjectID() ProjectID    { return p.projectID }
func (p *Plan) Name() string            { return p.name }
func (p *Plan) Description() string     { return p.description }
func (p *Plan) Status() PlanStatus      { return p.status }
func (p *Plan) CreatorRef() IdentityRef { return p.creatorRef }
func (p *Plan) ConversationID() string  { return p.conversationID }
func (p *Plan) TargetDate() *time.Time  { return p.targetDate }
func (p *Plan) CreatedAt() time.Time    { return p.createdAt }
func (p *Plan) UpdatedAt() time.Time    { return p.updatedAt }
func (p *Plan) Version() int            { return p.version }

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

// Start moves draft→running (the orchestrator becomes active, §3.4). Start
// VALIDATION (§9.6: acyclic, ≥1 task, resolvable assignees) is enforced by the
// AppService in #285, not here.
func (p *Plan) Start(at time.Time) error { return p.transition(PlanRunning, at) }

// Stop moves running→draft (§9.4: halt orchestration to edit the DAG).
func (p *Plan) Stop(at time.Time) error { return p.transition(PlanDraft, at) }

// MarkDone moves running→done (§9.1: every node terminal/done).
func (p *Plan) MarkDone(at time.Time) error { return p.transition(PlanDone, at) }

// Archive moves a NON-running Plan (draft or done) → archived (v2.9 P3): a
// terminal, IRREVERSIBLE state. A running Plan is rejected with ErrPlanRunning
// (it must be stopped/finished first); an already-archived Plan is rejected with
// ErrPlanArchived (mirrors Conversation.Archive idempotency). The service
// cascade-archives the Plan's tasks (orthogonal Task.archived) around this call.
func (p *Plan) Archive(at time.Time) error {
	switch p.status {
	case PlanArchived:
		return ErrPlanArchived
	case PlanRunning:
		return ErrPlanRunning
	}
	return p.transition(PlanArchived, at)
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

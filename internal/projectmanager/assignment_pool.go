package projectmanager

import (
	"errors"
	"strings"
	"time"
)

const (
	AssignmentPoolBackground        = "background"
	DefaultAssignmentPoolHoldingCap = 3
)

var (
	ErrAssignmentPoolNotFound     = errors.New("projectmanager: assignment pool not found")
	ErrAssignmentPoolExists       = errors.New("projectmanager: assignment pool already exists")
	ErrAssignmentPoolTaskNotFound = errors.New("projectmanager: task is not an assignment pool member")
)

// AssignmentPool is the per-Project low-priority pull queue. It deliberately has
// no Plan lifecycle, graph, Stage, conversation, or dispatch records.
type AssignmentPool struct {
	id                AssignmentPoolID
	projectID         ProjectID
	schedulingClass   string
	autoAssignEnabled bool
	holdingCap        int
	createdAt         time.Time
	updatedAt         time.Time
	version           int
}

type NewAssignmentPoolInput struct {
	ID                AssignmentPoolID
	ProjectID         ProjectID
	SchedulingClass   string
	AutoAssignEnabled bool
	HoldingCap        int
	CreatedAt         time.Time
}

func NewAssignmentPool(in NewAssignmentPoolInput) (*AssignmentPool, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: assignment pool id required")
	}
	if strings.TrimSpace(string(in.ProjectID)) == "" {
		return nil, ErrEmptyProjectScope
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("projectmanager: created_at required")
	}
	class := strings.TrimSpace(in.SchedulingClass)
	if class == "" {
		class = AssignmentPoolBackground
	}
	cap := in.HoldingCap
	if cap <= 0 {
		cap = DefaultAssignmentPoolHoldingCap
	}
	at := in.CreatedAt.UTC()
	return &AssignmentPool{id: in.ID, projectID: in.ProjectID, schedulingClass: class,
		autoAssignEnabled: in.AutoAssignEnabled, holdingCap: cap, createdAt: at, updatedAt: at, version: 1}, nil
}

type RehydrateAssignmentPoolInput struct {
	ID                AssignmentPoolID
	ProjectID         ProjectID
	SchedulingClass   string
	AutoAssignEnabled bool
	HoldingCap        int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Version           int
}

func RehydrateAssignmentPool(in RehydrateAssignmentPoolInput) (*AssignmentPool, error) {
	if in.Version < 1 {
		return nil, errors.New("projectmanager: version must be >= 1")
	}
	return &AssignmentPool{id: in.ID, projectID: in.ProjectID, schedulingClass: in.SchedulingClass,
		autoAssignEnabled: in.AutoAssignEnabled, holdingCap: in.HoldingCap,
		createdAt: in.CreatedAt.UTC(), updatedAt: in.UpdatedAt.UTC(), version: in.Version}, nil
}

func (p *AssignmentPool) ID() AssignmentPoolID    { return p.id }
func (p *AssignmentPool) ProjectID() ProjectID    { return p.projectID }
func (p *AssignmentPool) SchedulingClass() string { return p.schedulingClass }
func (p *AssignmentPool) AutoAssignEnabled() bool { return p.autoAssignEnabled }
func (p *AssignmentPool) HoldingCap() int         { return p.holdingCap }
func (p *AssignmentPool) CreatedAt() time.Time    { return p.createdAt }
func (p *AssignmentPool) UpdatedAt() time.Time    { return p.updatedAt }
func (p *AssignmentPool) Version() int            { return p.version }

// AssignmentPoolTask is membership/claim metadata. Task.status remains the work
// lifecycle source of truth; a claim sets Task.assignee while leaving it open.
type AssignmentPoolTask struct {
	PoolID         AssignmentPoolID
	TaskID         TaskID
	Priority       int
	AddedBy        IdentityRef
	AddedAt        time.Time
	ClaimedBy      IdentityRef
	ClaimedAt      time.Time
	ClaimExpiresAt time.Time
	Version        int
}

func NewAssignmentPoolTask(poolID AssignmentPoolID, taskID TaskID, priority int, actor IdentityRef, at time.Time) (AssignmentPoolTask, error) {
	if poolID == "" || taskID == "" || at.IsZero() {
		return AssignmentPoolTask{}, errors.New("projectmanager: pool_id, task_id and added_at required")
	}
	if err := actor.Validate(); err != nil {
		return AssignmentPoolTask{}, err
	}
	return AssignmentPoolTask{PoolID: poolID, TaskID: taskID, Priority: priority,
		AddedBy: actor, AddedAt: at.UTC(), Version: 1}, nil
}

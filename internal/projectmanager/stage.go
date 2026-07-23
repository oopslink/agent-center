package projectmanager

import (
	"errors"
	"strings"
	"time"
)

// Stage is a LIGHTWEIGHT first-class Plan aggregate (2026-07-03 plan-stage-model
// design §4.1). A Plan may (optionally, §2 决策1) be organized into Stages, each a
// sub-DAG of the plan's nodes bounded by a barrier + an optional acceptance gate.
//
// Stage is "轻量一等" (§2 决策6): it lands ONE addressable/queryable `pm_stages` row,
// but it drives NO execution and owns NO independent state machine. Its status is a
// PROJECTION of its member nodes (§4.1, see ProjectStageStatus) and its execution is
// fully delegated to the orchestration graph engine (§4.2 "不另起引擎"). There is no
// second, independently-advanced status here.
//
// The outer stage DAG (§4.2) is expressed by dependsOnStages (a downstream stage
// depends on its upstreams' gates); the intra-stage sub-DAG is expressed by the member
// nodes' own graph edges. gateNodeID names the stage's exit gate — a graph CONDITION
// node whose success releases the downstream stages and whose bounded reject re-runs
// this stage's sub-DAG (§5). "" ⇒ a pure barrier (only waits for全完成, no acceptance).
type Stage struct {
	id              StageID
	planID          PlanID
	name            string
	dependsOnStages []StageID
	// gateNodeID is the graph CONDITION node that gates this stage's exit (§4.2). ""
	// when the stage has no acceptance gate (a pure barrier). Stamped by buildPlanGraph
	// when the plan starts (the gate node lives on the orchestration graph, not here).
	gateNodeID string
	// gateTaskID is the executable producer for a human stage gate. It is a
	// supervisor-inline Decision task selected into the same plan and bound
	// one-to-one to the gate node. An empty value is legacy data that must be
	// migrated or rejected before a plan can start.
	gateTaskID TaskID
	gateSpec   GateSpec
	// maxRounds bounds the stage-local retry: how many times a gate reject may re-run
	// this stage's sub-DAG before the round exhausts and escalates to a human (§5). 0 ⇒
	// unlimited is intentionally NOT used — a stage is a CLOSED barrier (§5 卡死升级),
	// so an unset max_rounds falls back to DefaultStageMaxRounds at build time.
	maxRounds int
	createdAt time.Time
	updatedAt time.Time
	version   int
}

type GateEvaluatorKind string

const (
	GateEvaluatorAutomatic GateEvaluatorKind = "automatic"
	GateEvaluatorHuman     GateEvaluatorKind = "human"
)

type GateSpec struct {
	EvaluatorKind      GateEvaluatorKind `json:"evaluator_kind"`
	AssigneeRef        IdentityRef       `json:"assignee_ref,omitempty"`
	RoleRef            string            `json:"role_ref,omitempty"`
	AcceptanceContract string            `json:"acceptance_contract,omitempty"`
	PassRoute          string            `json:"pass_route"`
	RejectRoute        string            `json:"reject_route"`
	ExhaustedRoute     string            `json:"exhausted_route"`
}

func DefaultHumanGateSpec(assignee IdentityRef) GateSpec {
	return GateSpec{
		EvaluatorKind:      GateEvaluatorHuman,
		AssigneeRef:        assignee,
		AcceptanceContract: "Review the stage deliverables and record evidence against the acceptance criteria.",
		PassRoute:          "downstream",
		RejectRoute:        "reopen_stage",
		ExhaustedRoute:     "escalate",
	}
}

func (g GateSpec) Validate() error {
	if g.EvaluatorKind != GateEvaluatorHuman {
		return ErrUnsupportedGateEvaluator
	}
	if g.AssigneeRef == "" && strings.TrimSpace(g.RoleRef) == "" {
		return ErrMissingGateAssignee
	}
	if strings.TrimSpace(g.AcceptanceContract) == "" {
		return ErrMissingGateContract
	}
	if g.PassRoute != "downstream" || g.RejectRoute != "reopen_stage" || g.ExhaustedRoute != "escalate" {
		return ErrInvalidGateRoute
	}
	return nil
}

// DefaultStageMaxRounds is the fallback stage-local retry bound when a Stage is
// created with maxRounds==0 (§5: a closed barrier must not retry unbounded — an
// exhausted gate escalates to a human, so a finite default is required).
const DefaultStageMaxRounds = 3

// NewStageInput captures constructor args.
type NewStageInput struct {
	ID              StageID
	PlanID          PlanID
	Name            string
	DependsOnStages []StageID
	MaxRounds       int // 0 ⇒ DefaultStageMaxRounds
	GateTaskID      TaskID
	GateSpec        GateSpec
	CreatedAt       time.Time
}

// NewStage constructs a fresh Stage. A Stage must belong to a Plan and carry a name
// (its addressable label). depends_on_stages is normalized (trimmed, deduped, self
// removed); acyclicity across the plan's stage set is validated by the service (it
// needs the sibling stages). gateNodeID starts "" — it is stamped at plan start.
func NewStage(in NewStageInput) (*Stage, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("projectmanager: stage id required")
	}
	if strings.TrimSpace(string(in.PlanID)) == "" {
		return nil, errors.New("projectmanager: stage plan_id required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, ErrEmptyStageName
	}
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	maxRounds := in.MaxRounds
	if maxRounds <= 0 {
		maxRounds = DefaultStageMaxRounds
	}
	deps, err := normalizeStageDeps(in.ID, in.DependsOnStages)
	if err != nil {
		return nil, err
	}
	return &Stage{
		id:              in.ID,
		planID:          in.PlanID,
		name:            in.Name,
		dependsOnStages: deps,
		maxRounds:       maxRounds,
		gateTaskID:      in.GateTaskID,
		gateSpec:        in.GateSpec,
		createdAt:       at.UTC(),
		updatedAt:       at.UTC(),
		version:         1,
	}, nil
}

// RehydrateStageInput is for persistence round-trip.
type RehydrateStageInput struct {
	ID              StageID
	PlanID          PlanID
	Name            string
	DependsOnStages []StageID
	GateNodeID      string
	MaxRounds       int
	GateTaskID      TaskID
	GateSpec        GateSpec
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

// RehydrateStage reconstructs a Stage from stored fields without invariant checks
// (only version). depends_on is copied defensively; a stored row is trusted as-is.
func RehydrateStage(in RehydrateStageInput) (*Stage, error) {
	if in.Version < 1 {
		return nil, errors.New("projectmanager: stage version must be >= 1")
	}
	deps := make([]StageID, len(in.DependsOnStages))
	copy(deps, in.DependsOnStages)
	return &Stage{
		id:              in.ID,
		planID:          in.PlanID,
		name:            in.Name,
		dependsOnStages: deps,
		gateNodeID:      in.GateNodeID,
		maxRounds:       in.MaxRounds,
		gateTaskID:      in.GateTaskID,
		gateSpec:        in.GateSpec,
		createdAt:       in.CreatedAt.UTC(),
		updatedAt:       in.UpdatedAt.UTC(),
		version:         in.Version,
	}, nil
}

// normalizeStageDeps trims, dedups, and drops a self-reference from a depends_on set,
// preserving first-seen order (deterministic落图). A self-dependency is a hard error
// (a stage cannot barrier on its own gate).
func normalizeStageDeps(self StageID, deps []StageID) ([]StageID, error) {
	if len(deps) == 0 {
		return nil, nil
	}
	seen := make(map[StageID]struct{}, len(deps))
	out := make([]StageID, 0, len(deps))
	for _, d := range deps {
		d = StageID(strings.TrimSpace(string(d)))
		if d == "" {
			continue
		}
		if d == self {
			return nil, ErrStageSelfDependency
		}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// Getters.
func (s *Stage) ID() StageID          { return s.id }
func (s *Stage) PlanID() PlanID       { return s.planID }
func (s *Stage) Name() string         { return s.name }
func (s *Stage) GateNodeID() string   { return s.gateNodeID }
func (s *Stage) GateTaskID() TaskID   { return s.gateTaskID }
func (s *Stage) GateSpec() GateSpec   { return s.gateSpec }
func (s *Stage) MaxRounds() int       { return s.maxRounds }
func (s *Stage) CreatedAt() time.Time { return s.createdAt }
func (s *Stage) UpdatedAt() time.Time { return s.updatedAt }
func (s *Stage) Version() int         { return s.version }

// DependsOnStages returns a defensive copy of the outer stage DAG's upstream edges.
func (s *Stage) DependsOnStages() []StageID {
	out := make([]StageID, len(s.dependsOnStages))
	copy(out, s.dependsOnStages)
	return out
}

// SetGateNodeID stamps the graph CONDITION node that gates this stage's exit (§4.2).
// Called by buildPlanGraph once the gate node is created at plan start.
func (s *Stage) SetGateNodeID(nodeID string, at time.Time) {
	s.gateNodeID = nodeID
	s.touch(at)
}

func (s *Stage) SetGateTask(taskID TaskID, spec GateSpec, at time.Time) {
	s.gateTaskID = taskID
	s.gateSpec = spec
	s.touch(at)
}

// Rename updates the stage's display name.
func (s *Stage) Rename(name string, at time.Time) error {
	if strings.TrimSpace(name) == "" {
		return ErrEmptyStageName
	}
	s.name = name
	s.touch(at)
	return nil
}

func (s *Stage) touch(at time.Time) {
	if at.IsZero() {
		at = time.Now()
	}
	s.updatedAt = at.UTC()
	s.version++
}

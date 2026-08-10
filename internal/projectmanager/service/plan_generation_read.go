package service

import (
	"context"
	"sort"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanGenerationRead is the generation/evolution read model for a Plan. It is
// projected from existing immutable facts: Stage.generation, GateVerdict,
// Continuation, Task.stage_id, and the current derived PlanView.
type PlanGenerationRead struct {
	ActiveGeneration int
	Generations      []PlanGeneration
	Nodes            []PlanGenerationNode
}

type PlanGeneration struct {
	Generation     int
	Revision       int
	Active         bool
	Status         string
	StageIDs       []pm.StageID
	TaskIDs        []pm.TaskID
	Progress       pm.PlanProgress
	Reason         string
	Evidence       string
	VerdictID      pm.GateVerdictID
	ContinuationID pm.ContinuationID
	IdempotencyKey string
	CreatedAt      time.Time
	Diff           PlanEvolutionDiff
}

type PlanGenerationNode struct {
	TaskID          pm.TaskID
	StageID         pm.StageID
	Generation      int
	Revision        int
	OriginVerdictID pm.GateVerdictID
	ContinuationID  pm.ContinuationID
	Effective       bool
}

type PlanEvolutionDiff struct {
	FromGeneration int
	ToGeneration   int
	AddedNodes     []pm.TaskID
	AddedStages    []pm.StageID
	AddedEdges     []pm.Dependency
	RemovedNodes   []pm.TaskID
	RemovedEdges   []pm.Dependency
}

// GetPlanGenerations returns the stable read model used by the Web Console to
// show active/history generations, per-generation progress, node ownership, and
// each generation's additive topology diff.
func (s *Service) GetPlanGenerations(ctx context.Context, planID pm.PlanID) (*PlanGenerationRead, error) {
	detail, err := s.GetPlanDetail(ctx, planID)
	if err != nil {
		return nil, err
	}
	if detail.Generations == nil {
		if err := s.enrichGenerationView(ctx, detail); err != nil {
			return nil, err
		}
	}
	return detail.Generations, nil
}

func (s *Service) enrichGenerationView(ctx context.Context, detail *PlanDetail) error {
	if detail == nil || detail.Plan == nil {
		return nil
	}
	var stages []*pm.Stage
	if s.stages != nil {
		var err error
		stages, err = s.stages.ListByPlan(ctx, detail.Plan.ID())
		if err != nil {
			return err
		}
	}
	edges, err := s.plans.ListDependencies(ctx, detail.Plan.ID())
	if err != nil {
		return err
	}
	detail.Generations = buildPlanGenerationRead(detail, stages, edges)
	return nil
}

func buildPlanGenerationRead(detail *PlanDetail, stages []*pm.Stage, edges []pm.Dependency) *PlanGenerationRead {
	stageByID := make(map[pm.StageID]*pm.Stage, len(stages))
	verdictByID := make(map[pm.GateVerdictID]pm.GateVerdict, len(detail.GateVerdicts))
	for _, verdict := range detail.GateVerdicts {
		verdictByID[verdict.ID] = verdict
	}
	continuationByID := make(map[pm.ContinuationID]*pm.PlanContinuation, len(detail.Continuations))
	for _, continuation := range detail.Continuations {
		if continuation != nil {
			continuationByID[continuation.ID] = continuation
		}
	}

	activeGeneration := 0
	for _, st := range stages {
		if st == nil {
			continue
		}
		stageByID[st.ID()] = st
		if gen := normalizedGeneration(st.Generation()); gen > activeGeneration {
			activeGeneration = gen
		}
	}

	nodeByTask := make(map[pm.TaskID]pm.PlanNodeView, len(detail.View.Nodes))
	for _, node := range detail.View.Nodes {
		nodeByTask[node.TaskID] = node
	}

	groups := map[int]*PlanGeneration{}
	ensure := func(gen int) *PlanGeneration {
		gen = normalizedGeneration(gen)
		if groups[gen] == nil {
			groups[gen] = &PlanGeneration{
				Generation: gen,
				Revision:   gen + 1,
				Status:     "historical",
				Diff: PlanEvolutionDiff{
					FromGeneration: maxInt(0, gen-1),
					ToGeneration:   gen,
				},
			}
		}
		return groups[gen]
	}
	ensure(0)

	for _, st := range stages {
		if st == nil {
			continue
		}
		gen := normalizedGeneration(st.Generation())
		group := ensure(gen)
		group.StageIDs = append(group.StageIDs, st.ID())
		if st.CreatedAt().After(group.CreatedAt) {
			group.CreatedAt = st.CreatedAt()
		}
		if group.VerdictID == "" && st.OriginVerdictID() != "" {
			group.VerdictID = st.OriginVerdictID()
			if verdict, ok := verdictByID[st.OriginVerdictID()]; ok {
				group.Evidence = verdict.Evidence
				group.IdempotencyKey = verdict.IdempotencyKey
				if group.CreatedAt.IsZero() {
					group.CreatedAt = verdict.CreatedAt
				}
			}
		}
		if group.ContinuationID == "" && st.ContinuationID() != "" {
			group.ContinuationID = st.ContinuationID()
		}
	}

	var nodes []PlanGenerationNode
	genByTask := make(map[pm.TaskID]int, len(detail.Tasks))
	for _, task := range detail.Tasks {
		if task == nil {
			continue
		}
		stageID := task.StageID()
		gen := 0
		var origin pm.GateVerdictID
		var continuation pm.ContinuationID
		if st := stageByID[stageID]; st != nil {
			gen = normalizedGeneration(st.Generation())
			origin = st.OriginVerdictID()
			continuation = st.ContinuationID()
		} else {
			origin = task.OriginVerdictID()
		}
		group := ensure(gen)
		group.TaskIDs = append(group.TaskIDs, task.ID())
		genByTask[task.ID()] = gen
		view := nodeByTask[task.ID()]
		if view.TaskID == "" {
			view.Effective = true
		}
		if view.Effective && view.NodeStatus == pm.NodeDone {
			group.Progress.Done++
		}
		if view.Effective {
			group.Progress.Total++
		}
		nodes = append(nodes, PlanGenerationNode{
			TaskID: task.ID(), StageID: stageID, Generation: gen, Revision: gen + 1,
			OriginVerdictID: origin, ContinuationID: continuation, Effective: view.Effective,
		})
	}

	for _, edge := range edges {
		fromGen := genByTask[edge.FromTaskID]
		toGen := genByTask[edge.ToTaskID]
		gen := fromGen
		if toGen > gen {
			gen = toGen
		}
		group := ensure(gen)
		group.Diff.AddedEdges = append(group.Diff.AddedEdges, edge)
	}

	out := make([]PlanGeneration, 0, len(groups))
	for gen, group := range groups {
		sortStageIDs(group.StageIDs)
		sortTaskIDsLocal(group.TaskIDs)
		group.Diff.AddedStages = append([]pm.StageID(nil), group.StageIDs...)
		group.Diff.AddedNodes = append([]pm.TaskID(nil), group.TaskIDs...)
		if gen == activeGeneration {
			group.Active = true
			group.Status = "active"
		}
		if gen == 0 {
			group.Reason = "initial_plan"
			if group.CreatedAt.IsZero() && detail.Plan != nil {
				group.CreatedAt = detail.Plan.CreatedAt()
			}
		} else {
			group.Reason = "gate_rejection"
			if group.ContinuationID != "" {
				if continuation := continuationByID[group.ContinuationID]; continuation != nil && group.CreatedAt.IsZero() {
					group.CreatedAt = continuation.CreatedAt
				}
			}
		}
		out = append(out, *group)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Generation < out[j].Generation })
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Generation != nodes[j].Generation {
			return nodes[i].Generation < nodes[j].Generation
		}
		return nodes[i].TaskID < nodes[j].TaskID
	})
	return &PlanGenerationRead{ActiveGeneration: activeGeneration, Generations: out, Nodes: nodes}
}

func normalizedGeneration(g int) int {
	if g < 0 {
		return 0
	}
	return g
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sortStageIDs(ids []pm.StageID) {
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

func sortTaskIDsLocal(ids []pm.TaskID) {
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] < ids[j] })
}

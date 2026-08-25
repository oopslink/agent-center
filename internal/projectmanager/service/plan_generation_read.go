package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// PlanGenerationRead is the product-facing projection of the immutable
// PlanGeneration ledger. Generation identity and lineage always come from the
// persisted ledger; Revision is only a zero-based display position in that
// lineage and is never used as domain identity.
type PlanGenerationRead struct {
	PlanID             pm.PlanID
	ActiveGenerationID pm.PlanGenerationID
	PlanVersion        int
	Generations        []PlanGenerationRevision
	Nodes              []PlanGenerationNodeOwnership
}

type PlanGenerationRevision struct {
	Generation *pm.PlanGeneration
	Revision   int
	Active     bool
	Progress   pm.PlanProgress
}

// PlanGenerationNodeOwnership records the first immutable generation snapshot
// in which a task node appeared. PresentInActive distinguishes historical nodes
// that a later generation superseded from nodes in the active snapshot.
type PlanGenerationNodeOwnership struct {
	TaskID          pm.TaskID
	NodeID          string
	StageID         pm.StageID
	GenerationID    pm.PlanGenerationID
	Revision        int
	PresentInActive bool
}

// GetPlanGenerations follows Plan.active_generation_id through persisted parent
// links to G0. It fails closed on broken, cyclic, cross-Plan, or internally
// inconsistent lineage instead of manufacturing generations from Stage fields.
func (s *Service) GetPlanGenerations(ctx context.Context, planID pm.PlanID) (*PlanGenerationRead, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	read := &PlanGenerationRead{
		PlanID:             p.ID(),
		ActiveGenerationID: p.ActiveGenerationID(),
		PlanVersion:        p.Version(),
		Generations:        []PlanGenerationRevision{},
		Nodes:              []PlanGenerationNodeOwnership{},
	}
	if p.ActiveGenerationID() == "" {
		return read, nil
	}

	lineage, err := s.planGenerationLineage(ctx, p)
	if err != nil {
		return nil, err
	}
	activeTasks := make(map[pm.TaskID]bool)
	activeGeneration := lineage[len(lineage)-1]
	for _, task := range activeGeneration.Snapshot.Tasks {
		activeTasks[task.TaskID] = true
	}
	for _, decision := range activeGeneration.Diff.NodeDecisions {
		if decision.Action == pm.EvolutionSupersede {
			delete(activeTasks, decision.TaskID)
		}
	}
	owned := make(map[pm.TaskID]bool)
	for revision, generation := range lineage {
		read.Generations = append(read.Generations, PlanGenerationRevision{
			Generation: generation,
			Revision:   revision,
			Active:     generation.ID == p.ActiveGenerationID(),
			Progress:   generationSnapshotProgress(generation.Snapshot),
		})
		for _, task := range generation.Snapshot.Tasks {
			if owned[task.TaskID] {
				continue
			}
			owned[task.TaskID] = true
			read.Nodes = append(read.Nodes, PlanGenerationNodeOwnership{
				TaskID:          task.TaskID,
				NodeID:          task.NodeID,
				StageID:         task.StageID,
				GenerationID:    generation.ID,
				Revision:        revision,
				PresentInActive: activeTasks[task.TaskID],
			})
		}
	}
	sort.SliceStable(read.Nodes, func(i, j int) bool {
		if read.Nodes[i].Revision != read.Nodes[j].Revision {
			return read.Nodes[i].Revision < read.Nodes[j].Revision
		}
		return read.Nodes[i].TaskID < read.Nodes[j].TaskID
	})
	return read, nil
}

func (s *Service) planGenerationLineage(ctx context.Context, p *pm.Plan) ([]*pm.PlanGeneration, error) {
	return loadPlanGenerationLineage(ctx, p, s.plans)
}

type planGenerationFinder interface {
	FindGenerationByID(context.Context, pm.PlanGenerationID) (*pm.PlanGeneration, error)
}

func loadPlanGenerationLineage(ctx context.Context, p *pm.Plan, finder planGenerationFinder) ([]*pm.PlanGeneration, error) {
	seen := make(map[pm.PlanGenerationID]bool)
	reversed := make([]*pm.PlanGeneration, 0, 4)
	for id := p.ActiveGenerationID(); id != ""; {
		if seen[id] {
			return nil, fmt.Errorf("%w: cycle at generation %s", pm.ErrPlanGenerationConflict, id)
		}
		seen[id] = true
		generation, err := finder.FindGenerationByID(ctx, id)
		if err != nil {
			if errors.Is(err, pm.ErrPlanGenerationNotFound) {
				return nil, fmt.Errorf("%w: missing generation %s", pm.ErrPlanGenerationConflict, id)
			}
			return nil, err
		}
		if generation.PlanID != p.ID() {
			return nil, fmt.Errorf("%w: generation %s belongs to plan %s", pm.ErrPlanGenerationConflict, id, generation.PlanID)
		}
		if generation.Snapshot.PlanID != p.ID() || generation.Snapshot.ActiveGenerationID != generation.ID {
			return nil, fmt.Errorf("%w: generation %s snapshot identity mismatch", pm.ErrPlanGenerationConflict, id)
		}
		reversed = append(reversed, generation)
		id = generation.ParentGenerationID
	}

	lineage := make([]*pm.PlanGeneration, len(reversed))
	for i := range reversed {
		lineage[len(reversed)-1-i] = reversed[i]
	}
	if len(lineage) == 0 || lineage[0].ParentGenerationID != "" {
		return nil, fmt.Errorf("%w: lineage does not terminate at G0", pm.ErrPlanGenerationConflict)
	}
	for i := 1; i < len(lineage); i++ {
		if lineage[i].ParentGenerationID != lineage[i-1].ID {
			return nil, fmt.Errorf("%w: generation %s does not descend from %s", pm.ErrPlanGenerationConflict, lineage[i].ID, lineage[i-1].ID)
		}
	}
	return lineage, nil
}

func generationSnapshotProgress(snapshot pm.PlanGenerationSnapshot) pm.PlanProgress {
	progress := pm.PlanProgress{Total: len(snapshot.Tasks)}
	for _, task := range snapshot.Tasks {
		switch task.Status {
		case pm.TaskCompleted, pm.TaskDiscarded:
			progress.Done++
		}
	}
	return progress
}

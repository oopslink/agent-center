package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
)

// EvolvePlanGenerationCommand commits one immutable generation derived from the
// active generation. It is the server-side Evolution API: optimistic-concurrency
// checked, idempotent, and dispatch-atomic for running plans.
type EvolvePlanGenerationCommand struct {
	PlanID             pm.PlanID
	ParentGenerationID pm.PlanGenerationID
	BaseVersion        int
	IdempotencyKey     string
	Reason             string
	Evidence           string
	Creator            pm.IdentityRef
	Diff               pm.PlanGenerationDiff
}

type EvolvePlanGenerationResult struct {
	Generation *pm.PlanGeneration
	Duplicate  bool
	Dispatched []pm.TaskID
}

func (s *Service) EvolvePlanGeneration(ctx context.Context, cmd EvolvePlanGenerationCommand) (EvolvePlanGenerationResult, error) {
	var result EvolvePlanGenerationResult
	if s.plans == nil {
		return result, ErrPlansUnavailable
	}
	if err := cmd.Creator.Validate(); err != nil {
		return result, err
	}
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	cmd.Reason = strings.TrimSpace(cmd.Reason)
	cmd.Evidence = strings.TrimSpace(cmd.Evidence)
	if cmd.IdempotencyKey == "" {
		return result, errors.New("projectmanager: evolution idempotency_key required")
	}
	if cmd.Reason == "" || cmd.Evidence == "" {
		return result, errors.New("projectmanager: evolution reason and evidence required")
	}
	fingerprint, err := evolutionRequestFingerprint(cmd)
	if err != nil {
		return result, err
	}
	now := s.clock.Now()
	generationUniqueRace := false

	err = s.runInTx(ctx, func(txCtx context.Context) error {
		if existing, found, ferr := s.plans.FindGenerationByIdempotencyKey(txCtx, cmd.PlanID, cmd.IdempotencyKey); ferr != nil {
			return ferr
		} else if found {
			if existing.RequestFingerprint != fingerprint {
				return pm.ErrIdempotencyConflict
			}
			result.Generation = existing
			result.Duplicate = true
			result.Dispatched = append([]pm.TaskID(nil), existing.DispatchedTaskIDs...)
			return nil
		}

		p, err := s.plans.FindByID(txCtx, cmd.PlanID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), cmd.Creator); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		if p.IsBuiltin() {
			return pm.ErrBuiltinPlanNoEdges
		}
		switch p.Status() {
		case pm.PlanPending, pm.PlanRunning, pm.PlanPaused:
		default:
			return pm.ErrPlanNotRunning
		}
		if diffUsesStages(cmd.Diff) && s.stages == nil {
			return ErrStagesUnavailable
		}
		if p.Version() != cmd.BaseVersion {
			return fmt.Errorf("%w: base_version=%d current=%d", pm.ErrPlanVersionConflict, cmd.BaseVersion, p.Version())
		}
		parentGenerationID := cmd.ParentGenerationID
		if p.ActiveGenerationID() == "" && (p.Status() == pm.PlanRunning || p.Status() == pm.PlanPaused) {
			if cmd.ParentGenerationID != "" {
				return fmt.Errorf("%w: parent_generation_id=%s active_generation_id=%s",
					pm.ErrPlanGenerationConflict, cmd.ParentGenerationID, p.ActiveGenerationID())
			}
			legacy, berr := s.backfillLegacyActiveGeneration(txCtx, p, cmd.Creator, now)
			if berr != nil {
				return berr
			}
			parentGenerationID = legacy.ID
		} else if p.ActiveGenerationID() != cmd.ParentGenerationID {
			return fmt.Errorf("%w: parent_generation_id=%s active_generation_id=%s",
				pm.ErrPlanGenerationConflict, cmd.ParentGenerationID, p.ActiveGenerationID())
		}

		tasks, err := s.tasks.ListByPlan(txCtx, p.ID())
		if err != nil {
			return err
		}
		edges, err := s.plans.ListDependencies(txCtx, p.ID())
		if err != nil {
			return err
		}
		records, err := s.plans.ListDispatchRecords(txCtx, p.ID())
		if err != nil {
			return err
		}
		taskByID := make(map[pm.TaskID]*pm.Task, len(tasks))
		for _, t := range tasks {
			taskByID[t.ID()] = t
		}
		dispatchedSet := make(map[pm.TaskID]bool, len(records))
		for _, r := range records {
			dispatchedSet[r.TaskID] = true
		}

		superseded, err := s.validateEvolutionNodeDecisions(cmd.Diff.NodeDecisions, taskByID, edges, dispatchedSet)
		if err != nil {
			return err
		}
		if err := s.applySupersededNodes(txCtx, p, superseded, taskByID, edges, now); err != nil {
			return err
		}

		stageRefToID, stageGateTaskIDs, affectedStages, err := s.createEvolutionStages(txCtx, p, cmd.Diff.Stages, cmd.Creator, now)
		if err != nil {
			return err
		}
		newTaskIDs, refToTask, err := s.createEvolutionTasks(txCtx, p, cmd, stageRefToID, now)
		if err != nil {
			return err
		}
		newTaskIDs = append(stageGateTaskIDs, newTaskIDs...)
		for _, id := range newTaskIDs {
			task, terr := s.tasks.FindByID(txCtx, id)
			if terr != nil {
				return terr
			}
			if task.StageID() != "" {
				affectedStages[task.StageID()] = true
			}
		}
		stageUpdates, err := s.applyEvolutionStageUpdates(txCtx, p, cmd.Diff.StageUpdates, stageRefToID, taskByID, dispatchedSet, now)
		if err != nil {
			return err
		}
		for id := range stageUpdates {
			affectedStages[id] = true
		}
		membershipStages, err := s.applyEvolutionStageMemberships(txCtx, p, cmd.Diff.StageMemberships, stageRefToID, refToTask, taskByID, dispatchedSet, now)
		if err != nil {
			return err
		}
		for id := range membershipStages {
			affectedStages[id] = true
		}
		newDeps, err := s.resolveEvolutionEdges(txCtx, p, cmd.Diff.Edges, refToTask)
		if err != nil {
			return err
		}
		if err := s.validateEvolutionStageStructure(txCtx, p, newDeps); err != nil {
			return err
		}
		if err := s.addEvolutionEdges(txCtx, newDeps); err != nil {
			return err
		}
		if err := s.applyGenerationGraphDelta(txCtx, p, newTaskIDs, superseded, newDeps, now); err != nil {
			return err
		}
		if err := s.applyEvolutionStageGraphDelta(txCtx, p, affectedStages, now); err != nil {
			return err
		}

		var dispatched []pm.TaskID
		if p.Status() == pm.PlanRunning {
			dispatched, err = s.dispatchReadyNodes(txCtx, p)
			if err != nil {
				return err
			}
		}

		nextVersion := cmd.BaseVersion + 1
		freshTasks, err := s.tasks.ListByPlan(txCtx, p.ID())
		if err != nil {
			return err
		}
		freshEdges, err := s.plans.ListDependencies(txCtx, p.ID())
		if err != nil {
			return err
		}
		freshRecords, err := s.plans.ListDispatchRecords(txCtx, p.ID())
		if err != nil {
			return err
		}
		freshStages, err := s.listStagesForSnapshot(txCtx, p.ID())
		if err != nil {
			return err
		}
		generationID := pm.PlanGenerationID(s.idgen.NewEntityID("generation"))
		generation, err := pm.NewPlanGeneration(pm.PlanGeneration{
			ID:                 generationID,
			PlanID:             p.ID(),
			ParentGenerationID: parentGenerationID,
			Reason:             cmd.Reason,
			Evidence:           cmd.Evidence,
			CreatorRef:         cmd.Creator,
			Diff:               cmd.Diff,
			Snapshot:           planGenerationSnapshot(p.ID(), generationID, nextVersion, freshStages, freshTasks, freshEdges, freshRecords),
			IdempotencyKey:     cmd.IdempotencyKey,
			RequestFingerprint: fingerprint,
			DispatchedTaskIDs:  dispatched,
			CreatedAt:          now,
		})
		if err != nil {
			return err
		}
		if err := s.plans.SaveGeneration(txCtx, generation); err != nil {
			if errors.Is(err, pm.ErrPlanGenerationExists) {
				generationUniqueRace = true
			}
			return err
		}
		ok, err := s.plans.ActivateGeneration(txCtx, p.ID(), generation.ID, cmd.BaseVersion, nextVersion, now)
		if err != nil {
			return err
		}
		if !ok {
			return pm.ErrPlanVersionConflict
		}
		s.auditPlanByID(txCtx, p.ProjectID(), p.ID(), pm.AuditPlanTopologyCommit, cmd.Creator, map[string]any{
			"generation_id":        string(generation.ID),
			"parent_generation_id": string(generation.ParentGenerationID),
			"from_version":         cmd.BaseVersion,
			"to_version":           nextVersion,
			"reason":               cmd.Reason,
		})
		if err := s.emit(txCtx, EvtPlanGenerationEvolved,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "generation_id": string(generation.ID)}),
			map[string]any{
				"plan_id":              string(p.ID()),
				"generation_id":        string(generation.ID),
				"parent_generation_id": string(generation.ParentGenerationID),
				"creator_ref":          string(generation.CreatorRef),
				"created_at":           generation.CreatedAt.Format(time.RFC3339Nano),
			}); err != nil {
			return err
		}
		result.Generation = generation
		result.Dispatched = dispatched
		return nil
	})
	if errors.Is(err, pm.ErrPlanGenerationExists) && generationUniqueRace {
		existing, found, ferr := s.plans.FindGenerationByIdempotencyKey(ctx, cmd.PlanID, cmd.IdempotencyKey)
		if ferr != nil {
			return result, ferr
		}
		if found {
			if existing.RequestFingerprint != fingerprint {
				return result, pm.ErrIdempotencyConflict
			}
			result.Generation = existing
			result.Duplicate = true
			result.Dispatched = append([]pm.TaskID(nil), existing.DispatchedTaskIDs...)
			return result, nil
		}
	}
	return result, err
}

func evolutionRequestFingerprint(cmd EvolvePlanGenerationCommand) (string, error) {
	body := struct {
		PlanID             pm.PlanID             `json:"plan_id"`
		ParentGenerationID pm.PlanGenerationID   `json:"parent_generation_id"`
		BaseVersion        int                   `json:"base_version"`
		Reason             string                `json:"reason"`
		Evidence           string                `json:"evidence"`
		Creator            pm.IdentityRef        `json:"creator"`
		Diff               pm.PlanGenerationDiff `json:"diff"`
	}{cmd.PlanID, cmd.ParentGenerationID, cmd.BaseVersion, strings.TrimSpace(cmd.Reason), strings.TrimSpace(cmd.Evidence), cmd.Creator, cmd.Diff}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func systemGenerationFingerprint(kind string, snapshot pm.PlanGenerationSnapshot) (string, error) {
	body := struct {
		Kind     string                    `json:"kind"`
		Snapshot pm.PlanGenerationSnapshot `json:"snapshot"`
	}{kind, snapshot}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func (s *Service) listStagesForSnapshot(ctx context.Context, planID pm.PlanID) ([]*pm.Stage, error) {
	if s.stages == nil {
		return nil, nil
	}
	return s.stages.ListByPlan(ctx, planID)
}

func (s *Service) saveSystemPlanGeneration(
	ctx context.Context,
	p *pm.Plan,
	id pm.PlanGenerationID,
	parent pm.PlanGenerationID,
	reason string,
	evidence string,
	creator pm.IdentityRef,
	idempotencyKey string,
	createdAt time.Time,
) (*pm.PlanGeneration, error) {
	tasks, err := s.tasks.ListByPlan(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	edges, err := s.plans.ListDependencies(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	records, err := s.plans.ListDispatchRecords(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	stages, err := s.listStagesForSnapshot(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	snapshot := planGenerationSnapshot(p.ID(), id, p.Version(), stages, tasks, edges, records)
	fp, err := systemGenerationFingerprint(idempotencyKey, snapshot)
	if err != nil {
		return nil, err
	}
	g, err := pm.NewPlanGeneration(pm.PlanGeneration{
		ID:                 id,
		PlanID:             p.ID(),
		ParentGenerationID: parent,
		Reason:             reason,
		Evidence:           evidence,
		CreatorRef:         creator,
		Diff:               pm.PlanGenerationDiff{},
		Snapshot:           snapshot,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: fp,
		CreatedAt:          createdAt,
	})
	if err != nil {
		return nil, err
	}
	if err := s.plans.SaveGeneration(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) backfillLegacyActiveGeneration(ctx context.Context, p *pm.Plan, creator pm.IdentityRef, at time.Time) (*pm.PlanGeneration, error) {
	id := pm.PlanGenerationID(s.idgen.NewEntityID("generation"))
	return s.saveSystemPlanGeneration(ctx, p, id, "",
		"legacy generation backfill",
		"running/paused plan existed before active_generation_id; snapshot captured before first Evolution",
		creator,
		"legacy-backfill:"+string(p.ID())+":"+fmt.Sprint(p.Version()),
		at)
}

func (s *Service) validateEvolutionNodeDecisions(
	decisions []pm.PlanGenerationNodeDecision,
	taskByID map[pm.TaskID]*pm.Task,
	edges []pm.Dependency,
	dispatched map[pm.TaskID]bool,
) (map[pm.TaskID]bool, error) {
	superseded := map[pm.TaskID]bool{}
	for _, d := range decisions {
		if !d.Action.IsValid() {
			return nil, fmt.Errorf("%w: invalid evolution action %q", pm.ErrInvalidStatus, d.Action)
		}
		t := taskByID[d.TaskID]
		if t == nil {
			return nil, ErrTaskNotInPlan
		}
		mutable := pm.NodeMutable(t.Status(), dispatched[d.TaskID])
		switch d.Action {
		case pm.EvolutionPreserve:
			continue
		case pm.EvolutionSupersede:
			if !mutable {
				return nil, fmt.Errorf("%w: supersede task %s", pm.ErrPlanNodeInFlight, d.TaskID)
			}
			superseded[d.TaskID] = true
		case pm.EvolutionHoldAtGate:
			for _, e := range edges {
				if e.ToTaskID != d.TaskID {
					continue
				}
				downstream := taskByID[e.FromTaskID]
				if downstream == nil {
					continue
				}
				if !pm.NodeMutable(downstream.Status(), dispatched[downstream.ID()]) {
					return nil, fmt.Errorf("%w: downstream task %s already in-flight", pm.ErrPlanGenerationConflict, downstream.ID())
				}
			}
		}
	}
	return superseded, nil
}

func (s *Service) applySupersededNodes(
	ctx context.Context,
	p *pm.Plan,
	superseded map[pm.TaskID]bool,
	taskByID map[pm.TaskID]*pm.Task,
	edges []pm.Dependency,
	now time.Time,
) error {
	if len(superseded) == 0 {
		return nil
	}
	for _, e := range edges {
		if superseded[e.FromTaskID] || superseded[e.ToTaskID] {
			if err := s.plans.RemoveDependency(ctx, e); err != nil {
				return err
			}
		}
	}
	for id := range superseded {
		t := taskByID[id]
		if t == nil {
			continue
		}
		if err := t.ClearPlan(now); err != nil {
			return err
		}
		if err := s.tasks.Update(ctx, t); err != nil {
			return err
		}
		s.auditPlanByID(ctx, p.ProjectID(), p.ID(), pm.AuditPlanNodeRemoved, p.CreatorRef(), map[string]any{
			"task": string(id), "evolution_action": string(pm.EvolutionSupersede),
		})
	}
	return nil
}

func diffUsesStages(diff pm.PlanGenerationDiff) bool {
	if len(diff.Stages) > 0 || len(diff.StageUpdates) > 0 || len(diff.StageMemberships) > 0 {
		return true
	}
	for _, t := range diff.Tasks {
		if t.StageID != "" || strings.TrimSpace(t.StageRef) != "" {
			return true
		}
	}
	return false
}

func normalizeEvolutionGateSpec(spec pm.GateSpec, actor pm.IdentityRef) pm.GateSpec {
	if spec.EvaluatorKind == "" {
		spec = pm.DefaultHumanGateSpec(actor)
	}
	if spec.RejectRoute == "reopen_stage" {
		spec.RejectRoute = "append_remediation"
	}
	return spec
}

func stageIDSet(ids []pm.StageID) map[pm.StageID]bool {
	out := make(map[pm.StageID]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			out[id] = true
		}
	}
	return out
}

func (s *Service) existingStagesByID(ctx context.Context, planID pm.PlanID) (map[pm.StageID]*pm.Stage, []*pm.Stage, error) {
	stages, err := s.listStagesForSnapshot(ctx, planID)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[pm.StageID]*pm.Stage, len(stages))
	for _, st := range stages {
		byID[st.ID()] = st
	}
	return byID, stages, nil
}

func resolveStageRefs(raw []string, stageRefToID map[string]pm.StageID) []pm.StageID {
	out := make([]pm.StageID, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if id, ok := stageRefToID[r]; ok {
			out = append(out, id)
			continue
		}
		out = append(out, pm.StageID(r))
	}
	return out
}

func (s *Service) resolveEvolutionStageRef(ctx context.Context, planID pm.PlanID, raw string, stageRefToID map[string]pm.StageID) (pm.StageID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if id, ok := stageRefToID[raw]; ok {
		return id, nil
	}
	if s.stages == nil {
		return "", ErrStagesUnavailable
	}
	st, err := s.stages.FindByID(ctx, pm.StageID(raw))
	if err != nil {
		return "", err
	}
	if st.PlanID() != planID {
		return "", pm.ErrStageProjectMismatch
	}
	return st.ID(), nil
}

func (s *Service) createEvolutionStages(
	ctx context.Context,
	p *pm.Plan,
	specs []pm.PlanGenerationStageDraft,
	actor pm.IdentityRef,
	now time.Time,
) (map[string]pm.StageID, []pm.TaskID, map[pm.StageID]bool, error) {
	refToStage := map[string]pm.StageID{}
	affected := map[pm.StageID]bool{}
	var gateTaskIDs []pm.TaskID
	if len(specs) == 0 {
		return refToStage, nil, affected, nil
	}
	if s.stages == nil {
		return nil, nil, nil, ErrStagesUnavailable
	}
	for _, spec := range specs {
		ref := strings.TrimSpace(spec.Ref)
		if ref == "" || strings.TrimSpace(spec.Name) == "" {
			return nil, nil, nil, pm.ErrRemediationProposalInvalid
		}
		if _, exists := refToStage[ref]; exists {
			return nil, nil, nil, pm.ErrRemediationProposalInvalid
		}
		refToStage[ref] = pm.StageID(s.idgen.NewEntityID("stage"))
	}
	_, existing, err := s.existingStagesByID(ctx, p.ID())
	if err != nil {
		return nil, nil, nil, err
	}
	created := make([]*pm.Stage, 0, len(specs))
	for _, spec := range specs {
		gateSpec := normalizeEvolutionGateSpec(spec.GateSpec, actor)
		if err := gateSpec.Validate(); err != nil {
			return nil, nil, nil, err
		}
		stageID := refToStage[strings.TrimSpace(spec.Ref)]
		st, err := pm.NewStage(pm.NewStageInput{
			ID:                 stageID,
			PlanID:             p.ID(),
			Name:               spec.Name,
			DependsOnStages:    resolveStageRefs(spec.DependsOnStages, refToStage),
			MaxRounds:          spec.MaxRounds,
			GateSpec:           gateSpec,
			OriginVerdictID:    spec.OriginVerdictID,
			ContinuationID:     spec.ContinuationID,
			Generation:         spec.Generation,
			AcceptanceContract: spec.AcceptanceContract,
			CreatedAt:          now,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		created = append(created, st)
	}
	all := append(append([]*pm.Stage{}, existing...), created...)
	if err := pm.ValidateStageDAG(all); err != nil {
		return nil, nil, nil, err
	}
	for _, st := range created {
		if err := s.stages.Save(ctx, st); err != nil {
			return nil, nil, nil, err
		}
		gateTaskID, err := s.provisionStageGateTask(ctx, p, st, actor, now)
		if err != nil {
			return nil, nil, nil, err
		}
		gateTaskIDs = append(gateTaskIDs, gateTaskID)
		affected[st.ID()] = true
	}
	return refToStage, gateTaskIDs, affected, nil
}

func (s *Service) applyEvolutionStageUpdates(
	ctx context.Context,
	p *pm.Plan,
	updates []pm.PlanGenerationStageUpdate,
	stageRefToID map[string]pm.StageID,
	initialTasks map[pm.TaskID]*pm.Task,
	dispatched map[pm.TaskID]bool,
	now time.Time,
) (map[pm.StageID]bool, error) {
	affected := map[pm.StageID]bool{}
	if len(updates) == 0 {
		return affected, nil
	}
	if s.stages == nil {
		return nil, ErrStagesUnavailable
	}
	byID, stages, err := s.existingStagesByID(ctx, p.ID())
	if err != nil {
		return nil, err
	}
	for _, upd := range updates {
		st := byID[upd.StageID]
		if st == nil {
			return nil, pm.ErrStageNotFound
		}
		if strings.TrimSpace(upd.Name) != "" {
			if err := st.Rename(upd.Name, now); err != nil {
				return nil, err
			}
		}
		if upd.DependsOnStages != nil {
			nextDeps := resolveStageRefs(*upd.DependsOnStages, stageRefToID)
			if p.GraphID() != "" && removesStageDependency(st.DependsOnStages(), nextDeps) {
				return nil, fmt.Errorf("%w: removing stage dependencies from graphed plan is unsafe", pm.ErrPlanGenerationConflict)
			}
			if err := s.requireStageMembersMutable(st.ID(), initialTasks, dispatched); err != nil {
				return nil, err
			}
			if err := st.SetDependsOnStages(nextDeps, now); err != nil {
				return nil, err
			}
		}
		if upd.MaxRounds != nil {
			if err := s.requireGateTaskMutable(st, initialTasks, dispatched); err != nil {
				return nil, err
			}
			st.SetMaxRounds(*upd.MaxRounds, now)
		}
		if upd.GateSpec != nil {
			if err := s.requireGateTaskMutable(st, initialTasks, dispatched); err != nil {
				return nil, err
			}
			spec := normalizeEvolutionGateSpec(*upd.GateSpec, p.CreatorRef())
			if err := st.SetGateSpec(spec, now); err != nil {
				return nil, err
			}
		}
		if err := s.stages.Update(ctx, st); err != nil {
			return nil, err
		}
		affected[st.ID()] = true
	}
	if err := pm.ValidateStageDAG(stages); err != nil {
		return nil, err
	}
	return affected, nil
}

func removesStageDependency(oldDeps, newDeps []pm.StageID) bool {
	next := stageIDSet(newDeps)
	for _, old := range oldDeps {
		if !next[old] {
			return true
		}
	}
	return false
}

func (s *Service) requireStageMembersMutable(stageID pm.StageID, initialTasks map[pm.TaskID]*pm.Task, dispatched map[pm.TaskID]bool) error {
	for id, task := range initialTasks {
		if task.StageID() != stageID {
			continue
		}
		if !pm.NodeMutable(task.Status(), dispatched[id]) {
			return fmt.Errorf("%w: stage member task %s already in-flight", pm.ErrPlanGenerationConflict, id)
		}
	}
	return nil
}

func (s *Service) requireGateTaskMutable(st *pm.Stage, initialTasks map[pm.TaskID]*pm.Task, dispatched map[pm.TaskID]bool) error {
	if st.GateTaskID() == "" {
		return nil
	}
	task := initialTasks[st.GateTaskID()]
	if task == nil {
		return nil
	}
	if !pm.NodeMutable(task.Status(), dispatched[task.ID()]) {
		return fmt.Errorf("%w: stage gate task %s already in-flight", pm.ErrPlanGenerationConflict, task.ID())
	}
	return nil
}

func (s *Service) applyEvolutionStageMemberships(
	ctx context.Context,
	p *pm.Plan,
	memberships []pm.PlanGenerationStageMembership,
	stageRefToID map[string]pm.StageID,
	refToTask map[string]pm.TaskID,
	initialTasks map[pm.TaskID]*pm.Task,
	dispatched map[pm.TaskID]bool,
	now time.Time,
) (map[pm.StageID]bool, error) {
	affected := map[pm.StageID]bool{}
	if len(memberships) == 0 {
		return affected, nil
	}
	if s.stages == nil {
		return nil, ErrStagesUnavailable
	}
	for _, m := range memberships {
		taskID, err := resolveEvolutionTaskRef(ctx, s.tasks, p.ID(), m.Task, refToTask)
		if err != nil {
			return nil, err
		}
		stageID, err := s.resolveEvolutionStageRef(ctx, p.ID(), m.Stage, stageRefToID)
		if err != nil {
			return nil, err
		}
		task, err := s.tasks.FindByID(ctx, taskID)
		if err != nil {
			return nil, err
		}
		oldStage := task.StageID()
		if oldStage == stageID {
			continue
		}
		if old := initialTasks[taskID]; old != nil && !pm.NodeMutable(old.Status(), dispatched[taskID]) {
			return nil, fmt.Errorf("%w: stage membership task %s already in-flight", pm.ErrPlanNodeInFlight, taskID)
		}
		if p.GraphID() != "" && oldStage != "" && oldStage != stageID {
			return nil, fmt.Errorf("%w: moving an already-staged graphed node is unsafe", pm.ErrPlanGenerationConflict)
		}
		if err := task.SetStage(stageID, now); err != nil {
			return nil, err
		}
		if err := s.tasks.Update(ctx, task); err != nil {
			return nil, err
		}
		if oldStage != "" {
			affected[oldStage] = true
		}
		if stageID != "" {
			affected[stageID] = true
		}
		if err := s.updateTaskGraphStageMetadata(ctx, p, task, stageID); err != nil {
			return nil, err
		}
	}
	return affected, nil
}

func (s *Service) createEvolutionTasks(
	ctx context.Context,
	p *pm.Plan,
	cmd EvolvePlanGenerationCommand,
	stageRefToID map[string]pm.StageID,
	now time.Time,
) ([]pm.TaskID, map[string]pm.TaskID, error) {
	refToTask := map[string]pm.TaskID{}
	var created []pm.TaskID
	planHasStages := false
	if s.stages != nil {
		stages, err := s.stages.ListByPlan(ctx, p.ID())
		if err != nil {
			return nil, nil, err
		}
		planHasStages = len(stages) > 0
	}
	membershipLater := map[string]bool{}
	for _, membership := range cmd.Diff.StageMemberships {
		if task := strings.TrimSpace(membership.Task); task != "" {
			membershipLater[task] = true
		}
	}
	for _, spec := range cmd.Diff.Tasks {
		ref := strings.TrimSpace(spec.Ref)
		if ref == "" || strings.TrimSpace(spec.Title) == "" {
			return nil, nil, pm.ErrRemediationProposalInvalid
		}
		if _, exists := refToTask[ref]; exists {
			return nil, nil, pm.ErrRemediationProposalInvalid
		}
		taskID, err := s.CreateTask(ctx, CreateTaskCommand{
			ProjectID: p.ProjectID(), Title: spec.Title, Description: spec.Description,
			CreatedBy: cmd.Creator, Assignee: spec.AssigneeRef, DispatchMode: spec.DispatchMode,
			DeliveryContract: spec.DeliveryContract, FollowsTaskID: spec.FollowsTaskID,
		})
		if err != nil {
			return nil, nil, err
		}
		task, err := s.tasks.FindByID(ctx, taskID)
		if err != nil {
			return nil, nil, err
		}
		if err := task.SetPlan(p.ID(), now); err != nil {
			return nil, nil, err
		}
		stageID := spec.StageID
		if strings.TrimSpace(spec.StageRef) != "" {
			if stageID != "" {
				return nil, nil, pm.ErrRemediationProposalInvalid
			}
			resolved, rerr := s.resolveEvolutionStageRef(ctx, p.ID(), spec.StageRef, stageRefToID)
			if rerr != nil {
				return nil, nil, rerr
			}
			stageID = resolved
		} else if stageID != "" {
			resolved, rerr := s.resolveEvolutionStageRef(ctx, p.ID(), string(stageID), stageRefToID)
			if rerr != nil {
				return nil, nil, rerr
			}
			stageID = resolved
		}
		if stageID != "" {
			if err := task.SetStage(stageID, now); err != nil {
				return nil, nil, err
			}
		} else if planHasStages && !membershipLater[ref] {
			return nil, nil, fmt.Errorf("%w: %s", pm.ErrStageStagelessNode, task.ID())
		}
		if err := s.tasks.Update(ctx, task); err != nil {
			return nil, nil, err
		}
		if assignee := string(task.Assignee()); assignee != "" {
			if err := s.emit(ctx, EvtPlanParticipantsChanged,
				refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
				planEventPayload{
					PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
					OwnerRef: "pm://plans/" + string(p.ID()), Participants: []string{assignee},
				}); err != nil {
				return nil, nil, err
			}
		}
		refToTask[ref] = taskID
		created = append(created, taskID)
	}
	return created, refToTask, nil
}

func (s *Service) resolveEvolutionEdges(ctx context.Context, p *pm.Plan, specs []pm.PlanGenerationEdgeDraft, refToTask map[string]pm.TaskID) ([]pm.Dependency, error) {
	deps := make([]pm.Dependency, 0, len(specs))
	for _, spec := range specs {
		from, err := resolveEvolutionTaskRef(ctx, s.tasks, p.ID(), spec.From, refToTask)
		if err != nil {
			return nil, err
		}
		to, err := resolveEvolutionTaskRef(ctx, s.tasks, p.ID(), spec.To, refToTask)
		if err != nil {
			return nil, err
		}
		dep := pm.Dependency{PlanID: p.ID(), FromTaskID: from, ToTaskID: to, Kind: pm.NormalizeEdgeKind(spec.Kind), When: spec.When, MaxRounds: spec.MaxRounds}
		if err := pm.ValidateControlEdgeShape(dep); err != nil {
			return nil, err
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

func (s *Service) addEvolutionEdges(ctx context.Context, deps []pm.Dependency) error {
	for _, dep := range deps {
		if err := s.plans.AddDependency(ctx, dep); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validateEvolutionStageStructure(ctx context.Context, p *pm.Plan, newDeps []pm.Dependency) error {
	if s.stages == nil {
		return nil
	}
	stages, err := s.stages.ListByPlan(ctx, p.ID())
	if err != nil {
		return err
	}
	if len(stages) == 0 {
		return nil
	}
	if err := pm.ValidateStageDAG(stages); err != nil {
		return err
	}
	tasks, err := s.tasks.ListByPlan(ctx, p.ID())
	if err != nil {
		return err
	}
	edges, err := s.plans.ListDependencies(ctx, p.ID())
	if err != nil {
		return err
	}
	edges = append(edges, newDeps...)
	if err := pm.ValidateStageEdges(pm.StageOf(tasks), edges); err != nil {
		return err
	}
	if p.Status() == pm.PlanPending {
		return pm.ValidateStageMembership(tasks)
	}
	return nil
}

type taskFinder interface {
	FindByID(context.Context, pm.TaskID) (*pm.Task, error)
}

func resolveEvolutionTaskRef(ctx context.Context, tasks taskFinder, planID pm.PlanID, ref string, refToTask map[string]pm.TaskID) (pm.TaskID, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", pm.ErrTaskNotFound
	}
	if id, ok := refToTask[ref]; ok {
		return id, nil
	}
	taskID := pm.TaskID(ref)
	t, err := tasks.FindByID(ctx, taskID)
	if err != nil {
		return "", err
	}
	if t.PlanID() != planID {
		return "", ErrTaskNotInPlan
	}
	return taskID, nil
}

func (s *Service) updateTaskGraphStageMetadata(ctx context.Context, p *pm.Plan, task *pm.Task, stageID pm.StageID) error {
	if s.orch == nil || strings.TrimSpace(p.GraphID()) == "" || task.NodeID() == "" {
		return nil
	}
	n, err := s.orch.GetNode(ctx, orch.NodeID(task.NodeID()))
	if err != nil {
		return err
	}
	meta := n.Metadata()
	if stageID == "" {
		delete(meta, "stage_id")
	} else {
		meta["stage_id"] = string(stageID)
	}
	return s.orch.UpdateNode(ctx, orch.NodeID(n.ID()), n.Title(), meta)
}

func (s *Service) applyEvolutionStageGraphDelta(ctx context.Context, p *pm.Plan, affected map[pm.StageID]bool, now time.Time) error {
	if len(affected) == 0 || s.orch == nil || s.stages == nil || strings.TrimSpace(p.GraphID()) == "" || p.Status() == pm.PlanPending {
		return nil
	}
	graphID := orch.GraphID(p.GraphID())
	stages, err := s.stages.ListByPlan(ctx, p.ID())
	if err != nil {
		return err
	}
	stageByID := make(map[pm.StageID]*pm.Stage, len(stages))
	for _, st := range stages {
		stageByID[st.ID()] = st
	}
	tasks, err := s.tasks.ListByPlan(ctx, p.ID())
	if err != nil {
		return err
	}
	taskByID := make(map[pm.TaskID]*pm.Task, len(tasks))
	for _, task := range tasks {
		taskByID[task.ID()] = task
	}
	edges, err := s.plans.ListDependencies(ctx, p.ID())
	if err != nil {
		return err
	}
	stageOf := pm.StageOf(tasks)
	graph, err := s.orch.GetGraph(ctx, graphID)
	if err != nil {
		return err
	}
	addEdge := func(from, to orch.NodeID) error {
		if from == "" || to == "" {
			return nil
		}
		if err := s.orch.AddEdge(ctx, graphID, from, to); err != nil && !errors.Is(err, orch.ErrEdgeExists) {
			return err
		}
		return nil
	}
	for stageID := range affected {
		st := stageByID[stageID]
		if st == nil {
			continue
		}
		gateTask := taskByID[st.GateTaskID()]
		if gateTask == nil || gateTask.NodeID() == "" {
			return pm.ErrMissingGateEvaluator
		}
		members := make([]pm.TaskID, 0)
		for _, member := range pm.StageMembers(tasks, st.ID()) {
			if member != st.GateTaskID() {
				members = append(members, member)
			}
		}
		entries := pm.StageEntries(members, stageOf, st.ID(), edges)
		onFailure := make([]any, 0, len(entries))
		for _, entry := range entries {
			if task := taskByID[entry]; task != nil && task.NodeID() != "" {
				onFailure = append(onFailure, task.NodeID())
			}
		}
		meta := map[string]any{
			"evaluator":     string(orch.EvaluatorManual),
			"stage_gate":    string(st.ID()),
			"max_rounds":    st.MaxRounds(),
			"condition_for": string(st.GateTaskID()),
			"pass_whens":    []any{"pass"},
		}
		if len(onFailure) > 0 {
			meta["on_failure"] = onFailure
		}
		gateID := orch.NodeID(st.GateNodeID())
		if gateID == "" {
			newGate, err := s.orch.AddNode(ctx, graphID, string(orch.NodeCategoryControl), string(orch.ControlKindCondition), "gate:"+st.Name(), meta)
			if err != nil {
				return err
			}
			gateID = newGate
			st.SetGateNodeID(string(gateID), now)
			if err := s.stages.Update(ctx, st); err != nil {
				return err
			}
		} else {
			if err := s.orch.UpdateNode(ctx, gateID, "gate:"+st.Name(), meta); err != nil {
				return err
			}
		}
		gateTaskNode := orch.NodeID(gateTask.NodeID())
		for _, memberID := range members {
			member := taskByID[memberID]
			if member == nil || member.NodeID() == "" {
				continue
			}
			if err := addEdge(orch.NodeID(member.NodeID()), gateTaskNode); err != nil {
				return err
			}
		}
		if err := addEdge(gateTaskNode, gateID); err != nil {
			return err
		}
		for _, upstreamID := range st.DependsOnStages() {
			upstream := stageByID[upstreamID]
			if upstream == nil || upstream.GateNodeID() == "" {
				return fmt.Errorf("%w: upstream stage %s has no gate node", pm.ErrPlanGenerationConflict, upstreamID)
			}
			for _, entry := range entries {
				entryTask := taskByID[entry]
				if entryTask == nil || entryTask.NodeID() == "" {
					continue
				}
				if err := addEdge(orch.NodeID(upstream.GateNodeID()), orch.NodeID(entryTask.NodeID())); err != nil {
					return err
				}
			}
		}
		if err := addEdge(gateID, graph.EndNodeID()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyGenerationGraphDelta(
	ctx context.Context,
	p *pm.Plan,
	newTaskIDs []pm.TaskID,
	superseded map[pm.TaskID]bool,
	newDeps []pm.Dependency,
	now time.Time,
) error {
	if p.Status() == pm.PlanPending || s.orch == nil || strings.TrimSpace(p.GraphID()) == "" {
		return nil
	}
	graphID := orch.GraphID(p.GraphID())
	for id := range superseded {
		t, err := s.tasks.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if t.NodeID() != "" {
			if err := s.orch.RemoveNode(ctx, orch.NodeID(t.NodeID())); err != nil {
				return err
			}
		}
	}
	nodeOf := map[pm.TaskID]orch.NodeID{}
	for _, t := range newTaskIDs {
		task, err := s.tasks.FindByID(ctx, t)
		if err != nil {
			return err
		}
		meta := map[string]any{
			"task_id": string(task.ID()), "generation_ref": true,
		}
		if task.StageID() != "" {
			meta["stage_id"] = string(task.StageID())
		}
		nodeID, err := s.orch.AddNode(ctx, graphID, string(orch.NodeCategoryBusiness), "", nodeTitle(task), meta)
		if err != nil {
			return err
		}
		task.SetNodeID(string(nodeID), now)
		if err := s.tasks.Update(ctx, task); err != nil {
			return err
		}
		nodeOf[task.ID()] = nodeID
	}
	resolveNode := func(taskID pm.TaskID) (orch.NodeID, error) {
		if n, ok := nodeOf[taskID]; ok {
			return n, nil
		}
		t, err := s.tasks.FindByID(ctx, taskID)
		if err != nil {
			return "", err
		}
		if t.NodeID() == "" {
			return "", fmt.Errorf("%w: task %s has no graph node", pm.ErrPlanGenerationConflict, taskID)
		}
		return orch.NodeID(t.NodeID()), nil
	}
	for _, dep := range newDeps {
		if pm.NormalizeEdgeKind(dep.Kind) != pm.EdgeSeq {
			return pm.ErrInvalidEdgeKind
		}
		fromNode, err := resolveNode(dep.FromTaskID)
		if err != nil {
			return err
		}
		toNode, err := resolveNode(dep.ToTaskID)
		if err != nil {
			return err
		}
		if err := s.orch.AddEdge(ctx, graphID, toNode, fromNode); err != nil && !errors.Is(err, orch.ErrEdgeExists) {
			return err
		}
	}
	return nil
}

func planGenerationSnapshot(
	planID pm.PlanID,
	activeID pm.PlanGenerationID,
	planVersion int,
	stages []*pm.Stage,
	tasks []*pm.Task,
	edges []pm.Dependency,
	records []pm.DispatchRecord,
) pm.PlanGenerationSnapshot {
	sort.SliceStable(stages, func(i, j int) bool {
		if !stages[i].CreatedAt().Equal(stages[j].CreatedAt()) {
			return stages[i].CreatedAt().Before(stages[j].CreatedAt())
		}
		return stages[i].ID() < stages[j].ID()
	})
	sort.SliceStable(tasks, func(i, j int) bool { return tasks[i].ID() < tasks[j].ID() })
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].FromTaskID != edges[j].FromTaskID {
			return edges[i].FromTaskID < edges[j].FromTaskID
		}
		return edges[i].ToTaskID < edges[j].ToTaskID
	})
	sort.SliceStable(records, func(i, j int) bool { return records[i].TaskID < records[j].TaskID })
	snap := pm.PlanGenerationSnapshot{
		PlanID:             planID,
		PlanVersion:        planVersion,
		ActiveGenerationID: activeID,
		Stages:             make([]pm.PlanGenerationStageSnapshot, 0, len(stages)),
		Tasks:              make([]pm.PlanGenerationTaskSnapshot, 0, len(tasks)),
		Edges:              make([]pm.PlanGenerationEdgeSnapshot, 0, len(edges)),
		DispatchRecords:    make([]pm.PlanGenerationDispatchSnapshot, 0, len(records)),
	}
	for _, st := range stages {
		snap.Stages = append(snap.Stages, pm.PlanGenerationStageSnapshot{
			StageID:             st.ID(),
			Name:                st.Name(),
			DependsOnStages:     st.DependsOnStages(),
			GateNodeID:          st.GateNodeID(),
			GateTaskID:          st.GateTaskID(),
			GateSpec:            st.GateSpec(),
			MaxRounds:           st.MaxRounds(),
			OriginVerdictID:     st.OriginVerdictID(),
			ContinuationID:      st.ContinuationID(),
			Generation:          st.Generation(),
			AcceptanceContract:  st.AcceptanceContract(),
			TopologyFingerprint: st.TopologyFingerprint(),
			CreatedAt:           st.CreatedAt(),
			UpdatedAt:           st.UpdatedAt(),
			Version:             st.Version(),
		})
	}
	for _, t := range tasks {
		snap.Tasks = append(snap.Tasks, pm.PlanGenerationTaskSnapshot{
			TaskID:           t.ID(),
			StageID:          t.StageID(),
			NodeID:           t.NodeID(),
			Title:            t.Title(),
			Description:      t.Description(),
			AssigneeRef:      t.Assignee(),
			Status:           t.Status(),
			DispatchMode:     t.DispatchMode(),
			DeliveryContract: t.DeliveryContract(),
			FollowsTaskID:    t.FollowsTaskID(),
			OriginVerdictID:  t.OriginVerdictID(),
		})
	}
	for _, e := range edges {
		snap.Edges = append(snap.Edges, pm.PlanGenerationEdgeSnapshot{
			FromTaskID: e.FromTaskID, ToTaskID: e.ToTaskID,
			Kind: pm.NormalizeEdgeKind(e.Kind), When: e.When, MaxRounds: e.MaxRounds,
		})
	}
	for _, r := range records {
		snap.DispatchRecords = append(snap.DispatchRecords, pm.PlanGenerationDispatchSnapshot{
			TaskID: r.TaskID, DispatchedAt: r.DispatchedAt, DispatchMessageID: r.DispatchMessageID,
		})
	}
	return snap
}

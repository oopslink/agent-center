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
	PlanID              pm.PlanID
	ParentGenerationID  pm.PlanGenerationID
	BaseVersion         int
	IdempotencyKey      string
	Reason              string
	Evidence            string
	Creator             pm.IdentityRef
	Diff                pm.PlanGenerationDiff
	ResolveBlockEventID string
	ResolutionKind      string
	ResolutionNote      string
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
	cmd.ResolveBlockEventID = strings.TrimSpace(cmd.ResolveBlockEventID)
	cmd.ResolutionKind = strings.TrimSpace(cmd.ResolutionKind)
	cmd.ResolutionNote = strings.TrimSpace(cmd.ResolutionNote)
	if cmd.IdempotencyKey == "" {
		return result, errors.New("projectmanager: evolution idempotency_key required")
	}
	if cmd.Reason == "" || cmd.Evidence == "" {
		return result, errors.New("projectmanager: evolution reason and evidence required")
	}
	if err := validateEvolutionBlockResolution(cmd); err != nil {
		return result, err
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
		case pm.PlanRunning, pm.PlanPaused:
		default:
			return pm.ErrPlanNotRunning
		}
		if p.Version() != cmd.BaseVersion {
			return fmt.Errorf("%w: base_version=%d current=%d", pm.ErrPlanVersionConflict, cmd.BaseVersion, p.Version())
		}
		if p.ActiveGenerationID() == "" {
			return fmt.Errorf("%w: plan %s has no active G0 baseline", pm.ErrPlanGenerationConflict, p.ID())
		}
		if p.ActiveGenerationID() != cmd.ParentGenerationID {
			return fmt.Errorf("%w: parent_generation_id=%s active_generation_id=%s",
				pm.ErrPlanGenerationConflict, cmd.ParentGenerationID, p.ActiveGenerationID())
		}
		if cmd.ResolveBlockEventID != "" {
			if err := s.resolveEvolutionBlockEvent(txCtx, p.ProjectID(), p.ID(), cmd); err != nil {
				return err
			}
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

		superseded, err := s.validateEvolutionNodeDecisions(txCtx, p, cmd.Creator, cmd.Diff.NodeDecisions, taskByID, edges, dispatchedSet)
		if err != nil {
			return err
		}
		if err := validateEvolutionInFlightEdges(cmd.Diff, taskByID, dispatchedSet); err != nil {
			return err
		}
		if err := validateEvolutionNewRootsConnected(cmd.Diff); err != nil {
			return err
		}
		if err := s.applySupersededNodes(txCtx, p, superseded, taskByID, edges, now); err != nil {
			return err
		}

		newTaskIDs, refToTask, err := s.createEvolutionTasks(txCtx, p, cmd, supersedeLineageSource(superseded), now)
		if err != nil {
			return err
		}
		if err := s.addEvolutionEdges(txCtx, p, cmd.Diff.Edges, refToTask); err != nil {
			return err
		}
		if err := s.applyGenerationGraphDelta(txCtx, p, newTaskIDs, refToTask, superseded, cmd.Diff.Edges, now); err != nil {
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
		generationID := pm.PlanGenerationID(s.idgen.NewEntityID("generation"))
		generation, err := pm.NewPlanGeneration(pm.PlanGeneration{
			ID:                 generationID,
			PlanID:             p.ID(),
			ParentGenerationID: cmd.ParentGenerationID,
			Reason:             cmd.Reason,
			Evidence:           cmd.Evidence,
			CreatorRef:         cmd.Creator,
			Diff:               cmd.Diff,
			Snapshot:           planGenerationSnapshot(p.ID(), generationID, nextVersion, freshTasks, freshEdges, freshRecords),
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

// validateEvolutionInFlightEdges runs before any task/edge mutation. An edge's
// From endpoint is the dependent node whose prerequisite set changes; if that
// endpoint already dispatched or left open state, the entire Evolution request
// must fail. To may be immutable because a new mutable node may safely depend on
// completed history.
func validateEvolutionInFlightEdges(
	diff pm.PlanGenerationDiff,
	taskByID map[pm.TaskID]*pm.Task,
	dispatched map[pm.TaskID]bool,
) error {
	newRefs := make(map[string]bool, len(diff.Tasks))
	for _, task := range diff.Tasks {
		ref := strings.TrimSpace(task.Ref)
		if ref != "" {
			newRefs[ref] = true
		}
	}
	for _, edge := range diff.Edges {
		from := strings.TrimSpace(edge.From)
		if from == "" || newRefs[from] {
			continue
		}
		task := taskByID[pm.TaskID(from)]
		if task == nil {
			continue // resolveEvolutionTaskRef reports the scoped reference error.
		}
		if !pm.NodeMutable(task.Status(), dispatched[task.ID()]) {
			return fmt.Errorf("%w: task %s", pm.ErrPlanNodeInFlight, task.ID())
		}
	}
	return nil
}

func validateEvolutionNewRootsConnected(diff pm.PlanGenerationDiff) error {
	newRefs := make(map[string]pm.PlanGenerationTaskDraft, len(diff.Tasks))
	for _, task := range diff.Tasks {
		ref := strings.TrimSpace(task.Ref)
		if ref == "" {
			continue
		}
		newRefs[ref] = task
	}
	if len(newRefs) == 0 {
		return nil
	}
	hasExplicitPrereq := make(map[string]bool, len(newRefs))
	for _, edge := range diff.Edges {
		from := strings.TrimSpace(edge.From)
		if _, ok := newRefs[from]; ok {
			hasExplicitPrereq[from] = true
		}
	}
	for ref, task := range newRefs {
		if task.Detached || hasExplicitPrereq[ref] {
			continue
		}
		return fmt.Errorf("%w: task ref %s has no explicit prerequisite edge; add an edge from this task to prior execution or set detached=true", pm.ErrPlanGenerationDisconnected, ref)
	}
	return nil
}

func evolutionRequestFingerprint(cmd EvolvePlanGenerationCommand) (string, error) {
	body := struct {
		PlanID              pm.PlanID             `json:"plan_id"`
		ParentGenerationID  pm.PlanGenerationID   `json:"parent_generation_id"`
		BaseVersion         int                   `json:"base_version"`
		Reason              string                `json:"reason"`
		Evidence            string                `json:"evidence"`
		Creator             pm.IdentityRef        `json:"creator"`
		Diff                pm.PlanGenerationDiff `json:"diff"`
		ResolveBlockEventID string                `json:"resolve_block_event_id,omitempty"`
		ResolutionKind      string                `json:"resolution_kind,omitempty"`
		ResolutionNote      string                `json:"resolution_note,omitempty"`
	}{cmd.PlanID, cmd.ParentGenerationID, cmd.BaseVersion, strings.TrimSpace(cmd.Reason), strings.TrimSpace(cmd.Evidence), cmd.Creator, cmd.Diff, cmd.ResolveBlockEventID, cmd.ResolutionKind, strings.TrimSpace(cmd.ResolutionNote)}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateEvolutionBlockResolution(cmd EvolvePlanGenerationCommand) error {
	if cmd.ResolveBlockEventID == "" {
		if cmd.ResolutionKind != "" || cmd.ResolutionNote != "" {
			return fmt.Errorf("%w: resolve_block_event_id is required with resolution fields", pm.ErrPlanGenerationConflict)
		}
		return nil
	}
	switch cmd.ResolutionKind {
	case "replace", "bypass":
	default:
		return fmt.Errorf("%w: invalid resolution_kind %q", pm.ErrInvalidStatus, cmd.ResolutionKind)
	}
	if strings.TrimSpace(cmd.ResolutionNote) == "" {
		return errors.New("projectmanager: resolution_note required")
	}
	return nil
}

func (s *Service) resolveEvolutionBlockEvent(ctx context.Context, projectID pm.ProjectID, planID pm.PlanID, cmd EvolvePlanGenerationCommand) error {
	taskID := pm.TaskID(cmd.ResolveBlockEventID)
	blocked, found, err := s.plans.GetBlockedOn(ctx, planID, taskID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: resolve_block_event_id=%s is not active on plan %s", pm.ErrPlanGenerationConflict, cmd.ResolveBlockEventID, planID)
	}
	if err := s.plans.ClearBlockedOn(ctx, planID, taskID); err != nil {
		return err
	}
	s.auditPlanByID(ctx, projectID, planID, pm.AuditPlanTopologyCommit, cmd.Creator, map[string]any{
		"resolve_block_event_id": cmd.ResolveBlockEventID,
		"resolution_kind":        cmd.ResolutionKind,
		"resolution_note":        cmd.ResolutionNote,
		"wait_type":              string(blocked.WaitType),
	})
	return nil
}

func (s *Service) validateEvolutionNodeDecisions(
	ctx context.Context,
	p *pm.Plan,
	actor pm.IdentityRef,
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
				if !ownerMaySupersedeSettledNode(t.Status(), dispatched[d.TaskID]) {
					return nil, fmt.Errorf("%w: supersede task %s", pm.ErrPlanNodeInFlight, d.TaskID)
				}
				if err := s.requirePlanCreatorOrProjectOwner(ctx, p, actor); err != nil {
					return nil, err
				}
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

func ownerMaySupersedeSettledNode(status pm.TaskStatus, dispatched bool) bool {
	if status == pm.TaskRunning {
		return false
	}
	return dispatched || status.IsParked() || status.IsTerminal()
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
		// A superseded node remains immutable Plan history. It no longer participates
		// in the active graph/stage, but plan_id stays as historical/container
		// attribution for terminal and non-terminal nodes alike; otherwise a rejected
		// generation can leak settled plan facts back into Backlog.
		if err := t.SetStage("", now); err != nil {
			return err
		}
		if err := s.tasks.Update(ctx, t); err != nil {
			return err
		}
		if err := s.plans.ClearDispatch(ctx, p.ID(), id); err != nil {
			return err
		}
		if err := s.plans.ClearBlockedOn(ctx, p.ID(), id); err != nil {
			return err
		}
		s.auditPlanByID(ctx, p.ProjectID(), p.ID(), pm.AuditPlanNodeRemoved, p.CreatorRef(), map[string]any{
			"task": string(id), "evolution_action": string(pm.EvolutionSupersede),
		})
	}
	return nil
}

func supersedeLineageSource(superseded map[pm.TaskID]bool) pm.TaskID {
	if len(superseded) != 1 {
		return ""
	}
	for id := range superseded {
		return id
	}
	return ""
}

func (s *Service) createEvolutionTasks(
	ctx context.Context,
	p *pm.Plan,
	cmd EvolvePlanGenerationCommand,
	lineageSource pm.TaskID,
	now time.Time,
) ([]pm.TaskID, map[string]pm.TaskID, error) {
	refToTask := map[string]pm.TaskID{}
	var created []pm.TaskID
	for _, spec := range cmd.Diff.Tasks {
		ref := strings.TrimSpace(spec.Ref)
		if ref == "" || strings.TrimSpace(spec.Title) == "" {
			return nil, nil, pm.ErrRemediationProposalInvalid
		}
		if _, exists := refToTask[ref]; exists {
			return nil, nil, pm.ErrRemediationProposalInvalid
		}
		followsTaskID := spec.FollowsTaskID
		if followsTaskID == "" {
			followsTaskID = lineageSource
		}
		taskID, err := s.CreateTask(ctx, CreateTaskCommand{
			ProjectID: p.ProjectID(), Title: spec.Title, Description: spec.Description,
			CreatedBy: cmd.Creator, Assignee: spec.AssigneeRef, DispatchMode: spec.DispatchMode,
			DeliveryContract: spec.DeliveryContract, FollowsTaskID: followsTaskID,
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
		if spec.StageID != "" {
			if err := task.SetStage(spec.StageID, now); err != nil {
				return nil, nil, err
			}
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

func (s *Service) addEvolutionEdges(ctx context.Context, p *pm.Plan, specs []pm.PlanGenerationEdgeDraft, refToTask map[string]pm.TaskID) error {
	for _, spec := range specs {
		from, err := resolveEvolutionTaskRef(ctx, s.tasks, p.ID(), spec.From, refToTask)
		if err != nil {
			return err
		}
		to, err := resolveEvolutionTaskRef(ctx, s.tasks, p.ID(), spec.To, refToTask)
		if err != nil {
			return err
		}
		dep := pm.Dependency{PlanID: p.ID(), FromTaskID: from, ToTaskID: to, Kind: pm.NormalizeEdgeKind(spec.Kind), When: spec.When, MaxRounds: spec.MaxRounds}
		if err := pm.ValidateControlEdgeShape(dep); err != nil {
			return err
		}
		if err := s.plans.AddDependency(ctx, dep); err != nil {
			return err
		}
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

func (s *Service) applyGenerationGraphDelta(
	ctx context.Context,
	p *pm.Plan,
	newTaskIDs []pm.TaskID,
	refToTask map[string]pm.TaskID,
	superseded map[pm.TaskID]bool,
	edgeSpecs []pm.PlanGenerationEdgeDraft,
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
			if err := s.removeSupersededGraphNode(ctx, orch.NodeID(t.NodeID())); err != nil {
				return err
			}
			// A retained history task must no longer point at the removed active graph
			// node. Clear the binding only after removal so the delta can still locate
			// the node above.
			if t.PlanID() == p.ID() {
				t.SetNodeID("", now)
				if err := s.tasks.Update(ctx, t); err != nil {
					return err
				}
			}
		}
	}
	nodeOf := map[pm.TaskID]orch.NodeID{}
	for _, t := range newTaskIDs {
		task, err := s.tasks.FindByID(ctx, t)
		if err != nil {
			return err
		}
		nodeID, err := s.orch.AddNode(ctx, graphID, string(orch.NodeCategoryBusiness), "", nodeTitle(task), map[string]any{
			"task_id": string(task.ID()), "generation_ref": true,
		})
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
	for _, spec := range edgeSpecs {
		if pm.NormalizeEdgeKind(spec.Kind) != pm.EdgeSeq {
			return pm.ErrInvalidEdgeKind
		}
		from, err := resolveEvolutionTaskRef(ctx, s.tasks, p.ID(), spec.From, refToTask)
		if err != nil {
			return err
		}
		to, err := resolveEvolutionTaskRef(ctx, s.tasks, p.ID(), spec.To, refToTask)
		if err != nil {
			return err
		}
		fromNode, err := resolveNode(from)
		if err != nil {
			return err
		}
		toNode, err := resolveNode(to)
		if err != nil {
			return err
		}
		if err := s.orch.AddEdge(ctx, graphID, toNode, fromNode); err != nil && !errors.Is(err, orch.ErrEdgeExists) {
			return err
		}
	}
	return nil
}

func (s *Service) removeSupersededGraphNode(ctx context.Context, nodeID orch.NodeID) error {
	n, err := s.orch.GetNode(ctx, nodeID)
	if err != nil {
		return err
	}
	switch n.Status() {
	case orch.NodeRunning, orch.NodeCompleted:
		if err := s.orch.ReopenNode(ctx, nodeID, "superseded by plan evolution"); err != nil {
			return err
		}
	}
	return s.orch.RemoveNode(ctx, nodeID)
}

func planGenerationSnapshot(
	planID pm.PlanID,
	activeID pm.PlanGenerationID,
	planVersion int,
	tasks []*pm.Task,
	edges []pm.Dependency,
	records []pm.DispatchRecord,
) pm.PlanGenerationSnapshot {
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
		Tasks:              make([]pm.PlanGenerationTaskSnapshot, 0, len(tasks)),
		Edges:              make([]pm.PlanGenerationEdgeSnapshot, 0, len(edges)),
		DispatchRecords:    make([]pm.PlanGenerationDispatchSnapshot, 0, len(records)),
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

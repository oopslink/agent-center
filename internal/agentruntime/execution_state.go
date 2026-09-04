package agentruntime

import (
	"context"
	"sort"
	"strings"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/concurrency"
)

// SnapshotExecutionState builds the runtime-local supervisor control view. It treats
// center responses as task authority only; executor state, terminal delivery, and
// task->executor mapping all come from this agent-runtime process and its local file
// protocol.
func (r *LocalRuntime) SnapshotExecutionState(ctx context.Context) (concurrency.ExecutionStateSnapshot, error) {
	now := r.now().UTC()
	snap := concurrency.ExecutionStateSnapshot{
		AgentID:             r.cfg.AgentID,
		AvailableTasks:      []concurrency.TaskAuthorityRow{},
		ActiveTasks:         []concurrency.ExecutionTaskRow{},
		TaskExecutorMapping: []concurrency.TaskExecutorBinding{},
		Executors:           []concurrency.ExecutorStateRow{},
		UpdatedAt:           now,
	}
	taskRows, integrityErrs := r.executionTaskAuthority(ctx)
	tasksByID := make(map[string]concurrency.TaskAuthorityRow, len(taskRows))
	for _, task := range taskRows {
		task.TaskID = strings.TrimSpace(task.TaskID)
		if task.TaskID == "" {
			continue
		}
		tasksByID[task.TaskID] = task
	}

	ee := r.execEngine()
	liveByTask := make(map[string]concurrency.ExecutorStateRow)
	liveByID := make(map[string]concurrency.ExecutorStateRow)
	for _, ex := range r.SnapshotConcurrency() {
		row := executorStateRow(ex)
		if ee != nil && ee.fx != nil {
			enrichExecutorRowFromFiles(ee.fx, &row)
		}
		snap.Executors = append(snap.Executors, row)
		liveByID[row.ExecutorID] = row
		if row.TaskID != "" {
			liveByTask[row.TaskID] = row
		}
	}

	finalizedByTask := map[string]concurrency.ExecutorStateRow{}
	if ee != nil && ee.fx != nil {
		finalizedByTask = finalizedExecutorsByTask(ee.fx)
	}
	pendingByTask := r.pendingJudgmentsByTask()
	explicitMap := r.taskExecutorSnapshot()
	currentTaskID := strings.TrimSpace(r.CurrentTaskID())

	activeIDs := make(map[string]struct{})
	for taskID := range pendingByTask {
		activeIDs[taskID] = struct{}{}
	}
	for taskID := range explicitMap {
		activeIDs[taskID] = struct{}{}
	}
	for taskID := range liveByTask {
		activeIDs[taskID] = struct{}{}
	}
	for taskID, task := range tasksByID {
		if strings.EqualFold(strings.TrimSpace(task.Status), "running") {
			activeIDs[taskID] = struct{}{}
		}
	}
	if currentTaskID != "" {
		activeIDs[currentTaskID] = struct{}{}
	}

	for _, task := range taskRows {
		if _, active := activeIDs[task.TaskID]; active {
			continue
		}
		if centerStatusIsAvailable(task.Status) {
			task.RequiredNextAction = concurrency.NextActionForkExecutor
			snap.AvailableTasks = append(snap.AvailableTasks, task)
		}
	}

	activeTaskIDs := make([]string, 0, len(activeIDs))
	for taskID := range activeIDs {
		activeTaskIDs = append(activeTaskIDs, taskID)
	}
	sort.Strings(activeTaskIDs)
	for _, taskID := range activeTaskIDs {
		task := tasksByID[taskID]
		if task.TaskID == "" {
			task = concurrency.TaskAuthorityRow{TaskID: taskID}
		}
		row := concurrency.ExecutionTaskRow{
			TaskID:             taskID,
			Title:              task.Title,
			TaskStatus:         task.Status,
			Runnable:           task.Runnable,
			ExecutionMode:      concurrency.ExecutionModeExecutor,
			ExecutorState:      concurrency.ExecutorStateUnknown,
			DeliveryState:      concurrency.DeliveryStateUnknown,
			RequiredNextAction: concurrency.NextActionWaitExecutor,
		}
		var ex concurrency.ExecutorStateRow
		if live, ok := liveByTask[taskID]; ok {
			ex = live
			row.ExecutorID = live.ExecutorID
			row.ExecutorState = taskExecutorState(live.State)
			row.DeliveryState = coalesceDelivery(live.DeliveryState, concurrency.DeliveryStateUnknown)
			row.Worktree = live.Worktree
			row.Evidence = append(row.Evidence, live.Evidence...)
			switch row.ExecutorState {
			case concurrency.ExecutorStateActive:
				row.RequiredNextAction = concurrency.NextActionWaitExecutor
			case concurrency.ExecutorStateTerminal:
				row.RequiredNextAction = concurrency.NextActionJudgeExecutor
			default:
				row.RequiredNextAction = concurrency.NextActionResetStale
			}
		} else if execID := explicitMap[taskID]; execID != "" {
			row.ExecutorID = execID
			if byID, ok := liveByID[execID]; ok {
				ex = byID
			}
		} else if terminal, ok := finalizedByTask[taskID]; ok {
			ex = terminal
			row.ExecutorID = terminal.ExecutorID
			row.ExecutorState = concurrency.ExecutorStateTerminal
			row.DeliveryState = coalesceDelivery(terminal.DeliveryState, concurrency.DeliveryStateUnknown)
			row.Worktree = terminal.Worktree
			row.Evidence = append(row.Evidence, terminal.Evidence...)
			row.RequiredNextAction = concurrency.NextActionJudgeExecutor
		} else if taskID == currentTaskID {
			row.ExecutionMode = concurrency.ExecutionModeInline
			row.ExecutorState = concurrency.ExecutorStateNone
			row.DeliveryState = concurrency.DeliveryStateNone
			row.RequiredNextAction = concurrency.NextActionHandleInline
		} else {
			row.ExecutorState = concurrency.ExecutorStateStale
			row.DeliveryState = concurrency.DeliveryStateUnknown
			row.RequiredNextAction = concurrency.NextActionResetStale
			row.Evidence = append(row.Evidence, concurrency.ExecutionEvidence{
				Source:  "runtime",
				Kind:    "runtime_gap",
				Message: "center/task state has an active task but runtime has no live executor, finalized executor, or inline binding",
			})
		}
		if pending, ok := pendingByTask[taskID]; ok {
			row.Evidence = append(row.Evidence, pendingEvidence(pending))
			row.ExecutorState = concurrency.ExecutorStateTerminal
			if pending.MustBlock {
				row.ExecutorState = concurrency.ExecutorStateNonDelivery
				row.DeliveryState = concurrency.DeliveryStateInvalid
				row.RequiredNextAction = concurrency.NextActionRepairNonDelivery
			} else if row.RequiredNextAction != concurrency.NextActionResetStale {
				row.RequiredNextAction = concurrency.NextActionJudgeExecutor
			}
		}
		if row.ExecutorID != "" && row.ExecutionMode != concurrency.ExecutionModeInline {
			snap.TaskExecutorMapping = append(snap.TaskExecutorMapping, concurrency.TaskExecutorBinding{
				TaskID:        taskID,
				Mode:          concurrency.ExecutionModeExecutor,
				ExecutorID:    row.ExecutorID,
				ExecutorState: row.ExecutorState,
				UpdatedAt:     now,
			})
		} else if row.ExecutionMode == concurrency.ExecutionModeInline {
			snap.TaskExecutorMapping = append(snap.TaskExecutorMapping, concurrency.TaskExecutorBinding{
				TaskID:        taskID,
				Mode:          concurrency.ExecutionModeInline,
				ExecutorState: row.ExecutorState,
				UpdatedAt:     now,
			})
		}
		if ex.ExecutorID != "" {
			row.ExecutorID = ex.ExecutorID
		}
		snap.ActiveTasks = append(snap.ActiveTasks, row)
	}

	if len(integrityErrs) > 0 {
		snap.Integrity = "degraded"
		snap.IntegrityError = strings.Join(integrityErrs, "; ")
	}
	return snap, nil
}

func (r *LocalRuntime) executionTaskAuthority(ctx context.Context) ([]concurrency.TaskAuthorityRow, []string) {
	var errs []string
	rowsByID := map[string]concurrency.TaskAuthorityRow{}
	if lister := NewInflightTaskLister(r.toolCaller()); lister != nil {
		tasks, err := lister.ListMyInflightTasks(ctx, r.cfg.AgentID)
		if err != nil {
			errs = append(errs, "list_my_inflight_tasks: "+err.Error())
		} else {
			for _, t := range tasks {
				row := taskAuthorityFromInflight(t, false)
				if row.TaskID != "" {
					rowsByID[row.TaskID] = row
				}
			}
		}
	}
	if lister := NewRunnableTaskLister(r.toolCaller()); lister != nil {
		tasks, err := lister.ListMyRunnableTasks(ctx, r.cfg.AgentID)
		if err != nil {
			errs = append(errs, "list_my_tasks: "+err.Error())
		} else {
			for _, task := range tasks {
				if task.TaskID == "" {
					continue
				}
				prev := rowsByID[task.TaskID]
				task.Runnable = true
				if prev.TaskID != "" && task.Status == "" {
					task.Status = prev.Status
				}
				rowsByID[task.TaskID] = task
			}
		}
	}
	rows := make([]concurrency.TaskAuthorityRow, 0, len(rowsByID))
	for _, row := range rowsByID {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TaskID < rows[j].TaskID })
	return rows, errs
}

func (r *LocalRuntime) taskExecutorSnapshot() map[string]string {
	r.forkStateMu.Lock()
	defer r.forkStateMu.Unlock()
	out := make(map[string]string, len(r.taskExecutors))
	for taskID, execID := range r.taskExecutors {
		out[taskID] = execID
	}
	return out
}

func (r *LocalRuntime) pendingJudgmentsByTask() map[string]pendingJudgment {
	out := map[string]pendingJudgment{}
	if r.pending == nil {
		return out
	}
	for _, p := range r.pending.snapshot() {
		if taskID := strings.TrimSpace(p.TaskRef); taskID != "" {
			out[taskID] = p
		}
	}
	return out
}

func taskAuthorityFromInflight(t InflightTask, runnable bool) concurrency.TaskAuthorityRow {
	return concurrency.TaskAuthorityRow{
		TaskID:            strings.TrimSpace(t.TaskID),
		Title:             t.Title,
		Status:            t.Status,
		Runnable:          runnable,
		BlockedReason:     t.BlockedReason,
		BlockedReasonType: t.BlockedReasonType,
		BlockedComment:    t.BlockedComment,
		LeaseExpiresAt:    t.LeaseExpiresAt,
	}
}

func centerStatusIsAvailable(status string) bool {
	switch strings.TrimSpace(status) {
	case "", "open", "reopened":
		return true
	default:
		return false
	}
}

func executorStateRow(s concurrency.ExecutorSnapshot) concurrency.ExecutorStateRow {
	return concurrency.ExecutorStateRow{
		ExecutorID:      s.ExecutorID,
		TaskID:          s.TaskID,
		SlotIndex:       s.SlotIndex,
		State:           s.State,
		PID:             s.PID,
		StartedAt:       s.StartedAt,
		LastProgressAt:  s.LastProgressAt,
		CurrentActivity: s.CurrentActivity,
		CLI:             s.CLI,
		Model:           s.Model,
	}
}

func enrichExecutorRowFromFiles(fx *executor.FileExchange, row *concurrency.ExecutorStateRow) {
	if fx == nil || row == nil || row.ExecutorID == "" {
		return
	}
	if in, err := fx.ReadInput(row.ExecutorID); err == nil {
		if row.TaskID == "" {
			row.TaskID = strings.TrimSpace(in.Source.TaskRef)
		}
		if row.CLI == "" {
			row.CLI = in.CLI
		}
		if row.Model == "" {
			row.Model = in.Model
		}
	}
	if out, err := fx.ReadOutput(row.ExecutorID); err == nil {
		row.DeliveryState = deliveryStateFromOutput(out)
		if out.Error != nil {
			row.ReasonCodes = append(row.ReasonCodes, out.Error.ReasonCodes...)
			if out.Error.NextAction != "" {
				row.RequiredAction = out.Error.NextAction
			}
			row.Evidence = append(row.Evidence, concurrency.ExecutionEvidence{Source: "output", Kind: out.Error.Kind, Message: out.Error.Message})
		}
	}
	if refs, err := fx.ListFinalized(); err == nil {
		for _, ref := range refs {
			if ref.ExecutorID != row.ExecutorID || ref.Git == nil {
				continue
			}
			row.Worktree = ref.Git.Worktree
			if row.DeliveryState == "" {
				row.DeliveryState = deliveryStateFromGit(ref.Git)
			}
		}
	}
	if row.DeliveryState == "" {
		row.DeliveryState = concurrency.DeliveryStateUnknown
	}
}

func finalizedExecutorsByTask(fx *executor.FileExchange) map[string]concurrency.ExecutorStateRow {
	out := map[string]concurrency.ExecutorStateRow{}
	if fx == nil {
		return out
	}
	refs, err := fx.ListFinalized()
	if err != nil {
		return out
	}
	for _, ref := range refs {
		in, err := fx.ReadInput(ref.ExecutorID)
		if err != nil {
			continue
		}
		taskID := strings.TrimSpace(in.Source.TaskRef)
		if taskID == "" {
			continue
		}
		row := concurrency.ExecutorStateRow{
			ExecutorID:     ref.ExecutorID,
			TaskID:         taskID,
			State:          concurrency.ExecutorStateTerminal,
			CLI:            in.CLI,
			Model:          in.Model,
			DeliveryState:  concurrency.DeliveryStateUnknown,
			RequiredAction: concurrency.NextActionJudgeExecutor,
		}
		if ref.Git != nil {
			row.Worktree = ref.Git.Worktree
			row.DeliveryState = deliveryStateFromGit(ref.Git)
		}
		if st, err := fx.ReadStatus(ref.ExecutorID); err == nil && st.Error != nil {
			row.Evidence = append(row.Evidence, concurrency.ExecutionEvidence{Source: "status", Kind: st.Error.Kind, Message: st.Error.Message})
			row.ReasonCodes = append(row.ReasonCodes, st.Error.ReasonCodes...)
		}
		out[taskID] = row
	}
	return out
}

func taskExecutorState(state string) string {
	switch state {
	case concurrency.StateStarting, concurrency.StateRunning:
		return concurrency.ExecutorStateActive
	case concurrency.StateFinishing, concurrency.ExecutorStateTerminal:
		return concurrency.ExecutorStateTerminal
	case concurrency.StateOrphan:
		return concurrency.ExecutorStateStale
	case "":
		return concurrency.ExecutorStateUnknown
	default:
		return state
	}
}

func deliveryStateFromOutput(out executor.Output) string {
	if out.Success {
		return concurrency.DeliveryStateValid
	}
	if out.Error != nil && strings.TrimSpace(out.Error.Kind) == "non_delivery" {
		return concurrency.DeliveryStateInvalid
	}
	return concurrency.DeliveryStateUnknown
}

func deliveryStateFromGit(g *executor.FinalizedGitStatus) string {
	if g == nil || !g.Probed {
		return concurrency.DeliveryStateUnknown
	}
	if g.Pushed && !g.Dirty && (!g.BaseKnown || g.AheadOfBase > 0) {
		return concurrency.DeliveryStateValid
	}
	return concurrency.DeliveryStateInvalid
}

func coalesceDelivery(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func pendingEvidence(p pendingJudgment) concurrency.ExecutionEvidence {
	kind := "pending_judgment"
	if p.MustBlock {
		kind = "pending_non_delivery"
	}
	return concurrency.ExecutionEvidence{
		Source:  "pending_judgments",
		Kind:    kind,
		Message: strings.TrimSpace(p.Prompt),
	}
}

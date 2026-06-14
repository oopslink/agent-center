package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Read-through methods for the webconsole (B3). Reads need no transaction;
// they delegate to the repos. Org/project scoping is enforced by the HTTP
// layer (project-in-org check) + requireProjectMember on writes.

func (s *Service) ListProjects(ctx context.Context, orgID string) ([]*pm.Project, error) {
	return s.projects.ListByOrg(ctx, orgID)
}

func (s *Service) GetProject(ctx context.Context, id pm.ProjectID) (*pm.Project, error) {
	return s.projects.FindByID(ctx, id)
}

func (s *Service) ListMembers(ctx context.Context, projectID pm.ProjectID) ([]*pm.ProjectMember, error) {
	return s.members.ListByProject(ctx, projectID)
}

func (s *Service) ListIssues(ctx context.Context, projectID pm.ProjectID) ([]*pm.Issue, error) {
	return s.issues.ListByProject(ctx, projectID)
}

func (s *Service) GetIssue(ctx context.Context, id pm.IssueID) (*pm.Issue, error) {
	return s.issues.FindByID(ctx, id)
}

func (s *Service) ListTasks(ctx context.Context, projectID pm.ProjectID) ([]*pm.Task, error) {
	return s.tasks.ListByProject(ctx, projectID)
}

// ListProjectTasksForMember lists a project's tasks, GUARDED by project membership
// (org-isolation §5.7: a non-member / cross-org actor gets requireProjectMember's
// error — mapped to 404 at the edge, no existence disclosure). Used by the
// list_tasks MCP tool so a PD/agent can see the whole board (status/assignee
// filtering is applied by the caller).
func (s *Service) ListProjectTasksForMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) ([]*pm.Task, error) {
	if err := s.requireProjectMember(ctx, projectID, actor); err != nil {
		return nil, err
	}
	return s.tasks.ListByProject(ctx, projectID)
}

// ListUnplannedTasks returns the project's backlog (v2.9): tasks not yet
// selected into any Plan (empty plan_id). It is the complement of the Plan's
// task list, for the Work Board's Backlog column.
func (s *Service) ListUnplannedTasks(ctx context.Context, projectID pm.ProjectID) ([]*pm.Task, error) {
	return s.tasks.ListUnplannedByProject(ctx, projectID)
}

func (s *Service) GetTask(ctx context.Context, id pm.TaskID) (*pm.Task, error) {
	return s.tasks.FindByID(ctx, id)
}

func (s *Service) ListCodeRepos(ctx context.Context, projectID pm.ProjectID) ([]*pm.CodeRepoRef, error) {
	return s.codeRepoRefs.ListByProject(ctx, projectID)
}

func (s *Service) ListTaskSubscribers(ctx context.Context, taskID pm.TaskID) ([]*pm.TaskSubscriber, error) {
	return s.taskSubs.ListByTask(ctx, taskID)
}

// ClaimableTask bundles a CLAIMABLE pool task with its derived node status
// (ADR-0047). A task is claimable iff pm.TaskClaimable(task, nodeStatus) — not
// archived, status==open, has an assignee, is IN a plan, and that plan node is
// `dispatched`. The built-in pull pool reaches `dispatched` via a dispatch record
// (no wake/WorkItem), so these tasks would otherwise be invisible to get_my_work.
type ClaimableTask struct {
	Task       *pm.Task
	NodeStatus pm.NodeStatus
}

// ListClaimableTasks returns the CLAIMABLE tasks assigned to `assignee` across all
// its plans (ADR-0047). It backs get_my_work's pull-pool surface: an agent's
// built-in-pool tasks have no WorkItem (pull/no-wake), so the work tool must query
// pm directly + apply the claimable predicate. Mechanism: list the assignee's
// tasks, then per distinct plan derive node statuses (via planDetail / the plan
// view) and keep only tasks whose node is `dispatched` (TaskClaimable true).
//
// Cost is bounded by the number of DISTINCT plans the assignee's open tasks belong
// to (one planDetail load each), de-duplicated so the same plan view is derived
// once. Backlog tasks (planID=="") are skipped — they are never claimable.
func (s *Service) ListClaimableTasks(ctx context.Context, assignee pm.IdentityRef) ([]ClaimableTask, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	tasks, err := s.tasks.ListByAssignee(ctx, assignee)
	if err != nil {
		return nil, err
	}
	// Cache one node-status map per plan so N tasks in the same plan derive the
	// plan view exactly once (no per-task plan-view re-derivation).
	nodeStatusByPlan := make(map[pm.PlanID]map[pm.TaskID]pm.NodeStatus)
	planView := func(planID pm.PlanID) (map[pm.TaskID]pm.NodeStatus, error) {
		if m, ok := nodeStatusByPlan[planID]; ok {
			return m, nil
		}
		p, err := s.plans.FindByID(ctx, planID)
		if err != nil {
			return nil, err
		}
		detail, err := s.planDetail(ctx, p)
		if err != nil {
			return nil, err
		}
		m := make(map[pm.TaskID]pm.NodeStatus, len(detail.View.Nodes))
		for _, n := range detail.View.Nodes {
			m[n.TaskID] = n.NodeStatus
		}
		nodeStatusByPlan[planID] = m
		return m, nil
	}

	var out []ClaimableTask
	for _, t := range tasks {
		planID := t.PlanID()
		if planID == "" {
			continue // backlog tasks are never claimable
		}
		statuses, err := planView(planID)
		if err != nil {
			return nil, err
		}
		ns := statuses[t.ID()]
		if pm.TaskClaimable(t, ns) {
			out = append(out, ClaimableTask{Task: t, NodeStatus: ns})
		}
	}
	return out, nil
}

// TaskClaimableByID derives whether a single task is claimable right now (ADR-0047
// §-1: expose `claimable` on get_task too). A backlog task (no plan) is never
// claimable; otherwise it derives the task's node_status from its plan view and
// applies the claimable predicate. Nil-safe on the plan repo (→ false).
func (s *Service) TaskClaimableByID(ctx context.Context, taskID pm.TaskID) (bool, error) {
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return false, err
	}
	planID := t.PlanID()
	if planID == "" || s.plans == nil {
		return false, nil
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return false, err
	}
	detail, err := s.planDetail(ctx, p)
	if err != nil {
		return false, err
	}
	var ns pm.NodeStatus
	for _, n := range detail.View.Nodes {
		if n.TaskID == taskID {
			ns = n.NodeStatus
			break
		}
	}
	return pm.TaskClaimable(t, ns), nil
}

// --- Plan reads (v2.9 #285) -------------------------------------------------

// ListPlans returns a project's Plans (parallel plans, §2), stable-ordered.
func (s *Service) ListPlans(ctx context.Context, projectID pm.ProjectID) ([]*pm.Plan, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	return s.plans.ListByProject(ctx, projectID)
}

// GetPlan returns one Plan AR.
func (s *Service) GetPlan(ctx context.Context, id pm.PlanID) (*pm.Plan, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	return s.plans.FindByID(ctx, id)
}

// PlanDetail bundles a Plan with its selected tasks and the DERIVED view (§9.2):
// per-node status, ready-set, has_failed, progress. The HTTP layer renders the
// Plan DTO (nodes + ready-set + has_failed + progress) from this.
type PlanDetail struct {
	Plan  *pm.Plan
	Tasks []*pm.Task
	View  pm.PlanView
}

// GetPlanDetail loads a Plan + its tasks + edges + dispatch records and derives
// the whole-Plan read model (§9.2). Node status is DERIVED here, never stored.
func (s *Service) GetPlanDetail(ctx context.Context, id pm.PlanID) (*PlanDetail, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	p, err := s.plans.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.planDetail(ctx, p)
}

// planDetail loads one Plan's tasks + edges + dispatch records and derives the
// whole-Plan read model (§9.2) for the given (already-loaded) Plan AR. Shared by
// GetPlanDetail (single plan) and ListPlanSummaries (per-plan in a loop) so the
// load+derive sequence lives in exactly one place. Node status is DERIVED here,
// never stored.
func (s *Service) planDetail(ctx context.Context, p *pm.Plan) (*PlanDetail, error) {
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
	return &PlanDetail{Plan: p, Tasks: tasks, View: pm.ComputePlanView(tasks, edges, records)}, nil
}

// ListPlanSummaries returns one PlanDetail per Plan in the project (the same
// DERIVED read model as GetPlanDetail), so the Work Board can render every kanban
// Plan column — progress, has_failed, a capped node preview — from this ONE call
// instead of N+1 GetPlanDetail fetches.
//
// N+1-free: it issues a CONSTANT number of queries regardless of plan count —
//  1. plans.ListByProject  → the project's plans.
//  2. tasks.ListByProject  → ALL project tasks once, grouped in-memory by PlanID
//     (unplanned tasks with an empty plan_id are skipped).
//  3. plans.ListDependenciesByPlans   → ALL plans' DAG edges in one IN(...) query.
//  4. plans.ListDispatchRecordsByPlans → ALL plans' dispatch records in one query.
//
// Then each plan's view is derived purely in-memory via ComputePlanView over its
// grouped tasks/edges/records — no per-plan repo round-trip (no 3×N N+1). The
// project-wide task list is ordered (created_at, id) identically to ListByPlan, so
// each plan's grouped task slice — and therefore its derived node order — matches
// what GetPlanDetail produces. §9.2: node status stays DERIVED, never stored.
func (s *Service) ListPlanSummaries(ctx context.Context, projectID pm.ProjectID) ([]*PlanDetail, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	plans, err := s.plans.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return []*PlanDetail{}, nil
	}

	planIDs := make([]pm.PlanID, 0, len(plans))
	for _, p := range plans {
		planIDs = append(planIDs, p.ID())
	}

	// 1 query: all project tasks, grouped by PlanID (skip unplanned). Iteration
	// preserves the (created_at, id) order so each group mirrors ListByPlan.
	allTasks, err := s.tasks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	tasksByPlan := make(map[pm.PlanID][]*pm.Task, len(plans))
	for _, t := range allTasks {
		pid := t.PlanID()
		if pid == "" {
			continue
		}
		tasksByPlan[pid] = append(tasksByPlan[pid], t)
	}

	// 1 query: all plans' DAG edges, grouped by PlanID.
	allEdges, err := s.plans.ListDependenciesByPlans(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	edgesByPlan := make(map[pm.PlanID][]pm.Dependency, len(plans))
	for _, e := range allEdges {
		edgesByPlan[e.PlanID] = append(edgesByPlan[e.PlanID], e)
	}

	// 1 query: all plans' dispatch records, grouped by PlanID.
	allRecords, err := s.plans.ListDispatchRecordsByPlans(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	recordsByPlan := make(map[pm.PlanID][]pm.DispatchRecord, len(plans))
	for _, rec := range allRecords {
		recordsByPlan[rec.PlanID] = append(recordsByPlan[rec.PlanID], rec)
	}

	// Per-plan view derivation is pure in-memory (no query).
	out := make([]*PlanDetail, 0, len(plans))
	for _, p := range plans {
		tasks := tasksByPlan[p.ID()]
		view := pm.ComputePlanView(tasks, edgesByPlan[p.ID()], recordsByPlan[p.ID()])
		out = append(out, &PlanDetail{Plan: p, Tasks: tasks, View: view})
	}
	return out, nil
}

package service

import (
	"context"
	"errors"
	"sort"
	"strings"

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

// GetIssueForMember reads an Issue GUARDED by project membership (v2.10.3 T170):
// the actor must be a member of the issue's project, else requireProjectMember's
// error surfaces (ErrNotMember → 403, ErrIssueNotFound → 404 if the issue is
// missing). This backs the RELAXED get_issue MCP tool — an agent that is a
// member of the issue's project may read ANY issue in it (the prior OQ4 own-link
// scope, which required holding a WorkItem for a task derived from the issue, was
// too tight: a PD/agent could not read sibling issues in its own project). The
// project-membership write-gate (#5a) now also gates issue reads, deliberately
// (owner-approved T170 relaxation).
func (s *Service) GetIssueForMember(ctx context.Context, id pm.IssueID, actor pm.IdentityRef) (*pm.Issue, error) {
	i, err := s.issues.FindByID(ctx, id)
	if err != nil {
		return nil, err // pm.ErrIssueNotFound when missing
	}
	if err := s.requireProjectMember(ctx, i.ProjectID(), actor); err != nil {
		return nil, err
	}
	return i, nil
}

// ListProjectIssuesForMember lists a project's issues, GUARDED by project
// membership (v2.10.3 T170, the Issue analogue of ListProjectTasksForMember).
// Backs the list_issues MCP tool; status/author filtering is applied by the caller.
func (s *Service) ListProjectIssuesForMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) ([]*pm.Issue, error) {
	if err := s.requireProjectMember(ctx, projectID, actor); err != nil {
		return nil, err
	}
	return s.issues.ListByProject(ctx, projectID)
}

// ListTasksDerivedFromIssueForMember lists the tasks DERIVED from an issue
// (task.DerivedFromIssue == issueID), GUARDED by membership of the issue's
// project (v2.10.3 T170). Backs the list_tasks_of_issue MCP tool — the reverse
// of create_task's derived_from_issue link, so an agent can see the executable
// work an issue spawned. A missing issue yields ErrIssueNotFound (404).
func (s *Service) ListTasksDerivedFromIssueForMember(ctx context.Context, issueID pm.IssueID, actor pm.IdentityRef) ([]*pm.Task, error) {
	i, err := s.issues.FindByID(ctx, issueID)
	if err != nil {
		return nil, err
	}
	if err := s.requireProjectMember(ctx, i.ProjectID(), actor); err != nil {
		return nil, err
	}
	all, err := s.tasks.ListByProject(ctx, i.ProjectID())
	if err != nil {
		return nil, err
	}
	out := make([]*pm.Task, 0, len(all))
	for _, t := range all {
		if t.DerivedFromIssue() == issueID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *Service) ListTasks(ctx context.Context, projectID pm.ProjectID) ([]*pm.Task, error) {
	return s.tasks.ListByProject(ctx, projectID)
}

// ListRelatedPlans returns the OTHER structured plans derived from the SAME source
// issue as planID (T581) — the data behind the plan-detail rail's "Related Plans"
// list. A cycle plan's generated nodes all carry derived_from_issue == the plan's
// source issue (scaffold_cycle_plan / T462), so the plan's issue is taken as the FIRST
// non-empty derived_from_issue among its tasks (cycle-plan nodes share one issue). The
// related set is then the DISTINCT non-builtin plans whose project tasks derive from
// that issue, EXCLUDING this plan, ordered (created_at, id) for a stable list. Empty
// when the plan has no source issue (no derived link) or no siblings. Membership-
// guarded on the plan's project (mirrors GetPlanDetail's read scope).
func (s *Service) ListRelatedPlans(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) ([]*pm.Plan, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	plan, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := s.requireProjectMember(ctx, plan.ProjectID(), actor); err != nil {
		return nil, err
	}
	planTasks, err := s.tasks.ListByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	var issueID pm.IssueID
	for _, t := range planTasks {
		if t.DerivedFromIssue() != "" {
			issueID = t.DerivedFromIssue()
			break
		}
	}
	if issueID == "" {
		return []*pm.Plan{}, nil // no source issue → nothing is "related" by issue
	}
	// Every project task derived from that issue → the distinct plans that own them,
	// EXCLUDING this plan (the rail lists siblings, not self).
	return s.plansDerivedFromIssue(ctx, plan.ProjectID(), issueID, planID)
}

// plansDerivedFromIssue returns the DISTINCT non-builtin plans whose project tasks
// derive from issueID, EXCLUDING excludePlan (pass "" to exclude none), ordered
// (created_at, id) for a stable list. The built-in assignment pool is never counted.
// Shared by ListRelatedPlans (plan rail, excludes self) and ListPlansForIssue (issue
// panel, excludes none) so the issue↔plan derive relationship is resolved in one place.
func (s *Service) plansDerivedFromIssue(ctx context.Context, projectID pm.ProjectID, issueID pm.IssueID, excludePlan pm.PlanID) ([]*pm.Plan, error) {
	projTasks, err := s.tasks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	seen := map[pm.PlanID]struct{}{}
	if excludePlan != "" {
		seen[excludePlan] = struct{}{}
	}
	out := make([]*pm.Plan, 0)
	for _, t := range projTasks {
		if t.DerivedFromIssue() != issueID {
			continue
		}
		pid := t.PlanID()
		if pid == "" {
			continue // a derived task not (yet) in a plan
		}
		if _, dup := seen[pid]; dup {
			continue
		}
		seen[pid] = struct{}{}
		p, ferr := s.plans.FindByID(ctx, pid)
		if ferr != nil {
			return nil, ferr
		}
		if p == nil || p.IsBuiltin() {
			continue // the built-in pool is never a "related plan"
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt().Equal(out[j].CreatedAt()) {
			return out[i].CreatedAt().Before(out[j].CreatedAt())
		}
		return out[i].ID() < out[j].ID()
	})
	return out, nil
}

// ListRelatedIssues returns the DISTINCT source issues this plan's tasks derive from —
// the plan-detail rail's "Related Issues" list (the issue-side mirror of the issue
// sidebar's Derived Tasks). A cycle plan's generated nodes carry derived_from_issue ==
// the plan's source issue (scaffold_cycle_plan / T462); a hand-built plan may mix tasks
// from several issues, so ALL distinct non-empty derived_from_issue values are resolved
// to their Issue and returned, ordered (created_at, id). A derived link to a since-
// deleted issue is skipped (not an error). Empty when no task derives from an issue.
// Membership-guarded on the plan's project (mirrors ListRelatedPlans' read scope).
func (s *Service) ListRelatedIssues(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) ([]*pm.Issue, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	plan, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, err
	}
	if err := s.requireProjectMember(ctx, plan.ProjectID(), actor); err != nil {
		return nil, err
	}
	planTasks, err := s.tasks.ListByPlan(ctx, planID)
	if err != nil {
		return nil, err
	}
	seen := map[pm.IssueID]struct{}{}
	out := make([]*pm.Issue, 0)
	for _, t := range planTasks {
		iid := t.DerivedFromIssue()
		if iid == "" {
			continue
		}
		if _, dup := seen[iid]; dup {
			continue
		}
		seen[iid] = struct{}{}
		iss, ferr := s.issues.FindByID(ctx, iid)
		if ferr != nil {
			if errors.Is(ferr, pm.ErrIssueNotFound) {
				continue // a derived link to a deleted issue → skip, don't fail the list
			}
			return nil, ferr
		}
		if iss == nil {
			continue
		}
		out = append(out, iss)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt().Equal(out[j].CreatedAt()) {
			return out[i].CreatedAt().Before(out[j].CreatedAt())
		}
		return out[i].ID() < out[j].ID()
	})
	return out, nil
}

// ListPlansForIssue returns the DISTINCT non-builtin plans derived from issueID — the
// issue-detail "Related Plans" panel (the plan-side mirror of the issue's Derived Tasks
// list, and the reverse of ListRelatedIssues). Membership-guarded on the issue's project
// (mirrors ListTasksDerivedFromIssueForMember). A missing issue → ErrIssueNotFound (404).
func (s *Service) ListPlansForIssue(ctx context.Context, issueID pm.IssueID, actor pm.IdentityRef) ([]*pm.Plan, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	iss, err := s.issues.FindByID(ctx, issueID)
	if err != nil {
		return nil, err // pm.ErrIssueNotFound when missing
	}
	if err := s.requireProjectMember(ctx, iss.ProjectID(), actor); err != nil {
		return nil, err
	}
	return s.plansDerivedFromIssue(ctx, iss.ProjectID(), issueID, "")
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

// ListCodeReposForMember lists a project's repo references, PROJECT-MEMBER gated
// (v2.18.4 BE-2 agent MCP list_project_repos). A non-member → ErrNotMember (403); a
// missing project → ErrProjectNotFound (404).
func (s *Service) ListCodeReposForMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) ([]*pm.CodeRepoRef, error) {
	if err := s.requireProjectMember(ctx, projectID, actor); err != nil {
		return nil, err
	}
	return s.codeRepoRefs.ListByProject(ctx, projectID)
}

// ResolveProjectRepoForMember resolves ONE of a project's repo references for the
// agent MCP get_repo_info (v2.18.4 BE-2), PROJECT-MEMBER gated. When repoID is set,
// it returns the reference whose repo_id matches (scoped to the project); when
// repoID is empty it returns the project's PRIMARY reference. ErrCodeRepoRefNotFound
// when no such reference exists.
func (s *Service) ResolveProjectRepoForMember(ctx context.Context, projectID pm.ProjectID, repoID string, actor pm.IdentityRef) (*pm.CodeRepoRef, error) {
	if err := s.requireProjectMember(ctx, projectID, actor); err != nil {
		return nil, err
	}
	refs, err := s.codeRepoRefs.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	repoID = strings.TrimSpace(repoID)
	for _, ref := range refs {
		if repoID != "" {
			if ref.RepoID() == repoID {
				return ref, nil
			}
			continue
		}
		if ref.IsPrimary() {
			return ref, nil
		}
	}
	return nil, pm.ErrCodeRepoRefNotFound
}

// GetCodeRepoRef reads one project↔repo reference by id (v2.18.4 BE-1).
func (s *Service) GetCodeRepoRef(ctx context.Context, id string) (*pm.CodeRepoRef, error) {
	return s.codeRepoRefs.FindByID(ctx, id)
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

// ListRunnableAgentTasks returns the open/running tasks assigned to `assignee` that
// are RUNNABLE right now — the agent's "what do I have to do?" queue in the Task
// model (v2.14.0 I14/F5 §五, MCP `list_my_tasks`, replacing get_my_work). It is the
// pull counterpart of the §13.A run-ahead gate: a task is included only when
// EnsureTaskRunnable passes (its blockedBy dependencies are satisfied — a node the
// plan engine has derived to ready/dispatched, or a task already running), so the
// agent never treats a dependency-blocked task as work it can start. Terminal tasks
// (completed/discarded) are history and omitted. A blocked task stays in the list
// (it is still running and assigned) so the agent sees the blocked_reason /
// blocked_comment an Unblock left for it.
//
// Cost is one ListByAssignee plus a per-task runnable derivation; EnsureTaskRunnable
// short-circuits a running task and is the single source of truth shared with the
// start gate, so the list and start_task never disagree on "runnable".
// AgentTaskLoads returns the per-assignee active-task split (Running "doing" +
// Pending "open") for the whole surface in ONE grouped query — the agent-load
// metric source (T342). Keyed by the task assignee ref ("agent:<id>" /
// "user:<id>"); the handler computes load = Running / (Running+Pending).
func (s *Service) AgentTaskLoads(ctx context.Context) (map[pm.IdentityRef]pm.AgentTaskLoad, error) {
	return s.tasks.CountActiveByAssignee(ctx)
}

func (s *Service) ListRunnableAgentTasks(ctx context.Context, assignee pm.IdentityRef) ([]*pm.Task, error) {
	tasks, err := s.tasks.ListByAssignee(ctx, assignee)
	if err != nil {
		return nil, err
	}
	out := make([]*pm.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Status() != pm.TaskOpen && t.Status() != pm.TaskRunning {
			continue // terminal (completed/discarded) — history, not actionable
		}
		if err := s.EnsureTaskRunnable(ctx, t.ID()); err != nil {
			if errors.Is(err, pm.ErrTaskNotRunnable) {
				continue // deps unsatisfied / dead branch — not yet pullable (§13.A)
			}
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ListAssignedAgentTasks returns ALL of an agent's active (open/running) tasks
// that are not in a terminal plan — the same set the "backlog: N" badge counts
// (pm.AgentTaskLoad). Unlike ListRunnableAgentTasks it does NOT drop tasks whose
// dependencies are unsatisfied: those are pending/queued work the agent is still
// assigned, they just are not pullable yet. The human-facing Agent-detail Tasks
// panel uses this so the list reconciles with the backlog count (a non-zero
// backlog with an empty Tasks tab was the @oopslink-reported mismatch); the
// agent-facing list_my_tasks keeps using ListRunnableAgentTasks ("what can I
// start now").
func (s *Service) ListAssignedAgentTasks(ctx context.Context, assignee pm.IdentityRef) ([]*pm.Task, error) {
	return s.tasks.ListActiveByAssignee(ctx, assignee)
}

// AgentFreedFromTask reports whether the given task no longer occupies its agent
// assignee's single-active slot — i.e. the agent is free to start its next task.
// True when the task is terminal (completed/discarded) OR a blocked, lease-free
// RUNNING task (Block keeps status=running but releases the §13.B single-active
// index, so the agent CAN start another). False for a plain running task (a fresh
// start/claim/resume) so the dispatch-wake re-push trigger does not misfire on
// open→running. Used by T465's re-push trigger (issue I34).
func (s *Service) AgentFreedFromTask(ctx context.Context, taskID pm.TaskID) (bool, error) {
	t, err := s.tasks.FindByID(ctx, taskID)
	if err != nil {
		return false, err
	}
	switch t.Status() {
	case pm.TaskCompleted, pm.TaskDiscarded:
		return true, nil
	case pm.TaskRunning:
		return t.BlockedReason() != "", nil // blocked running task frees the slot
	default:
		return false, nil
	}
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
	// T83 §3.2/§5: a built-in pool task is OPEN-claim (no assignee requirement),
	// so get_task.claimable matches what ClaimPoolTask will actually accept. A
	// structured-plan node stays assignee-gated.
	if p.IsBuiltin() {
		return pm.ClaimableInPool(t.IsArchived(), t.Status(), planID, ns), nil
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

// GetPlanDetailForMember loads a Plan's detail GUARDED by project membership
// (issue I44): the actor must be a member of the plan's project, else
// requireProjectMember's error surfaces (ErrNotMember → 403, ErrProjectNotFound →
// 404; a missing plan → ErrPlanNotFound → 404). This backs the RELAXED get_plan
// MCP tool — any member of the plan's project may read it. It closes the prior
// gap where the get_plan handler enforced only a plan-in-project name match (the
// caller's membership was never checked, so any agent on a worker could read a
// plan whose project_id + plan_id it could name). Mirrors GetIssueForMember.
func (s *Service) GetPlanDetailForMember(ctx context.Context, id pm.PlanID, actor pm.IdentityRef) (*PlanDetail, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	p, err := s.plans.FindByID(ctx, id)
	if err != nil {
		return nil, err // pm.ErrPlanNotFound when missing
	}
	if err := s.requireProjectMember(ctx, p.ProjectID(), actor); err != nil {
		return nil, err
	}
	return s.planDetail(ctx, p)
}

// PlanDetail bundles a Plan with its selected tasks and the DERIVED view (§9.2):
// per-node status, ready-set, has_failed, progress. The HTTP layer renders the
// Plan DTO (nodes + ready-set + has_failed + progress) from this.
type PlanDetail struct {
	Plan  *pm.Plan
	Tasks []*pm.Task
	View  pm.PlanView
	// Starved (v2.18.3 BE-2) maps a builtin-pool task id → true when it is STARVED:
	// it carries required_capabilities but no eligible online agent can take it (a
	// capability-supply gap), so the FE renders a "waiting for an eligible agent"
	// badge. Populated ONLY by the FE-facing reads (GetPlanDetail / plan summaries)
	// for builtin pool plans; nil on the internal planDetail path (the reconciler /
	// claim flow don't pay the directory read). A nil/absent entry ⇒ not starved.
	Starved map[pm.TaskID]bool
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
	detail, err := s.planDetail(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := s.fillStarved(ctx, detail); err != nil {
		return nil, err
	}
	return detail, nil
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
	paused, err := s.pausedSet(ctx, tasks)
	if err != nil {
		return nil, err
	}
	outcomes, err := s.plans.ListDecisionOutcomes(ctx, p.ID()) // B1 control-flow routing
	if err != nil {
		return nil, err
	}
	// T807 ④: read the plan view off DerivePlanView (the graph-era read-view derivation)
	// — the reader path no longer references ComputePlanView. Also covers the runnable
	// gate (planNodeStatus reads this detail's View). Byte-for-byte with the prior
	// ComputePlanView (same pure algorithm), over LIVE task/dep/outcome/dispatch state.
	return &PlanDetail{Plan: p, Tasks: tasks, View: pm.DerivePlanView(tasks, edges, records, outcomes, paused)}, nil
}

// pausedSet queries the optional PausedTaskPort (T53) for the given tasks' ids,
// returning the TaskID→true map the plan view overlays as `paused` nodes. nil-safe:
// no port wired or no tasks ⇒ nil (no overlay, running stays running). A port error
// is PROPAGATED so the read fails loudly rather than silently dropping the overlay
// (a stuck node mis-shown as running is exactly the bug being fixed).
func (s *Service) pausedSet(ctx context.Context, tasks []*pm.Task) (map[pm.TaskID]bool, error) {
	if s.pausedTasks == nil || len(tasks) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, string(t.ID()))
	}
	pausedIDs, err := s.pausedTasks.PausedTasks(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(pausedIDs) == 0 {
		return nil, nil
	}
	out := make(map[pm.TaskID]bool, len(pausedIDs))
	for id, p := range pausedIDs {
		if p {
			out[pm.TaskID(id)] = true
		}
	}
	return out, nil
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
	out, _, err := s.planSummaries(ctx, projectID, false, 0, 0)
	return out, err
}

// ListPlanSummariesPage paginates the project's (non-archived) plan summaries for
// the agent-facing list_plans tool. The page window is applied to the plan ROWS
// BEFORE the per-plan view derivation, so only the returned page is enriched (the
// builtin pool plan is included, matching ListPlanSummaries). Returns the page +
// the pre-page total. limit<=0 ⇒ no window (all rows).
func (s *Service) ListPlanSummariesPage(ctx context.Context, projectID pm.ProjectID, limit, offset int) ([]*PlanDetail, int, error) {
	return s.planSummaries(ctx, projectID, false, limit, offset)
}

// ListPlanSummariesIncludingArchived is the archived-aware variant (T124/T98): it
// returns ALL plans incl. archived, so a caller that applies its OWN status
// filter (the org Plan list's statusPasses — which default-excludes archived but
// surfaces them on `?status=archived`/`all`) can actually see archived plans. The
// default ListPlanSummaries still excludes archived (Work Board / agent-tools).
func (s *Service) ListPlanSummariesIncludingArchived(ctx context.Context, projectID pm.ProjectID) ([]*PlanDetail, error) {
	out, _, err := s.planSummaries(ctx, projectID, true, 0, 0)
	return out, err
}

// ListIssuesOrgPage / ListTasksOrgPage / ListOrgPlansPage are the server-side
// paginated cross-project reads behind the org Issues/Tasks/Plans list pages:
// filter + sort + LIMIT/OFFSET + COUNT pushed into SQL (real pagination, no
// handler-side aggregate-then-slice). The handler resolves the project-id set +
// status default and passes them in q; the repo returns the page + total.

func (s *Service) ListIssuesOrgPage(ctx context.Context, q pm.OrgListQuery) ([]*pm.Issue, int, error) {
	return s.issues.ListOrgPage(ctx, q)
}

// ListProjectIssuesPageForMember paginates a SINGLE project's issues (SQL
// LIMIT/OFFSET + COUNT) behind the §5.7 project-member guard — the paged read
// path for the agent-facing list_issues tool (replaces the unbounded
// ListProjectIssuesForMember). q.ProjectIDs is forced to [projectID]; the caller
// supplies status/author filters + page window.
func (s *Service) ListProjectIssuesPageForMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef, q pm.OrgListQuery) ([]*pm.Issue, int, error) {
	if err := s.requireProjectMember(ctx, projectID, actor); err != nil {
		return nil, 0, err
	}
	q.ProjectIDs = []pm.ProjectID{projectID}
	return s.issues.ListOrgPage(ctx, q)
}

func (s *Service) ListTasksOrgPage(ctx context.Context, q pm.OrgListQuery) ([]*pm.Task, int, error) {
	return s.tasks.ListOrgPage(ctx, q)
}

// ListProjectTasksPageForMember paginates a SINGLE project's tasks (SQL
// LIMIT/OFFSET + COUNT) behind the §5.7 project-member guard — the paged read
// path for the agent-facing list_tasks tool. It replaces the unbounded
// ListProjectTasksForMember (which returned the whole board) so a busy project's
// task history can't blow the MCP tool-result token cap. q.ProjectIDs is forced
// to [projectID]; the caller supplies status/assignee filters + page window.
func (s *Service) ListProjectTasksPageForMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef, q pm.OrgListQuery) ([]*pm.Task, int, error) {
	if err := s.requireProjectMember(ctx, projectID, actor); err != nil {
		return nil, 0, err
	}
	q.ProjectIDs = []pm.ProjectID{projectID}
	return s.tasks.ListOrgPage(ctx, q)
}

// ListOrgPlansPage SQL-paginates the NON-builtin base plan rows across the
// query's projects, then enriches ONLY the returned page with the derived view
// (progress / has_failed) via planDetail. Returns the page's PlanDetails + the
// total (pre-page) count.
func (s *Service) ListOrgPlansPage(ctx context.Context, q pm.OrgListQuery) ([]*PlanDetail, int, error) {
	if s.plans == nil {
		return nil, 0, ErrPlansUnavailable
	}
	plans, total, err := s.plans.ListOrgPage(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]*PlanDetail, 0, len(plans))
	for _, p := range plans {
		d, derr := s.planDetail(ctx, p)
		if derr != nil {
			return nil, 0, derr
		}
		out = append(out, d)
	}
	return out, total, nil
}

func (s *Service) planSummaries(ctx context.Context, projectID pm.ProjectID, includeArchived bool, limit, offset int) ([]*PlanDetail, int, error) {
	if s.plans == nil {
		return nil, 0, ErrPlansUnavailable
	}
	plans, err := s.plans.ListByProject(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	// v2.9.2 (task-1099941e): the Work Board EXCLUDES archived plans by default —
	// an archived plan leaves the active board, mirroring project (#310) / channel
	// archive semantics. Filtered here (the single shared read both list mirrors —
	// web + agent-tools — go through), so neither surface leaks an archived plan,
	// and an archived plan's tasks/edges/records aren't even derived below. T124:
	// includeArchived keeps them (the org list's own statusPasses then filters). A
	// dedicated archived-plans view, if added, would use a separate read path.
	if !includeArchived {
		active := plans[:0]
		for _, p := range plans {
			if p.Status() == pm.PlanArchived {
				continue
			}
			active = append(active, p)
		}
		plans = active
	}
	// total = matching plan rows BEFORE the page window; the window is applied to
	// the rows so only the returned page gets the (per-plan) view derivation below.
	total := len(plans)
	if total == 0 {
		return []*PlanDetail{}, 0, nil
	}
	if limit > 0 {
		lo := offset
		if lo < 0 {
			lo = 0
		}
		if lo > len(plans) {
			lo = len(plans)
		}
		hi := lo + limit
		if hi > len(plans) {
			hi = len(plans)
		}
		plans = plans[lo:hi]
	}
	if len(plans) == 0 { // offset past the end → empty page, real total
		return []*PlanDetail{}, total, nil
	}

	planIDs := make([]pm.PlanID, 0, len(plans))
	for _, p := range plans {
		planIDs = append(planIDs, p.ID())
	}

	// 1 query: all project tasks, grouped by PlanID (skip unplanned). Iteration
	// preserves the (created_at, id) order so each group mirrors ListByPlan.
	allTasks, err := s.tasks.ListByProject(ctx, projectID)
	if err != nil {
		return nil, 0, err
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
		return nil, 0, err
	}
	edgesByPlan := make(map[pm.PlanID][]pm.Dependency, len(plans))
	for _, e := range allEdges {
		edgesByPlan[e.PlanID] = append(edgesByPlan[e.PlanID], e)
	}

	// 1 query: all plans' dispatch records, grouped by PlanID.
	allRecords, err := s.plans.ListDispatchRecordsByPlans(ctx, planIDs)
	if err != nil {
		return nil, 0, err
	}
	recordsByPlan := make(map[pm.PlanID][]pm.DispatchRecord, len(plans))
	for _, rec := range allRecords {
		recordsByPlan[rec.PlanID] = append(recordsByPlan[rec.PlanID], rec)
	}

	// 1 query (B1 control-flow): all plans' decision outcomes, grouped by PlanID —
	// so conditional/loopback routing is reflected in the summary view too (N+1-free).
	allOutcomes, err := s.plans.ListDecisionOutcomesByPlans(ctx, planIDs)
	if err != nil {
		return nil, 0, err
	}
	outcomesByPlan := make(map[pm.PlanID][]pm.DecisionOutcome, len(plans))
	for _, o := range allOutcomes {
		outcomesByPlan[o.PlanID] = append(outcomesByPlan[o.PlanID], o)
	}

	// 1 query (T53): which of ALL project tasks have a paused work item — one map
	// reused across every plan's pure derivation, so the N+1-free guarantee holds
	// (a single extra port call regardless of plan count). nil when no port wired.
	paused, err := s.pausedSet(ctx, allTasks)
	if err != nil {
		return nil, 0, err
	}

	// Per-plan view derivation is pure in-memory (no query).
	out := make([]*PlanDetail, 0, len(plans))
	for _, p := range plans {
		tasks := tasksByPlan[p.ID()]
		// T807 ④: list enrich reads the plan view off DerivePlanView (no ComputePlanView
		// in the reader path); byte-for-byte with the prior derivation.
		view := pm.DerivePlanView(tasks, edgesByPlan[p.ID()], recordsByPlan[p.ID()], outcomesByPlan[p.ID()], paused)
		detail := &PlanDetail{Plan: p, Tasks: tasks, View: view}
		// v2.18.3 BE-2: the Work Board pool card shows the starved badge → fill the
		// starved set for the builtin pool plan (a no-op for every structured plan, and
		// at most ONE directory read per project since a project has one builtin pool).
		if err := s.fillStarved(ctx, detail); err != nil {
			return nil, 0, err
		}
		out = append(out, detail)
	}
	return out, total, nil
}

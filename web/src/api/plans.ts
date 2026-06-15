import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Task } from './types';

// Plans — v2.9 Plan Orchestration P1 (#286 foundation + backlog→Plan selection).
//
// A Plan groups project backlog tasks into a depends_on DAG. Project-scoped:
// every read/write nests under /projects/{project_id}/plans (mirrors the v2.7
// Task BC hook idiom in tasks.ts — apiClient + react-query, namespaced keys).
//
// mock=contract day-0: these types + paths track the LOCKED v2.9 backend
// contract (backend by @AgentCenterDev in parallel). The MSW handlers in
// src/mocks/handlers.ts implement the SAME shapes so the UI is verifiable now;
// verify against the real endpoint once it lands.

// ---------------------------------------------------------------------------
// Contract types
// ---------------------------------------------------------------------------

// Plan lifecycle (§2 / §9.1). draft = DAG editable; running = orchestrating;
// done ⟺ every node done. A failed node keeps the Plan `running` and surfaces
// as the derived `has_failed` flag — the Plan never auto-enters a terminal
// failed in v1.
//
// archived (v2.9 Stage B / #290) is a TERMINAL state reached via the explicit
// ArchivePlan action (POST .../archive, non-running only). It is IRREVERSIBLE:
// archiving cascades plan→archived + ALL plan tasks→archived (task.status is
// preserved — orthogonal). An archived plan is still GET-able (read-only); a
// re-archive → 409 ErrPlanArchived.
export type PlanStatus = 'draft' | 'running' | 'done' | 'archived';

// Plan-node status (§9.2) — DERIVED by the orchestrator, never stored as a
// competing field. blocked (some upstream not done) → ready (all upstream done,
// not yet dispatched) → dispatched/running (mention posted; task in progress) →
// done / failed (mirror the task terminal).
export type PlanNodeStatus =
  | 'blocked'
  | 'ready'
  | 'dispatched'
  | 'running'
  | 'paused' // T53: running task whose agent paused its work item (set aside)
  | 'done'
  | 'failed';

// PlanNode (§9.2) — a task's projection inside a Plan's DAG. `depends_on` is the
// list of upstream task ids this node depends on (those complete first).
export interface PlanNode {
  task_id: string;
  title: string;
  assignee_ref: string;
  task_status: string;
  node_status: PlanNodeStatus;
  depends_on: string[];
  dispatched_at?: string | null;
  // v2.9.2 (task-0543ece9): the human Task id ("T123", org_ref) now rides on the
  // node DTO so the Work Board card + agent-facing list show the T-number WITHOUT
  // a second task-list resolver. Omitted when unallocated (pre-allocator rows) —
  // the card falls back to the #id-tail handle, the established #192 pattern.
  org_ref?: string;
  // v2.9 Stage B (#283): the plan task DTO (pmTaskMap) now carries an `archived`
  // flag (+ audit fields) set when the plan is archived. ORTHOGONAL to task_status
  // / node_status — the archive badge reads `archived` and coexists with the
  // status chip. Optional so a pre-archive / not-yet-enriched node is assignable.
  archived?: boolean;
  archived_at?: string | null;
  archived_by?: string | null;
  // ADR-0047: a COMPUTED flag on a built-in assignment-pool node — true when the
  // task is assigned+dispatched and so CLAIMABLE by its assignee (pull, no-wake).
  // Backend-derived; absent / false on backlog + structured-plan nodes. The Work
  // Board renders a "claimable" chip on a pool node when this is true.
  claimable?: boolean;
}

// PlanEdge — a directed dependency edge. `from` (the dependent / downstream
// task) depends on `to` (the depended / upstream task); `to` completes first.
export interface PlanEdge {
  from_task_id: string;
  to_task_id: string;
}

// Plan DTO. `progress` + `has_failed` are derived (§9.1).
//
// Two read shapes carry derived nodes (both via the SAME backend pmPlanNodeMap
// helper, so a node is byte-identical between them — verified vs merged PR #272
// → v2.9 trunk 654d30e):
//   • detail (GET /{id})  → `nodes`: the FULL DAG (every PlanNode).
//   • list   (GET /)      → `nodes_preview`: EVERY PlanNode (v2.9.2 task-0543ece9
//                            removed the old 4-node cap — the board card no longer
//                            silently truncates), plus `node_count` (== the node
//                            count). `node_count` is kept so a degraded/partial
//                            payload that sends fewer preview nodes still drives an
//                            "…and M more" overflow hint (belt-and-braces).
// Both are optional on the type so either response is assignable; the Work Board
// (#291) reads the list pair (nodes_preview / node_count) and the Plan detail
// (#287) reads `nodes`. Field names match the real DTO EXACTLY.
export interface Plan {
  id: string;
  project_id: string;
  name: string;
  description: string;
  status: PlanStatus;
  creator_ref: string;
  conversation_id: string;
  // v2.10.1 [T99]: the human Plan id ("P123", org-scoped org_ref). Optional —
  // omitted for the builtin pool + rows predating the allocator (UI falls back
  // to the #id-tail handle).
  org_ref?: string;
  target_date?: string | null;
  has_failed: boolean;
  progress: { done: number; total: number };
  created_at: string;
  // v2.9 Stage B (#290): set when the plan reaches the terminal archived state.
  // Optional — only an archived plan carries them.
  archived_at?: string | null;
  archived_by?: string | null;
  // ADR-0047: exactly one plan per project is the BUILT-IN assignment pool
  // (name "[Built-in]"). Flat (no DAG edges), always running, "pull, no-wake":
  // its assigned+dispatched nodes are CLAIMABLE. The Work Board renders the
  // is_builtin plan as a DISTINCT segment, NOT a generic structured-plan column.
  // Optional so a legacy / pre-ADR-0047 payload (no flag) treats every plan as
  // structured.
  is_builtin?: boolean;
  // detail read (GET /{id}) — full DAG.
  nodes?: PlanNode[];
  // list read (GET /) — capped preview + total count (enriched PR #272).
  nodes_preview?: PlanNode[];
  node_count?: number;
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

const plansBase = (projectId: string) => `/projects/${projectId}/plans`;

// GET / — the parallel Plan list for a project. Response wrapped under `plans`
// (mirrors the Task list `{ tasks: [] }` convention). Each row is enriched
// (PR #272): progress + has_failed + node_count + nodes_preview (capped 4).
export function usePlans(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.plansByProject(projectId ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ plans: Plan[] }>(plansBase(projectId ?? ''));
      return resp.plans;
    },
    enabled: !!projectId,
  });
}

// ---------------------------------------------------------------------------
// v2.10.0 [T6] — global (org-scoped, cross-project) Plan list.
// GET /api/orgs/{slug}/plans → { items: OrgPlanItem[], total }. Mirrors the
// org-scoped Issues/Tasks aggregation: the /orgs/{slug} segment is auto-injected
// by the api client, so the hook just calls /plans. Each row is a plan summary
// (progress/has_failed/node_count) PLUS project{id,name} + updated_at for the
// cross-project list + the detail link. Excludes the builtin assignment pool.
// ---------------------------------------------------------------------------

// An org Plan list row = the plan summary enriched with its project context and
// updated_at (the base Plan DTO omits updated_at; the org list needs it for the
// "Updated" column + the updated_at-DESC order).
export interface OrgPlanItem extends Plan {
  project: { id: string; name: string };
  updated_at: string;
}

export interface OrgPlanFilters {
  /** project ids (multi) — narrow the aggregation to specific projects. */
  project?: string[];
  /** status values (multi). Omitted = backend default (excludes archived). */
  status?: string[];
}

function buildOrgPlanQuery(f?: OrgPlanFilters): string {
  if (!f) return '';
  const p = new URLSearchParams();
  for (const id of f.project ?? []) p.append('project', id);
  for (const s of f.status ?? []) p.append('status', s);
  const s = p.toString();
  return s ? `?${s}` : '';
}

export function useOrgPlans(slug: string | undefined, filters?: OrgPlanFilters) {
  return useQuery({
    queryKey: qk.orgPlans({ slug, filters }),
    // org_slug auto-injected by the client; slug only scopes the cache key + gate.
    queryFn: () => api.get<{ items: OrgPlanItem[]; total: number }>(`/plans${buildOrgPlanQuery(filters)}`),
    enabled: !!slug,
  });
}

// GET /projects/{pid}/tasks?unplanned=1 — the Backlog column source (v2.9 #291
// Work Board). Returns ONLY the project tasks with NO plan (plan_id null), org-
// gated (Dev's endpoint). Same wrapped `{ tasks: Task[] }` shape as the full
// project task list (useTasksList) — mock=contract; VERIFY the real endpoint
// honours `?unplanned=1` + returns the identical Task shape once it lands.
export function useUnplannedTasks(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.unplannedTasksByProject(projectId ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ tasks: Task[] }>(
        `/projects/${projectId}/tasks?unplanned=1`,
      );
      return resp.tasks;
    },
    enabled: !!projectId,
  });
}

// GET /{id} — a single Plan with its derived nodes + DAG.
export function usePlan(projectId: string | undefined, planId: string | undefined) {
  return useQuery({
    queryKey: qk.plan(planId ?? ''),
    queryFn: () => api.get<Plan>(`${plansBase(projectId ?? '')}/${planId}`),
    enabled: !!projectId && !!planId,
  });
}

// ---------------------------------------------------------------------------
// Writes
// ---------------------------------------------------------------------------

// POST / — create an empty Plan (name + optional goal/target_date). The DAG is
// populated afterwards by selecting backlog tasks (#286 step 3).
export interface CreatePlanInput {
  name: string;
  description?: string;
  target_date?: string | null;
}

export function useCreatePlan(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreatePlanInput) =>
      api.post<Plan>(plansBase(projectId), input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.plansByProject(projectId) });
    },
  });
}

// PATCH /{id} — edit name / goal / target_date. draft-only (the backend rejects
// edits to a running Plan, §9.4); send only the changed fields.
export interface PatchPlanInput {
  name?: string;
  description?: string;
  target_date?: string | null;
}

export function usePatchPlan(projectId: string, planId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: PatchPlanInput) =>
      api.patch<Plan>(`${plansBase(projectId)}/${planId}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.plan(planId) });
      void qc.invalidateQueries({ queryKey: qk.plansByProject(projectId) });
    },
  });
}

// Shared invalidation for the per-Plan write actions (add/remove task, deps,
// lifecycle): refresh both the single Plan (derived nodes) and the parallel list
// (progress / status). Task ↔ Plan is 0..1, so adding/removing also changes
// which tasks are "backlog" — invalidate the project task list too.
function invalidatePlanWrite(
  qc: ReturnType<typeof useQueryClient>,
  projectId: string,
  planId: string,
) {
  void qc.invalidateQueries({ queryKey: qk.plan(planId) });
  void qc.invalidateQueries({ queryKey: qk.plansByProject(projectId) });
  void qc.invalidateQueries({ queryKey: qk.tasksByProject(projectId) });
  // v2.9 #291: add/remove-task changes the Backlog (unplanned) set too.
  void qc.invalidateQueries({ queryKey: qk.unplannedTasksByProject(projectId) });
}

function usePlanWrite<TVars, TResult>(
  projectId: string,
  planId: string,
  fn: (vars: TVars) => Promise<TResult>,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => invalidatePlanWrite(qc, projectId, planId),
  });
}

// POST /{id}/tasks { task_id } — select a backlog task into the Plan (#286).
export function useAddTaskToPlan(projectId: string, planId: string) {
  return usePlanWrite<{ task_id: string }, Plan>(projectId, planId, (vars) =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/tasks`, vars),
  );
}

// DELETE /{id}/tasks/{task_id} — remove a task from the Plan (back to backlog).
export function useRemoveTaskFromPlan(projectId: string, planId: string) {
  return usePlanWrite<string, void>(projectId, planId, (taskId) =>
    api.del(`${plansBase(projectId)}/${planId}/tasks/${taskId}`),
  );
}

// A7 (Work Board cross-column task drag): the SAME select/remove ops, but the
// target/source plan is only known at DROP time (any draft plan, or the source
// plan a card was dragged out of). React forbids calling a per-plan hook
// conditionally per drop, so these variants take the planId as a MUTATION
// VARIABLE rather than a closure — one stable hook drives drops to/from any
// plan. Backed by the identical endpoints + the shared usePlanWrite
// invalidation (plan / list / tasks / unplanned), so a drag-move and the
// keyboard add/remove buttons converge on the same refetch.
export function useAddTaskToAnyPlan(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ planId, taskId }: { planId: string; taskId: string }) =>
      api.post<Plan>(`${plansBase(projectId)}/${planId}/tasks`, { task_id: taskId }),
    onSuccess: (_d, { planId }) => invalidatePlanWrite(qc, projectId, planId),
  });
}

export function useRemoveTaskFromAnyPlan(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ planId, taskId }: { planId: string; taskId: string }) =>
      api.del(`${plansBase(projectId)}/${planId}/tasks/${taskId}`),
    onSuccess: (_d, { planId }) => invalidatePlanWrite(qc, projectId, planId),
  });
}

// ---------------------------------------------------------------------------
// #287 (DAG view) hooks — dependency edits + lifecycle. Stubbed here so the
// contract surface is complete and the keys/invalidation stay in one place;
// #287 builds the DAG UI on top of these.
// ---------------------------------------------------------------------------

// POST /{id}/dependencies { from_task_id, to_task_id } — add a DAG edge
// (from depends on to). DELETE removes it.
export function useAddDependency(projectId: string, planId: string) {
  return usePlanWrite<PlanEdge, Plan>(projectId, planId, (vars) =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/dependencies`, vars),
  );
}

export function useRemoveDependency(projectId: string, planId: string) {
  return usePlanWrite<PlanEdge, void>(projectId, planId, (vars) =>
    api.del(
      `${plansBase(projectId)}/${planId}/dependencies` +
        `?from_task_id=${encodeURIComponent(vars.from_task_id)}` +
        `&to_task_id=${encodeURIComponent(vars.to_task_id)}`,
    ),
  );
}

// Lifecycle (§9.4): start (draft→running), stop (running→draft), advance
// (manual orchestrator tick in P1, no auto-orchestrator).
export function useStartPlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/start`),
  );
}

export function useStopPlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/stop`),
  );
}

export function useAdvancePlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/advance`),
  );
}

// T53: operator recovery — resume a `paused` plan node (its agent set the work
// item aside and went idle). Resumes the node's work item + wakes the agent;
// returns the refreshed plan so the DAG reflects the node leaving `paused`.
export function useResumePausedNode(projectId: string, planId: string) {
  return usePlanWrite<string, Plan>(projectId, planId, (taskId) =>
    api.post<Plan>(
      `${plansBase(projectId)}/${planId}/nodes/${encodeURIComponent(taskId)}/resume`,
    ),
  );
}

// ---------------------------------------------------------------------------
// v2.9 Stage B (#280/#283/#290) — DESTRUCTIVE plan lifecycle: Delete + Archive.
// Both are non-running-only (the backend rejects a running plan with 409
// plan_conflict) and IRREVERSIBLE. Each goes through usePlanWrite so it shares
// the plan / plansByProject / tasks / unplanned invalidation:
//   • Delete unloads ALL the plan's tasks back to the Backlog (→ tasks +
//     unplanned must refetch) + cascade-deletes the conversation + deletes the
//     plan. The plan itself is GONE, so the caller navigates away on success.
//   • Archive flips the plan + ALL its tasks to archived (→ plan + list refetch
//     so the archived chip/badge show); the plan stays GET-able (read-only).
// ---------------------------------------------------------------------------

// DELETE /{id} → { deleted: true }. IRREVERSIBLE. Only a NON-running plan
// (running → 409 plan_conflict). On success the plan no longer exists — the
// caller must navigate away (the detail route would 404).
export function useDeletePlan(projectId: string, planId: string) {
  return usePlanWrite<void, { deleted: boolean }>(projectId, planId, () =>
    api.del<{ deleted: boolean }>(`${plansBase(projectId)}/${planId}`),
  );
}

// POST /{id}/archive → the archived plan detail. IRREVERSIBLE (archived is
// terminal; re-archive → 409 ErrPlanArchived). Only a NON-running plan
// (running → 409). Cascade: plan→archived + ALL plan tasks→archived (task.status
// preserved). The plan stays readable, so the caller can stay/refresh.
export function useArchivePlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/archive`),
  );
}

// #218 friendly error for the destructive 409s (running / already-archived).
// STATUS-AGNOSTIC: match by MESSAGE substring (mirrors friendlyDependencyError),
// never the raw API error. Shared by the Delete + Archive confirm-modals.
export function friendlyDestructivePlanError(error: unknown): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  // v2.9 #299: ErrPlanHasRunningTasks ("…plan has running tasks — complete or
  // stop them before archiving") guards MEMBER-TASK state and is DISTINCT from
  // ErrPlanRunning (plan-state). Its "running tasks" substring also contains
  // bare "running", so this match MUST come FIRST or it would mis-label as the
  // plan-is-running message.
  if (lower.includes('running task')) {
    return 'This plan has running tasks — wait for them to finish or stop the plan first.';
  }
  if (lower.includes('running')) {
    return 'This plan is running. Stop it first, then try again.';
  }
  if (lower.includes('archiv')) {
    return 'This plan is already archived.';
  }
  return "Couldn't complete that action. Please try again.";
}

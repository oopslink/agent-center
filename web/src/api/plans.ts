import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

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
export type PlanStatus = 'draft' | 'running' | 'done';

// Plan-node status (§9.2) — DERIVED by the orchestrator, never stored as a
// competing field. blocked (some upstream not done) → ready (all upstream done,
// not yet dispatched) → dispatched/running (mention posted; task in progress) →
// done / failed (mirror the task terminal).
export type PlanNodeStatus =
  | 'blocked'
  | 'ready'
  | 'dispatched'
  | 'running'
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
}

// PlanEdge — a directed dependency edge. `from` (the dependent / downstream
// task) depends on `to` (the depended / upstream task); `to` completes first.
export interface PlanEdge {
  from_task_id: string;
  to_task_id: string;
}

// Plan DTO. `nodes` are present on the single-Plan read (GET /{id}); the list
// read (GET /) may omit them. `progress` + `has_failed` are derived (§9.1).
export interface Plan {
  id: string;
  project_id: string;
  name: string;
  description: string;
  status: PlanStatus;
  creator_ref: string;
  conversation_id: string;
  target_date?: string | null;
  has_failed: boolean;
  progress: { done: number; total: number };
  created_at: string;
  nodes?: PlanNode[];
}

// ---------------------------------------------------------------------------
// Reads
// ---------------------------------------------------------------------------

const plansBase = (projectId: string) => `/projects/${projectId}/plans`;

// GET / — the parallel Plan list for a project. Response wrapped under `plans`
// (mirrors the Task list `{ tasks: [] }` convention). nodes may be omitted in
// the list shape.
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
function usePlanWrite<TVars, TResult>(
  projectId: string,
  planId: string,
  fn: (vars: TVars) => Promise<TResult>,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.plan(planId) });
      void qc.invalidateQueries({ queryKey: qk.plansByProject(projectId) });
      void qc.invalidateQueries({ queryKey: qk.tasksByProject(projectId) });
    },
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

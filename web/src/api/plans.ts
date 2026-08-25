import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Issue, Task, TaskStatus } from './types';

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

// Plan lifecycle (ADR-0055): pending → running ↔ paused → done; done may reopen
// to paused for follow-up generation evolution. Discarded history is permanent.
// Archive is an orthogonal retention marker on a terminal plan; it never replaces
// the lifecycle status.
export type PlanStatus = 'pending' | 'running' | 'paused' | 'done' | 'discarded';

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
  effective?: boolean;
  superseded_by?: string[];
  superseded_reason?: string;
  // The underlying task's creation time (RFC3339). Always emitted by the backend
  // node DTO (pmPlanNodeMap); optional here for legacy payloads. The Plan detail
  // task list shows it in a "Created" column (full local timestamp + tz).
  created_at?: string;
  dispatched_at?: string | null;
  // T570: when node_status is 'done', the task's completion time (statusChangedAt
  // of the terminal transition). Present only on done nodes — the task list shows
  // it beside the DONE chip. Absent for live/blocked nodes.
  completed_at?: string | null;
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
  // T566 (issue-577a7b0e): set by the auto-assign reconciler (BE-2) on a
  // claimable pool task that declares required_capabilities but has NO eligible
  // online agent right now — it stays in the pool ("starved"). The Work Board
  // shows a "waiting for a qualified agent" badge. Absent/false when not starved.
  // Contract locked with PD: field name `starved` on the pool node DTO.
  starved?: boolean;
  blocked_on?: PlanBlockedOn;
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
  version?: number;
  creator_ref: string;
  conversation_id: string;
  // v2.10.1 [T99]: the human Plan id ("P123", org-scoped org_ref). Optional —
  // omitted for the builtin pool + rows predating the allocator (UI falls back
  // to the #id-tail handle).
  org_ref?: string;
  target_date?: string | null;
  has_failed: boolean;
  historical_failures?: string[];
  active_failures?: string[];
  progress: { done: number; total: number };
  created_at: string;
  // The backend plan map (pmPlanMap) serializes updated_at; the base Plan DTO had
  // historically omitted it. Optional so older typed call sites stay valid; the
  // project-detail "recent activity" feed reads it (falling back to created_at).
  updated_at?: string;
  // v2.9 Stage B (#290): set when the plan reaches the terminal archived state.
  // Optional — only an archived plan carries them.
  archived_at?: string | null;
  archived_by?: string | null;
  // Read-only migration marker for legacy rows. New AssignmentPools use their
  // own endpoint and never appear in the Plans collection.
  is_builtin?: boolean;
  // detail read (GET /{id}) — full DAG.
  nodes?: PlanNode[];
  // list read (GET /) — capped preview + total count (enriched PR #272).
  nodes_preview?: PlanNode[];
  node_count?: number;
  gate_verdicts?: GateVerdict[];
  continuations?: PlanContinuation[];
  active_generation_id?: string;
  owner_ref?: string;
  backup_owner_ref?: string;
  attention_required?: boolean;
  recovery_policy?: PlanRecoveryPolicy;
  ready_set?: string[];
  frontier?: PlanFrontier;
  pending_decisions?: PlanBlockedOn[];
}

export interface PlanRecoveryPolicy {
  notify_after_seconds?: number;
  remind_after_seconds?: number;
  escalate_after_seconds?: number;
}

export interface PlanBlockedOn {
  task_id: string;
  node_id?: string;
  wait_type?: string;
  wait_keys?: string[];
  trigger_condition?: string;
  waited_since?: string;
  deadline?: string;
  on_timeout?: string;
  last_probe_at?: string;
  probe_count?: number;
  blocked_by?: string;
  reason_type?: string;
}

export interface PlanFrontierGroup {
  wait_type: string;
  count: number;
  items: PlanBlockedOn[];
}

export interface PlanFrontier {
  total: number;
  groups: PlanFrontierGroup[];
}

export interface GateVerdict {
  id: string;
  project_id: string;
  plan_id: string;
  stage_id: string;
  gate_task_id: string;
  outcome: 'pass' | 'reject';
  evidence: string;
  reviewed_sha: string;
  actor_ref: string;
  idempotency_key: string;
  created_at: string;
}

export type ContinuationStatus = 'awaiting_remediation' | 'executing' | 'budget_exhausted' | 'closed';

export interface PlanContinuation {
  id: string;
  project_id: string;
  plan_id: string;
  root_stage_id: string;
  current_stage_id: string;
  trigger_verdict_id: string;
  status: ContinuationStatus;
  generation: number;
  remaining_budget: number;
  boundary_fingerprint: string;
  pending_proposal_id?: string;
  closed_by_verdict_id?: string;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface AssignmentPoolTask extends Task {
  priority: number;
  claimable: boolean;
  starved?: boolean;
  archived?: boolean;
  archived_at?: string | null;
  archived_by?: string | null;
  claimed_by?: string;
  claim_expires_at?: string;
}

export interface AssignmentPool {
  id: string;
  project_id: string;
  scheduling_class: 'background' | string;
  auto_assign_enabled: boolean;
  holding_cap: number;
  tasks: AssignmentPoolTask[];
}

// ---------------------------------------------------------------------------
// T769 — orchestration-engine GRAPH read model (plan-detail DAG).
//
// The plan-detail DAG historically reconstructed Start/End anchors + depends_on
// edges client-side from the derived PlanNode list. T768 wired a real engine graph
// behind every started plan; GET …/plans/{id}/graph exposes it so the DAG reflects
// the ACTUAL graph — real control nodes (Start/End/Condition) and edges tagged by
// kind (seq/conditional/loopback).
//
// NON-BREAKING: a plan with NO graph (pre-T768 / pending / never-started, or the
// engine unwired) returns { has_graph: false } and the DAG falls back to the
// legacy PlanNode/depends_on rendering — zero regression.
// ---------------------------------------------------------------------------

// A control node sub-kind. Business nodes carry no control_kind.
export type PlanGraphControlKind = 'start' | 'end' | 'condition';

// Raw engine node status (distinct from the derived 6-state PlanNodeStatus). The
// DAG maps a business node's bound task back to its PlanNode for the 6-state chip;
// control nodes render from this raw status.
export type PlanGraphNodeStatus = 'open' | 'running' | 'completed' | 'reopen' | 'discarded';

// One graph node. category business|control; control nodes carry control_kind.
// A business node binds a task — task_id + the bound task's status/org_ref ride
// along so the DAG can label it without a second lookup.
export interface PlanGraphNode {
  id: string;
  category: 'business' | 'control';
  control_kind?: PlanGraphControlKind;
  title: string;
  status: PlanGraphNodeStatus;
  task_id?: string;
  task_status?: string;
  org_ref?: string;
  assignee_ref?: string;
}

// One directed edge from→to, tagged by kind.
export type PlanGraphEdgeKind = 'seq' | 'conditional' | 'loopback';
export interface PlanGraphEdge {
  from: string;
  to: string;
  kind: PlanGraphEdgeKind;
}

// The graph read response. `has_graph:false` = the fallback signal (no fields
// beyond the flag); `has_graph:true` carries the graph.
export interface PlanGraphView {
  has_graph: boolean;
  graph_id?: string;
  status?: string;
  nodes?: PlanGraphNode[];
  edges?: PlanGraphEdge[];
}

// ---------------------------------------------------------------------------
// Immutable PlanGeneration ledger. IDs, parent lineage, diff, and snapshots are
// persisted domain facts; revision is only a zero-based presentation position.
// ---------------------------------------------------------------------------

export type PlanGenerationNodeAction = 'preserve' | 'hold_at_gate' | 'supersede';

export interface PlanGenerationNodeDecision {
  task_id: string;
  action: PlanGenerationNodeAction;
  reason?: string;
}

export interface PlanGenerationTaskDraft {
  ref: string;
  title: string;
  description?: string;
  assignee_ref: string;
  dispatch_mode?: string;
  delivery_contract?: string;
  stage_id?: string;
  follows_task_id?: string;
  detached?: boolean;
}

export interface PlanGenerationEdgeDraft {
  from: string;
  to: string;
  kind?: PlanGraphEdgeKind | 'seq' | 'conditional' | 'loopback';
  when?: string;
  max_rounds?: number;
}

export interface PlanGenerationDiff {
  node_decisions: PlanGenerationNodeDecision[];
  tasks: PlanGenerationTaskDraft[];
  edges: PlanGenerationEdgeDraft[];
}

export interface PlanGenerationTaskSnapshot {
  task_id: string;
  stage_id?: string;
  node_id?: string;
  title: string;
  description?: string;
  assignee_ref?: string;
  status: string;
  dispatch_mode?: string;
  delivery_contract?: string;
  follows_task_id?: string;
  origin_verdict_id?: string;
}

export interface PlanGenerationEdgeSnapshot {
  from_task_id: string;
  to_task_id: string;
  kind?: string;
  when?: string;
  max_rounds?: number;
}

export interface PlanGenerationDispatchSnapshot {
  task_id: string;
  dispatched_at: string;
  dispatch_message_id?: string;
}

export interface PlanGenerationSnapshot {
  plan_id: string;
  plan_version: number;
  active_generation_id: string;
  tasks: PlanGenerationTaskSnapshot[];
  edges: PlanGenerationEdgeSnapshot[];
  dispatch_records: PlanGenerationDispatchSnapshot[];
}

export interface PlanGeneration {
  id: string;
  plan_id: string;
  parent_generation_id: string;
  revision: number;
  active: boolean;
  reason: string;
  evidence: string;
  creator_ref: string;
  diff: PlanGenerationDiff;
  snapshot: PlanGenerationSnapshot;
  snapshot_progress: { done: number; total: number };
  idempotency_key: string;
  dispatched_task_ids: string[];
  created_at: string;
}

export interface PlanGenerationNode {
  task_id: string;
  node_id?: string;
  stage_id?: string;
  generation_id: string;
  revision: number;
  present_in_active: boolean;
}

export interface PlanGenerationRead {
  plan_id: string;
  active_generation_id: string;
  plan_version: number;
  generations: PlanGeneration[];
  nodes: PlanGenerationNode[];
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

// useProjectPlansList — the project Plans LIST panel (T302): the SAME project
// plans endpoint but with sort/page params, so the backend SQL-paginates and
// EXCLUDES the builtin pool, returning { items, total }. Distinct from usePlans
// (Work Board / AgentContextPanel), which stays unpaginated + includes builtin.
export function useProjectPlansList(projectId: string | undefined, filters?: OrgPlanFilters) {
  return useQuery({
    queryKey: [...qk.plansByProject(projectId ?? ''), 'list', filters ?? null],
    queryFn: async () => {
      const resp = await api.get<{ plans: Plan[]; total?: number }>(
        `${plansBase(projectId ?? '')}${buildOrgPlanQuery(filters)}`,
      );
      return { items: resp.plans ?? [], total: resp.total ?? (resp.plans ?? []).length };
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
  /** server-side name search (contains, case-insensitive). */
  q?: string;
  /** sort column key: created_at | updated_at | status | name | org_ref. */
  sort?: string;
  dir?: 'asc' | 'desc';
  /** 1-based page (with page_size). */
  page?: number;
  page_size?: number;
}

function buildOrgPlanQuery(f?: OrgPlanFilters): string {
  if (!f) return '';
  const p = new URLSearchParams();
  for (const id of f.project ?? []) p.append('project', id);
  for (const s of f.status ?? []) p.append('status', s);
  if (f.q) p.set('q', f.q);
  if (f.sort) p.set('sort', f.sort);
  if (f.dir) p.set('dir', f.dir);
  if (f.page && f.page > 1) p.set('page', String(f.page));
  if (f.page_size) p.set('page_size', String(f.page_size));
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

export function useAssignmentPool(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.assignmentPoolByProject(projectId ?? ''),
    queryFn: () => api.get<AssignmentPool>(`/projects/${projectId}/assignment-pool`),
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

// GET /{id}/graph — the orchestration-engine graph read model (T769). Returns
// { has_graph:false } for an ungraphed plan (the legacy-DAG fallback signal), or
// the graph (control nodes + edges by kind) once T768 has built it on plan start.
// Cached under the plan key so it invalidates alongside the plan detail.
export function usePlanGraph(projectId: string | undefined, planId: string | undefined) {
  return useQuery({
    queryKey: [...qk.plan(planId ?? ''), 'graph'],
    queryFn: () => api.get<PlanGraphView>(`${plansBase(projectId ?? '')}/${planId}/graph`),
    enabled: !!projectId && !!planId,
  });
}

// GET /{id}/generations — persisted active/parent lineage, immutable snapshots,
// real PlanGenerationDiff, and first-generation node ownership.
export function usePlanGenerations(projectId: string | undefined, planId: string | undefined) {
  return useQuery({
    queryKey: [...qk.plan(planId ?? ''), 'generations'],
    queryFn: () => api.get<PlanGenerationRead>(`${plansBase(projectId ?? '')}/${planId}/generations`),
    enabled: !!projectId && !!planId,
  });
}

// --- Stage read model (T981, plan-stage-model §7) --------------------------
// A Plan may be organized as a DAG of Stages (Spark-style), each stage being a
// sub-DAG of nodes closed by a barrier + optional gate. Stage status is a DERIVED
// projection (never stored): open → nothing started; running → a member runs or the
// gate is pending; legacy reopen is read-only migration evidence; done means all
// members done and the gate passed.
export type PlanStageStatus = 'open' | 'running' | 'done' | 'reopen'; // reopen: legacy read only

// One member node of a stage — the bound task's identity + its live status (the raw
// task status, open|running|completed|discarded|reopened). The FE groups the plan's
// graph/DAG nodes into stages by matching these task_ids.
export interface PlanStageMember {
  task_id: string;
  title: string;
  task_status: TaskStatus;
}

// One stage's derived read model. rounds = completed gate-reject reopen rounds;
// max_rounds = the stage-local bounded-retry cap.
export interface PlanStage {
  id: string;
  name: string;
  status: PlanStageStatus;
  rounds: number;
  max_rounds: number;
  depends_on_stages: string[];
  gate_node_id: string;
  gate_task_id: string;
  gate_spec: {
    evaluator_kind: 'automatic' | 'human';
    assignee_ref?: string;
    role_ref?: string;
    acceptance_contract?: string;
    pass_route: string;
    reject_route: string;
    exhausted_route: string;
  };
  gate_outcome?: 'pass' | 'reject';
  gate_evidence?: string;
  gate_reviewed_sha?: string;
  origin_verdict_id?: string;
  continuation_id?: string;
  generation?: number;
  acceptance_contract?: string;
  topology_fingerprint?: string;
  diagnostics?: Array<{ node_id?: string; code: string; message: string; hint?: string }>;
  members: PlanStageMember[];
}

// GET /{id}/stages — the plan's stage-level read model (T981 §7). Returns an EMPTY
// array for a plan with no stages (the FE then renders the legacy no-stage view,
// byte-identical to before — §8 backward compat). Cached under the plan key so it
// invalidates alongside the plan detail / graph.
export function usePlanStages(projectId: string | undefined, planId: string | undefined) {
  return useQuery({
    queryKey: [...qk.plan(planId ?? ''), 'stages'],
    queryFn: async () => {
      const resp = await api.get<{ stages: PlanStage[] }>(
        `${plansBase(projectId ?? '')}/${planId}/stages`,
      );
      return resp.stages ?? [];
    },
    enabled: !!projectId && !!planId,
  });
}

// GET /{id}/related-issues — the source issue(s) this plan's tasks derive from (the
// plan detail rail's "Related Issues" list, the issue-side mirror of the issue sidebar's
// Derived Tasks). A cycle plan resolves to its one source issue; a hand-built plan may
// span several. Empty array when no task derives from an issue.
export function useRelatedIssues(projectId: string | undefined, planId: string | undefined) {
  return useQuery({
    queryKey: [...qk.plan(planId ?? ''), 'related-issues'],
    queryFn: async () => {
      const resp = await api.get<{ issues: Issue[] }>(
        `${plansBase(projectId ?? '')}/${planId}/related-issues`,
      );
      return resp.issues ?? [];
    },
    enabled: !!projectId && !!planId,
  });
}

// GET …/issues/{id}/related-plans — the structured plans derived from this issue (the
// issue detail's "Related Plans" panel, the plan-side mirror of Derived Tasks; the
// reverse of useRelatedIssues). The backend EXCLUDES the built-in pool, so the caller
// renders the rows as-is. Empty array when no plan is derived from the issue.
export function useRelatedPlansForIssue(projectId: string | undefined, issueId: string | undefined) {
  return useQuery({
    queryKey: [...qk.issue(issueId ?? ''), 'related-plans'],
    queryFn: async () => {
      const resp = await api.get<{ plans: Plan[] }>(
        `/projects/${projectId}/issues/${issueId}/related-plans`,
      );
      return resp.plans ?? [];
    },
    enabled: !!projectId && !!issueId,
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

// PATCH /{id} — edit name / goal / target_date. pending-only (the backend rejects
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
  void qc.invalidateQueries({ queryKey: qk.assignmentPoolByProject(projectId) });
}

function invalidateAssignmentPoolWrite(
  qc: ReturnType<typeof useQueryClient>,
  projectId: string,
) {
  void qc.invalidateQueries({ queryKey: qk.assignmentPoolByProject(projectId) });
  void qc.invalidateQueries({ queryKey: qk.unplannedTasksByProject(projectId) });
  void qc.invalidateQueries({ queryKey: qk.tasksByProject(projectId) });
  void qc.invalidateQueries({ queryKey: qk.plansByProject(projectId) });
}

export function useAddTaskToAssignmentPool(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ taskId, priority = 0 }: { taskId: string; priority?: number }) =>
      api.post<{ ok: boolean }>(`/projects/${projectId}/assignment-pool/tasks`, {
        task_id: taskId,
        priority,
      }),
    onSuccess: () => invalidateAssignmentPoolWrite(qc, projectId),
  });
}

export function useRemoveTaskFromAssignmentPool(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (taskId: string) =>
      api.del<{ ok: boolean }>(
        `/projects/${projectId}/assignment-pool/tasks/${encodeURIComponent(taskId)}`,
      ),
    onSuccess: () => invalidateAssignmentPoolWrite(qc, projectId),
  });
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
// target/source plan is only known at DROP time (any pending plan, or the source
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

export interface CommitPlanEvolutionInput {
  parent_generation_id: string;
  base_version: number;
  reason: string;
  evidence: string;
  idempotency_key: string;
  diff: PlanGenerationDiff;
}

export interface PlanEvolutionCommitResponse {
  ok: boolean;
  duplicate: boolean;
  active_generation_id: string;
  version: number;
  dispatched: string[];
  generation: PlanGeneration;
}

// POST /{id}/evolution — EvolvePlanGeneration for running/paused plans; done
// plans must reopen to paused first.
export function useCommitPlanEvolution(projectId: string, planId: string) {
  return usePlanWrite<CommitPlanEvolutionInput, PlanEvolutionCommitResponse>(projectId, planId, (vars) =>
    api.post<PlanEvolutionCommitResponse>(`${plansBase(projectId)}/${planId}/evolution`, vars),
  );
}

export type ResolvePlanBlockAction =
  | 'acknowledge'
  | 'resume_original'
  | 'pause_or_discard_plan';

export interface ResolvePlanBlockInput {
  task_id: string;
  action: ResolvePlanBlockAction;
  disposition?: 'pause' | 'discard';
  note?: string;
  idempotency_key: string;
}

export interface ResolvePlanBlockResponse {
  ok: boolean;
  plan?: Plan;
}

// POST /{id}/blocks/{task_id}/resolve — ResolvePlanBlock. Resume for a blocked
// plan must use action=resume_original; paused-node resume is a different API.
export function useResolvePlanBlock(projectId: string, planId: string) {
  return usePlanWrite<ResolvePlanBlockInput, ResolvePlanBlockResponse>(projectId, planId, (vars) => {
    const { task_id, ...body } = vars;
    return api.post<ResolvePlanBlockResponse>(
      `${plansBase(projectId)}/${planId}/blocks/${encodeURIComponent(task_id)}/resolve`,
      body,
    );
  });
}

// ADR-0055 lifecycle: pending→running↔paused; done can reopen to paused.
export function useStartPlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/start`),
  );
}

export function usePausePlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/pause`),
  );
}

/** @deprecated usePausePlan; retained for source compatibility. */
export const useStopPlan = usePausePlan;

export function useResumePlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/resume`),
  );
}

export function useReopenPlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/reopen`),
  );
}

export function useCompletePlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/complete`),
  );
}

export function useDiscardPlan(projectId: string, planId: string) {
  return usePlanWrite<void, Plan>(projectId, planId, () =>
    api.post<Plan>(`${plansBase(projectId)}/${planId}/discard`),
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
// Destructive Plan controls. Delete is only for never-started pending Plans;
// archive is an orthogonal marker allowed only on done/discarded Plans. Each
// goes through usePlanWrite so it shares
// the plan / plansByProject / tasks / unplanned invalidation:
//   • Delete unloads ALL the plan's tasks back to the Backlog (→ tasks +
//     unplanned must refetch) + cascade-deletes the conversation + deletes the
//     plan. The plan itself is GONE, so the caller navigates away on success.
//   • Archive marks the terminal Plan + its tasks archived without changing its
//     done/discarded lifecycle truth; the Plan stays GET-able (read-only).
// ---------------------------------------------------------------------------

// DELETE /{id} → { deleted: true }. IRREVERSIBLE. Only a never-started pending
// Plan may be deleted. On success the Plan no longer exists — the
// caller must navigate away (the detail route would 404).
export function useDeletePlan(projectId: string, planId: string) {
  return usePlanWrite<void, { deleted: boolean }>(projectId, planId, () =>
    api.del<{ deleted: boolean }>(`${plansBase(projectId)}/${planId}`),
  );
}

// POST /{id}/archive → terminal Plan detail with archived_at. Re-archive returns
// 409; lifecycle remains done/discarded and task.status is preserved.
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
    return 'This plan still has running tasks. Finish them or discard the plan before archiving.';
  }
  if (lower.includes('running')) {
    return 'This plan has already started. Its history cannot be deleted; discard it to stop.';
  }
  if (lower.includes('archiv')) {
    return 'This plan is already archived.';
  }
  return "Couldn't complete that action. Please try again.";
}

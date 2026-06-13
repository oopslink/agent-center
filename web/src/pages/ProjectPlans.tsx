import React, { useState } from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useProject } from '@/api/projects';
import {
  usePlans,
  useCreatePlan,
  useUnplannedTasks,
  useAddTaskToPlan,
  useRemoveTaskFromPlan,
  useAddTaskToAnyPlan,
  useRemoveTaskFromAnyPlan,
  type Plan,
  type PlanNode,
  type CreatePlanInput,
} from '@/api/plans';
import { refKind, useDisplayNameResolver, normalizeIdentityRef } from '@/api/members';
import type { Task } from '@/api/types';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { TaskTitleLink } from '@/components/TaskTitleLink';
import { StatusChip } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, AutoAdvancingIndicator, TaskArchivedBadge, planProgressLabel } from '@/components/planDisplay';

// ProjectPlans (/projects/:id/plans) — v2.9 #291 WORK BOARD (the headline
// Plan-Orchestration PLANNING view). A horizontal kanban: a first Backlog
// column (the project's UNPLANNED tasks) + one column per Plan + a trailing
// "New Plan" column. Planning = drag (or use the keyboard add-button on) a
// Backlog task into a DRAFT Plan column (§9.4 draft-only select-into-plan).
// Reached via the project detail Plans link (§4.2); each Plan column's
// "Open ▸" reaches the Plan detail (#287 DAG, the EXECUTION view).
//
// Refactored FROM the #286 Plan-card list into this board.
export default function ProjectPlans(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const project = useProject(id);
  const plans = usePlans(id);
  const backlog = useUnplannedTasks(id);
  const [createOpen, setCreateOpen] = useState(false);
  // The task currently being dragged (HTML5 DnD) + WHERE it came from. Held in
  // state so a draft Plan column can light up its drop-zone + reject running
  // columns, AND so a drop knows the source (Backlog vs another Plan) to pick
  // SELECT (backlog→plan) vs MOVE (plan→plan) vs REMOVE (plan→backlog). A7:
  // fromPlanId === null ⟺ dragged from the Backlog; non-null ⟺ from that Plan.
  const [dragSource, setDragSource] = useState<DragSource | null>(null);

  const projectName = project.data?.name ?? id;

  return (
    <section className="space-y-4" data-testid="page-ProjectPlans" data-project-id={id}>
      <Breadcrumb
        items={[
          { label: 'Projects', to: '/projects' },
          { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
          { label: 'Work Board' },
        ]}
      />
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border-base pb-3">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">Work Board</h1>
          <p className="mt-0.5 text-xs text-text-muted">
            Three segments · Backlog (unscheduled) · Assignment Pool (claimable) · structured Plans.
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="plan-create-btn"
        >
          + New Plan
        </button>
      </header>

      {createOpen && <PlanCreateModal projectId={id} onClose={() => setCreateOpen(false)} />}

      <Board
        projectId={id}
        plans={plans}
        backlog={backlog}
        dragSource={dragSource}
        setDragSource={setDragSource}
        onNewPlan={() => setCreateOpen(true)}
      />
    </section>
  );
}

// DragSource — the in-flight drag's identity: which task + where it came from.
// fromPlanId === null ⟺ from the Backlog; a plan id ⟺ from that Plan column.
// A7 carries the source so a drop can choose SELECT / MOVE / REMOVE correctly.
export interface DragSource {
  taskId: string;
  fromPlanId: string | null;
}

// A7 race-proof drop: a PLAN-task drag stamps its SOURCE plan id into the
// HTML5 dataTransfer (a custom MIME) AT dragStart — synchronously, on the
// native event. Unlike the React `dragSource` STATE (which only lands after a
// re-render+commit and so can lag the browser's first `dragover`), dataTransfer
// is available on EVERY dragover/drop the instant the drag begins. The Backlog
// reads the source from here so its drop-acceptance never depends on the state
// update having committed first (the run-real bug: the onDragOver preventDefault
// read stale state → the browser never registered the Backlog as a drop zone,
// so data-droppable stayed false + 0 RemoveTaskFromPlan fired). The MIME's mere
// PRESENCE in `dataTransfer.types` is readable during dragover in every browser
// (the value is protected then), so the Backlog can decide "this is a plan-task
// → accept" without needing the value mid-drag.
const FROM_PLAN_MIME = 'application/x-slock-from-plan';

// Read the dragged task's source plan from dataTransfer (race-proof, set on
// dragStart) with the React state as a fallback. taskId comes from text/plain
// (also set on dragStart); fromPlanId from the custom MIME. A backlog-origin
// drag never stamps FROM_PLAN_MIME, so fromPlanId resolves null there.
function readDragSource(e: React.DragEvent, state: DragSource | null): DragSource | null {
  const taskId = e.dataTransfer.getData('text/plain') || state?.taskId || '';
  const fromPlan = e.dataTransfer.getData(FROM_PLAN_MIME);
  if (!taskId) return state;
  return { taskId, fromPlanId: fromPlan ? fromPlan : state?.fromPlanId ?? null };
}

interface PlansQuery {
  data?: Plan[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}
interface TasksQuery {
  data?: Task[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}

// Board — the horizontal scrolling kanban (.board: flex, gap, overflow-x-auto).
// ADR-0047 THREE-segment Work Board, left→right:
//   1. Backlog        — unscheduled tasks (plan_id == ""), NOT claimable, flat.
//   2. Assignment Pool — the is_builtin plan (exactly one): a FLAT list whose
//                        assigned+dispatched nodes are CLAIMABLE. No DAG edges.
//   3. Structured Plans — every non-builtin plan, the existing DAG columns.
// Then the trailing New-Plan column (creates a structured plan).
function Board({
  projectId,
  plans,
  backlog,
  dragSource,
  setDragSource,
  onNewPlan,
}: {
  projectId: string;
  plans: PlansQuery;
  backlog: TasksQuery;
  dragSource: DragSource | null;
  setDragSource: (s: DragSource | null) => void;
  onNewPlan: () => void;
}): React.ReactElement {
  // A board-level load is only "failed" if the Plans list (the columns) fails —
  // surface the #218 friendly ErrorState. The Backlog has its own inline state.
  if (plans.isError) {
    return (
      <ErrorState message="Couldn't load the work board." error={plans.error} testId="board-error" />
    );
  }
  if (plans.isLoading) {
    return (
      <div className="flex gap-3" data-testid="board-loading">
        <Skeleton height="12rem" width="14.75rem" />
        <Skeleton height="12rem" width="14.75rem" />
        <Skeleton height="12rem" width="14.75rem" />
      </div>
    );
  }
  const planList = plans.data ?? [];

  // ADR-0047 partition: the BUILT-IN assignment pool (exactly one is_builtin
  // plan) is its own segment; every other plan is a STRUCTURED plan column.
  const builtinPool = planList.find((p) => p.is_builtin === true) ?? null;
  const structuredPlans = planList.filter((p) => p.is_builtin !== true);

  // The draft STRUCTURED Plans are the only valid add/drop targets (§9.4
  // select-into-plan is draft-only); shared by every Backlog card's add-menu.
  // The built-in pool is offered separately (it is always-running, never draft).
  const draftPlans = structuredPlans.filter((p) => p.status === 'draft');

  return (
    <div
      className="flex items-start gap-3 overflow-x-auto pb-2"
      data-testid="work-board"
      role="list"
      aria-label="Work board"
    >
      <BacklogColumn
        projectId={projectId}
        backlog={backlog}
        draftPlans={draftPlans}
        builtinPool={builtinPool}
        dragSource={dragSource}
        setDragSource={setDragSource}
      />
      {builtinPool && (
        <BuiltinPoolColumn
          projectId={projectId}
          plan={builtinPool}
          dragSource={dragSource}
        />
      )}
      {structuredPlans.map((plan) => (
        <PlanColumn
          key={plan.id}
          projectId={projectId}
          plan={plan}
          dragSource={dragSource}
          setDragSource={setDragSource}
        />
      ))}
      <NewPlanColumn onClick={onNewPlan} />
    </div>
  );
}

// HIDDEN_NODE / HIDDEN_TASK — ADR-0047: completed + discarded work is HIDDEN by
// default in the Backlog and the Assignment Pool (those are "live capacity"
// segments). Structured plans KEEP done nodes (history). The BE may already
// exclude them; we also filter on the FE so a degraded payload never leaks them.
function isLiveTaskStatus(status: string | undefined): boolean {
  return status !== 'completed' && status !== 'discarded';
}

// columnBase — the shared .col look (fixed ~236px, solid subtle bg, border).
// SOLID theme tokens only (bg-bg-subtle / border-border-base) — no alpha-tint,
// AA in both modes.
const columnBase =
  'flex w-[14.75rem] shrink-0 flex-col rounded-lg border p-2.5';

// BacklogColumn — first column (distinct .col.backlog bg). Lists the project's
// UNPLANNED tasks; each card has the keyboard-accessible "Add to plan" menu and
// is HTML5-draggable into a draft Plan column.
function BacklogColumn({
  projectId,
  backlog,
  draftPlans,
  builtinPool,
  dragSource,
  setDragSource,
}: {
  projectId: string;
  backlog: TasksQuery;
  draftPlans: Plan[];
  builtinPool: Plan | null;
  dragSource: DragSource | null;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  // ADR-0047: HIDE completed/discarded in the Backlog by default (live capacity
  // only). The BE `?unplanned=1` may already exclude them; the FE filter is the
  // belt-and-braces guard so a degraded payload never leaks terminal work.
  const tasks = (backlog.data ?? []).filter((t) => isLiveTaskStatus(t.status));
  const remove = useRemoveTaskFromAnyPlan(projectId);
  const [dropActive, setDropActive] = useState(false);
  // A7: the Backlog accepts a drop only when a PLAN-task is being dragged
  // (fromPlanId != null) → REMOVE = back to backlog. A backlog card dropped on
  // the backlog is a no-op (fromPlanId == null). The source plan was draft (only
  // draft cards are draggable), so RemoveTaskFromPlan is allowed (§9.4).
  const canDrop = dragSource !== null && dragSource.fromPlanId !== null;

  // A7 race-proof acceptance: the Backlog is a valid drop target whenever the
  // in-flight drag is a PLAN-task. That's known the instant dragStart stamps
  // FROM_PLAN_MIME — readable on every dragover via `dataTransfer.types` even
  // before the React `dragSource` state commits. We OR the state-derived
  // `canDrop` (for the synthetic-event test path / robustness) with the
  // dataTransfer marker so the real browser always registers the drop zone.
  const acceptsDrag = (e: React.DragEvent) =>
    canDrop || e.dataTransfer.types.includes(FROM_PLAN_MIME);

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDropActive(false);
    // Read the source from dataTransfer first (set synchronously on dragStart),
    // falling back to the React state — so a drop fires the remove even if the
    // state update hadn't committed when the native drop landed.
    const src = readDragSource(e, dragSource);
    if (!src || src.fromPlanId === null) return; // backlog→backlog no-op.
    remove.mutate({ planId: src.fromPlanId, taskId: src.taskId });
  };

  return (
    <div
      className={`${columnBase} bg-bg-subtle ${
        dropActive ? 'border-accent ring-2 ring-accent' : 'border-border-strong'
      }`}
      data-testid="backlog-column"
      data-droppable={canDrop ? 'true' : 'false'}
      role="listitem"
      onDragOver={(e) => {
        if (!acceptsDrag(e)) return; // a backlog card over the backlog = no drop.
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        setDropActive(true);
      }}
      onDragLeave={() => setDropActive(false)}
      onDrop={handleDrop}
    >
      <div className="px-0.5 pb-2">
        <div className="flex items-center justify-between">
          <span className="flex items-center gap-1.5 text-sm font-bold text-text-primary">
            <BacklogIcon />
            Backlog
          </span>
          <span className="tabular-nums text-[0.6875rem] text-text-muted" data-testid="backlog-count">
            {tasks.length}
          </span>
        </div>
        <p className="mt-0.5 text-[0.625rem] leading-tight text-text-muted" data-testid="backlog-subtitle">
          Unscheduled — not claimable
        </p>
      </div>
      {backlog.isError ? (
        <ErrorState
          message="Couldn't load the backlog."
          error={backlog.error}
          testId="backlog-error"
        />
      ) : backlog.isLoading ? (
        <Skeleton height="3rem" />
      ) : tasks.length === 0 ? (
        <p className="py-4 text-center text-[0.6875rem] text-text-muted" data-testid="backlog-empty">
          No unplanned tasks. Every task is in a plan.
        </p>
      ) : (
        tasks.map((task) => (
          <BacklogCard
            key={task.id}
            projectId={projectId}
            task={task}
            draftPlans={draftPlans}
            builtinPool={builtinPool}
            setDragSource={setDragSource}
          />
        ))
      )}
    </div>
  );
}

// BacklogCard — a draggable task card with the keyboard-accessible "Add to
// plan ▾" menu. Both the menu (a11y PRIMARY) + drag-drop fire the SAME
// select-into-plan (useAddTaskToPlan), draft-only.
function BacklogCard({
  projectId,
  task,
  draftPlans,
  builtinPool,
  setDragSource,
}: {
  projectId: string;
  task: Task;
  draftPlans: Plan[];
  builtinPool: Plan | null;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  const [menuOpen, setMenuOpen] = useState(false);
  return (
    <div
      className="mb-1.5 rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1"
      data-testid="backlog-card"
      data-task-id={task.id}
      draggable
      onDragStart={(e) => {
        // A7: from the Backlog → fromPlanId null (SELECT into a draft plan).
        setDragSource({ taskId: task.id, fromPlanId: null });
        e.dataTransfer.setData('text/plain', task.id);
        e.dataTransfer.effectAllowed = 'move';
      }}
      onDragEnd={() => setDragSource(null)}
    >
      <div className="mb-1.5 text-xs font-semibold leading-tight text-text-primary">
        <TaskTitleLink projectId={projectId} taskId={task.id} title={task.title} />
      </div>
      <div className="flex items-center justify-between gap-1.5">
        <AssigneeBadge assignee={task.assignee} />
        <StatusChip status={task.status} />
      </div>
      <div className="relative mt-1.5">
        <button
          type="button"
          className="flex w-full items-center justify-center gap-1 rounded-md border border-dashed border-border-strong bg-bg-elevated px-1 py-1 text-[0.6875rem] font-medium text-accent hover:border-accent"
          onClick={() => setMenuOpen((v) => !v)}
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          data-testid={`backlog-add-${task.id}`}
        >
          Add to plan
          <ChevronDownIcon />
        </button>
        {menuOpen && (
          <AddToPlanMenu
            projectId={projectId}
            taskId={task.id}
            draftPlans={draftPlans}
            builtinPool={builtinPool}
            onClose={() => setMenuOpen(false)}
          />
        )}
      </div>
    </div>
  );
}

// AddToPlanMenu — the draft-Plan picker. §9.4: only DRAFT Plans are listed (a
// running Plan is never a select-into-plan target). Each item calls
// useAddTaskToPlan. Keyboard-operable (real <button>s, focusable).
function AddToPlanMenu({
  projectId,
  taskId,
  draftPlans,
  builtinPool,
  onClose,
}: {
  projectId: string;
  taskId: string;
  draftPlans: Plan[];
  builtinPool: Plan | null;
  onClose: () => void;
}): React.ReactElement {
  const noTargets = draftPlans.length === 0 && !builtinPool;
  return (
    <div
      className="absolute left-0 right-0 top-full z-10 mt-1 rounded-md border border-border-base bg-bg-elevated p-1 shadow-1"
      role="menu"
      data-testid={`add-menu-${taskId}`}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
    >
      {noTargets ? (
        <p className="px-2 py-1.5 text-[0.6875rem] text-text-muted" data-testid="add-menu-empty">
          No draft plan or pool. Create a plan to schedule this task.
        </p>
      ) : (
        <>
          {/* ADR-0047: the built-in Assignment Pool is a select target (BE permits
              moving a backlog task into the pool → it becomes claimable). */}
          {builtinPool && (
            <AddToPlanItem
              projectId={projectId}
              planId={builtinPool.id}
              planName="Assignment Pool"
              taskId={taskId}
              onDone={onClose}
            />
          )}
          {draftPlans.map((plan) => (
            <AddToPlanItem
              key={plan.id}
              projectId={projectId}
              planId={plan.id}
              planName={plan.name}
              taskId={taskId}
              onDone={onClose}
            />
          ))}
        </>
      )}
    </div>
  );
}

function AddToPlanItem({
  projectId,
  planId,
  planName,
  taskId,
  onDone,
}: {
  projectId: string;
  planId: string;
  planName: string;
  taskId: string;
  onDone: () => void;
}): React.ReactElement {
  const add = useAddTaskToPlan(projectId, planId);
  return (
    <button
      type="button"
      role="menuitem"
      className="block w-full truncate rounded px-2 py-1.5 text-left text-xs text-text-primary hover:bg-bg-subtle disabled:opacity-60"
      disabled={add.isPending}
      data-testid={`add-to-plan-${taskId}-${planId}`}
      onClick={async () => {
        try {
          await add.mutateAsync({ task_id: taskId });
          onDone();
        } catch {
          // surfaced by the (re-fetched) board; menu stays open on failure.
        }
      }}
    >
      {planName}
    </button>
  );
}

// ClaimableChip — ADR-0047 affordance on a built-in-pool node that is currently
// CLAIMABLE (assigned + dispatched; pull, no-wake). Both-mode AA: a CURATED
// SOLID emerald-100 / emerald-800 pair (theme-independent literal Tailwind
// colors → the same light-block-dark-text in BOTH modes, contrast 6.78 — AA).
// NO alpha-tint. Text label "Claimable" (never color alone) + tiny inline SVG
// (NOT emoji). Renders nothing when the node is not claimable.
function ClaimableChip({
  claimable,
  taskId,
}: {
  claimable: boolean | undefined;
  taskId: string;
}): React.ReactElement | null {
  if (!claimable) return null;
  return (
    <span
      className="inline-flex items-center gap-1 rounded bg-emerald-100 px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-emerald-800"
      data-testid={`claimable-chip-${taskId}`}
      title="Claimable now — assigned & dispatched (pull, no-wake)."
    >
      {/* hand / pull glyph */}
      <svg viewBox="0 0 24 24" className="h-2.5 w-2.5" fill="none" stroke="currentColor" strokeWidth="2.4" aria-hidden="true">
        <path d="M5 12V7a2 2 0 0 1 4 0v5" />
        <path d="M9 11V5a2 2 0 0 1 4 0v6" />
        <path d="M13 11V6a2 2 0 0 1 4 0v8a6 6 0 0 1-6 6h-1a6 6 0 0 1-5-3l-2-3" />
      </svg>
      Claimable
    </span>
  );
}

// BuiltinPoolColumn — ADR-0047 segment 2: the is_builtin assignment pool, a
// DISTINCT segment (not a generic plan column). A FLAT list of its nodes (no
// DAG / edge editing, no remove affordance, no drag-out — the pool is always
// running, "pull, no-wake"). completed/discarded nodes are HIDDEN by default.
// A claimable node shows the ClaimableChip. It IS a drop target for a backlog
// task being dragged in (BE permits selecting a backlog task into the pool).
function BuiltinPoolColumn({
  projectId,
  plan,
  dragSource,
}: {
  projectId: string;
  plan: Plan;
  dragSource: DragSource | null;
}): React.ReactElement {
  const add = useAddTaskToPlan(projectId, plan.id);
  const [dropActive, setDropActive] = useState(false);
  // Defensive reads (mirror PlanColumn) — degrade to an empty pool, never crash.
  const preview = plan.nodes_preview ?? [];
  const nodeCount = plan.node_count ?? 0;
  // ADR-0047: hide completed/discarded in the pool (live capacity only).
  const shown = preview.filter((n) => isLiveTaskStatus(n.task_status));
  // Overflow uses the LIVE count when known; fall back to node_count − shown.
  const overflow = nodeCount - preview.length > 0 ? nodeCount - preview.length : 0;

  // A backlog-origin drag (fromPlanId === null) can be dropped INTO the pool →
  // SELECT. A plan/pool-origin drag is not a pool target (the pool is flat).
  const dragTaskId = dragSource?.taskId ?? null;
  const canDrop = dragTaskId !== null && dragSource?.fromPlanId == null;

  const acceptsDrag = (e: React.DragEvent) =>
    canDrop && !e.dataTransfer.types.includes(FROM_PLAN_MIME);

  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setDropActive(false);
    const src = readDragSource(e, dragSource);
    if (!src || src.fromPlanId !== null) return; // only a backlog task selects in.
    try {
      await add.mutateAsync({ task_id: src.taskId });
    } catch {
      // surfaced by the board re-fetch.
    }
  };

  return (
    <div
      className={`${columnBase} bg-bg-elevated ${
        dropActive && canDrop ? 'border-accent ring-2 ring-accent' : 'border-emerald-300'
      }`}
      data-testid="builtin-pool-column"
      data-plan-id={plan.id}
      data-builtin="true"
      data-droppable={canDrop ? 'true' : 'false'}
      role="listitem"
      onDragOver={(e) => {
        if (!acceptsDrag(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        setDropActive(true);
      }}
      onDragLeave={() => setDropActive(false)}
      onDrop={handleDrop}
    >
      <div className="px-0.5 pb-2">
        <div className="flex items-center justify-between">
          <span className="flex items-center gap-1.5 text-sm font-bold text-text-primary">
            <PoolIcon />
            Assignment Pool
          </span>
          <span className="tabular-nums text-[0.6875rem] text-text-muted" data-testid="builtin-pool-count">
            {shown.length}
          </span>
        </div>
        <p className="mt-0.5 text-[0.625rem] leading-tight text-text-muted" data-testid="builtin-pool-subtitle">
          Built-in · always running · claimable
        </p>
      </div>
      {shown.length === 0 ? (
        <p className="py-3 text-center text-[0.6875rem] text-text-muted" data-testid="builtin-pool-empty">
          No claimable tasks yet.
        </p>
      ) : (
        shown.map((node) => (
          <PoolTaskCard key={node.task_id} projectId={projectId} node={node} />
        ))
      )}
      {overflow > 0 && (
        <p className="px-0.5 text-[0.6875rem] text-text-muted" data-testid={`pool-overflow-${plan.id}`}>
          …and {overflow} more
        </p>
      )}
    </div>
  );
}

// PoolTaskCard — a single built-in-pool task card (flat, no remove / drag-out /
// DAG affordance). Shows the ClaimableChip when the node is claimable, alongside
// the task status chip + archive badge.
function PoolTaskCard({
  projectId,
  node,
}: {
  projectId: string;
  node: PlanNode;
}): React.ReactElement {
  return (
    <div
      className="mb-1.5 rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1"
      data-testid="pool-task-card"
      data-task-id={node.task_id}
    >
      <div className="mb-1.5 text-xs font-semibold leading-tight text-text-primary">
        <TaskTitleLink projectId={projectId} taskId={node.task_id} title={node.title} />
      </div>
      <div className="flex flex-wrap items-center justify-between gap-1.5">
        <AssigneeBadge assignee={node.assignee_ref} />
        <span className="inline-flex items-center gap-1">
          <ClaimableChip claimable={node.claimable} taskId={node.task_id} />
          <TaskArchivedBadge archived={node.archived} taskId={node.task_id} />
          <StatusChip status={node.task_status} />
        </span>
      </div>
    </div>
  );
}

// PlanColumn — one column per Plan. Header = name + status chip + Open ▸ link;
// sub-header = progress + has_failed. Cards = the Plan's nodes_preview (capped 4
// by the backend, no add-button), with an "…and M more" overflow from node_count.
// A DRAFT column is a valid HTML5 drop target (select-into-plan); a running/done
// column is NOT (§9.4) — it rejects the drop + never highlights.
//
// DEFENSIVE DEFAULTS (resilience): every enriched field is read through a guard
// so a partial / bare response (e.g. a not-yet-enriched endpoint, or a degraded
// payload) degrades to an EMPTY column instead of crashing the ErrorBoundary —
// this is the regression guard for the run-real `reading 'done'` white-screen
// (progress was undefined on the original bare GET /plans, see PR #272). Each:
//   progress      ?? { done: 0, total: 0 }
//   has_failed    ?? false
//   nodes_preview ?? []
//   node_count    ?? 0
function PlanColumn({
  projectId,
  plan,
  dragSource,
  setDragSource,
}: {
  projectId: string;
  plan: Plan;
  dragSource: DragSource | null;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  const add = useAddTaskToPlan(projectId, plan.id);
  // A7: cross-column MOVE needs to remove from the SOURCE plan + add to THIS
  // plan; the source plan is only known at drop time → the any-plan variants.
  const addAny = useAddTaskToAnyPlan(projectId);
  const removeAny = useRemoveTaskFromAnyPlan(projectId);
  const [dropActive, setDropActive] = useState(false);
  const isDraft = plan.status === 'draft';
  const dragTaskId = dragSource?.taskId ?? null;
  // Defensive reads — see the DEFENSIVE DEFAULTS note above.
  const progress = plan.progress ?? { done: 0, total: 0 };
  const hasFailed = plan.has_failed ?? false;
  const preview = plan.nodes_preview ?? [];
  const nodeCount = plan.node_count ?? 0;
  // Cards = the (already-capped) preview; overflow = total − shown, only if > 0.
  const shown = preview;
  const overflow = nodeCount - shown.length > 0 ? nodeCount - shown.length : 0;

  // A drop is valid only on a draft column while a Backlog task is being dragged.
  const canDrop = isDraft && dragTaskId !== null;

  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setDropActive(false);
    if (!isDraft) return; // running/done columns reject the drop (§9.4).
    // Read the source race-proof from dataTransfer (set on dragStart), state as
    // fallback — same source-of-truth the Backlog REMOVE uses, so MOVE/REMOVE
    // are consistent and neither waits on a state commit.
    const src = readDragSource(e, dragSource);
    const taskId = src?.taskId ?? null;
    if (!taskId) return;
    const fromPlanId = src?.fromPlanId ?? null;
    try {
      if (fromPlanId === null) {
        // From the Backlog → SELECT into this draft plan (EXISTING behavior).
        await add.mutateAsync({ task_id: taskId });
      } else if (fromPlanId === plan.id) {
        // Dropped back onto its own plan → no-op (don't fire mutations).
        return;
      } else {
        // From ANOTHER draft plan → MOVE = remove from source THEN add to this
        // plan. Remove first (the source was draft, so it's allowed §9.4), then
        // add to the target. Both invalidate; the task ends in this plan. The
        // backend ops are idempotent + INSERT-OR-IGNORE — we fire each once.
        await removeAny.mutateAsync({ planId: fromPlanId, taskId });
        await addAny.mutateAsync({ planId: plan.id, taskId });
      }
    } catch {
      // surfaced by the board re-fetch.
    }
  };

  return (
    <div
      className={`${columnBase} bg-bg-elevated ${
        dropActive && canDrop ? 'border-accent ring-2 ring-accent' : 'border-border-base'
      }`}
      data-testid="plan-column"
      data-plan-id={plan.id}
      data-status={plan.status}
      data-droppable={isDraft ? 'true' : 'false'}
      role="listitem"
      onDragOver={(e) => {
        if (!canDrop) return; // don't allow drop on a running/done column.
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        setDropActive(true);
      }}
      onDragLeave={() => setDropActive(false)}
      onDrop={handleDrop}
    >
      <div className="flex items-start justify-between gap-1.5 px-0.5">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-sm font-bold text-text-primary" title={plan.name}>
            {plan.name}
          </span>
          <PlanStatusChip status={plan.status} />
        </span>
        <OrgLink
          to={`/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(plan.id)}`}
          className="shrink-0 text-[0.6875rem] font-semibold text-accent hover:underline"
          data-testid={`plan-open-${plan.id}`}
        >
          Open ▸
        </OrgLink>
      </div>
      <div className="flex items-center gap-1.5 px-0.5 pb-2 pt-0.5">
        <span className="tabular-nums text-[0.6875rem] text-text-muted" data-testid="plan-progress">
          {plan.status === 'draft' ? 'Planning' : 'In progress'} · {planProgressLabel(progress)}
        </span>
        {/* P2-4: a running plan self-progresses (auto-advance). Compact suffix. */}
        {plan.status === 'running' && <AutoAdvancingIndicator variant="column" />}
        <PlanFailedIndicator hasFailed={hasFailed} />
      </div>
      {shown.length === 0 ? (
        <p className="py-3 text-center text-[0.6875rem] text-text-muted" data-testid="plan-empty">
          No tasks yet.
        </p>
      ) : (
        shown.map((node) => (
          <PlanTaskCard
            key={node.task_id}
            projectId={projectId}
            planId={plan.id}
            node={node}
            // §9.4: removing a task from a Plan is a PLANNING action — only a
            // DRAFT Plan exposes the remove affordance (mirrors add-to-plan /
            // the A1 edge editor). running/done columns render NO remove control.
            // A7: canRemove (= isDraft) ALSO gates drag — only draft-plan cards
            // are draggable (the source must be draft so MOVE/REMOVE is allowed).
            canRemove={isDraft}
            setDragSource={setDragSource}
          />
        ))
      )}
      {overflow > 0 && (
        <p className="px-0.5 text-[0.6875rem] text-text-muted" data-testid={`plan-overflow-${plan.id}`}>
          …and {overflow} more
        </p>
      )}
    </div>
  );
}

// PlanTaskCard — a single Plan-column task card (from nodes_preview). A DRAFT
// Plan exposes a keyboard-accessible "Remove from plan" affordance per card
// (§9.4 planning-only); on success the task returns to the Backlog (the board's
// query invalidation refetches both the Plan list + the unplanned set). A
// running/done Plan renders NO remove control. #218: a remove failure surfaces a
// friendly inline message (never a raw API error) — the card stays put.
function PlanTaskCard({
  projectId,
  planId,
  node,
  canRemove,
  setDragSource,
}: {
  projectId: string;
  planId: string;
  node: PlanNode;
  canRemove: boolean;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  const remove = useRemoveTaskFromPlan(projectId, planId);
  // A7: a Plan-task card is draggable ONLY when its plan is draft (canRemove ==
  // isDraft) — moving it out runs RemoveTaskFromPlan on the source, which the
  // backend allows only for a draft plan (§9.4). running/done cards: no drag.
  const draggable = canRemove;
  return (
    <div
      className="mb-1.5 rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1"
      data-testid="plan-task-card"
      data-task-id={node.task_id}
      data-draggable={draggable ? 'true' : 'false'}
      draggable={draggable}
      onDragStart={
        draggable
          ? (e) => {
              // Carry the task AND its SOURCE plan so a drop can MOVE / REMOVE.
              // The state set is the render hint; the dataTransfer stamps are the
              // race-proof source (read at dragover/drop, no state-commit wait).
              setDragSource({ taskId: node.task_id, fromPlanId: planId });
              e.dataTransfer.setData('text/plain', node.task_id);
              // FROM_PLAN_MIME marks this as a plan-task drag (presence readable
              // during dragover) AND carries the source plan id (read at drop) so
              // the Backlog REMOVE + a cross-plan MOVE work without the React
              // `dragSource` state having committed first.
              e.dataTransfer.setData(FROM_PLAN_MIME, planId);
              e.dataTransfer.effectAllowed = 'move';
            }
          : undefined
      }
      onDragEnd={draggable ? () => setDragSource(null) : undefined}
    >
      <div className="flex items-start justify-between gap-1.5">
        <div className="min-w-0 flex-1 text-xs font-semibold leading-tight text-text-primary">
          <TaskTitleLink projectId={projectId} taskId={node.task_id} title={node.title} />
        </div>
        {canRemove && (
          <button
            type="button"
            className="-mr-0.5 -mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle hover:text-text-primary disabled:opacity-50"
            disabled={remove.isPending}
            aria-label={`Remove ${node.title} from plan`}
            title="Remove from plan (back to backlog)"
            data-testid={`plan-task-remove-${node.task_id}`}
            onClick={() => {
              // Direct remove (no confirm modal): removing from a Plan is
              // reversible — the task just returns to the Backlog (≠ delete).
              remove.mutate(node.task_id);
            }}
          >
            <RemoveIcon />
          </button>
        )}
      </div>
      <div className="mt-1.5 flex items-center justify-between gap-1.5">
        <AssigneeBadge assignee={node.assignee_ref} />
        <span className="inline-flex items-center gap-1">
          {/* Stage B (#283): archive badge coexists with the status chip. */}
          <TaskArchivedBadge archived={node.archived} taskId={node.task_id} />
          <StatusChip status={node.task_status} />
        </span>
      </div>
      {remove.isError && (
        <p
          className="mt-1.5 text-[0.6875rem] text-danger"
          role="alert"
          data-testid={`plan-task-remove-error-${node.task_id}`}
        >
          Couldn't remove this task from the plan. Please try again.
        </p>
      )}
    </div>
  );
}

// NewPlanColumn — the trailing dashed "New Plan" column (.newcol). Opens the
// reused New-Plan modal → useCreatePlan (= a new column).
function NewPlanColumn({ onClick }: { onClick: () => void }): React.ReactElement {
  return (
    <button
      type="button"
      className="flex w-[9.375rem] shrink-0 flex-col items-center justify-center gap-0.5 rounded-lg border border-dashed border-border-strong bg-bg-base px-2 py-3.5 text-xs font-medium text-accent hover:border-accent"
      onClick={onClick}
      data-testid="new-plan-column"
      role="listitem"
    >
      + New Plan
      <span className="text-[0.625rem] text-text-muted">(adds a column)</span>
    </button>
  );
}

// AssigneeBadge — the mockup's solid avatar + handle. agent = violet, human =
// cyan (both SOLID, white text → AA in BOTH modes; theme-independent literals,
// NOT alpha-tint). Empty assignee → a neutral "Unassigned" badge. The raw ref
// is on hover (title); the visible text is the resolved DISPLAY NAME (#160) —
// falling back to the clean handle (#192) when the member directory doesn't
// resolve the ref. NO emoji. The resolver's internal useMembers() query is what
// loads the org member directory into this page, so no extra load hook is needed.
function AssigneeBadge({ assignee }: { assignee?: string | null }): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  if (!assignee) {
    return (
      <span className="inline-flex items-center gap-1 text-[0.6875rem] text-text-muted" data-testid="assignee">
        <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-bg-subtle text-[0.5rem] font-bold text-text-muted">
          ?
        </span>
        Unassigned
      </span>
    );
  }
  const kind = refKind(assignee); // 'agent' | 'user'
  // Resolve the identity ref → display name; on a miss the resolver returns the
  // raw ref unchanged (the #192/#215 sentinel), so fall back to the clean handle
  // (prefix stripped) — NEVER paint the raw prefixed ref as visible text.
  const resolved = resolveName(assignee);
  const label = resolved === assignee ? normalizeIdentityRef(assignee) : resolved;
  const disc = kind === 'agent' ? 'bg-status-violet-solid' : 'bg-status-cyan-solid';
  return (
    <span
      className="inline-flex items-center gap-1 text-[0.6875rem] text-text-secondary"
      data-testid="assignee"
      data-kind={kind}
      title={assignee}
    >
      <span
        className={`inline-flex h-4 w-4 items-center justify-center rounded-full text-[0.5rem] font-bold text-white ${disc}`}
        aria-hidden="true"
      >
        {label.charAt(0).toUpperCase()}
      </span>
      {label}
    </span>
  );
}

// --- inline SVG icons (no emoji pictographs, per the a11y guardrail) ----------

function BacklogIcon(): React.ReactElement {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="2.5" y="2" width="11" height="12" rx="1.5" stroke="currentColor" strokeWidth="1.4" />
      <path d="M5.5 5.5h5M5.5 8h5M5.5 10.5h3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  );
}

// PoolIcon — a "layers / stack" glyph for the Assignment Pool header (NOT an
// emoji, per the a11y guardrail). aria-hidden — the visible "Assignment Pool"
// label is the accessible name.
function PoolIcon(): React.ReactElement {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M8 1.5L14.5 5 8 8.5 1.5 5 8 1.5z" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" />
      <path d="M1.5 8L8 11.5 14.5 8M1.5 11L8 14.5 14.5 11" stroke="currentColor" strokeWidth="1.4" strokeLinejoin="round" />
    </svg>
  );
}

// "×" remove glyph as an inline SVG (NOT an emoji/pictograph, per the a11y
// guardrail). aria-hidden — the accessible name lives on the wrapping <button>.
function RemoveIcon(): React.ReactElement {
  return (
    <svg width="11" height="11" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  );
}

function ChevronDownIcon(): React.ReactElement {
  return (
    <svg width="10" height="10" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M4 6l4 4 4-4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

const modalInputClass =
  'mt-1 block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

// PlanCreateModal — create an empty Plan (name + optional description + target
// date). Reused by the board's "New Plan" column. target_date is a native date
// input; converted to an RFC3339 instant (local-offset) so the backend stores
// an absolute time, not a naive UTC date.
export function PlanCreateModal({
  projectId,
  onClose,
}: {
  projectId: string;
  onClose: () => void;
}): React.ReactElement {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [targetDate, setTargetDate] = useState('');
  const create = useCreatePlan(projectId);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    const input: CreatePlanInput = { name: name.trim() };
    if (description.trim()) input.description = description.trim();
    // YYYY-MM-DD → RFC3339 with local offset (absolute instant, not naive UTC).
    if (targetDate) {
      const d = new Date(`${targetDate}T00:00:00`);
      if (!Number.isNaN(d.getTime())) input.target_date = d.toISOString();
    }
    try {
      await create.mutateAsync(input);
      onClose();
    } catch {
      // surfaced inline below
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="plan-create-modal"
      role="dialog"
      aria-modal="true"
      aria-label="New plan"
    >
      <form onSubmit={submit} className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-4 text-lg font-semibold">New Plan</h2>
        <label className="block text-xs font-medium" htmlFor="plan-name">
          Name
        </label>
        <input
          id="plan-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={modalInputClass}
          data-testid="plan-create-name"
          autoFocus
        />
        <label className="mt-3 block text-xs font-medium" htmlFor="plan-description">
          Description
        </label>
        <textarea
          id="plan-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          className={modalInputClass}
          data-testid="plan-create-description"
        />
        <label className="mt-3 block text-xs font-medium" htmlFor="plan-target-date">
          Target date
        </label>
        <input
          id="plan-target-date"
          type="date"
          lang="en"
          value={targetDate}
          onChange={(e) => setTargetDate(e.target.value)}
          className={modalInputClass}
          data-testid="plan-create-target-date"
        />
        {create.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="plan-create-error">
            {(create.error as Error).message}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={create.isPending || !name.trim()}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="plan-create-submit"
          >
            {create.isPending ? 'Creating…' : 'Create Plan'}
          </button>
        </div>
      </form>
    </div>
  );
}

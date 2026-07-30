import React, { createContext, useContext, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useBoardTouchDrag } from './useBoardTouchDrag';
import { type DropTarget } from './boardDrop';
import { useProject } from '@/api/projects';
import {
  usePlans,
  useAssignmentPool,
  useCreatePlan,
  useUnplannedTasks,
  useAddTaskToPlan,
  useRemoveTaskFromPlan,
  useAddTaskToAnyPlan,
  useRemoveTaskFromAnyPlan,
  useAddTaskToAssignmentPool,
  useRemoveTaskFromAssignmentPool,
  type Plan,
  type PlanNode,
  type AssignmentPool,
  type AssignmentPoolTask,
  type CreatePlanInput,
} from '@/api/plans';
import { refKind, useDisplayNameResolver, normalizeIdentityRef } from '@/api/members';
import type { Task } from '@/api/types';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { BoardTaskCreateModal } from '@/components/BoardTaskCreateModal';
import { ErrorState } from '@/components/ErrorState';
import { TaskTitleLink } from '@/components/TaskTitleLink';
import { StatusChip, refLabel } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, AutoAdvancingIndicator, TaskArchivedBadge, planProgressLabel } from '@/components/planDisplay';
import { SenderSidebarProvider, useSenderSidebar } from '@/components/SenderSidebarContext';

// v2.10.1 [M5] — touch long-press drag plumbing. The board's cards pick up the
// `startLongPress` handler from this context so a touch drag can be started from
// any card type without prop-drilling through three column components. null on
// desktop-only renders (the cards then rely on native HTML5 drag for mouse).
type StartLongPress = (
  e: React.PointerEvent,
  taskId: string,
  fromPlanId: string | null,
  title: string,
) => void;
const BoardTouchDragContext = createContext<StartLongPress | null>(null);

// v2.10.1 [M5] — owner ask: the Work Board is naturally wide, so on a phone in
// PORTRAIT we nudge "↻ rotate to landscape" (mockup workboard-mobile: landscape
// fits 3–4 columns + smoother drag). Mobile-only + dismissible for the session.
const ROTATE_HINT_KEY = 'ac.workboard.rotateHintDismissed';
function rotateHintDismissed(): boolean {
  try {
    return (
      typeof sessionStorage !== 'undefined' &&
      typeof sessionStorage.getItem === 'function' &&
      sessionStorage.getItem(ROTATE_HINT_KEY) === '1'
    );
  } catch {
    return false;
  }
}
function RotateForBoardHint(): React.ReactElement | null {
  const { t } = useTranslation('work');
  const [portrait, setPortrait] = useState(false);
  const [dismissed, setDismissed] = useState(rotateHintDismissed);
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const mq = window.matchMedia('(max-width: 767px) and (orientation: portrait)');
    const update = (): void => setPortrait(mq.matches);
    update();
    mq.addEventListener?.('change', update);
    return () => mq.removeEventListener?.('change', update);
  }, []);
  if (dismissed || !portrait) return null;
  return (
    <div
      className="flex items-center justify-between gap-2 rounded-lg border border-status-blue-border bg-status-blue-bg px-3 py-2 text-xs text-status-blue-fg md:hidden"
      role="status"
      data-testid="workboard-rotate-hint"
    >
      <span>{t('plan.board.rotateHint')}</span>
      <button
        type="button"
        aria-label={t('plan.board.rotateDismiss')}
        data-testid="workboard-rotate-dismiss"
        className="-mr-1 shrink-0 rounded px-1.5 py-0.5 font-semibold hover:bg-status-blue-border/40"
        onClick={() => {
          setDismissed(true);
          try {
            sessionStorage.setItem(ROTATE_HINT_KEY, '1');
          } catch {
            // ignore — best-effort session persistence
          }
        }}
      >
        ×
      </button>
    </div>
  );
}

// ProjectPlans (/projects/:id/plans) — v2.9 #291 WORK BOARD (the headline
// Plan-Orchestration PLANNING view). A horizontal kanban: a first Backlog
// column (the project's UNPLANNED tasks) + one column per Plan + a trailing
// "New Plan" column. Planning = drag (or use the keyboard add-button on) a
// Backlog task into a PENDING Plan column (§9.4 pending-only select-into-plan).
// Reached via the project detail Plans link (§4.2); each Plan column's
// "Open ▸" reaches the Plan detail (#287 DAG, the EXECUTION view).
//
// Refactored FROM the #286 Plan-card list into this board.
export default function ProjectPlans(): React.ReactElement {
  const { t } = useTranslation('work');
  const { id = '' } = useParams<{ id: string }>();
  const project = useProject(id);
  const plans = usePlans(id);
  const assignmentPool = useAssignmentPool(id);
  const backlog = useUnplannedTasks(id);
  const [createOpen, setCreateOpen] = useState(false);
  // T231: the header "+ New Task" modal — creates a task with a chosen
  // destination (Backlog / Assignment Pool / a pending Plan).
  const [taskCreateOpen, setTaskCreateOpen] = useState(false);
  // The task currently being dragged (HTML5 DnD) + WHERE it came from. Held in
  // state so a pending Plan column can light up its drop-zone + reject running
  // columns, AND so a drop knows the source (Backlog vs another Plan) to pick
  // SELECT (backlog→plan) vs MOVE (plan→plan) vs REMOVE (plan→backlog). A7:
  // fromPlanId === null ⟺ dragged from the Backlog; non-null ⟺ from that Plan.
  const [dragSource, setDragSource] = useState<DragSource | null>(null);

  const projectName = project.data?.name ?? id;

  return (
    // Wrap the board in the sender-sidebar provider so clicking an agent assignee
    // on any card opens the shared agent-activity sidebar (owner ask 2026-07-03).
    <SenderSidebarProvider>
    <section className="space-y-4" data-testid="page-ProjectPlans" data-project-id={id}>
      <Breadcrumb
        items={[
          { label: t('plan.board.breadcrumb.projects'), to: '/projects' },
          { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
          { label: t('plan.board.breadcrumb.workBoard') },
        ]}
      />
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div>
          {/* v2.10.0 [T5]: header carries the project name (mockup
              docs/design/v2.10.0/workboard.html — "<project> · Work Board"); the
              col② project sub-nav already provides the in-project navigation. */}
          <h1 className="font-heading text-2xl font-semibold text-text-primary">
            <span data-testid="workboard-project-name">{projectName}</span>
            <span className="text-text-muted">{t('plan.board.titleSuffix')}</span>
          </h1>
          <p className="mt-0.5 text-xs text-text-muted">
            {t('plan.board.subtitle')}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {/* T231: create a task straight from the board, choosing its
              destination (Backlog / Assignment Pool / a pending Plan). */}
          <button
            type="button"
            className="rounded border border-brand px-2.5 py-1 text-xs font-medium text-brand hover:bg-brand/10"
            onClick={() => setTaskCreateOpen(true)}
            data-testid="task-create-btn"
          >
            {t('plan.board.newTask')}
          </button>
          <button
            type="button"
            className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
            onClick={() => setCreateOpen(true)}
            data-testid="plan-create-btn"
          >
            {t('plan.board.newPlan')}
          </button>
        </div>
      </header>

      {createOpen && <PlanCreateModal projectId={id} onClose={() => setCreateOpen(false)} />}
      {taskCreateOpen && (
        <BoardTaskCreateModal
          projectId={id}
          plans={plans.data}
          assignmentPool={assignmentPool.data}
          onClose={() => setTaskCreateOpen(false)}
        />
      )}

      {/* Mobile portrait nudge → landscape (owner ask). */}
      <RotateForBoardHint />

      <Board
        projectId={id}
        plans={plans}
        assignmentPool={assignmentPool}
        backlog={backlog}
        dragSource={dragSource}
        setDragSource={setDragSource}
        onNewPlan={() => setCreateOpen(true)}
      />

      {/* Mobile FAB → New Plan (sits above the bottom tab bar + safe area). */}
      <button
        type="button"
        onClick={() => setCreateOpen(true)}
        aria-label={t('plan.board.newPlanAria')}
        data-testid="workboard-fab"
        className="fixed bottom-[calc(env(safe-area-inset-bottom)+4.5rem)] right-4 z-30 flex h-14 w-14 items-center justify-center rounded-full bg-brand text-white shadow-2 hover:bg-brand-hover md:hidden"
      >
        <span aria-hidden="true" className="text-3xl font-light leading-none">+</span>
      </button>
    </section>
    </SenderSidebarProvider>
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
interface AssignmentPoolQuery {
  data?: AssignmentPool;
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}

const POOL_SOURCE_PREFIX = 'pool:';
const poolSource = (poolId: string): string => `${POOL_SOURCE_PREFIX}${poolId}`;
const isPoolSource = (source: string | null): boolean => source?.startsWith(POOL_SOURCE_PREFIX) ?? false;

// Board — the horizontal scrolling kanban (.board: flex, gap, overflow-x-auto).
// ADR-0047 THREE-segment Work Board, left→right:
//   1. Backlog        — unscheduled tasks (plan_id == ""), NOT claimable, flat.
//   2. Assignment Pool — an independent Project child: a FLAT low-priority pull
//                        queue whose eligible tasks are CLAIMABLE. No DAG edges.
//   3. Structured Plans — every Plan, rendered as an existing DAG column.
// Then the trailing New-Plan column (creates a structured plan).
function Board({
  projectId,
  plans,
  assignmentPool,
  backlog,
  dragSource,
  setDragSource,
  onNewPlan,
}: {
  projectId: string;
  plans: PlansQuery;
  assignmentPool: AssignmentPoolQuery;
  backlog: TasksQuery;
  dragSource: DragSource | null;
  setDragSource: (s: DragSource | null) => void;
  onNewPlan: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  // v2.10.1 [M5] touch drag: the any-plan mutations let a touch drop run the same
  // SELECT / MOVE / REMOVE the HTML5 handlers do, decided by the pure decideDrop.
  // dragSource is set on pickup so the columns' data-droppable validity (and thus
  // the hit-test) matches mouse DnD exactly. Hooks run before the early returns.
  const addAny = useAddTaskToAnyPlan(projectId);
  const removeAny = useRemoveTaskFromAnyPlan(projectId);
  const addPool = useAddTaskToAssignmentPool(projectId);
  const removePool = useRemoveTaskFromAssignmentPool(projectId);
  const onTouchDrop = (taskId: string, fromPlanId: string | null, target: DropTarget): void => {
    const sourceIsPool = isPoolSource(fromPlanId);
    const removeSource = async (): Promise<void> => {
      if (fromPlanId === null) return;
      if (sourceIsPool) await removePool.mutateAsync(taskId);
      else await removeAny.mutateAsync({ planId: fromPlanId, taskId });
    };
    if (target.kind === 'backlog') {
      void removeSource();
      return;
    }
    if (target.kind === 'pool') {
      if (sourceIsPool) return;
      void removeSource().then(() => addPool.mutate({ taskId }));
      return;
    }
    if (fromPlanId === target.planId) return;
    void removeSource().then(() => addAny.mutate({ planId: target.planId, taskId }));
  };
  const { preview, startLongPress } = useBoardTouchDrag({
    onStart: (taskId, fromPlanId) => setDragSource({ taskId, fromPlanId }),
    onEnd: () => setDragSource(null),
    onDrop: onTouchDrop,
  });

  // A board-level load is only "failed" if the Plans list (the columns) fails —
  // surface the #218 friendly ErrorState. The Backlog has its own inline state.
  if (plans.isError || assignmentPool.isError) {
    return (
      <ErrorState message={t('plan.board.loadError')} error={plans.error ?? assignmentPool.error} testId="board-error" />
    );
  }
  if (plans.isLoading || assignmentPool.isLoading) {
    return (
      <div className="flex gap-3" data-testid="board-loading">
        <Skeleton height="12rem" width="14.75rem" />
        <Skeleton height="12rem" width="14.75rem" />
        <Skeleton height="12rem" width="14.75rem" />
      </div>
    );
  }
  // task-1099941e: the Work Board excludes ARCHIVED plans (mirrors project/channel
  // archive — archived work leaves the active board). The backend list already
  // default-excludes them (ListPlanSummaries); this FE filter is the belt-and-
  // braces guard so a degraded/stale payload never leaks an archived plan column.
  const planList = (plans.data ?? []).filter((p) => !p.archived_at);
  const structuredPlans = planList.filter((p) => p.is_builtin !== true);
  const pool = assignmentPool.data ?? null;

  // The pending STRUCTURED Plans are the only valid add/drop targets (§9.4
  // select-into-plan is pending-only); shared by every Backlog card's add-menu.
  // The independent pool is offered separately and has no Plan lifecycle.
  const pendingPlans = structuredPlans.filter((p) => p.status === 'pending');

  return (
    <BoardTouchDragContext.Provider value={startLongPress}>
      {/* Portrait scroll-snap (mobile): one-handed column browsing; snap off ≥md. */}
      <div
        className="flex snap-x snap-mandatory items-start gap-3 overflow-x-auto pb-2 md:snap-none"
        data-testid="work-board"
        role="list"
        aria-label={t('plan.board.boardAria')}
      >
        <BacklogColumn
          projectId={projectId}
          backlog={backlog}
          pendingPlans={pendingPlans}
          assignmentPool={pool}
          dragSource={dragSource}
          setDragSource={setDragSource}
        />
        {pool && (
          <AssignmentPoolColumn
            projectId={projectId}
            pool={pool}
            dragSource={dragSource}
            setDragSource={setDragSource}
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

      {/* Floating drag preview that follows the finger during a touch drag.
          pointer-events-none so the column hit-test (elementFromPoint) sees through it. */}
      {preview && (
        <div
          className="pointer-events-none fixed z-50 max-w-[12rem] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-accent bg-bg-elevated px-2 py-1.5 text-xs font-semibold text-text-primary shadow-2"
          style={{ left: preview.x, top: preview.y }}
          data-testid="board-drag-preview"
        >
          <span className="line-clamp-2">{preview.title}</span>
        </div>
      )}
    </BoardTouchDragContext.Provider>
  );
}

// HIDDEN_NODE / HIDDEN_TASK — ADR-0047: completed + discarded work is HIDDEN by
// default in the Backlog and the Assignment Pool (those are "live capacity"
// segments). Structured plans KEEP done nodes (history). The BE may already
// exclude them; we also filter on the FE so a degraded payload never leaks them.
function isLiveTaskStatus(status: string | undefined): boolean {
  return status !== 'completed' && status !== 'discarded';
}

// planLockReason — T121: the human reason a plan's task assignments are frozen, so
// the Work Board can explain (tooltip + in-drag banner) why a running / terminal
// plan can't be dragged out of or dropped into. Only a PENDING plan's task-set is
// editable; the always-running built-in pool is the deliberate exception and never
// renders as a locked column. Archived plans are excluded from the board entirely.
function planLockReason(status: string, t: (key: string) => string): string {
  if (status === 'running') {
    return t('plan.board.lock.running');
  }
  if (status === 'paused') {
    return t('plan.board.lock.paused');
  }
  if (status === 'done') {
    return t('plan.board.lock.done');
  }
  if (status === 'discarded') {
    return t('plan.board.lock.archived');
  }
  return t('plan.board.lock.default');
}

// columnBase — the shared .col look (fixed ~236px, solid subtle bg, border).
// SOLID theme tokens only (bg-bg-subtle / border-border-base) — no alpha-tint,
// AA in both modes.
const columnBase =
  'flex w-[14.75rem] shrink-0 snap-start flex-col rounded-lg border p-2.5';

// BacklogColumn — first column (distinct .col.backlog bg). Lists the project's
// UNPLANNED tasks; each card has the keyboard-accessible "Add to plan" menu and
// is HTML5-draggable into a pending Plan column.
function BacklogColumn({
  projectId,
  backlog,
  pendingPlans,
  assignmentPool,
  dragSource,
  setDragSource,
}: {
  projectId: string;
  backlog: TasksQuery;
  pendingPlans: Plan[];
  assignmentPool: AssignmentPool | null;
  dragSource: DragSource | null;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  // ADR-0047: HIDE completed/discarded in the Backlog by default (live capacity
  // only). The BE `?unplanned=1` may already exclude them; the FE filter is the
  // belt-and-braces guard so a degraded payload never leaks terminal work.
  const tasks = (backlog.data ?? []).filter((t) => isLiveTaskStatus(t.status));
  const remove = useRemoveTaskFromAnyPlan(projectId);
  const removePool = useRemoveTaskFromAssignmentPool(projectId);
  const [dropActive, setDropActive] = useState(false);
  // A7: the Backlog accepts a drop only when a PLAN-task is being dragged
  // (fromPlanId != null) → REMOVE = back to backlog. A backlog card dropped on
  // the backlog is a no-op (fromPlanId == null). The source plan was pending (only
  // pending cards are draggable), so RemoveTaskFromPlan is allowed (§9.4).
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
    if (isPoolSource(src.fromPlanId)) removePool.mutate(src.taskId);
    else remove.mutate({ planId: src.fromPlanId, taskId: src.taskId });
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
            {t('plan.board.backlog.title')}
          </span>
          <span className="tabular-nums text-[0.6875rem] text-text-muted" data-testid="backlog-count">
            {tasks.length}
          </span>
        </div>
        <p className="mt-0.5 text-[0.625rem] leading-tight text-text-muted" data-testid="backlog-subtitle">
          {t('plan.board.backlog.subtitle')}
        </p>
      </div>
      {backlog.isError ? (
        <ErrorState
          message={t('plan.board.backlog.loadError')}
          error={backlog.error}
          testId="backlog-error"
        />
      ) : backlog.isLoading ? (
        <Skeleton height="3rem" />
      ) : tasks.length === 0 ? (
        <p className="py-4 text-center text-[0.6875rem] text-text-muted" data-testid="backlog-empty">
          {t('plan.board.backlog.empty')}
        </p>
      ) : (
        tasks.map((task) => (
          <BacklogCard
            key={task.id}
            projectId={projectId}
            task={task}
            pendingPlans={pendingPlans}
            assignmentPool={assignmentPool}
            setDragSource={setDragSource}
          />
        ))
      )}
    </div>
  );
}

// BacklogCard — a draggable task card with the keyboard-accessible "Add to
// plan ▾" menu. Both the menu (a11y PRIMARY) + drag-drop fire the SAME
// select-into-plan (useAddTaskToPlan), pending-only.
function BacklogCard({
  projectId,
  task,
  pendingPlans,
  assignmentPool,
  setDragSource,
}: {
  projectId: string;
  task: Task;
  pendingPlans: Plan[];
  assignmentPool: AssignmentPool | null;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const [menuOpen, setMenuOpen] = useState(false);
  // v2.10.1 [M5]: touch long-press starts a drag (mouse keeps native HTML5 DnD).
  const startLongPress = useContext(BoardTouchDragContext);
  return (
    <div
      className="mb-1.5 rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1"
      data-testid="backlog-card"
      data-task-id={task.id}
      draggable
      onPointerDown={(e) => startLongPress?.(e, task.id, null, task.title)}
      onDragStart={(e) => {
        // A7: from the Backlog → fromPlanId null (SELECT into a pending plan).
        setDragSource({ taskId: task.id, fromPlanId: null });
        e.dataTransfer.setData('text/plain', task.id);
        e.dataTransfer.effectAllowed = 'move';
      }}
      onDragEnd={() => setDragSource(null)}
    >
      {/* Owner ask (2026-07-03): a backlog card shows its human task id (T123),
          same as the pool / plan-node cards. */}
      <div className="mb-1 flex items-center gap-1">
        <TaskIdTag taskId={task.id} orgRef={task.org_ref} />
      </div>
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
          {t('plan.board.addToPlan')}
          <ChevronDownIcon />
        </button>
        {menuOpen && (
          <AddToPlanMenu
            projectId={projectId}
            taskId={task.id}
            pendingPlans={pendingPlans}
            assignmentPool={assignmentPool}
            onClose={() => setMenuOpen(false)}
          />
        )}
      </div>
    </div>
  );
}

// AddToPlanMenu — the pending-Plan picker. §9.4: only PENDING Plans are listed (a
// running Plan is never a select-into-plan target). Each item calls
// useAddTaskToPlan. Keyboard-operable (real <button>s, focusable).
function AddToPlanMenu({
  projectId,
  taskId,
  pendingPlans,
  assignmentPool,
  onClose,
}: {
  projectId: string;
  taskId: string;
  pendingPlans: Plan[];
  assignmentPool: AssignmentPool | null;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const noTargets = pendingPlans.length === 0 && !assignmentPool;
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
          {t('plan.board.addMenu.empty')}
        </p>
      ) : (
        <>
          {/* ADR-0047: the built-in Assignment Pool is a select target (BE permits
              moving a backlog task into the pool → it becomes claimable). */}
          {assignmentPool && (
            <AddToPoolItem
              projectId={projectId}
              taskId={taskId}
              onDone={onClose}
            />
          )}
          {pendingPlans.map((plan) => (
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

function AddToPoolItem({
  projectId,
  taskId,
  onDone,
}: {
  projectId: string;
  taskId: string;
  onDone: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const add = useAddTaskToAssignmentPool(projectId);
  return (
    <button
      type="button"
      role="menuitem"
      className="block w-full truncate rounded px-2 py-1.5 text-left text-xs text-text-primary hover:bg-bg-subtle disabled:opacity-60"
      disabled={add.isPending}
      data-testid={`add-to-pool-${taskId}`}
      onClick={async () => {
        try {
          await add.mutateAsync({ taskId });
          onDone();
        } catch {
          // Mutation error remains visible through the refreshed board state.
        }
      }}
    >
      {t('plan.board.assignmentPool')}
    </button>
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
// CLAIMABLE (assigned + dispatched; pull, no-wake). Both-mode AA: SOLID
// status-emerald token chip (bg=emerald-100 hex light + AA-paired dark, fg=
// emerald-800 — contrast ≥4.5 in both modes via the no-raw-colors token system).
// NO alpha-tint. Text label "Claimable" (never color alone) + tiny inline SVG
// (NOT emoji). Renders nothing when the node is not claimable.
function ClaimableChip({
  claimable,
  taskId,
}: {
  claimable: boolean | undefined;
  taskId: string;
}): React.ReactElement | null {
  const { t } = useTranslation('work');
  if (!claimable) return null;
  return (
    <span
      className="inline-flex items-center gap-1 rounded bg-status-emerald-bg px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-status-emerald-fg"
      data-testid={`claimable-chip-${taskId}`}
      title={t('plan.board.claimable.title')}
    >
      {/* hand / pull glyph */}
      <svg viewBox="0 0 24 24" className="h-2.5 w-2.5" fill="none" stroke="currentColor" strokeWidth="2.4" aria-hidden="true">
        <path d="M5 12V7a2 2 0 0 1 4 0v5" />
        <path d="M9 11V5a2 2 0 0 1 4 0v6" />
        <path d="M13 11V6a2 2 0 0 1 4 0v8a6 6 0 0 1-6 6h-1a6 6 0 0 1-5-3l-2-3" />
      </svg>
      {t('plan.board.claimable.label')}
    </span>
  );
}

// StarvedBadge — T566 (issue-577a7b0e). A claimable pool task that declares
// required_capabilities but has NO eligible online agent right now stays in the
// pool; the reconciler (BE-2) marks it `starved`. We surface a clear "waiting for
// a qualified agent" badge (never color alone — label + icon) so it's visible
// that the task is stuck on capability matching, not silently idle. Renders
// nothing when the node is not starved.
function StarvedBadge({
  starved,
  taskId,
}: {
  starved: boolean | undefined;
  taskId: string;
}): React.ReactElement | null {
  const { t } = useTranslation('work');
  if (!starved) return null;
  return (
    <span
      className="inline-flex items-center gap-1 rounded bg-status-amber-bg px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-status-amber-fg"
      data-testid={`starved-badge-${taskId}`}
      title={t('plan.board.starved.title')}
    >
      {/* hourglass / waiting glyph */}
      <svg viewBox="0 0 24 24" className="h-2.5 w-2.5" fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden="true">
        <path d="M6 3h12M6 21h12M8 3v4l4 4 4-4V3M8 21v-4l4-4 4 4v4" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      {t('plan.board.starved.label')}
    </span>
  );
}

// AssignmentPoolColumn is the first-class low-priority pull queue. It is a flat
// Project child, not a Plan and therefore has no lifecycle/DAG/progress state.
function AssignmentPoolColumn({
  projectId,
  pool,
  dragSource,
  setDragSource,
}: {
  projectId: string;
  pool: AssignmentPool;
  dragSource: DragSource | null;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const add = useAddTaskToAssignmentPool(projectId);
  const removeAny = useRemoveTaskFromAnyPlan(projectId);
  const [dropActive, setDropActive] = useState(false);
  // Defensive reads (mirror PlanColumn) — degrade to an empty pool, never crash.
  const shown = (pool.tasks ?? []).filter((task) => isLiveTaskStatus(task.status));

  // T121: the pool accepts ANY in-flight task drag that is not its own card —
  // a Backlog task (SELECT) or a task from another plan (MOVE-in). A drag of one
  // of the pool's OWN cards back onto the pool is the no-op self case.
  const dragTaskId = dragSource?.taskId ?? null;
  const ownSource = poolSource(pool.id);
  const canDrop = dragTaskId !== null && dragSource?.fromPlanId !== ownSource;

  // Race-proof acceptance (mirror the Backlog): accept on the state-derived
  // canDrop OR the dataTransfer plan-task marker (readable on every dragover
  // before the React state commits). The self-pool case at worst briefly
  // highlights; handleDrop's readDragSource resolves it to a no-op.
  const acceptsDrag = (e: React.DragEvent) =>
    canDrop || e.dataTransfer.types.includes(FROM_PLAN_MIME);

  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setDropActive(false);
    const src = readDragSource(e, dragSource);
    const taskId = src?.taskId ?? null;
    if (!taskId) return;
    const fromPlanId = src?.fromPlanId ?? null;
    if (fromPlanId === ownSource) return;
    try {
      if (fromPlanId === null) {
        await add.mutateAsync({ taskId });
      } else if (isPoolSource(fromPlanId)) {
        return;
      } else {
        await removeAny.mutateAsync({ planId: fromPlanId, taskId });
        await add.mutateAsync({ taskId });
      }
    } catch {
      // surfaced by the board re-fetch.
    }
  };

  return (
    <div
      className={`${columnBase} bg-bg-elevated ${
        dropActive && canDrop ? 'border-accent ring-2 ring-accent' : 'border-status-emerald-border'
      }`}
      data-testid="builtin-pool-column"
      data-plan-id={pool.id}
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
            {t('plan.board.assignmentPool')}
          </span>
          <span className="tabular-nums text-[0.6875rem] text-text-muted" data-testid="builtin-pool-count">
            {shown.length}
          </span>
        </div>
        <p className="mt-0.5 text-[0.625rem] leading-tight text-text-muted" data-testid="builtin-pool-subtitle">
          {t('plan.board.pool.subtitle')}
        </p>
        <p className="mt-1 font-mono text-[0.5625rem] uppercase tracking-wide text-text-muted">
          {pool.scheduling_class} · cap {pool.holding_cap}
        </p>
      </div>
      {shown.length === 0 ? (
        <p className="py-3 text-center text-[0.6875rem] text-text-muted" data-testid="builtin-pool-empty">
          {t('plan.board.pool.empty')}
        </p>
      ) : (
        // task-0543ece9: all live pool nodes in a bounded scroll area (no cap).
        <div className="max-h-[26rem] space-y-0 overflow-y-auto" data-testid={`pool-cards-${pool.id}`}>
          {shown.map((task) => (
            <PoolTaskCard
              key={task.id}
              projectId={projectId}
              poolId={pool.id}
              task={task}
              setDragSource={setDragSource}
            />
          ))}
        </div>
      )}
    </div>
  );
}

// PoolTaskCard — a single built-in-pool task card. Shows the ClaimableChip when
// the node is claimable, alongside the task status chip + archive badge.
// T121: the card is DRAGGABLE — the pool's task-set is freely editable, so a pool
// task can be dragged out to the Backlog (REMOVE) or to another pending plan
// (MOVE). It stamps its SOURCE plan (the pool id) into dataTransfer exactly like a
// PlanTaskCard so the drop targets pick SELECT/MOVE/REMOVE correctly. The pool is
// flat (no DAG), so there is no per-card remove button — drag is the affordance.
function PoolTaskCard({
  projectId,
  poolId,
  task,
  setDragSource,
}: {
  projectId: string;
  poolId: string;
  task: AssignmentPoolTask;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  // v2.10.1 [M5]: touch long-press drag (mouse keeps native HTML5 DnD).
  const startLongPress = useContext(BoardTouchDragContext);
  return (
    <div
      className="mb-1.5 cursor-grab rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1 active:cursor-grabbing"
      data-testid="pool-task-card"
      data-task-id={task.id}
      data-draggable="true"
      draggable
      onPointerDown={(e) => startLongPress?.(e, task.id, poolSource(poolId), task.title)}
      onDragStart={(e) => {
        const source = poolSource(poolId);
        setDragSource({ taskId: task.id, fromPlanId: source });
        e.dataTransfer.setData('text/plain', task.id);
        e.dataTransfer.setData(FROM_PLAN_MIME, source);
        e.dataTransfer.effectAllowed = 'move';
      }}
      onDragEnd={() => setDragSource(null)}
    >
      <div className="mb-1 flex items-center gap-1">
        <TaskIdTag taskId={task.id} orgRef={task.org_ref} />
      </div>
      <div className="mb-1.5 text-xs font-semibold leading-tight text-text-primary">
        <TaskTitleLink projectId={projectId} taskId={task.id} title={task.title} />
      </div>
      <div className="flex flex-wrap items-center justify-between gap-1.5">
        <AssigneeBadge assignee={task.assignee} />
        <span className="inline-flex items-center gap-1">
          <ClaimableChip claimable={task.claimable} taskId={task.id} />
          <StarvedBadge starved={task.starved} taskId={task.id} />
          <TaskArchivedBadge archived={task.archived} taskId={task.id} />
          <StatusChip status={task.status} />
        </span>
      </div>
    </div>
  );
}

// TaskIdTag — a small monospace pill showing the human Task id (org_ref "T123"),
// falling back to "#"+id-tail when the node has no org_ref (pre-allocator rows,
// #192 id-as-content). v2.9.2 (task-0543ece9): the board card now shows the
// T-number directly from `node.org_ref` (the node DTO carries it — no resolver).
// Mirrors PlanDetail's TaskIdTag: solid theme tokens (both-mode AA, no alpha-tint),
// full task_id on hover.
function TaskIdTag({ taskId, orgRef }: { taskId: string; orgRef?: string }): React.ReactElement {
  const label = refLabel(orgRef, taskId);
  return (
    <span
      className="inline-flex shrink-0 items-center rounded bg-bg-subtle px-1 py-0.5 font-mono text-[0.625rem] font-semibold text-text-secondary"
      data-testid={`plan-card-taskid-${taskId}`}
      title={taskId}
    >
      {label}
    </span>
  );
}

// PlanColumn — one column per Plan. Header = name + status chip + Open ▸ link;
// sub-header = progress + has_failed. Cards = the Plan's nodes_preview (capped 4
// by the backend, no add-button), with an "…and M more" overflow from node_count.
// A PENDING column is a valid HTML5 drop target (select-into-plan); a running/done
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
  const { t } = useTranslation('work');
  const add = useAddTaskToPlan(projectId, plan.id);
  // A7: cross-column MOVE needs to remove from the SOURCE plan + add to THIS
  // plan; the source plan is only known at drop time → the any-plan variants.
  const addAny = useAddTaskToAnyPlan(projectId);
  const removeAny = useRemoveTaskFromAnyPlan(projectId);
  const removePool = useRemoveTaskFromAssignmentPool(projectId);
  const [dropActive, setDropActive] = useState(false);
  const isPending = plan.status === 'pending';
  const dragTaskId = dragSource?.taskId ?? null;
  // Defensive reads — see the DEFENSIVE DEFAULTS note above.
  const progress = plan.progress ?? { done: 0, total: 0 };
  const hasFailed = plan.has_failed ?? false;
  const preview = plan.nodes_preview ?? [];
  const nodeCount = plan.node_count ?? 0;
  // Cards = the (already-capped) preview; overflow = total − shown, only if > 0.
  const shown = preview;
  const overflow = nodeCount - shown.length > 0 ? nodeCount - shown.length : 0;

  // A drop is valid only on a pending column while a task is being dragged.
  const canDrop = isPending && dragTaskId !== null;
  // T121 locked state: a running / done (terminal) plan's task-set is frozen — its
  // cards can't be dragged out and it can't be a drop target. `dropBlocked` is the
  // in-drag feedback: a drag is in flight but THIS column rejects it (so we show a
  // no-drop affordance + the reason instead of silently doing nothing).
  const locked = !isPending;
  const dropBlocked = locked && dragTaskId !== null;

  const handleDrop = async (e: React.DragEvent) => {
    e.preventDefault();
    setDropActive(false);
    if (!isPending) return;
    // Read the source race-proof from dataTransfer (set on dragStart), state as
    // fallback — same source-of-truth the Backlog REMOVE uses, so MOVE/REMOVE
    // are consistent and neither waits on a state commit.
    const src = readDragSource(e, dragSource);
    const taskId = src?.taskId ?? null;
    if (!taskId) return;
    const fromPlanId = src?.fromPlanId ?? null;
    try {
      if (fromPlanId === null) {
        // From the Backlog → SELECT into this pending plan (EXISTING behavior).
        await add.mutateAsync({ task_id: taskId });
      } else if (fromPlanId === plan.id) {
        // Dropped back onto its own plan → no-op (don't fire mutations).
        return;
      } else if (isPoolSource(fromPlanId)) {
        await removePool.mutateAsync(taskId);
        await addAny.mutateAsync({ planId: plan.id, taskId });
      } else {
        // From ANOTHER pending plan → MOVE = remove from source THEN add to this
        // plan. Remove first (the source was pending, so it's allowed §9.4), then
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
      className={`${columnBase} ${
        dropActive && canDrop
          ? 'border-accent bg-bg-elevated ring-2 ring-accent'
          : dropBlocked
            ? 'cursor-no-drop border-dashed border-border-strong bg-bg-subtle'
            : 'border-border-base bg-bg-elevated'
      }`}
      data-testid="plan-column"
      data-plan-id={plan.id}
      data-status={plan.status}
      data-droppable={isPending ? 'true' : 'false'}
      data-locked={locked ? 'true' : 'false'}
      // T121: hovering a locked column (always) / dragging over it explains why it
      // won't accept tasks — the reason tooltip the owner asked for.
      title={locked ? planLockReason(plan.status, t) : undefined}
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
      {/* T121: while a drag is in flight, a locked column shows a clear "no-drop"
          banner with the reason (not just a silent reject). */}
      {dropBlocked && (
        <p
          className="mb-1.5 flex items-center gap-1 rounded border border-border-strong bg-bg-base px-1.5 py-1 text-[0.625rem] font-medium text-text-muted"
          data-testid={`plan-drop-blocked-${plan.id}`}
          role="status"
        >
          <LockIcon />
          {planLockReason(plan.status, t)}
        </p>
      )}
      <div className="flex items-start justify-between gap-1.5 px-0.5">
        <span className="flex min-w-0 items-center gap-1.5">
          {/* T144: the plan NAME is the open affordance — click it to reach the
              Plan detail (replaces the separate "Open ▸" link, consistent with
              the agent-list name-click in T133/T143). */}
          <OrgLink
            to={`/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(plan.id)}`}
            className="truncate text-sm font-bold text-accent hover:underline"
            title={plan.name}
            data-testid={`plan-name-link-${plan.id}`}
          >
            {plan.name}
          </OrgLink>
          <PlanStatusChip status={plan.status} />
          {/* T121: a persistent lock glyph marks a plan whose task assignments are
              frozen (running / terminal) — reinforces the status chip. */}
          {locked && (
            <span
              className="inline-flex items-center text-text-muted"
              data-testid={`plan-locked-${plan.id}`}
              title={planLockReason(plan.status, t)}
              aria-label={planLockReason(plan.status, t)}
            >
              <LockIcon />
            </span>
          )}
        </span>
      </div>
      <div className="flex items-center gap-1.5 px-0.5 pb-2 pt-0.5">
        <span className="tabular-nums text-[0.6875rem] text-text-muted" data-testid="plan-progress">
          {plan.status === 'pending' ? t('plan.board.phase.planning') : t('plan.board.phase.inProgress')} · {planProgressLabel(progress)}
        </span>
        {/* P2-4: a running plan self-progresses (auto-advance). Compact suffix. */}
        {plan.status === 'running' && <AutoAdvancingIndicator variant="column" />}
        <PlanFailedIndicator hasFailed={hasFailed} />
      </div>
      {shown.length === 0 ? (
        <p className="py-3 text-center text-[0.6875rem] text-text-muted" data-testid="plan-empty">
          {t('plan.board.column.empty')}
        </p>
      ) : (
        // task-0543ece9: render EVERY node (backend no longer caps the preview) in
        // a bounded, scrollable area so a large plan (12+ tasks) shows all cards
        // without a silent "…and N more" truncation AND without an unbounded-tall
        // column. The scroll keeps the board row height sane.
        <div className="max-h-[26rem] space-y-0 overflow-y-auto" data-testid={`plan-cards-${plan.id}`}>
          {shown.map((node) => (
            <PlanTaskCard
              key={node.task_id}
              projectId={projectId}
              planId={plan.id}
              node={node}
              // §9.4: removing a task from a Plan is a PLANNING action — only a
              // PENDING Plan exposes the remove affordance (mirrors add-to-plan /
              // the A1 edge editor). running/done columns render NO remove control.
              // A7: canRemove (= isPending) ALSO gates drag — only pending-plan cards
              // are draggable (the source must be pending so MOVE/REMOVE is allowed).
              canRemove={isPending}
              lockReason={locked ? planLockReason(plan.status, t) : undefined}
              setDragSource={setDragSource}
            />
          ))}
        </div>
      )}
      {/* Overflow hint is now a belt-and-braces safety net only: with the cap
          removed node_count == shown.length so this stays hidden, but a degraded
          partial payload (fewer preview nodes than node_count) still surfaces it. */}
      {overflow > 0 && (
        <p className="px-0.5 text-[0.6875rem] text-text-muted" data-testid={`plan-overflow-${plan.id}`}>
          {t('plan.board.overflow', { count: overflow })}
        </p>
      )}
    </div>
  );
}

// PlanTaskCard — a single Plan-column task card (from nodes_preview). A PENDING
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
  lockReason,
  setDragSource,
}: {
  projectId: string;
  planId: string;
  node: PlanNode;
  canRemove: boolean;
  // T121: when the plan is locked (running / terminal) this is the human reason its
  // tasks can't be moved — shown as the card's locked-state tooltip. undefined ⟺
  // a pending (editable) plan, where the card is draggable + shows the remove button.
  lockReason?: string;
  setDragSource: (s: DragSource | null) => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const remove = useRemoveTaskFromPlan(projectId, planId);
  // A7: a Plan-task card is draggable ONLY when its plan is pending (canRemove ==
  // isPending) — moving it out runs RemoveTaskFromPlan on the source, which the
  // backend allows only for a pending plan (§9.4). running/done cards: no drag.
  const draggable = canRemove;
  // v2.10.1 [M5]: touch long-press drag (pending cards only; mouse keeps HTML5 DnD).
  const startLongPress = useContext(BoardTouchDragContext);
  return (
    <div
      className={`mb-1.5 rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1 ${
        draggable ? 'cursor-grab active:cursor-grabbing' : ''
      }`}
      data-testid="plan-task-card"
      data-task-id={node.task_id}
      data-draggable={draggable ? 'true' : 'false'}
      // T121: a locked card carries the reason its plan can't be re-planned.
      title={!draggable ? lockReason : undefined}
      draggable={draggable}
      onPointerDown={
        draggable ? (e) => startLongPress?.(e, node.task_id, planId, node.title) : undefined
      }
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
      <div className="mb-1 flex items-center gap-1">
        <TaskIdTag taskId={node.task_id} orgRef={node.org_ref} />
      </div>
      <div className="flex items-start justify-between gap-1.5">
        <div className="min-w-0 flex-1 text-xs font-semibold leading-tight text-text-primary">
          <TaskTitleLink projectId={projectId} taskId={node.task_id} title={node.title} />
        </div>
        {canRemove && (
          <button
            type="button"
            className="-mr-0.5 -mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle hover:text-text-primary disabled:opacity-50"
            disabled={remove.isPending}
            aria-label={t('plan.board.removeTask.aria', { title: node.title })}
            title={t('plan.board.removeTask.title')}
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
        {/* T121: a locked (running / terminal) plan's card shows a padlock instead
            of the remove button — a visible "can't re-plan" affordance. */}
        {!canRemove && (
          <span
            className="-mr-0.5 -mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center text-text-muted"
            data-testid={`plan-task-locked-${node.task_id}`}
            title={lockReason}
            aria-label={lockReason}
          >
            <LockIcon />
          </span>
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
          {t('plan.board.removeTask.error')}
        </p>
      )}
    </div>
  );
}

// NewPlanColumn — the trailing dashed "New Plan" column (.newcol). Opens the
// reused New-Plan modal → useCreatePlan (= a new column).
function NewPlanColumn({ onClick }: { onClick: () => void }): React.ReactElement {
  const { t } = useTranslation('work');
  return (
    <button
      type="button"
      className="flex w-[9.375rem] shrink-0 snap-start flex-col items-center justify-center gap-0.5 rounded-lg border border-dashed border-border-strong bg-bg-base px-2 py-3.5 text-xs font-medium text-accent hover:border-accent"
      onClick={onClick}
      data-testid="new-plan-column"
      role="listitem"
    >
      {t('plan.board.newPlan')}
      <span className="text-[0.625rem] text-text-muted">{t('plan.board.newPlanHint')}</span>
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
  const { t } = useTranslation('work');
  const resolveName = useDisplayNameResolver();
  const openSender = useSenderSidebar();
  if (!assignee) {
    return (
      <span className="inline-flex items-center gap-1 text-[0.6875rem] text-text-muted" data-testid="assignee">
        <span className="inline-flex h-4 w-4 items-center justify-center rounded-full bg-bg-subtle text-[0.5rem] font-bold text-text-muted">
          ?
        </span>
        {t('plan.board.unassigned')}
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
  const avatar = (
    <span
      className={`inline-flex h-4 w-4 items-center justify-center rounded-full text-[0.5rem] font-bold text-white ${disc}`}
      aria-hidden="true"
    >
      {label.charAt(0).toUpperCase()}
    </span>
  );
  // Owner ask (2026-07-03): clicking an AGENT assignee opens the shared agent
  // activity sidebar (openSender routes agent:→AgentDetailBody). Only when a
  // SenderSidebarProvider is present (the Work Board wraps one). The button is
  // NOT draggable and stops propagation so it never starts a card drag.
  if (kind === 'agent' && openSender) {
    return (
      <button
        type="button"
        className="inline-flex items-center gap-1 rounded text-[0.6875rem] text-text-secondary hover:text-accent hover:underline"
        data-testid="assignee"
        data-kind={kind}
        // #192/#215: the raw prefixed ref stays on the title (hover) only — the
        // visible text is the resolved name / clean handle. aria-label conveys the
        // click action for a11y.
        title={assignee}
        aria-label={t('plan.board.openAgentActivity', { defaultValue: 'View agent activity' })}
        draggable={false}
        onDragStart={(e) => e.preventDefault()}
        onPointerDown={(e) => e.stopPropagation()}
        onClick={(e) => {
          e.stopPropagation();
          openSender(assignee);
        }}
      >
        {avatar}
        {label}
      </button>
    );
  }
  return (
    <span
      className="inline-flex items-center gap-1 text-[0.6875rem] text-text-secondary"
      data-testid="assignee"
      data-kind={kind}
      title={assignee}
    >
      {avatar}
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

// LockIcon — T121: a small padlock glyph marking a plan whose task assignments are
// frozen (running / terminal). aria-hidden — the accessible reason lives on the
// wrapping element's title / aria-label.
function LockIcon(): React.ReactElement {
  return (
    <svg width="11" height="11" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="3.5" y="7" width="9" height="6.5" rx="1.2" stroke="currentColor" strokeWidth="1.4" />
      <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
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
  const { t } = useTranslation('work');
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
      aria-label={t('plan.board.newPlanAria')}
    >
      <form onSubmit={submit} className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-4 text-lg font-semibold">{t('plan.board.createModal.heading')}</h2>
        <label className="block text-xs font-medium" htmlFor="plan-name">
          {t('plan.board.createModal.name')}
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
          {t('plan.board.createModal.description')}
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
          {t('plan.board.createModal.targetDate')}
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
            {t('plan.board.createModal.cancel')}
          </button>
          <button
            type="submit"
            disabled={create.isPending || !name.trim()}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="plan-create-submit"
          >
            {create.isPending ? t('plan.board.createModal.creating') : t('plan.board.createModal.submit')}
          </button>
        </div>
      </form>
    </div>
  );
}

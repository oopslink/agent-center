import React, { useState } from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useProject } from '@/api/projects';
import {
  usePlans,
  useCreatePlan,
  useUnplannedTasks,
  useAddTaskToPlan,
  type Plan,
  type CreatePlanInput,
} from '@/api/plans';
import { refKind } from '@/api/members';
import type { Task } from '@/api/types';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { StatusChip } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, planProgressLabel } from '@/components/planDisplay';

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
  // The single Backlog task currently being dragged (HTML5 DnD). Held in state
  // so a draft Plan column can light up its drop-zone + reject running columns.
  const [dragTaskId, setDragTaskId] = useState<string | null>(null);

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
            Planning · drag a Backlog task into a draft Plan column (or use its “Add to plan” button).
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
        dragTaskId={dragTaskId}
        setDragTaskId={setDragTaskId}
        onNewPlan={() => setCreateOpen(true)}
      />
    </section>
  );
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
// Backlog column first, then a column per Plan, then the New-Plan column.
function Board({
  projectId,
  plans,
  backlog,
  dragTaskId,
  setDragTaskId,
  onNewPlan,
}: {
  projectId: string;
  plans: PlansQuery;
  backlog: TasksQuery;
  dragTaskId: string | null;
  setDragTaskId: (id: string | null) => void;
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

  // The draft Plans are the only valid add/drop targets (§9.4 select-into-plan
  // is draft-only). Computed once + shared by every Backlog card's add-menu.
  const draftPlans = planList.filter((p) => p.status === 'draft');

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
        setDragTaskId={setDragTaskId}
      />
      {planList.map((plan) => (
        <PlanColumn
          key={plan.id}
          projectId={projectId}
          plan={plan}
          dragTaskId={dragTaskId}
        />
      ))}
      <NewPlanColumn onClick={onNewPlan} />
    </div>
  );
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
  setDragTaskId,
}: {
  projectId: string;
  backlog: TasksQuery;
  draftPlans: Plan[];
  setDragTaskId: (id: string | null) => void;
}): React.ReactElement {
  const tasks = backlog.data ?? [];
  return (
    <div
      className={`${columnBase} border-border-strong bg-bg-subtle`}
      data-testid="backlog-column"
      role="listitem"
    >
      <div className="flex items-center justify-between px-0.5 pb-2">
        <span className="flex items-center gap-1.5 text-sm font-bold text-text-primary">
          <BacklogIcon />
          Backlog
        </span>
        <span className="tabular-nums text-[0.6875rem] text-text-muted" data-testid="backlog-count">
          {tasks.length}
        </span>
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
            setDragTaskId={setDragTaskId}
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
  setDragTaskId,
}: {
  projectId: string;
  task: Task;
  draftPlans: Plan[];
  setDragTaskId: (id: string | null) => void;
}): React.ReactElement {
  const [menuOpen, setMenuOpen] = useState(false);
  return (
    <div
      className="mb-1.5 rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1"
      data-testid="backlog-card"
      data-task-id={task.id}
      draggable
      onDragStart={(e) => {
        setDragTaskId(task.id);
        e.dataTransfer.setData('text/plain', task.id);
        e.dataTransfer.effectAllowed = 'move';
      }}
      onDragEnd={() => setDragTaskId(null)}
    >
      <div className="mb-1.5 text-xs font-semibold leading-tight text-text-primary">{task.title}</div>
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
  onClose,
}: {
  projectId: string;
  taskId: string;
  draftPlans: Plan[];
  onClose: () => void;
}): React.ReactElement {
  return (
    <div
      className="absolute left-0 right-0 top-full z-10 mt-1 rounded-md border border-border-base bg-bg-elevated p-1 shadow-1"
      role="menu"
      data-testid={`add-menu-${taskId}`}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose();
      }}
    >
      {draftPlans.length === 0 ? (
        <p className="px-2 py-1.5 text-[0.6875rem] text-text-muted" data-testid="add-menu-empty">
          No draft plan. Create one to plan this task.
        </p>
      ) : (
        draftPlans.map((plan) => (
          <AddToPlanItem
            key={plan.id}
            projectId={projectId}
            planId={plan.id}
            planName={plan.name}
            taskId={taskId}
            onDone={onClose}
          />
        ))
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
  dragTaskId,
}: {
  projectId: string;
  plan: Plan;
  dragTaskId: string | null;
}): React.ReactElement {
  const add = useAddTaskToPlan(projectId, plan.id);
  const [dropActive, setDropActive] = useState(false);
  const isDraft = plan.status === 'draft';
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
    const taskId = e.dataTransfer.getData('text/plain') || dragTaskId;
    if (!taskId) return;
    try {
      await add.mutateAsync({ task_id: taskId });
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
        <PlanFailedIndicator hasFailed={hasFailed} />
      </div>
      {shown.length === 0 ? (
        <p className="py-3 text-center text-[0.6875rem] text-text-muted" data-testid="plan-empty">
          No tasks yet.
        </p>
      ) : (
        shown.map((node) => (
          <div
            key={node.task_id}
            className="mb-1.5 rounded-lg border border-border-base bg-bg-elevated p-2 shadow-1"
            data-testid="plan-task-card"
            data-task-id={node.task_id}
          >
            <div className="mb-1.5 text-xs font-semibold leading-tight text-text-primary">
              {node.title}
            </div>
            <div className="flex items-center justify-between gap-1.5">
              <AssigneeBadge assignee={node.assignee_ref} />
              <StatusChip status={node.task_status} />
            </div>
          </div>
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
// is on hover (title), the visible text is the handle tail (#192). NO emoji.
function AssigneeBadge({ assignee }: { assignee?: string | null }): React.ReactElement {
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
  const handle = assignee.replace(/^(agent:|user:)/, '');
  const tail = handle.length > 8 ? handle.slice(-8) : handle;
  const disc = kind === 'agent' ? 'bg-violet-700' : 'bg-cyan-700';
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
        {tail.charAt(0).toUpperCase()}
      </span>
      {tail}
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

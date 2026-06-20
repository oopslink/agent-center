// BoardTaskCreateModal — T231: create a Task from the Work Board with a chosen
// destination. Unlike the plain TaskCreateModal (which always lands a task in the
// Backlog), the Work Board needs to route the new task to one of three places:
//   • Backlog          — unscheduled, unplanned (the default; useCreateTask only).
//   • Assignment Pool  — the built-in claimable pool (is_builtin plan).
//   • a specific Plan  — a DRAFT structured plan (§9.4 select-into-plan is
//                        draft-only; running/done plans can't accept new tasks).
// It creates the task (always a backlog task first), then — for a non-Backlog
// destination — selects it into the target plan via the same add endpoint the
// board's drag-and-drop uses, so the board refetch converges identically.
import React, { useMemo, useState } from 'react';
import { useCreateTask } from '@/api/tasks';
import { useAddTaskToAnyPlan, type Plan } from '@/api/plans';
import { useModalA11y } from './useModalA11y';

interface Props {
  projectId: string;
  /** The board's plans (from usePlans) — drives the destination options. */
  plans: Plan[] | undefined;
  onClose: () => void;
  onCreated?: (taskId: string) => void;
}

// Sentinel destination value for "leave it in the Backlog" (no plan selection).
const BACKLOG = 'backlog';

export function BoardTaskCreateModal({
  projectId,
  plans,
  onClose,
  onCreated,
}: Props): React.ReactElement {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [destination, setDestination] = useState<string>(BACKLOG);
  const create = useCreateTask(projectId);
  const addToPlan = useAddTaskToAnyPlan(projectId);
  // a11y: Escape closes + focus-trap (rendered = open).
  const containerRef = useModalA11y({ open: true, onClose });

  // The built-in Assignment Pool (exactly one is_builtin plan, if present) and
  // the DRAFT structured plans — the only plans that accept a freshly-created
  // task (running/done plans reject select-into-plan, §9.4).
  const pool = useMemo(() => (plans ?? []).find((p) => p.is_builtin === true) ?? null, [plans]);
  const draftPlans = useMemo(
    () => (plans ?? []).filter((p) => p.is_builtin !== true && p.status === 'draft'),
    [plans],
  );

  const pending = create.isPending || addToPlan.isPending;
  const trimmedTitle = title.trim();
  const canSubmit = trimmedTitle.length > 0 && !pending;
  const error = (create.error ?? addToPlan.error) as Error | null;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      // Always create the task first (it lands in the Backlog) …
      const res = await create.mutateAsync({
        title: trimmedTitle,
        description: description.trim() || undefined,
      });
      // … then route it to the chosen plan/pool when not staying in the Backlog.
      if (destination !== BACKLOG) {
        await addToPlan.mutateAsync({ planId: destination, taskId: res.id });
      }
      onCreated?.(res.id);
      onClose();
    } catch {
      // Surfaced via `error` below; the task may already exist in the Backlog if
      // only the plan-select leg failed (the board refetch will show it there).
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="board-task-create-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">New Task</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="board-task-create-close"
          >
            X
          </button>
        </div>

        <Field label="Title" required>
          <input
            data-testid="board-task-create-title"
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What should happen?"
          />
        </Field>

        <Field label="Description">
          <textarea
            data-testid="board-task-create-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={4}
            placeholder="Optional. Context, acceptance criteria, links…"
          />
        </Field>

        <Field
          label="Destination"
          hint="Where the task starts. Only draft plans can accept a new task."
        >
          <select
            data-testid="board-task-create-destination"
            className={inputClass}
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
          >
            <option value={BACKLOG}>Backlog (unscheduled)</option>
            {pool && (
              <option value={pool.id} data-testid="board-task-create-dest-pool">
                Assignment Pool (claimable)
              </option>
            )}
            {draftPlans.length > 0 && (
              <optgroup label="Plans (draft)">
                {draftPlans.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </optgroup>
            )}
          </select>
        </Field>

        {error && (
          <p className="mb-3 text-xs text-danger" data-testid="board-task-create-error">
            {error.message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="board-task-create-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="board-task-create-submit"
          >
            {pending ? 'Creating…' : 'Create Task'}
          </button>
        </div>
      </form>
    </div>
  );
}

function Field({
  label,
  hint,
  required,
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="mb-3">
      <label className="mb-1 block text-xs font-medium text-text-primary">
        {label}
        {required && <span className="ml-1 text-danger">*</span>}
      </label>
      {children}
      {hint && <p className="mt-1 text-[0.6875rem] text-text-muted">{hint}</p>}
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

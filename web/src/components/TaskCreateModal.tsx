// TaskCreateModal — v2.7 create a Task inside a project.
import React, { useState } from 'react';
import { useCreateTask } from '@/api/tasks';

interface Props {
  projectId: string;
  onClose: () => void;
  onCreated?: (taskId: string) => void;
}

export function TaskCreateModal({
  projectId,
  onClose,
  onCreated,
}: Props): React.ReactElement {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const create = useCreateTask(projectId);

  const trimmedTitle = title.trim();
  const canSubmit = trimmedTitle.length > 0 && !create.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      const res = await create.mutateAsync({
        title: trimmedTitle,
        description: description.trim() || undefined,
      });
      onCreated?.(res.id);
      onClose();
    } catch {
      // Surfaced via create.error.
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="task-create-modal"
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
            data-testid="task-create-close"
          >
            X
          </button>
        </div>

        <Field label="Title" required>
          <input
            data-testid="task-create-title"
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What should happen?"
          />
        </Field>

        <Field label="Description">
          <textarea
            data-testid="task-create-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={4}
            placeholder="Optional. Context, acceptance criteria, links…"
          />
        </Field>

        {create.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="task-create-error">
            {(create.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="task-create-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="task-create-submit"
          >
            {create.isPending ? 'Creating…' : 'Create Task'}
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

// TaskEditModal — v2.7 metadata edit (title / description) on a task.
import React, { useState } from 'react';
import { useUpdateTask } from '@/api/tasks';
import type { Task } from '@/api/types';

interface Props {
  projectId: string;
  task: Pick<Task, 'id' | 'title' | 'description'>;
  onClose: () => void;
  onSaved?: () => void;
}

export function TaskEditModal({ projectId, task, onClose, onSaved }: Props): React.ReactElement {
  const [title, setTitle] = useState(task.title ?? '');
  const [description, setDescription] = useState(task.description ?? '');
  const update = useUpdateTask(projectId, task.id);

  const trimmedTitle = title.trim();
  const canSubmit = trimmedTitle.length > 0 && !update.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      await update.mutateAsync({
        title: trimmedTitle,
        description: description.trim() || undefined,
      });
      onSaved?.();
      onClose();
    } catch {
      // surfaced via update.error
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="task-edit-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Edit Task</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="task-edit-close"
          >
            X
          </button>
        </div>

        <div className="mb-3">
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Title<span className="ml-1 text-danger">*</span>
          </label>
          <input
            data-testid="task-edit-title"
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </div>

        <div className="mb-3">
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Description
          </label>
          <textarea
            data-testid="task-edit-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={4}
          />
        </div>

        {update.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="task-edit-error">
            {(update.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="task-edit-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="task-edit-submit"
          >
            {update.isPending ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </form>
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

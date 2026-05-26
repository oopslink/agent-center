// TaskAbandonModal — v2.5.x #62 confirm dialog for abandoning a task.
// reason + message both required (conventions § 16).
import React, { useState } from 'react';
import { useAbandonTask } from '@/api/tasks';

interface Props {
  taskId: string;
  onClose: () => void;
  onAbandoned?: () => void;
}

export function TaskAbandonModal({
  taskId,
  onClose,
  onAbandoned,
}: Props): React.ReactElement {
  const [reason, setReason] = useState('');
  const [message, setMessage] = useState('');
  const abandon = useAbandonTask(taskId);

  const canSubmit =
    reason.trim().length > 0 &&
    message.trim().length > 0 &&
    !abandon.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      await abandon.mutateAsync({
        reason: reason.trim(),
        message: message.trim(),
      });
      onAbandoned?.();
      onClose();
    } catch {
      // surfaced via abandon.error
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="task-abandon-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-3">
          <h2 className="text-lg font-semibold">Abandon Task</h2>
          <p className="mt-1 text-xs text-text-muted">
            This sets the task to <span className="font-mono">abandoned</span>{' '}
            (terminal). Active execution must already be killed; the AR
            rejects abandon mid-run.
          </p>
        </div>

        <div className="mb-3">
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Reason<span className="ml-1 text-danger">*</span>
          </label>
          <input
            data-testid="task-abandon-reason"
            className={inputClass}
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="obsolete / superseded / wrong_scope / …"
          />
        </div>

        <div className="mb-3">
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Message<span className="ml-1 text-danger">*</span>
          </label>
          <textarea
            data-testid="task-abandon-message"
            className={inputClass}
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            rows={3}
            placeholder="Short explanation that will land on the task event log."
          />
        </div>

        {abandon.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="task-abandon-error">
            {(abandon.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="task-abandon-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="task-abandon-submit"
          >
            {abandon.isPending ? 'Abandoning…' : 'Abandon task'}
          </button>
        </div>
      </form>
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

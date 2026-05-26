// IssueConcludeModal — v2.5.x #61 Conclude UI for an open Issue.
//
// Three kinds (matching discussion.ResolutionKind):
//   - closed_no_action: "decided not to do" — summary only
//   - closed_with_tasks: spawn N >= 1 tasks — summary + task list
//   - withdrawn: opener pulled it back — summary only
//
// For closed_with_tasks the modal accepts N rows with at minimum a
// title; description + priority are optional. Local_id is auto-assigned
// by the backend if missing (handler.go fills t1/t2/…).
import React, { useState } from 'react';
import {
  useConcludeIssue,
  type ConcludeKind,
  type ConcludeTaskSpec,
} from '@/api/issues';

interface Props {
  issueId: string;
  onClose: () => void;
  onConcluded?: () => void;
}

const KIND_OPTIONS: Array<{ value: ConcludeKind; label: string; hint: string }> = [
  {
    value: 'closed_no_action',
    label: 'No action',
    hint: 'We decided not to do anything about this issue.',
  },
  {
    value: 'closed_with_tasks',
    label: 'Spawn tasks',
    hint: 'We agreed; the work below should be picked up as one or more tasks.',
  },
  {
    value: 'withdrawn',
    label: 'Withdraw',
    hint: 'The opener pulled this back — no longer relevant.',
  },
];

export function IssueConcludeModal({
  issueId,
  onClose,
  onConcluded,
}: Props): React.ReactElement {
  const [kind, setKind] = useState<ConcludeKind>('closed_no_action');
  const [summary, setSummary] = useState('');
  const [tasks, setTasks] = useState<ConcludeTaskSpec[]>([
    { title: '', description: '' },
  ]);
  const conclude = useConcludeIssue(issueId);

  const trimmedSummary = summary.trim();
  const taskTitlesValid =
    kind !== 'closed_with_tasks' ||
    (tasks.length > 0 && tasks.every((t) => t.title.trim().length > 0));
  const canSubmit =
    trimmedSummary.length > 0 && taskTitlesValid && !conclude.isPending;

  const addTaskRow = () =>
    setTasks([...tasks, { title: '', description: '' }]);
  const removeTaskRow = (i: number) =>
    setTasks(tasks.filter((_, idx) => idx !== i));
  const updateTask = (i: number, patch: Partial<ConcludeTaskSpec>) =>
    setTasks(tasks.map((t, idx) => (idx === i ? { ...t, ...patch } : t)));

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      await conclude.mutateAsync({
        kind,
        summary: trimmedSummary,
        tasks:
          kind === 'closed_with_tasks'
            ? tasks.map((t) => ({
                title: t.title.trim(),
                description: t.description?.trim() || undefined,
                priority: t.priority || undefined,
              }))
            : undefined,
      });
      onConcluded?.();
      onClose();
    } catch {
      // surfaced via conclude.error
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="issue-conclude-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="max-h-[90vh] w-full max-w-xl overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Conclude Issue</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="issue-conclude-close"
          >
            X
          </button>
        </div>

        <fieldset className="mb-4" data-testid="issue-conclude-kind">
          <legend className="mb-2 text-xs font-medium uppercase text-text-muted">
            Resolution
          </legend>
          <div className="space-y-2">
            {KIND_OPTIONS.map((opt) => (
              <label
                key={opt.value}
                className={[
                  'flex cursor-pointer items-start gap-3 rounded border p-3 text-sm',
                  kind === opt.value
                    ? 'border-accent bg-bg-subtle'
                    : 'border-border-base hover:bg-bg-subtle',
                ].join(' ')}
                data-testid={`issue-conclude-kind-${opt.value}`}
              >
                <input
                  type="radio"
                  name="kind"
                  className="mt-0.5"
                  checked={kind === opt.value}
                  onChange={() => setKind(opt.value)}
                />
                <div className="flex-1">
                  <div className="font-medium">{opt.label}</div>
                  <div className="text-xs text-text-muted">{opt.hint}</div>
                </div>
              </label>
            ))}
          </div>
        </fieldset>

        <div className="mb-4">
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Summary<span className="ml-1 text-danger">*</span>
          </label>
          <textarea
            data-testid="issue-conclude-summary"
            className={inputClass}
            value={summary}
            onChange={(e) => setSummary(e.target.value)}
            rows={3}
            placeholder="Why this conclusion?"
          />
        </div>

        {kind === 'closed_with_tasks' && (
          <div className="mb-4" data-testid="issue-conclude-tasks">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs font-medium uppercase text-text-muted">
                Tasks to spawn
              </span>
              <button
                type="button"
                onClick={addTaskRow}
                className="rounded border border-border-base px-2 py-0.5 text-xs hover:bg-bg-subtle"
                data-testid="issue-conclude-task-add"
              >
                + Add task
              </button>
            </div>
            <ul className="space-y-3">
              {tasks.map((t, i) => (
                <li
                  key={i}
                  className="rounded border border-border-base p-3"
                  data-testid="issue-conclude-task-row"
                >
                  <div className="mb-2 flex items-center justify-between">
                    <span className="text-xs text-text-muted">Task {i + 1}</span>
                    {tasks.length > 1 && (
                      <button
                        type="button"
                        onClick={() => removeTaskRow(i)}
                        className="text-xs text-text-muted hover:text-danger"
                        data-testid="issue-conclude-task-remove"
                        aria-label={`Remove task ${i + 1}`}
                      >
                        Remove
                      </button>
                    )}
                  </div>
                  <input
                    data-testid="issue-conclude-task-title"
                    className={inputClass}
                    placeholder="Task title (required)"
                    value={t.title}
                    onChange={(e) => updateTask(i, { title: e.target.value })}
                  />
                  <textarea
                    data-testid="issue-conclude-task-description"
                    className={inputClass + ' mt-2'}
                    placeholder="Description (optional)"
                    rows={2}
                    value={t.description ?? ''}
                    onChange={(e) =>
                      updateTask(i, { description: e.target.value })
                    }
                  />
                </li>
              ))}
            </ul>
          </div>
        )}

        {conclude.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="issue-conclude-error">
            {(conclude.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="issue-conclude-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="issue-conclude-submit"
          >
            {conclude.isPending ? 'Concluding…' : 'Conclude'}
          </button>
        </div>
      </form>
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

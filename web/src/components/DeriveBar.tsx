import type React from 'react';

interface Props {
  count: number;
  onOpenIssue: () => void;
  onOpenTask: () => void;
  onCancel: () => void;
}

// DeriveBar — sticky bottom action bar visible while in select mode.
// "N messages selected" + Open Issue / Open Task / Cancel.
//
// Renders nothing when count is 0 so picking nothing doesn't show a
// noisy bar.
export function DeriveBar({
  count,
  onOpenIssue,
  onOpenTask,
  onCancel,
}: Props): React.ReactElement | null {
  if (count === 0) return null;
  return (
    <div
      className="sticky bottom-0 z-10 flex items-center justify-between border-t border-border-base bg-bg-elevated px-4 py-2 shadow"
      data-testid="derive-bar"
    >
      <span className="text-sm text-text-primary" data-testid="derive-bar-count">
        {count} message{count === 1 ? '' : 's'} selected
      </span>
      <div className="flex gap-2">
        <button
          type="button"
          onClick={onOpenIssue}
          className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90"
          data-testid="derive-open-issue"
        >
          Open Issue
        </button>
        <button
          type="button"
          onClick={onOpenTask}
          className="rounded bg-text-secondary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90"
          data-testid="derive-open-task"
        >
          Open Task
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
          data-testid="derive-cancel"
        >
          Cancel
        </button>
      </div>
    </div>
  );
}

// IssueCreateModal — v2.7 create an Issue inside a project.
import React, { useState } from 'react';
import { useCreateIssue } from '@/api/issues';
import { useModalA11y } from './useModalA11y';

interface Props {
  projectId: string;
  onClose: () => void;
  onCreated?: (issueId: string) => void;
}

export function IssueCreateModal({
  projectId,
  onClose,
  onCreated,
}: Props): React.ReactElement {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const create = useCreateIssue(projectId);
  // a11y: Escape closes + focus-trap (rendered = open).
  const containerRef = useModalA11y({ open: true, onClose });

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
      // Surfaced via create.error below.
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="issue-create-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="issue-create-title"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 id="issue-create-title" className="text-lg font-semibold">Open Issue</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="issue-create-close"
          >
            X
          </button>
        </div>

        <div className="mb-3">
          <label htmlFor="issue-create-title-input" className="mb-1 block text-xs font-medium text-text-primary">
            Title<span className="ml-1 text-danger">*</span>
          </label>
          <input
            id="issue-create-title-input"
            data-testid="issue-create-title"
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What needs deciding or fixing?"
          />
        </div>

        <div className="mb-3">
          <label htmlFor="issue-create-desc-input" className="mb-1 block text-xs font-medium text-text-primary">
            Description
          </label>
          <textarea
            id="issue-create-desc-input"
            data-testid="issue-create-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={4}
            placeholder="Optional. Context, repro steps, links…"
          />
        </div>

        {create.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="issue-create-error">
            {(create.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="issue-create-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="issue-create-submit"
          >
            {create.isPending ? 'Opening…' : 'Open Issue'}
          </button>
        </div>
      </form>
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

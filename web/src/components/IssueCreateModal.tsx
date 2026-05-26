// IssueCreateModal — v2.5.x #61 Open Issue from scratch (no message
// source). The DeriveModal still exists for "create issue from selected
// messages"; this is the standalone path.
import React, { useState } from 'react';
import { useOpenIssue } from '@/api/issues';
import { useProjects } from '@/api/projects';

interface Props {
  defaultProjectId?: string;
  onClose: () => void;
  onCreated?: (issueId: string) => void;
}

export function IssueCreateModal({
  defaultProjectId,
  onClose,
  onCreated,
}: Props): React.ReactElement {
  const projects = useProjects();
  const [projectId, setProjectId] = useState<string>(defaultProjectId ?? '');
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const open = useOpenIssue();

  const trimmedTitle = title.trim();
  const canSubmit = projectId !== '' && trimmedTitle.length > 0 && !open.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      const res = await open.mutateAsync({
        project_id: projectId,
        title: trimmedTitle,
        description: description.trim() || undefined,
      });
      onCreated?.(res.issue_id);
      onClose();
    } catch {
      // Surfaced via open.error below.
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="issue-create-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Open Issue</h2>
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
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Project<span className="ml-1 text-danger">*</span>
          </label>
          <select
            data-testid="issue-create-project"
            className={inputClass}
            value={projectId}
            onChange={(e) => setProjectId(e.target.value)}
          >
            <option value="" disabled>
              Pick a project…
            </option>
            {(projects.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </div>

        <div className="mb-3">
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Title<span className="ml-1 text-danger">*</span>
          </label>
          <input
            data-testid="issue-create-title"
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What needs deciding or fixing?"
          />
        </div>

        <div className="mb-3">
          <label className="mb-1 block text-xs font-medium text-text-primary">
            Description
          </label>
          <textarea
            data-testid="issue-create-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={4}
            placeholder="Optional. Context, repro steps, links…"
          />
        </div>

        {open.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="issue-create-error">
            {(open.error as Error).message}
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
            {open.isPending ? 'Opening…' : 'Open Issue'}
          </button>
        </div>
      </form>
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

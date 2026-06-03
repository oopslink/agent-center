// ProjectCreateModal — v2.7 Add Project form. id is server-generated;
// `tags` was retired. Submission only requires `name`.
import React, { useState } from 'react';
import { useCreateProject } from '@/api/projects';

interface Props {
  onClose: () => void;
}

export function ProjectCreateModal({ onClose }: Props): React.ReactElement {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const create = useCreateProject();

  const trimmedName = name.trim();
  const canSubmit = trimmedName.length > 0 && !create.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      await create.mutateAsync({
        name: trimmedName,
        description: description || undefined,
      });
      onClose();
    } catch {
      // surfaced via create.error below
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="project-create-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Add Project</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="project-create-close"
          >
            X
          </button>
        </div>

        <Field label="Name" required>
          <input
            data-testid="project-create-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="My Project"
            className={inputClass}
          />
        </Field>

        <Field label="Description" hint="Optional. Shown in the project list.">
          <textarea
            data-testid="project-create-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={3}
            className={inputClass}
          />
        </Field>

        {create.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="project-create-error">
            {(create.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="project-create-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="project-create-submit"
          >
            {create.isPending ? 'Creating...' : 'Create project'}
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

// TaskCreateModal — v2.5.x #62 New Task from scratch (no message
// source). Fields: project picker, title, description, parent_task_id
// (optional), workspace_mode (requires_worktree checkbox).
import React, { useState } from 'react';
import { useCreateTask } from '@/api/tasks';
import { useProjects } from '@/api/projects';

interface Props {
  defaultProjectId?: string;
  onClose: () => void;
  onCreated?: (taskId: string) => void;
}

const PRIORITY_OPTIONS = [
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
];

export function TaskCreateModal({
  defaultProjectId,
  onClose,
  onCreated,
}: Props): React.ReactElement {
  const projects = useProjects();
  const [projectId, setProjectId] = useState<string>(defaultProjectId ?? '');
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [parentTaskId, setParentTaskId] = useState('');
  const [priority, setPriority] = useState<string>('medium');
  const [requiresWorktree, setRequiresWorktree] = useState(false);
  const create = useCreateTask();

  const trimmedTitle = title.trim();
  const canSubmit =
    projectId !== '' && trimmedTitle.length > 0 && !create.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      const res = await create.mutateAsync({
        project_id: projectId,
        title: trimmedTitle,
        description: description.trim() || undefined,
        parent_task_id: parentTaskId.trim() || undefined,
        priority,
        requires_worktree: requiresWorktree,
        // v2.5.16 (#69): always create a Conversation alongside the
        // Task so TaskDetail can host the discussion thread + composer
        // out of the box. Legacy tasks (without a conversation) get
        // the explicit "Start discussion" affordance on TaskDetail.
        with_conversation: true,
      });
      onCreated?.(res.task_id);
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

        <Field label="Project" required>
          <select
            data-testid="task-create-project"
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
        </Field>

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

        <Field label="Parent task id" hint="Optional. For nested subtasks.">
          <input
            data-testid="task-create-parent"
            className={inputClass}
            value={parentTaskId}
            onChange={(e) => setParentTaskId(e.target.value)}
            placeholder="e.g. 01HX… (leave blank for top-level)"
          />
        </Field>

        <Field label="Priority">
          <select
            data-testid="task-create-priority"
            className={inputClass}
            value={priority}
            onChange={(e) => setPriority(e.target.value)}
          >
            {PRIORITY_OPTIONS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </Field>

        <div className="mb-3 flex items-center gap-2">
          <input
            id="task-create-worktree"
            type="checkbox"
            checked={requiresWorktree}
            onChange={(e) => setRequiresWorktree(e.target.checked)}
            data-testid="task-create-worktree"
          />
          <label htmlFor="task-create-worktree" className="text-xs text-text-primary">
            Requires worktree (isolated workspace)
          </label>
        </div>

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

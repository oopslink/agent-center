import type React from 'react';
import { useState } from 'react';
import { useProjects } from '@/api/projects';
import { IssueCreateModal } from './IssueCreateModal';
import { TaskCreateModal } from './TaskCreateModal';

interface Props {
  kind: 'issue' | 'task';
  onClose: () => void;
}

// OrgWorkItemCreateModal — the OrgWorkItems list is cross-project (#258), so
// creating an issue/task from here first needs a project. Pick a project →
// delegate to the per-project IssueCreateModal / TaskCreateModal, which own the
// title/description form + create mutation (reuse, no duplicated create logic).
export function OrgWorkItemCreateModal({ kind, onClose }: Props): React.ReactElement {
  const projects = useProjects();
  const [projectId, setProjectId] = useState('');

  // Once a project is chosen, hand off to the existing per-project create modal.
  if (projectId) {
    return kind === 'issue' ? (
      <IssueCreateModal projectId={projectId} onClose={onClose} />
    ) : (
      <TaskCreateModal projectId={projectId} onClose={onClose} />
    );
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="org-create-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">New {kind === 'issue' ? 'Issue' : 'Task'}</h2>
          <button
            type="button"
            onClick={onClose}
            className="text-text-muted hover:text-text-primary"
            aria-label="Close"
            data-testid="org-create-close"
          >
            X
          </button>
        </div>
        <label className="block text-sm">
          <span className="mb-1 block text-text-secondary">Project</span>
          <select
            data-testid="org-create-project-select"
            value={projectId}
            onChange={(e) => setProjectId(e.target.value)}
            className="w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm"
          >
            <option value="">Select a project…</option>
            {projects.data?.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </label>
        <p className="mt-2 text-xs text-text-muted">
          Pick a project to create the {kind} in (issues/tasks are project-scoped).
        </p>
      </div>
    </div>
  );
}

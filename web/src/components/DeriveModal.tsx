import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useDeriveIssue, useDeriveTask } from '@/api/derive';
import { useProjects } from '@/api/projects';
import { useModalA11y } from './useModalA11y';

export type DeriveKind = 'issue' | 'task';

interface Props {
  kind: DeriveKind;
  open: boolean;
  sourceConversationId: string;
  sourceMessageIds: string[];
  onClose: () => void;
  /** Called after the modal closes (whether by success or cancel). The
   *  derived conversation id is provided on success so the page can clear
   *  selection state. */
  onCreated?: (newConversationId: string) => void;
}

// DeriveModal — form to derive an issue / task from selected messages.
// title is required; description is optional. On success the modal
// switches to a confirmation pane with a deep link to the new
// conversation (does not auto-navigate, per F9 oversight #6).
export function DeriveModal({
  kind,
  open,
  sourceConversationId,
  sourceMessageIds,
  onClose,
  onCreated,
}: Props): React.ReactElement | null {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [projectId, setProjectId] = useState('');
  const [createdId, setCreatedId] = useState<string | null>(null);
  const deriveIssue = useDeriveIssue();
  const deriveTask = useDeriveTask();
  const mut = kind === 'issue' ? deriveIssue : deriveTask;
  const projects = useProjects();

  const handleClose = () => {
    reset();
    onClose();
  };
  const containerRef = useModalA11y({ open, onClose: handleClose });
  if (!open) return null;

  function reset() {
    setTitle('');
    setDescription('');
    setProjectId('');
    setCreatedId(null);
  }

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim() || !projectId) return;
    try {
      const res = await mut.mutateAsync({
        source_conversation_id: sourceConversationId,
        source_message_ids: sourceMessageIds,
        project_id: projectId,
        title: title.trim(),
        description,
      });
      setCreatedId(res.conversation_id);
      onCreated?.(res.conversation_id);
    } catch {
      // Error renders below.
    }
  };

  const hasProjects = (projects.data?.length ?? 0) > 0;

  const targetPath =
    kind === 'issue' ? `/issues/${createdId}` : `/tasks/${createdId}`;
  const label = kind === 'issue' ? 'Issue' : 'Task';

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-20 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="derive-title"
      data-testid="derive-modal"
      data-kind={kind}
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-lg">
        <h2 id="derive-title" className="text-lg font-semibold">
          Open {label} from {sourceMessageIds.length} message
          {sourceMessageIds.length === 1 ? '' : 's'}
        </h2>

        {createdId === null ? (
          <form className="mt-4 space-y-3" onSubmit={submit}>
            <div>
              <label className="block text-xs font-medium text-text-primary">
                Project
              </label>
              {projects.isLoading ? (
                <p
                  className="mt-1 text-xs text-text-muted"
                  data-testid="derive-projects-loading"
                >
                  Loading projects…
                </p>
              ) : hasProjects ? (
                <select
                  value={projectId}
                  onChange={(e) => setProjectId(e.target.value)}
                  className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary focus:border-accent"
                  data-testid="derive-project-select"
                >
                  <option value="" disabled>
                    Select project…
                  </option>
                  {projects.data!.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </select>
              ) : (
                <p
                  className="mt-1 text-xs text-text-secondary"
                  data-testid="derive-no-projects"
                >
                  No projects yet — create one via{' '}
                  <code className="rounded bg-bg-subtle px-1">
                    agent-center project add &lt;id&gt; --name=…
                  </code>{' '}
                  first.
                </p>
              )}
            </div>
            <div>
              <label className="block text-xs font-medium text-text-primary">Title</label>
              <input
                type="text"
                autoFocus
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={`${label.toLowerCase()} title`}
                className="mt-1 w-full rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
                data-testid="derive-title-input"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-text-primary">
                Description
              </label>
              <textarea
                rows={3}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="optional"
                className="mt-1 w-full resize-none rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
                data-testid="derive-description-input"
              />
            </div>
            {mut.isError && (
              <p className="text-xs text-danger" data-testid="derive-error">
                {(mut.error as Error).message}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={handleClose}
                className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
                data-testid="derive-modal-cancel"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!title.trim() || !projectId || mut.isPending}
                className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90 disabled:bg-bg-subtle disabled:text-text-muted"
                data-testid="derive-modal-submit"
              >
                {mut.isPending ? 'Creating…' : `Create ${label}`}
              </button>
            </div>
          </form>
        ) : (
          <div className="mt-4 space-y-3" data-testid="derive-success">
            <p className="rounded bg-success/10 p-3 text-sm text-success">
              {label} created.
            </p>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={handleClose}
                className="rounded px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
                data-testid="derive-success-close"
              >
                Close
              </button>
              <OrgLink
                to={targetPath}
                onClick={handleClose}
                className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90"
                data-testid="derive-success-link"
              >
                View {label} →
              </OrgLink>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

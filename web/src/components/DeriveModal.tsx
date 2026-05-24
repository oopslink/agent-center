import type React from 'react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
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
      className="fixed inset-0 z-20 flex items-center justify-center bg-slate-900/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="derive-title"
      data-testid="derive-modal"
      data-kind={kind}
    >
      <div className="w-full max-w-md rounded-lg bg-white p-6 shadow-lg">
        <h2 id="derive-title" className="text-lg font-semibold">
          Open {label} from {sourceMessageIds.length} message
          {sourceMessageIds.length === 1 ? '' : 's'}
        </h2>

        {createdId === null ? (
          <form className="mt-4 space-y-3" onSubmit={submit}>
            <div>
              <label className="block text-xs font-medium text-slate-700">
                Project
              </label>
              {projects.isLoading ? (
                <p
                  className="mt-1 text-xs text-slate-500"
                  data-testid="derive-projects-loading"
                >
                  Loading projects…
                </p>
              ) : hasProjects ? (
                <select
                  value={projectId}
                  onChange={(e) => setProjectId(e.target.value)}
                  className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm focus:border-slate-500 focus:outline-none"
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
                  className="mt-1 text-xs text-slate-600"
                  data-testid="derive-no-projects"
                >
                  No projects yet — create one via{' '}
                  <code className="rounded bg-slate-100 px-1">
                    agent-center project add &lt;id&gt; --name=…
                  </code>{' '}
                  first.
                </p>
              )}
            </div>
            <div>
              <label className="block text-xs font-medium text-slate-700">Title</label>
              <input
                type="text"
                autoFocus
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={`${label.toLowerCase()} title`}
                className="mt-1 w-full rounded border border-slate-300 px-2 py-1 text-sm focus:border-slate-500 focus:outline-none"
                data-testid="derive-title-input"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-slate-700">
                Description
              </label>
              <textarea
                rows={3}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="optional"
                className="mt-1 w-full resize-none rounded border border-slate-300 px-2 py-1 text-sm focus:border-slate-500 focus:outline-none"
                data-testid="derive-description-input"
              />
            </div>
            {mut.isError && (
              <p className="text-xs text-red-600" data-testid="derive-error">
                {(mut.error as Error).message}
              </p>
            )}
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={handleClose}
                className="rounded px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-100"
                data-testid="derive-modal-cancel"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={!title.trim() || !projectId || mut.isPending}
                className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800 disabled:bg-slate-300"
                data-testid="derive-modal-submit"
              >
                {mut.isPending ? 'Creating…' : `Create ${label}`}
              </button>
            </div>
          </form>
        ) : (
          <div className="mt-4 space-y-3" data-testid="derive-success">
            <p className="rounded bg-emerald-50 p-3 text-sm text-emerald-800">
              {label} created.
            </p>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                onClick={handleClose}
                className="rounded px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-100"
                data-testid="derive-success-close"
              >
                Close
              </button>
              <Link
                to={targetPath}
                onClick={handleClose}
                className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800"
                data-testid="derive-success-link"
              >
                View {label} →
              </Link>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

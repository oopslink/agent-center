import React, { useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  useCreateProjectMapping,
  useDeleteProject,
  useDeleteProjectMapping,
  useProject,
  useProjectMappings,
  useUpdateProject,
  type DeleteProjectError,
  type Project,
} from '@/api/projects';
import { useFleet } from '@/api/fleet';
import { useIssues } from '@/api/issues';
import { useTasksList } from '@/api/tasks';
import { Skeleton } from '@/components/Skeleton';

// ProjectDetail (/projects/:id). Read-only profile + Issues/Tasks
// preview panels.
//
// v2.3-5b cutover (per § 0.6): the Issues/Tasks panels now read real
// per-project lists via the BC-native endpoints (Discussion BC for
// issues, TaskRuntime BC for tasks). Previously these were a
// cross-BC `useConversations({kind:'issue'|'task'})` read filtered
// purely on the client (the Conversation projection carries no
// project_id), which surfaced cross-project data with a hint chip.
// Cutover deletes that hint — the panels now answer the obvious
// question accurately.
export default function ProjectDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const project = useProject(id);

  if (project.isLoading) {
    return (
      <section className="space-y-3" data-testid="page-ProjectDetail">
        <Skeleton width="14rem" height="1.75rem" />
        <Skeleton width="20rem" height="1rem" />
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Skeleton height="8rem" />
          <Skeleton height="8rem" />
        </div>
      </section>
    );
  }
  if (project.isError) {
    return (
      <section className="space-y-3" data-testid="page-ProjectDetail">
        <p className="text-sm text-danger" data-testid="project-not-found">
          {(project.error as Error).message}
        </p>
        <Link to="/projects" className="text-xs text-accent hover:underline">
          ← Back to projects
        </Link>
      </section>
    );
  }
  if (!project.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-ProjectDetail">
        Project lookup failed.
      </section>
    );
  }

  const p = project.data;
  return (
    <section className="space-y-4" data-testid="page-ProjectDetail" data-project-id={p.id}>
      <ProjectHeader project={p} />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <IssuesPanel projectId={p.id} />
        <TasksPanel projectId={p.id} />
      </div>

      <WorkersPanel projectId={p.id} />
      <FleetLinkSection projectId={p.id} />
    </section>
  );
}

// ProjectHeader inlines the v2.5.3 Edit + Delete actions next to the
// existing read-view layout.
function ProjectHeader({ project: p }: { project: Project }): React.ReactElement {
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  return (
    <header className="space-y-2 border-b border-border-base pb-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{p.name}</h1>
          <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-muted">
            {p.id}
          </span>
          {p.tags.map((t) => (
            <span
              key={t}
              className="rounded bg-brand/10 px-2 py-0.5 text-[0.6875rem] tracking-wide text-brand"
              data-testid={`project-tag-${t}`}
            >
              {t}
            </span>
          ))}
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
            onClick={() => setEditing(true)}
            data-testid="project-edit-btn"
          >
            Edit
          </button>
          <button
            type="button"
            className="rounded border border-danger/40 px-2 py-1 text-xs text-danger hover:bg-danger/10"
            onClick={() => setDeleting(true)}
            data-testid="project-delete-btn"
          >
            Delete
          </button>
        </div>
      </div>
      {p.description && (
        <p className="max-w-3xl text-sm text-text-secondary" data-testid="project-description">
          {p.description}
        </p>
      )}
      {editing && <ProjectEditModal project={p} onClose={() => setEditing(false)} />}
      {deleting && <ProjectDeleteModal project={p} onClose={() => setDeleting(false)} />}
    </header>
  );
}

function ProjectEditModal({
  project: p,
  onClose,
}: {
  project: Project;
  onClose: () => void;
}): React.ReactElement {
  const [name, setName] = useState(p.name);
  const [description, setDescription] = useState(p.description ?? '');
  const [tags, setTags] = useState<string[]>(p.tags ?? []);
  const [tagInput, setTagInput] = useState('');
  const update = useUpdateProject(p.id);

  const addTag = (raw: string) => {
    const t = raw.trim();
    if (!t || tags.includes(t)) return;
    setTags([...tags, t]);
    setTagInput('');
  };
  const removeTag = (t: string) => setTags(tags.filter((x) => x !== t));
  const onTagKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addTag(tagInput);
    } else if (e.key === 'Backspace' && tagInput === '' && tags.length > 0) {
      removeTag(tags[tags.length - 1]);
    }
  };

  const sameTags = (a: string[], b: string[]) =>
    a.length === b.length && a.every((v, i) => v === b[i]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const fields: {
      version: number;
      name?: string;
      description?: string;
      tags?: string[];
    } = { version: p.version ?? 1 };
    if (name !== p.name) fields.name = name;
    if (description !== (p.description ?? '')) fields.description = description;
    if (!sameTags(tags, p.tags ?? [])) fields.tags = tags;
    try {
      await update.mutateAsync(fields);
      onClose();
    } catch {
      // surfaced inline
    }
  };
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="project-edit-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <h2 className="mb-4 text-lg font-semibold">Edit Project</h2>
        <label className="mb-2 block text-xs font-medium">Name</label>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={editInputClass}
          data-testid="project-edit-name"
        />
        <label className="mb-2 mt-3 block text-xs font-medium">Description</label>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          className={editInputClass}
          data-testid="project-edit-description"
        />
        <label className="mb-2 mt-3 block text-xs font-medium">Tags</label>
        <div className={editTagChipsContainerClass}>
          {tags.map((t) => (
            <span key={t} className={editTagChipClass} data-testid={`project-edit-tag-chip-${t}`}>
              {t}
              <button
                type="button"
                className="ml-1 text-text-muted hover:text-text-primary"
                onClick={() => removeTag(t)}
                aria-label={`Remove tag ${t}`}
              >
                x
              </button>
            </span>
          ))}
          <input
            value={tagInput}
            onChange={(e) => setTagInput(e.target.value)}
            onKeyDown={onTagKeyDown}
            placeholder={tags.length === 0 ? 'add a tag...' : ''}
            data-testid="project-edit-tag-input"
            className="flex-1 min-w-[6rem] bg-transparent text-sm text-text-primary placeholder:text-text-muted outline-none"
          />
        </div>
        {update.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="project-edit-error">
            {(update.error as Error).message}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={update.isPending}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="project-edit-save"
          >
            {update.isPending ? 'Saving...' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  );
}

function ProjectDeleteModal({
  project: p,
  onClose,
}: {
  project: Project;
  onClose: () => void;
}): React.ReactElement {
  const navigate = useNavigate();
  const del = useDeleteProject(p.id);
  const [conflict, setConflict] = useState<DeleteProjectError | null>(null);
  const [understood, setUnderstood] = useState(false);
  const handleDelete = async (force: boolean) => {
    try {
      await del.mutateAsync({ force });
      navigate('/projects');
    } catch (err) {
      const body = err as DeleteProjectError;
      if (body?.error === 'project_has_active_work') {
        setConflict(body);
        return;
      }
      // other errors surface below
    }
  };
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="project-delete-modal"
      role="dialog"
      aria-modal="true"
    >
      <div className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-4 text-lg font-semibold text-danger">Delete project?</h2>
        {!conflict && (
          <>
            <p className="mb-3 text-sm text-text-secondary">
              Delete <span className="font-mono">{p.name}</span> ({p.id})?
            </p>
            <p className="mb-4 text-xs text-text-muted">
              The Web Console will refuse if active tasks, open issues, or worker
              mappings reference this project; you can force-delete after confirming.
            </p>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
                onClick={onClose}
              >
                Cancel
              </button>
              <button
                type="button"
                className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90"
                onClick={() => void handleDelete(false)}
                data-testid="project-delete-confirm"
              >
                Delete
              </button>
            </div>
          </>
        )}
        {conflict && (
          <>
            <p className="mb-3 text-sm font-medium text-warning">
              Project has active dependencies.
            </p>
            <ul className="mb-4 list-inside list-disc text-xs text-text-secondary">
              {conflict.task_count ? <li>{conflict.task_count} open / suspended tasks</li> : null}
              {conflict.issue_count ? <li>{conflict.issue_count} open issues</li> : null}
              {conflict.mapping_count ? <li>{conflict.mapping_count} active worker mappings</li> : null}
            </ul>
            <p className="mb-3 text-xs text-text-muted">
              Force-delete will abandon tasks, leave issues orphaned, and invalidate the
              worker mappings. The Project row goes away. This is NOT REVERSIBLE.
            </p>
            <label className="mb-3 flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                checked={understood}
                onChange={(e) => setUnderstood(e.target.checked)}
                data-testid="project-delete-understood"
              />
              I understand
            </label>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
                onClick={onClose}
              >
                Cancel
              </button>
              <button
                type="button"
                disabled={!understood}
                className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
                onClick={() => void handleDelete(true)}
                data-testid="project-delete-force"
              >
                Force delete
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

// WorkersPanel lists existing mappings + lets the operator map a new
// worker. Combobox of all known workers from /api/fleet.
function WorkersPanel({ projectId }: { projectId: string }): React.ReactElement {
  const mappings = useProjectMappings(projectId);
  const fleet = useFleet();
  const create = useCreateProjectMapping(projectId);
  const del = useDeleteProjectMapping(projectId);
  const [showAdd, setShowAdd] = useState(false);
  const [workerID, setWorkerID] = useState('');
  const [path, setPath] = useState('');
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!workerID || !path) return;
    try {
      await create.mutateAsync({ worker_id: workerID, path });
      setShowAdd(false);
      setWorkerID('');
      setPath('');
    } catch {
      // surfaced inline
    }
  };
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-workers-panel"
    >
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">Mapped workers</h2>
        <button
          type="button"
          className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
          onClick={() => setShowAdd((v) => !v)}
          data-testid="project-map-worker-btn"
        >
          {showAdd ? 'Cancel' : '+ Map worker'}
        </button>
      </div>
      {showAdd && (
        <form onSubmit={submit} className="mb-3 space-y-2 rounded border border-border-base p-3">
          <label className="block text-xs">
            Worker
            <select
              value={workerID}
              onChange={(e) => setWorkerID(e.target.value)}
              className={editInputClass}
              data-testid="project-map-worker-select"
            >
              <option value="">Choose a worker…</option>
              {(fleet.data?.workers ?? []).map((w) => (
                <option key={w.worker_id} value={w.worker_id}>
                  {w.name} ({w.worker_id})
                </option>
              ))}
            </select>
          </label>
          <label className="block text-xs">
            Path on worker
            <input
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="/Users/me/projects/myp"
              className={editInputClass}
              data-testid="project-map-worker-path"
            />
          </label>
          {create.isError && (
            <p className="text-xs text-danger">{(create.error as Error).message}</p>
          )}
          <button
            type="submit"
            className="rounded bg-brand px-3 py-1 text-xs font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            disabled={!workerID || !path || create.isPending}
            data-testid="project-map-worker-submit"
          >
            {create.isPending ? 'Mapping...' : 'Map worker'}
          </button>
        </form>
      )}
      {mappings.isLoading ? (
        <p className="text-xs text-text-muted">Loading…</p>
      ) : (mappings.data ?? []).filter((m) => m.status === 'active').length === 0 ? (
        <p className="py-3 text-center text-xs text-text-muted">No workers mapped.</p>
      ) : (
        <ul className="divide-y divide-border-base">
          {(mappings.data ?? [])
            .filter((m) => m.status === 'active')
            .map((m) => (
              <li
                key={m.id}
                className="flex items-center justify-between gap-2 py-2"
                data-testid="project-mapping-row"
              >
                <div className="flex flex-col">
                  <span className="font-mono text-xs text-text-primary">{m.worker_id}</span>
                  <span className="font-mono text-[0.6875rem] text-text-muted">{m.path}</span>
                </div>
                <button
                  type="button"
                  className="rounded border border-border-base px-2 py-0.5 text-[0.6875rem] text-text-secondary hover:bg-bg-subtle"
                  onClick={() => void del.mutateAsync(m.id)}
                  data-testid="project-unmap-btn"
                >
                  Unmap
                </button>
              </li>
            ))}
        </ul>
      )}
    </div>
  );
}

const editInputClass =
  'mt-1 block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

const editTagChipsContainerClass =
  'mt-1 flex flex-wrap items-center gap-1.5 rounded border border-border-base bg-bg-elevated px-2 py-1.5 focus-within:border-accent';

const editTagChipClass =
  'inline-flex items-center rounded bg-bg-subtle px-2 py-0.5 text-xs text-text-primary';

// -----------------------------------------------------------------------------
// Inline PanelCard — mirrors the Home.tsx shape (per brief: don't extract a
// shared component this pass, just copy).
// -----------------------------------------------------------------------------
function PanelCard({
  title,
  to,
  empty,
  loading,
  children,
  ...rest
}: {
  title: string;
  to: string;
  empty: string;
  loading: boolean;
  children: React.ReactNode;
} & React.HTMLAttributes<HTMLDivElement>): React.ReactElement {
  const items = React.Children.toArray(children);
  return (
    <div className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1" {...rest}>
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">{title}</h2>
        <Link to={to} className="text-xs text-accent hover:underline">
          View all →
        </Link>
      </div>
      {loading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : items.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">{empty}</p>
      ) : (
        <ul className="divide-y divide-border-base">{items}</ul>
      )}
    </div>
  );
}

function IssuesPanel({ projectId }: { projectId: string }): React.ReactElement {
  const issues = useIssues({ projectId });
  const recent = (issues.data ?? []).slice(0, 5);
  return (
    <PanelCard
      title="Issues"
      to={`/issues?project=${encodeURIComponent(projectId)}`}
      empty="No issues yet"
      loading={issues.isLoading}
      data-testid="project-issues-panel"
    >
      {recent.map((iss) => (
        <li key={iss.id} className="flex items-center justify-between gap-3 py-1.5">
          <Link
            to={`/issues/${encodeURIComponent(iss.id)}`}
            className="truncate text-sm text-text-primary hover:text-accent"
          >
            {iss.title || iss.id}
          </Link>
          <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
            {iss.status.replace(/_/g, ' ')}
          </span>
        </li>
      ))}
    </PanelCard>
  );
}

function TasksPanel({ projectId }: { projectId: string }): React.ReactElement {
  const tasks = useTasksList({ projectId });
  const recent = (tasks.data ?? []).slice(0, 5);
  return (
    <PanelCard
      title="Tasks"
      to={`/tasks?project=${encodeURIComponent(projectId)}`}
      empty="No tasks yet"
      loading={tasks.isLoading}
      data-testid="project-tasks-panel"
    >
      {recent.map((tk) => (
        <li key={tk.id} className="flex items-center justify-between gap-3 py-1.5">
          <Link
            to={`/tasks/${encodeURIComponent(tk.id)}`}
            className="truncate text-sm text-text-primary hover:text-accent"
          >
            {tk.title || tk.id}
          </Link>
          <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
            {tk.status}
          </span>
        </li>
      ))}
    </PanelCard>
  );
}

function FleetLinkSection({ projectId }: { projectId: string }): React.ReactElement {
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-fleet-link"
    >
      <h2 className="font-heading text-sm font-semibold text-text-primary">Workers</h2>
      <p className="mt-1 text-xs text-text-secondary">
        Worker / execution rollups for this project live in the Fleet view.
      </p>
      <Link
        to={`/fleet?project=${encodeURIComponent(projectId)}`}
        className="mt-2 inline-block text-xs text-accent hover:underline"
      >
        View in Fleet →
      </Link>
    </div>
  );
}

import React, { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useNavigate, useParams } from 'react-router-dom';
import {
  useDeleteProject,
  useProject,
  useProjectCodeRepos,
  useProjectMembers,
  useRemoveProjectMember,
  useUpdateProject,
  type Project,
} from '@/api/projects';
import { useIssues } from '@/api/issues';
import { useTasksList } from '@/api/tasks';
import { useDisplayNameResolver, normalizeIdentityRef } from '@/api/members';
import { ApiError } from '@/api/client';
import { useAppStore } from '@/store/app';
import { IssueCreateModal } from '@/components/IssueCreateModal';
import { TaskCreateModal } from '@/components/TaskCreateModal';
import { EntityRef } from '@/components/EntityRef';
import { ConfirmModal } from '@/components/ConfirmModal';
import { ProjectMemberAddModal } from '@/components/ProjectMemberAddModal';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';

// ProjectDetail (/projects/:id). v2.7 ProjectManager BC: a single
// project hosts its Issues and Tasks as tabs/sections — there is no
// global Issues/Tasks page and no cross-project aggregation. Worker /
// project mapping was retired.
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
        <OrgLink to="/projects" className="text-xs text-accent hover:underline">
          ← Back to projects
        </OrgLink>
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
      <Breadcrumb items={[{ label: 'Projects', to: '/projects' }, { label: p.name }]} />
      <ProjectHeader project={p} />
      <ProjectWorkTabs projectId={p.id} />
      <FleetLinkSection />
    </section>
  );
}

function ProjectStatusBadge({ status }: { status: Project['status'] }): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        status === 'archived'
          ? 'bg-bg-subtle text-text-muted'
          : 'bg-success/10 text-success',
      ].join(' ')}
      data-testid={`project-status-${status}`}
    >
      {status}
    </span>
  );
}

function ProjectHeader({ project: p }: { project: Project }): React.ReactElement {
  const [editing, setEditing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  return (
    <header className="space-y-2 border-b border-border-base pb-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          {/* v2.7 #192: project name as heading; raw id on hover, no visible badge. */}
          <h1 className="font-heading text-2xl font-semibold text-text-primary" title={p.id}>{p.name}</h1>
          <ProjectStatusBadge status={p.status} />
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
            Archive
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
  const update = useUpdateProject(p.id);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const fields: { name?: string; description?: string } = {};
    if (name !== p.name) fields.name = name;
    if (description !== (p.description ?? '')) fields.description = description;
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
  const handleDelete = async () => {
    try {
      await del.mutateAsync();
      navigate('/projects');
    } catch {
      // surfaced below
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
        <h2 className="mb-4 text-lg font-semibold text-danger">Archive project?</h2>
        <p className="mb-3 text-sm text-text-secondary">
          Archive <span className="font-mono">{p.name}</span> ({p.id})?
        </p>
        <p className="mb-4 text-xs text-text-muted">
          Archiving hides the project from the active list. Its Issues and Tasks
          remain but the project is marked archived.
        </p>
        {del.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="project-delete-error">
            {(del.error as Error).message}
          </p>
        )}
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
            disabled={del.isPending}
            className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:opacity-50"
            onClick={() => void handleDelete()}
            data-testid="project-delete-confirm"
          >
            {del.isPending ? 'Archiving…' : 'Archive'}
          </button>
        </div>
      </div>
    </div>
  );
}

const editInputClass =
  'mt-1 block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

// -----------------------------------------------------------------------------
// Work tabs: Issues / Tasks live inside the project.
// -----------------------------------------------------------------------------
type WorkTab = 'issues' | 'tasks' | 'members' | 'repos';

function ProjectWorkTabs({ projectId }: { projectId: string }): React.ReactElement {
  const [tab, setTab] = useState<WorkTab>('issues');
  return (
    <div data-testid="project-work-tabs">
      <div className="flex gap-1" role="tablist" aria-label="project work">
        <TabButton label="Issues" value="issues" active={tab === 'issues'} onClick={() => setTab('issues')} />
        <TabButton label="Tasks" value="tasks" active={tab === 'tasks'} onClick={() => setTab('tasks')} />
        <TabButton label="Members" value="members" active={tab === 'members'} onClick={() => setTab('members')} />
        <TabButton label="Code repos" value="repos" active={tab === 'repos'} onClick={() => setTab('repos')} />
      </div>
      <div className="mt-3">
        {tab === 'issues' && <IssuesPanel projectId={projectId} />}
        {tab === 'tasks' && <TasksPanel projectId={projectId} />}
        {tab === 'members' && <MembersPanel projectId={projectId} />}
        {tab === 'repos' && <CodeReposPanel projectId={projectId} />}
      </div>
    </div>
  );
}

function TabButton({
  label,
  value,
  active,
  onClick,
}: {
  label: string;
  value: WorkTab;
  active: boolean;
  onClick: () => void;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      data-testid={`project-tab-${value}`}
      className={[
        'rounded px-3 py-1 text-xs uppercase tracking-wide',
        active
          ? 'bg-text-primary text-bg-elevated'
          : 'bg-bg-subtle text-text-secondary hover:bg-border-base',
      ].join(' ')}
    >
      {label}
    </button>
  );
}

// v2.7 #207: map the RemoveProjectMember guard codes to friendly copy (Rule 9).
function removeMemberErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'cannot_remove_owner') return "The project owner can't be removed.";
    if (err.code === 'not_member') return 'This member is no longer in the project.';
  }
  return err instanceof Error ? err.message : 'Remove failed, please try again.';
}

function MembersPanel({ projectId }: { projectId: string }): React.ReactElement {
  const members = useProjectMembers(projectId);
  // v2.7 #192: show member display names (raw identity id on hover).
  const resolveName = useDisplayNameResolver();
  // v2.7 #207: owner-gated add/remove.
  const me = useAppStore((s) => s.currentUserId);
  const remove = useRemoveProjectMember(projectId);
  const [addOpen, setAddOpen] = useState(false);
  const [pendingRemove, setPendingRemove] = useState<{ id: string; label: string } | null>(null);
  const data = members.data ?? [];
  const isOwner = data.some(
    (m) => normalizeIdentityRef(m.identity_id) === normalizeIdentityRef(me ?? '') && m.role === 'owner',
  );

  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-members-panel"
    >
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">Members</h2>
        {isOwner && (
          <button
            type="button"
            onClick={() => setAddOpen(true)}
            data-testid="project-add-member-button"
            className="rounded bg-bg-subtle px-2 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
          >
            + Add
          </button>
        )}
      </div>
      {members.isLoading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : members.isError ? (
        <p className="py-2 text-xs text-danger" data-testid="project-members-error">
          {(members.error as Error).message}
        </p>
      ) : data.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">No members yet</p>
      ) : (
        <ul className="divide-y divide-border-base">
          {data.map((m) => {
            const label = resolveName(m.identity_id) === m.identity_id ? m.identity_id : resolveName(m.identity_id);
            return (
              <li
                key={m.id}
                data-testid="member-row"
                data-member-id={m.id}
                className="flex items-center justify-between gap-3 py-1.5"
              >
                <EntityRef
                  id={m.identity_id}
                  name={resolveName(m.identity_id) === m.identity_id ? undefined : resolveName(m.identity_id)}
                  fallback={m.identity_id}
                  testId="project-member-ref"
                  className="truncate text-sm text-text-primary"
                />
                <span className="flex items-center gap-2">
                  <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                    {m.role}
                  </span>
                  {/* Owner-only; the owner row can't be removed (backend guards too). */}
                  {isOwner && m.role !== 'owner' && (
                    <button
                      type="button"
                      data-testid="project-member-remove"
                      data-identity={m.identity_id}
                      onClick={() => {
                        remove.reset();
                        setPendingRemove({ id: m.identity_id, label });
                      }}
                      className="rounded px-1.5 py-0.5 text-xs text-text-muted hover:bg-danger/10 hover:text-danger"
                    >
                      Remove
                    </button>
                  )}
                </span>
              </li>
            );
          })}
        </ul>
      )}

      {remove.isError && (
        <p className="mt-2 text-xs text-danger" data-testid="project-member-remove-error" role="alert">
          {removeMemberErrorMessage(remove.error)}
        </p>
      )}

      {addOpen && (
        <ProjectMemberAddModal projectId={projectId} existing={data} onClose={() => setAddOpen(false)} />
      )}

      <ConfirmModal
        open={pendingRemove !== null}
        danger
        busy={remove.isPending}
        title="Remove member"
        message={
          pendingRemove
            ? `Remove ${pendingRemove.label} from this project? They lose access to the project's tasks and issues.`
            : undefined
        }
        confirmLabel="Remove"
        onCancel={() => {
          if (remove.isPending) return;
          setPendingRemove(null);
          remove.reset();
        }}
        onConfirm={() => {
          if (!pendingRemove) return;
          remove.mutate(pendingRemove.id, { onSettled: () => setPendingRemove(null) });
        }}
      />
    </div>
  );
}

function CodeReposPanel({ projectId }: { projectId: string }): React.ReactElement {
  const repos = useProjectCodeRepos(projectId);
  const data = repos.data ?? [];
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-repos-panel"
    >
      <h2 className="mb-2 font-heading text-sm font-semibold text-text-primary">Code repos</h2>
      {repos.isLoading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : repos.isError ? (
        <p className="py-2 text-xs text-danger" data-testid="project-repos-error">
          {(repos.error as Error).message}
        </p>
      ) : data.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">No code repos linked</p>
      ) : (
        <ul className="divide-y divide-border-base">
          {data.map((r) => (
            <li
              key={r.id}
              data-testid="repo-row"
              data-repo-id={r.id}
              className="flex items-center justify-between gap-3 py-1.5"
            >
              <a
                href={r.url}
                target="_blank"
                rel="noreferrer"
                className="truncate text-sm text-accent hover:underline"
              >
                {r.label || r.url}
              </a>
              <span className="truncate font-mono text-[0.6875rem] text-text-muted">{r.url}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function IssuesPanel({ projectId }: { projectId: string }): React.ReactElement {
  const issues = useIssues(projectId);
  const [createOpen, setCreateOpen] = useState(false);
  const data = issues.data ?? [];
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-issues-panel"
    >
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">Issues</h2>
        <button
          type="button"
          className="rounded bg-brand px-2 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="project-issue-create-btn"
        >
          + Open Issue
        </button>
      </div>
      {createOpen && (
        <IssueCreateModal projectId={projectId} onClose={() => setCreateOpen(false)} />
      )}
      {issues.isLoading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : issues.isError ? (
        <p className="py-2 text-xs text-danger" data-testid="project-issues-error">
          {(issues.error as Error).message}
        </p>
      ) : data.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">No issues yet</p>
      ) : (
        <ul className="divide-y divide-border-base">
          {data.map((iss) => (
            <li
              key={iss.id}
              data-testid="issue-row"
              data-issue-id={iss.id}
              className="flex items-center justify-between gap-3 py-1.5"
            >
              <OrgLink
                to={`/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(iss.id)}`}
                className="truncate text-sm text-text-primary hover:text-accent"
              >
                {iss.title || iss.id}
              </OrgLink>
              <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                {iss.status.replace(/_/g, ' ')}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function TasksPanel({ projectId }: { projectId: string }): React.ReactElement {
  const tasks = useTasksList(projectId);
  const [createOpen, setCreateOpen] = useState(false);
  const data = tasks.data ?? [];
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-tasks-panel"
    >
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">Tasks</h2>
        <button
          type="button"
          className="rounded bg-brand px-2 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="project-task-create-btn"
        >
          + New Task
        </button>
      </div>
      {createOpen && (
        <TaskCreateModal projectId={projectId} onClose={() => setCreateOpen(false)} />
      )}
      {tasks.isLoading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : tasks.isError ? (
        <p className="py-2 text-xs text-danger" data-testid="project-tasks-error">
          {(tasks.error as Error).message}
        </p>
      ) : data.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">No tasks yet</p>
      ) : (
        <ul className="divide-y divide-border-base">
          {data.map((tk) => (
            <li
              key={tk.id}
              data-testid="task-row"
              data-task-id={tk.id}
              className="flex items-center justify-between gap-3 py-1.5"
            >
              <OrgLink
                to={`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(tk.id)}`}
                className="truncate text-sm text-text-primary hover:text-accent"
              >
                {tk.title || tk.id}
              </OrgLink>
              <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                {tk.status}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function FleetLinkSection(): React.ReactElement {
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-fleet-link"
    >
      <h2 className="font-heading text-sm font-semibold text-text-primary">Workers</h2>
      <p className="mt-1 text-xs text-text-secondary">
        Worker / execution rollups live in the Fleet view.
      </p>
      <OrgLink
        to="/fleet"
        className="mt-2 inline-block text-xs text-accent hover:underline"
      >
        View in Fleet →
      </OrgLink>
    </div>
  );
}

import React, { useEffect, useMemo, useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
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
import { useProjectPlansList } from '@/api/plans';
import { useAgents } from '@/api/agents';
import { formatLocalTime } from '@/utils/time';
import { useTasksList } from '@/api/tasks';
import { buildWorkItemFilters } from '@/api/orgWorkItems';
import { WorkItemFilterBar, EMPTY_DATE_RANGE, type DateRange } from '@/components/WorkItemFilterBar';
import { useDisplayNameResolver, normalizeIdentityRef, useCreatorLabel } from '@/api/members';
import { ApiError } from '@/api/client';
import { useAppStore } from '@/store/app';
import { IssueCreateModal } from '@/components/IssueCreateModal';
import { TaskCreateModal } from '@/components/TaskCreateModal';
import { EntityRef } from '@/components/EntityRef';
import { ConfirmModal } from '@/components/ConfirmModal';
import { ProjectMemberAddModal } from '@/components/ProjectMemberAddModal';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { StatusChip, refLabel, shortDate } from '@/components/workItemDisplay';
import { SortHeader, Pagination, useListControls } from '@/components/listControls';
import { PlanStatusChip, planProgressLabel, PlanFailedIndicator } from '@/components/planDisplay';
import { ToggleSwitch } from '@/components/ToggleSwitch';
import {
  useWorkspaceRepos,
  useAddProjectRepoRef,
  useRemoveProjectRepoRef,
  useSetPrimaryProjectRepo,
} from '@/api/repos';
import { ProviderBadge } from '@/components/repoDisplay';

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
      <ProjectStatsBlock project={p} />
      <ProjectWorkTabs projectId={p.id} />
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

// Shared box model for the project-header action controls (Work Board / Edit /
// Archive). Keeping the layout/sizing identical across the anchor and the
// <button>s guarantees they render the same height on mobile (#1). Per-control
// classes only add border/text/hover color.
const headerActionBtn =
  'inline-flex items-center rounded border px-2 py-1 text-xs leading-none';

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
        <div className="flex flex-wrap items-center gap-2">
          {/* v2.9 #286: entry to the project's Plan orchestration (the Work Board
              — parallel Plan list + DAG). Reachable here via the project detail
              page header (§4.2). Styled to match the sibling Edit/Archive buttons.
              All three share headerActionBtn so their box model matches; this one
              is an <a> (OrgLink), not a <button>, so the coarse-pointer 44px
              touch-target baseline (index.css) does NOT auto-apply as it does to
              the Edit/Archive <button>s. The explicit `touch-target` class opts it
              into the SAME 44px min-height so the three align on mobile (and the
              link meets the WCAG 2.5.5 touch target like its siblings). */}
          <OrgLink
            to={`/projects/${encodeURIComponent(p.id)}/plans`}
            className={`${headerActionBtn} touch-target gap-1 border-border-base text-text-primary hover:bg-bg-subtle`}
            data-testid="project-plans-link"
          >
            Work Board
          </OrgLink>
          <button
            type="button"
            className={`${headerActionBtn} border-border-base text-text-primary hover:bg-bg-subtle`}
            onClick={() => setEditing(true)}
            data-testid="project-edit-btn"
          >
            Edit
          </button>
          <button
            type="button"
            className={`${headerActionBtn} border-danger/40 text-danger hover:bg-danger/10`}
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
  // T566: auto-assign master switch — default ON (absent ⇒ true).
  const [autoAssign, setAutoAssign] = useState(p.auto_assign_enabled ?? true);
  const update = useUpdateProject(p.id);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const fields: { name?: string; description?: string; auto_assign_enabled?: boolean } = {};
    if (name !== p.name) fields.name = name;
    if (description !== (p.description ?? '')) fields.description = description;
    if (autoAssign !== (p.auto_assign_enabled ?? true)) fields.auto_assign_enabled = autoAssign;
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
        {/* T566 (issue-577a7b0e): project-level auto-assign master switch. */}
        <div className="mt-4 flex items-start gap-2.5">
          <ToggleSwitch
            checked={autoAssign}
            onChange={setAutoAssign}
            ariaLabel="Auto-assign pool tasks"
            testId="project-edit-auto-assign"
          />
          <span className="text-xs">
            <span className="font-medium text-text-primary">Auto-assign pool tasks</span>
            <span className="mt-0.5 block text-[0.6875rem] text-text-muted">
              When on, claimable pool tasks are automatically assigned to an eligible idle
              agent whose capabilities cover the task. Off = tasks wait for a manual
              assign/claim.
            </span>
          </span>
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
type WorkTab = 'issues' | 'tasks' | 'plans' | 'members' | 'repos';

// v2.10.0 [T4]: the active work tab is URL-param-driven (?tab=) so the col②
// project sub-nav (shell/nav/WorkspaceSecondaryNav) and the in-page tab bar
// stay in sync — clicking either updates the same `?tab=` and both highlight
// it. Defaults to 'issues' (unknown/absent param). Pre-T4 in-page state →
// the same testids/panels, so existing tests are unaffected.
function isWorkTab(v: string | null): v is WorkTab {
  return v === 'issues' || v === 'tasks' || v === 'plans' || v === 'members' || v === 'repos';
}

function ProjectWorkTabs({ projectId }: { projectId: string }): React.ReactElement {
  const [searchParams, setSearchParams] = useSearchParams();
  const raw = searchParams.get('tab');
  const tab: WorkTab = isWorkTab(raw) ? raw : 'issues';
  const setTab = (value: WorkTab) =>
    setSearchParams(
      (prev) => {
        prev.set('tab', value);
        return prev;
      },
      { replace: true },
    );
  return (
    <div data-testid="project-work-tabs">
      <div className="flex gap-1" role="tablist" aria-label="project work">
        <TabButton label="Issues" value="issues" active={tab === 'issues'} onClick={() => setTab('issues')} />
        <TabButton label="Tasks" value="tasks" active={tab === 'tasks'} onClick={() => setTab('tasks')} />
        <TabButton label="Plans" value="plans" active={tab === 'plans'} onClick={() => setTab('plans')} />
        <TabButton label="Code repos" value="repos" active={tab === 'repos'} onClick={() => setTab('repos')} />
        {/* Members rendered LAST (per @oopslink), after Code repos. */}
        <TabButton label="Members" value="members" active={tab === 'members'} onClick={() => setTab('members')} />
      </div>
      <div className="mt-3">
        {tab === 'issues' && <IssuesPanel projectId={projectId} />}
        {tab === 'tasks' && <TasksPanel projectId={projectId} />}
        {tab === 'plans' && <PlansPanel projectId={projectId} />}
        {tab === 'repos' && <CodeReposPanel projectId={projectId} />}
        {tab === 'members' && <MembersPanel projectId={projectId} />}
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
          ? 'bg-btn-primary-bg text-btn-primary-fg'
          : 'bg-bg-subtle text-text-secondary hover:bg-border-base',
      ].join(' ')}
    >
      {label}
    </button>
  );
}

// PlansPanel lists the project's plans (per @oopslink: a Plans tab after Tasks).
// Read-only table (ID / Name / Status / Progress) mirroring the Issues/Tasks
// panels; each name links to the plan detail page. Plans are created/edited on
// the Work Board, so there is no create button here.
function PlansPanel({ projectId }: { projectId: string }): React.ReactElement {
  // T302: the project plans endpoint with page params → SQL-paginated, builtin
  // pool excluded, returns { items, total }. usePlans (Work Board) stays
  // unpaginated + includes builtin.
  const controls = useListControls({ pageSize: 25, defaultSort: 'updated_at', defaultDir: 'desc' });
  const plans = useProjectPlansList(projectId, {
    sort: controls.sort,
    dir: controls.dir,
    page: controls.page,
    page_size: controls.pageSize,
  });
  const data = plans.data?.items ?? [];
  const total = plans.data?.total ?? 0;
  // Owner ask: show the creator's NAME (agent / human), not the raw id.
  const creatorLabel = useCreatorLabel();
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-plans-panel"
    >
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">Plans</h2>
        <OrgLink
          to={`/projects/${encodeURIComponent(projectId)}/plans`}
          className="rounded bg-bg-subtle px-2 py-1 text-xs font-medium text-text-secondary hover:bg-border-base"
          data-testid="project-plans-board-link"
        >
          Work Board
        </OrgLink>
      </div>
      {plans.isLoading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : plans.isError ? (
        <p className="py-2 text-xs text-danger" data-testid="project-plans-error">
          {(plans.error as Error).message}
        </p>
      ) : data.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">No plans yet</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs" data-testid="project-plans-table">
            <thead>
              <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                <SortHeader label="ID" sortKey="org_ref" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Name" sortKey="name" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Status" sortKey="status" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 pr-3 font-medium">Progress</th>
                <SortHeader label="Created" sortKey="created_at" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 font-medium">Creator</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {data.map((pl) => (
                <tr key={pl.id} data-testid="plan-row" data-plan-id={pl.id}>
                  <td className="py-1.5 pr-3 font-mono text-text-muted" data-testid="plan-id-handle" title={pl.id}>
                    {/* P123 org_ref when present; #id-tail handle otherwise. */}
                    {refLabel(pl.org_ref, pl.id)}
                  </td>
                  <td className="max-w-[18rem] truncate py-1.5 pr-3">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(pl.id)}`}
                      className="text-text-primary hover:text-accent"
                    >
                      {pl.name || pl.id}
                    </OrgLink>
                  </td>
                  <td className="py-1.5 pr-3">
                    <span className="inline-flex items-center gap-1">
                      <PlanStatusChip status={pl.status} />
                      <PlanFailedIndicator hasFailed={pl.has_failed} />
                    </span>
                  </td>
                  <td className="py-1.5 pr-3 tabular-nums text-text-muted">
                    {planProgressLabel(pl.progress)}
                  </td>
                  <td className="py-1.5 pr-3 tabular-nums text-text-muted" title={formatLocalTime(pl.created_at)}>
                    {shortDate(pl.created_at)}
                  </td>
                  <td className="py-1.5 text-text-secondary" title={pl.creator_ref}>
                    {creatorLabel(pl.creator_ref)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <Pagination page={controls.page} pageSize={controls.pageSize} total={total} onPageChange={controls.setPage} />
        </div>
      )}
    </div>
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
  // Click a member name → its detail page. Agent members link to their
  // execution Agent (identity_member_id == member identity_id, the unified-create
  // link; no match → no link for legacy identity-only rows). Human members link
  // to the user page. Mirrors the canonical resolution in MembersAgents.
  const agents = useAgents();
  const agentIDByIdentity = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents.data ?? []) {
      if (a.identity_member_id) map.set(a.identity_member_id, a.id);
    }
    return map;
  }, [agents.data]);
  // ProjectMember carries no `kind`, so resolve agent vs human from the
  // identity_id prefix (same compat check MembersAgents uses).
  const memberPath = (identityId: string): string | undefined => {
    if (identityId.startsWith('agent:') || identityId.startsWith('agent-')) {
      const aid = agentIDByIdentity.get(identityId);
      return aid ? `/agents/${encodeURIComponent(aid)}` : undefined;
    }
    return `/users/${encodeURIComponent(normalizeIdentityRef(identityId))}`;
  };
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
        <div className="overflow-x-auto">
          <table className="w-full text-sm" data-testid="project-members-table">
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                <th className="py-2 pr-3 font-medium">Identity ID</th>
                <th className="py-2 pr-3 font-medium">Display Name</th>
                <th className="py-2 pr-3 font-medium">Joined</th>
                <th className="py-2 pr-3 font-medium">Kind</th>
                <th className="py-2 pr-3 font-medium">Role</th>
                {isOwner && <th className="py-2 font-medium" />}
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {data.map((m) => {
                const displayName = resolveName(m.identity_id);
                const label = displayName === m.identity_id ? m.identity_id : displayName;
                const kind = m.identity_id.startsWith('agent:') || m.identity_id.startsWith('agent-') ? 'agent' : 'user';
                return (
                  <tr key={m.id} data-testid="member-row" data-member-id={m.id} className="text-text-secondary">
                    <td className="py-2 pr-3 font-mono text-xs text-text-muted">{m.identity_id}</td>
                    <td className="py-2 pr-3 text-text-primary">
                      {memberPath(m.identity_id) ? (
                        <OrgLink to={memberPath(m.identity_id)!} className="hover:underline" data-testid="project-member-ref" title={m.identity_id}>{displayName !== m.identity_id ? displayName : normalizeIdentityRef(m.identity_id)}</OrgLink>
                      ) : (
                        <span>{displayName !== m.identity_id ? displayName : normalizeIdentityRef(m.identity_id)}</span>
                      )}
                    </td>
                    <td className="py-2 pr-3 whitespace-nowrap text-text-muted">{shortDate(m.created_at)}</td>
                    <td className="py-2 pr-3">
                      <span className={`rounded px-1.5 py-0.5 text-[0.6875rem] font-medium ${kind === 'agent' ? 'bg-brand/10 text-brand' : 'bg-bg-subtle text-text-muted'}`}>{kind}</span>
                    </td>
                    <td className="py-2 pr-3">
                      <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">{m.role}</span>
                    </td>
                    {isOwner && (
                      <td className="py-2 text-right">
                        {m.role !== 'owner' && (
                          <button
                            type="button"
                            data-testid="project-member-remove"
                            data-identity={m.identity_id}
                            onClick={() => { remove.reset(); setPendingRemove({ id: m.identity_id, label }); }}
                            className="rounded px-1.5 py-0.5 text-xs text-text-muted hover:bg-danger/10 hover:text-danger"
                          >
                            Remove
                          </button>
                        )}
                      </td>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
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

// CodeReposPanel — T575 (issue-f980c8de): the project's "Referenced repositories"
// referencer. A project REFERENCES workspace repos (no url/credential config
// here — those live on the workspace Repos page). Each ref shows the joined
// workspace-repo info (provider/description/branch), a primary marker (the
// starred repo, used by Integrate merge-check), and an un-reference action; a selector adds a new
// reference to any not-yet-referenced workspace repo.
function CodeReposPanel({ projectId }: { projectId: string }): React.ReactElement {
  const refs = useProjectCodeRepos(projectId);
  const workspaceRepos = useWorkspaceRepos();
  const addRef = useAddProjectRepoRef(projectId);
  const removeRef = useRemoveProjectRepoRef(projectId);
  const setPrimary = useSetPrimaryProjectRepo(projectId);
  const [pick, setPick] = useState('');

  const data = refs.data ?? [];
  const repoById = useMemo(
    () => new Map((workspaceRepos.data ?? []).map((r) => [r.id, r])),
    [workspaceRepos.data],
  );
  // Workspace repos not yet referenced — the add-selector options.
  const referenced = useMemo(() => new Set(data.map((r) => r.repo_id).filter(Boolean)), [data]);
  const available = (workspaceRepos.data ?? []).filter((r) => !referenced.has(r.id));
  const hasPrimary = data.some((r) => r.is_primary);

  const onAdd = () => {
    if (!pick) return;
    const repo = repoById.get(pick);
    addRef.mutate(
      { repo_id: pick, url: repo?.url, label: repo?.label, is_primary: !hasPrimary },
      { onSuccess: () => setPick('') },
    );
  };

  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-repos-panel"
    >
      <h2 className="mb-1 font-heading text-sm font-semibold text-text-primary">Referenced repositories</h2>
      <p className="mb-3 text-xs text-text-muted">
        Workspace repos this project references. The primary repo (starred) drives Integrate
        merge-check. Configure url/credentials on the workspace Repos page.
      </p>
      {refs.isLoading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : refs.isError ? (
        <p className="py-2 text-xs text-danger" data-testid="project-repos-error">
          {(refs.error as Error).message}
        </p>
      ) : data.length === 0 ? (
        <p className="py-3 text-center text-xs text-text-muted" data-testid="project-repos-empty">
          No referenced repositories.
        </p>
      ) : (
        <ul className="space-y-2">
          {data.map((r) => {
            const repo = r.repo_id ? repoById.get(r.repo_id) : undefined;
            return (
              <li
                key={r.id}
                data-testid="repo-row"
                data-repo-id={r.id}
                className="flex items-start gap-2 rounded-lg border border-border-base p-2"
              >
                <button
                  type="button"
                  onClick={() => !r.is_primary && setPrimary.mutate(r.id)}
                  disabled={r.is_primary || setPrimary.isPending}
                  title={r.is_primary ? 'Primary repo' : 'Set as primary'}
                  aria-label={r.is_primary ? 'Primary repo' : 'Set as primary'}
                  data-testid="repo-row-primary"
                  data-primary={r.is_primary ? 'true' : 'false'}
                  className={`mt-0.5 ${r.is_primary ? 'text-warning' : 'text-text-muted hover:text-warning'}`}
                >
                  <svg
                    viewBox="0 0 20 20"
                    className="h-4 w-4"
                    fill={r.is_primary ? 'currentColor' : 'none'}
                    stroke="currentColor"
                    strokeWidth="1.5"
                    aria-hidden="true"
                  >
                    <path d="M10 2.5l2.35 4.76 5.25.76-3.8 3.7.9 5.23L10 14.94 5.3 16.95l.9-5.23-3.8-3.7 5.25-.76z" strokeLinejoin="round" />
                  </svg>
                </button>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    {repo && <ProviderBadge provider={repo.provider} />}
                    {/* Repo name links to its detail on the workspace Repos page
                        (per @oopslink). Only when the workspace repo is resolved;
                        otherwise fall back to plain text. */}
                    {r.repo_id ? (
                      <OrgLink
                        to={`/repos?repo=${encodeURIComponent(r.repo_id)}`}
                        data-testid="repo-row-name-link"
                        className="truncate text-sm font-medium text-text-primary hover:text-accent hover:underline"
                      >
                        {repo?.label || r.label || r.url}
                      </OrgLink>
                    ) : (
                      <span className="truncate text-sm font-medium text-text-primary">{repo?.label || r.label || r.url}</span>
                    )}
                    {r.is_primary && (
                      <span className="rounded bg-bg-subtle px-1 py-0.5 text-[0.5625rem] font-semibold uppercase tracking-wide text-warning">primary</span>
                    )}
                    {repo?.default_branch && (
                      <span className="ml-auto rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem] text-text-secondary">{repo.default_branch}</span>
                    )}
                  </div>
                  {repo?.description && (
                    <p className="truncate text-xs text-text-secondary">{repo.description}</p>
                  )}
                </div>
                <button
                  type="button"
                  onClick={() => removeRef.mutate(r.id)}
                  disabled={removeRef.isPending}
                  className="shrink-0 rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
                  data-testid="repo-row-remove"
                >
                  Unreference
                </button>
              </li>
            );
          })}
        </ul>
      )}

      {/* Add a reference to a workspace repo (only repos not already referenced). */}
      <div className="mt-3 flex items-center gap-2 rounded-lg border border-dashed border-border-base p-2">
        <select
          value={pick}
          onChange={(e) => setPick(e.target.value)}
          className="min-w-0 flex-1 rounded border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary"
          data-testid="project-repos-add-select"
          aria-label="Reference a workspace repo"
          disabled={available.length === 0}
        >
          <option value="">{available.length === 0 ? 'No more workspace repos to reference' : 'Select a workspace repo…'}</option>
          {available.map((r) => (
            <option key={r.id} value={r.id}>{r.label}</option>
          ))}
        </select>
        <button
          type="button"
          onClick={onAdd}
          disabled={!pick || addRef.isPending}
          className="shrink-0 rounded bg-brand px-3 py-1 text-xs font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
          data-testid="project-repos-add-btn"
        >
          Add reference
        </button>
      </div>
      {(addRef.isError || removeRef.isError || setPrimary.isError) && (
        <p className="mt-2 text-xs text-danger" data-testid="project-repos-mutate-error">
          {((addRef.error || removeRef.error || setPrimary.error) as Error).message}
        </p>
      )}
    </div>
  );
}

// T131: the project Issue list reuses the global FilterBar (project dimension
// fixed/hidden) and sends the same filter params to the project-scoped endpoint.
function IssuesPanel({ projectId }: { projectId: string }): React.ReactElement {
  const [selectedStatuses, setSelectedStatuses] = useState<string[]>([]);
  const [assignee, setAssignee] = useState<string>('');
  const [dateRange, setDateRange] = useState<DateRange>(EMPTY_DATE_RANGE);
  // T302: server-side sort + pagination (column headers + bottom pager).
  const controls = useListControls({ pageSize: 25, defaultSort: 'updated_at', defaultDir: 'desc' });
  const filters = {
    ...(buildWorkItemFilters({ selectedStatuses, selectedProjects: [], assignee, dateRange }) ?? {}),
    sort: controls.sort,
    dir: controls.dir,
    page: controls.page,
    page_size: controls.pageSize,
  };
  const issues = useIssues(projectId, filters);
  const [createOpen, setCreateOpen] = useState(false);
  const data = issues.data?.items ?? [];
  const total = issues.data?.total ?? 0;
  // reset to page 1 when a filter narrows the set.
  const setPage = controls.setPage;
  useEffect(() => {
    setPage(1);
  }, [selectedStatuses, assignee, dateRange, setPage]);
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
      <div className="mb-3">
        <WorkItemFilterBar
          kind="issue"
          hideProject
          selectedStatuses={selectedStatuses}
          onStatusesChange={setSelectedStatuses}
          selectedProjects={[]}
          onProjectsChange={() => {}}
          assignee={assignee}
          onAssigneeChange={setAssignee}
          dateRange={dateRange}
          onDateRangeChange={setDateRange}
        />
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
        <p className="py-4 text-center text-xs text-text-muted">
          {selectedStatuses.length > 0 || assignee ? 'No matching issues' : 'No issues yet'}
        </p>
      ) : (
        // v2.7.1 #242: table layout (ID / Title / Status / Updated). ID-first so
        // issues are easy to reference; ULID-tail handle (#126) + full id hover.
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs" data-testid="project-issues-table">
            <thead>
              <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                <SortHeader label="ID" sortKey="org_ref" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Title" sortKey="title" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Status" sortKey="status" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Created" sortKey="created_at" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Updated" sortKey="updated_at" controls={controls} className="py-1.5 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {data.map((iss) => (
                <tr key={iss.id} data-testid="issue-row" data-issue-id={iss.id}>
                  <td className="py-1.5 pr-3 font-mono text-text-muted" data-testid="issue-id-handle" title={iss.id}>
                    {/* v2.7.1 #245: org ref (I1234) when present; hash id on hover. */}
                    {refLabel(iss.org_ref, iss.id)}
                  </td>
                  <td className="max-w-[18rem] truncate py-1.5 pr-3">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(iss.id)}`}
                      className="text-text-primary hover:text-accent"
                    >
                      {iss.title || iss.id}
                    </OrgLink>
                  </td>
                  <td className="py-1.5 pr-3">
                    <StatusChip status={iss.status} />
                  </td>
                  <td className="py-1.5 pr-3 tabular-nums text-text-muted" title={formatLocalTime(iss.created_at)}>
                    {shortDate(iss.created_at)}
                  </td>
                  <td className="py-1.5 tabular-nums text-text-muted" title={formatLocalTime(iss.updated_at)}>
                    {shortDate(iss.updated_at)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <Pagination page={controls.page} pageSize={controls.pageSize} total={total} onPageChange={controls.setPage} />
        </div>
      )}
    </div>
  );
}

// T131: the project Task list reuses the global FilterBar (project dimension
// fixed/hidden) and sends the same filter params to the project-scoped endpoint.
function TasksPanel({ projectId }: { projectId: string }): React.ReactElement {
  const [selectedStatuses, setSelectedStatuses] = useState<string[]>([]);
  const [assignee, setAssignee] = useState<string>('');
  const [dateRange, setDateRange] = useState<DateRange>(EMPTY_DATE_RANGE);
  // T302: server-side sort + pagination.
  const controls = useListControls({ pageSize: 25, defaultSort: 'updated_at', defaultDir: 'desc' });
  const filters = {
    ...(buildWorkItemFilters({ selectedStatuses, selectedProjects: [], assignee, dateRange }) ?? {}),
    sort: controls.sort,
    dir: controls.dir,
    page: controls.page,
    page_size: controls.pageSize,
  };
  const tasks = useTasksList(projectId, filters);
  const [createOpen, setCreateOpen] = useState(false);
  // v2.7.1 #242: resolve the assignee ref → display name (raw ref on hover, #192).
  const resolveName = useDisplayNameResolver();
  const data = tasks.data?.items ?? [];
  const total = tasks.data?.total ?? 0;
  const setPage = controls.setPage;
  useEffect(() => {
    setPage(1);
  }, [selectedStatuses, assignee, dateRange, setPage]);
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
      <div className="mb-3">
        <WorkItemFilterBar
          kind="task"
          hideProject
          selectedStatuses={selectedStatuses}
          onStatusesChange={setSelectedStatuses}
          selectedProjects={[]}
          onProjectsChange={() => {}}
          assignee={assignee}
          onAssigneeChange={setAssignee}
          dateRange={dateRange}
          onDateRangeChange={setDateRange}
        />
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
        <p className="py-4 text-center text-xs text-text-muted">
          {selectedStatuses.length > 0 || assignee ? 'No matching tasks' : 'No tasks yet'}
        </p>
      ) : (
        // v2.7.1 #242: table (ID / Title / Status / Assigned to / Priority / Updated).
        // Priority has no schema yet → "—" fallback (v2.8 #231).
        <div className="overflow-x-auto">
          <table className="w-full text-left text-xs" data-testid="project-tasks-table">
            <thead>
              <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                <SortHeader label="ID" sortKey="org_ref" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Title" sortKey="title" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Status" sortKey="status" controls={controls} className="py-1.5 pr-3 font-medium" />
                <th className="py-1.5 pr-3 font-medium">Assigned to</th>
                <th className="py-1.5 pr-3 font-medium">Priority</th>
                <SortHeader label="Created" sortKey="created_at" controls={controls} className="py-1.5 pr-3 font-medium" />
                <SortHeader label="Updated" sortKey="updated_at" controls={controls} className="py-1.5 font-medium" />
              </tr>
            </thead>
            <tbody className="divide-y divide-border-base">
              {data.map((tk) => (
                <tr key={tk.id} data-testid="task-row" data-task-id={tk.id}>
                  <td className="py-1.5 pr-3 font-mono text-text-muted" data-testid="task-id-handle" title={tk.id}>
                    {/* v2.7.1 #245: org ref (T1234) when present; hash id on hover. */}
                    {refLabel(tk.org_ref, tk.id)}
                  </td>
                  <td className="max-w-[16rem] truncate py-1.5 pr-3">
                    <OrgLink
                      to={`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(tk.id)}`}
                      className="text-text-primary hover:text-accent"
                    >
                      {tk.title || tk.id}
                    </OrgLink>
                  </td>
                  <td className="py-1.5 pr-3">
                    <StatusChip status={tk.status} />
                  </td>
                  <td className="py-1.5 pr-3 text-text-secondary" data-testid="task-assignee">
                    {tk.assignee ? (
                      <EntityRef
                        id={tk.assignee}
                        name={resolveName(tk.assignee) === tk.assignee ? undefined : resolveName(tk.assignee)}
                        fallback={normalizeIdentityRef(tk.assignee)}
                      />
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className="py-1.5 pr-3 text-text-muted" data-testid="task-priority">—</td>
                  <td className="py-1.5 pr-3 tabular-nums text-text-muted" title={formatLocalTime(tk.created_at)}>
                    {shortDate(tk.created_at)}
                  </td>
                  <td className="py-1.5 tabular-nums text-text-muted" title={formatLocalTime(tk.updated_at)}>
                    {shortDate(tk.updated_at)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <Pagination page={controls.page} pageSize={controls.pageSize} total={total} onPageChange={controls.setPage} />
        </div>
      )}
    </div>
  );
}

// ProjectStatsBlock — the project-detail header summary the owner asked for
// (replaces the removed Workers/Environment-link panel): a counts row
// (issues / tasks / plans / repos, from the GET project DTO's *_count fields) and
// a "recent activity" feed of the 3 most-recently-updated work items merged
// across issues / tasks / plans.
function ProjectStatsBlock({ project: p }: { project: Project }): React.ReactElement {
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2" data-testid="project-stats-block">
      <ProjectCountsCard project={p} />
      <ProjectRecentActivityCard projectId={p.id} />
    </div>
  );
}

// fmtCount renders a count, degrading an absent count (older payload / not-yet-
// loaded) to an em-dash rather than a misleading 0.
function fmtCount(n: number | undefined): string {
  return typeof n === 'number' ? String(n) : '—';
}

function ProjectCountsCard({ project: p }: { project: Project }): React.ReactElement {
  const base = `/projects/${encodeURIComponent(p.id)}`;
  const tiles: { label: string; value: number | undefined; tab: WorkTab }[] = [
    { label: 'Issues', value: p.issue_count, tab: 'issues' },
    { label: 'Tasks', value: p.task_count, tab: 'tasks' },
    { label: 'Plans', value: p.plan_count, tab: 'plans' },
    { label: 'Repos', value: p.repo_count, tab: 'repos' },
  ];
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-counts-card"
    >
      <h2 className="mb-3 font-heading text-sm font-semibold text-text-primary">Overview</h2>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {tiles.map((t) => (
          <OrgLink
            key={t.tab}
            to={`${base}?tab=${t.tab}`}
            className="flex flex-col rounded-md border border-border-base bg-bg-subtle px-3 py-2 hover:border-accent"
            data-testid={`project-stat-${t.tab}`}
          >
            <span className="tabular-nums text-xl font-semibold text-text-primary" data-testid={`project-stat-${t.tab}-value`}>
              {fmtCount(t.value)}
            </span>
            <span className="text-[0.6875rem] uppercase tracking-wide text-text-muted">{t.label}</span>
          </OrgLink>
        ))}
      </div>
    </div>
  );
}

// ActivityKindBadge — a tiny pill marking a recent-activity row's entity kind.
function ActivityKindBadge({ kind }: { kind: 'issue' | 'task' | 'plan' }): React.ReactElement {
  const label = kind === 'issue' ? 'Issue' : kind === 'task' ? 'Task' : 'Plan';
  return (
    <span className="shrink-0 rounded bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-text-secondary">
      {label}
    </span>
  );
}

interface ActivityRow {
  kind: 'issue' | 'task' | 'plan';
  id: string;
  title: string;
  orgRef?: string;
  updatedAt: string;
  to: string;
}

// ProjectRecentActivityCard — the 3 most-recently-updated work items across the
// project's issues / tasks / plans. There is no dedicated project activity feed,
// so we derive it by fetching each list sorted by updated_at desc (small page)
// and merging — a self-contained "what changed recently" without a new endpoint.
function ProjectRecentActivityCard({ projectId }: { projectId: string }): React.ReactElement {
  const recent = { sort: 'updated_at', dir: 'desc' as const, page: 1, page_size: 3 };
  const issues = useIssues(projectId, recent);
  const tasks = useTasksList(projectId, recent);
  const plans = useProjectPlansList(projectId, recent);
  const base = `/projects/${encodeURIComponent(projectId)}`;

  const rows: ActivityRow[] = useMemo(() => {
    const out: ActivityRow[] = [];
    for (const it of issues.data?.items ?? []) {
      out.push({
        kind: 'issue', id: it.id, title: it.title, orgRef: it.org_ref, updatedAt: it.updated_at,
        to: `${base}/issues/${encodeURIComponent(it.id)}`,
      });
    }
    for (const it of tasks.data?.items ?? []) {
      out.push({
        kind: 'task', id: it.id, title: it.title, orgRef: it.org_ref, updatedAt: it.updated_at,
        to: `${base}/tasks/${encodeURIComponent(it.id)}`,
      });
    }
    for (const it of plans.data?.items ?? []) {
      out.push({
        kind: 'plan', id: it.id, title: it.name || it.id, orgRef: it.org_ref, updatedAt: it.updated_at ?? it.created_at,
        to: `${base}/plans/${encodeURIComponent(it.id)}`,
      });
    }
    // Newest-updated first; stable for equal timestamps. Take the top 3 overall.
    out.sort((a, b) => (a.updatedAt < b.updatedAt ? 1 : a.updatedAt > b.updatedAt ? -1 : 0));
    return out.slice(0, 3);
  }, [issues.data, tasks.data, plans.data, base]);

  const isLoading = issues.isLoading || tasks.isLoading || plans.isLoading;

  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-activity-card"
    >
      <h2 className="mb-3 font-heading text-sm font-semibold text-text-primary">Recent activity</h2>
      {isLoading ? (
        <div className="space-y-2">
          <Skeleton height="1.25rem" />
          <Skeleton height="1.25rem" />
          <Skeleton height="1.25rem" />
        </div>
      ) : rows.length === 0 ? (
        <p className="py-2 text-xs text-text-muted" data-testid="project-activity-empty">
          No recent activity yet.
        </p>
      ) : (
        <ul className="space-y-1.5" data-testid="project-activity-list">
          {rows.map((r) => (
            <li key={`${r.kind}:${r.id}`} className="flex items-center gap-2 text-xs" data-testid="project-activity-row">
              <ActivityKindBadge kind={r.kind} />
              {r.orgRef && <span className="shrink-0 font-mono text-[0.625rem] text-text-muted">{r.orgRef}</span>}
              <OrgLink to={r.to} className="min-w-0 flex-1 truncate text-text-primary hover:text-accent" title={r.title}>
                {r.title}
              </OrgLink>
              <span className="shrink-0 tabular-nums text-[0.6875rem] text-text-muted" title={formatLocalTime(r.updatedAt)}>
                {shortDate(r.updatedAt)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

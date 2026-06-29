import React, { useState } from 'react';
import {
  useWorkspaceRepos,
  useDeleteWorkspaceRepo,
  useRepoBranches,
  useRepoCommits,
} from '@/api/repos';
import type { WorkspaceRepo } from '@/api/types';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { ConfirmModal } from '@/components/ConfirmModal';
import { RepoFormModal } from '@/components/RepoFormModal';
import { ProviderBadge } from '@/components/repoDisplay';
import { formatLocalTime } from '@/utils/time';

// OrgRepos (/repos) — the workspace-level code-repo registry (T575,
// issue-f980c8de). Repos are top-level workspace entities; projects only
// reference them (see the project "Referenced repositories" referencer).
// Credentials are configured ONLY here. Selecting a repo opens a read-only
// remote viewer (commits / branches), served live by BE-2 (no clone).
export default function OrgRepos(): React.ReactElement {
  const repos = useWorkspaceRepos();
  const del = useDeleteWorkspaceRepo();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<WorkspaceRepo | null>(null);
  const [deleting, setDeleting] = useState<WorkspaceRepo | null>(null);
  const [selected, setSelected] = useState<WorkspaceRepo | null>(null);

  const openAdd = () => {
    setEditing(null);
    setFormOpen(true);
  };
  const openEdit = (repo: WorkspaceRepo) => {
    setEditing(repo);
    setFormOpen(true);
  };

  const confirmDelete = async () => {
    if (!deleting) return;
    try {
      await del.mutateAsync(deleting.id);
      if (selected?.id === deleting.id) setSelected(null);
      setDeleting(null);
    } catch {
      // surfaced via del.error on the confirm modal staying open
    }
  };

  return (
    <section className="space-y-4" data-testid="page-OrgRepos">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">Repositories</h1>
          <p className="text-xs text-text-muted">
            Workspace-level code repos. Projects reference them; credentials are configured here only.
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={openAdd}
          data-testid="repos-add-btn"
        >
          + Add repo
        </button>
      </header>

      {repos.isLoading && (
        <div className="space-y-2" data-testid="repos-loading">
          <Skeleton height="4rem" />
          <Skeleton height="4rem" />
        </div>
      )}
      {repos.isError && (
        <p className="text-sm text-danger" data-testid="repos-error">
          {(repos.error as Error).message}
        </p>
      )}
      {repos.isSuccess && repos.data.length === 0 && (
        <EmptyState
          testId="repos-empty"
          title="No repositories yet"
          body="Add a workspace repository so projects can reference it. Click + Add repo to create one."
        />
      )}
      {repos.isSuccess && repos.data.length > 0 && (
        <ul className="space-y-2" data-testid="repos-list">
          {repos.data.map((repo) => (
            <RepoCard
              key={repo.id}
              repo={repo}
              selected={selected?.id === repo.id}
              onView={() => setSelected((s) => (s?.id === repo.id ? null : repo))}
              onEdit={() => openEdit(repo)}
              onDelete={() => setDeleting(repo)}
            />
          ))}
        </ul>
      )}

      {selected && <RemoteViewerPanel repo={selected} />}

      {formOpen && (
        <RepoFormModal repo={editing ?? undefined} onClose={() => setFormOpen(false)} />
      )}

      <ConfirmModal
        open={!!deleting}
        title="Delete repository?"
        message={
          deleting
            ? `Delete "${deleting.label}"? ${
                deleting.reference_count
                  ? `This will drop its reference from ${deleting.reference_count} project${deleting.reference_count === 1 ? '' : 's'} and `
                  : 'This will '
              }permanently remove its stored credential. This cannot be undone.`
            : ''
        }
        confirmLabel="Delete"
        busy={del.isPending}
        onConfirm={() => void confirmDelete()}
        onCancel={() => setDeleting(null)}
      />
    </section>
  );
}

function RepoCard({
  repo,
  selected,
  onView,
  onEdit,
  onDelete,
}: {
  repo: WorkspaceRepo;
  selected: boolean;
  onView: () => void;
  onEdit: () => void;
  onDelete: () => void;
}): React.ReactElement {
  return (
    <li
      className={`rounded-lg border bg-bg-elevated p-3 shadow-1 ${selected ? 'border-accent' : 'border-border-base'}`}
      data-testid="repo-card"
      data-repo-id={repo.id}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="mb-0.5 flex items-center gap-2">
            <ProviderBadge provider={repo.provider} />
            <span className="truncate font-semibold text-text-primary" data-testid="repo-card-label">
              {repo.label}
            </span>
          </div>
          {repo.description && (
            <p className="mb-0.5 text-xs text-text-secondary" data-testid="repo-card-description">
              {repo.description}
            </p>
          )}
          <p className="truncate font-mono text-xs text-text-muted" title={repo.url}>
            {repo.url}
          </p>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1">
          {repo.default_branch && (
            <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem] text-text-secondary">
              {repo.default_branch}
            </span>
          )}
          {typeof repo.reference_count === 'number' && (
            <span className="text-[0.625rem] text-text-muted" data-testid="repo-card-usedby">
              used by {repo.reference_count} project{repo.reference_count === 1 ? '' : 's'}
            </span>
          )}
        </div>
      </div>
      <div className="mt-2 flex justify-end gap-2">
        <button
          type="button"
          className="rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
          onClick={onView}
          data-testid="repo-card-view"
        >
          {selected ? 'Hide remote' : 'View remote'}
        </button>
        <button
          type="button"
          className="rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
          onClick={onEdit}
          data-testid="repo-card-edit"
        >
          Edit
        </button>
        <button
          type="button"
          className="rounded border border-danger px-2 py-0.5 text-xs text-danger hover:bg-bg-subtle"
          onClick={onDelete}
          data-testid="repo-card-delete"
        >
          Delete
        </button>
      </div>
    </li>
  );
}

// RemoteViewerPanel — read-only Commits / Branches for the selected repo, served
// live by BE-2 (go-github / git ls-remote, no clone). When BE-2 isn't wired the
// requests fail; we degrade to a friendly "unavailable" notice rather than error.
function RemoteViewerPanel({ repo }: { repo: WorkspaceRepo }): React.ReactElement {
  const [tab, setTab] = useState<'commits' | 'branches'>('commits');
  const [branch, setBranch] = useState(repo.default_branch || '');

  const branches = useRepoBranches(repo.id);
  const commits = useRepoCommits(repo.id, branch, tab === 'commits');

  return (
    <div className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1" data-testid="repo-remote-viewer">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-text-primary">
          Remote — <span className="font-mono">{repo.label}</span>
        </h2>
        <span className="inline-flex items-center gap-1 rounded bg-status-green-bg px-1.5 py-0.5 text-[0.625rem] font-semibold text-status-green-fg">
          <span className="h-1.5 w-1.5 rounded-full bg-status-green-fg" aria-hidden="true" /> live · remote
        </span>
      </div>

      <div className="mb-3 flex items-center justify-between border-b border-border-base">
        <div className="flex gap-1" role="tablist">
          <TabBtn id="commits" active={tab === 'commits'} onClick={() => setTab('commits')}>Commits</TabBtn>
          <TabBtn id="branches" active={tab === 'branches'} onClick={() => setTab('branches')}>Branches</TabBtn>
        </div>
        {tab === 'commits' && (
          <select
            className="mb-1 rounded border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            value={branch}
            onChange={(e) => setBranch(e.target.value)}
            data-testid="repo-remote-branch-select"
            aria-label="Branch"
          >
            {(branches.data?.branches ?? [{ name: branch || repo.default_branch || 'main' }]).map((b) => (
              <option key={b.name} value={b.name}>{b.name}</option>
            ))}
          </select>
        )}
      </div>

      {tab === 'commits' ? (
        <RemoteList
          query={commits}
          empty="No commits."
          render={(data) => (
            <ul className="space-y-2" data-testid="repo-remote-commits">
              {(data.commits ?? []).map((c) => (
                <li key={c.sha} className="border-b border-border-base pb-2 last:border-0">
                  <div className="flex items-baseline gap-2">
                    <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.625rem] text-accent">{c.sha.slice(0, 7)}</span>
                    <span className="text-sm text-text-primary">{c.message}</span>
                  </div>
                  <p className="mt-0.5 text-[0.6875rem] text-text-muted">
                    {c.author}{c.date ? ` · ${formatLocalTime(c.date)}` : ''}
                  </p>
                </li>
              ))}
            </ul>
          )}
        />
      ) : (
        <RemoteList
          query={branches}
          empty="No branches."
          render={(data) => (
            <ul className="flex flex-wrap gap-2" data-testid="repo-remote-branches">
              {(data.branches ?? []).map((b) => (
                <li
                  key={b.name}
                  className="inline-flex items-center gap-1 rounded border border-border-base bg-bg-subtle px-2 py-1 font-mono text-xs text-text-secondary"
                >
                  {b.name}
                  {b.is_default && <span className="text-[0.5625rem] uppercase text-text-muted">default</span>}
                </li>
              ))}
            </ul>
          )}
        />
      )}

      <p className="mt-3 text-[0.6875rem] text-text-muted">
        Live from the remote (go-github; falls back to git ls-remote). Never cloned.
      </p>
    </div>
  );
}

// RemoteList — shared loading / error(degraded) / empty / data states for the
// viewer's two tabs. BE-2 not wired ⇒ the request errors ⇒ "unavailable" notice.
function RemoteList<T extends { commits?: unknown[]; branches?: unknown[] }>({
  query,
  empty,
  render,
}: {
  query: { isLoading: boolean; isError: boolean; data?: T };
  empty: string;
  render: (data: T) => React.ReactElement;
}): React.ReactElement {
  if (query.isLoading) return <Skeleton height="3rem" />;
  if (query.isError || !query.data) {
    return (
      <p className="text-xs italic text-text-muted" data-testid="repo-remote-unavailable">
        Remote viewing isn't available right now.
      </p>
    );
  }
  const data = query.data;
  const len = (data.commits ?? data.branches ?? []).length;
  if (len === 0) return <p className="text-xs italic text-text-muted">{empty}</p>;
  return render(data);
}

function TabBtn({
  id,
  active,
  onClick,
  children,
}: {
  id: string;
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      data-testid={`repo-remote-tab-${id}`}
      className={`-mb-px border-b-2 px-3 py-1.5 text-sm font-medium ${
        active ? 'border-accent text-accent' : 'border-transparent text-text-muted hover:text-text-primary'
      }`}
    >
      {children}
    </button>
  );
}

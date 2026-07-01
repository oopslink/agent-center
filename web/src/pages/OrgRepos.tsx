import React, { useEffect, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
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
import { formatDayLabel, formatLocalTime, formatRelativeTime, localDayKey } from '@/utils/time';
import type { RepoCommit } from '@/api/types';

// OrgRepos (/repos) — the workspace-level code-repo registry (T575,
// issue-f980c8de). Repos are top-level workspace entities; projects only
// reference them (see the project "Referenced repositories" referencer).
// Credentials are configured ONLY here. Selecting a repo opens a read-only
// remote viewer (commits / branches), served live by BE-2 (no clone).
export default function OrgRepos(): React.ReactElement {
  const { t } = useTranslation('admin');
  const repos = useWorkspaceRepos();
  const del = useDeleteWorkspaceRepo();
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<WorkspaceRepo | null>(null);
  const [deleting, setDeleting] = useState<WorkspaceRepo | null>(null);
  const [selected, setSelected] = useState<WorkspaceRepo | null>(null);

  // Deep-link: /repos?repo=<id> (e.g. clicking a repo name in a project's
  // Referenced repositories list) auto-opens that repo's detail. Applied once
  // per param value so the user can still close it manually afterwards.
  const [searchParams] = useSearchParams();
  const repoParam = searchParams.get('repo');
  const appliedRepoParam = useRef<string | null>(null);
  useEffect(() => {
    if (!repoParam || !repos.isSuccess) return;
    if (appliedRepoParam.current === repoParam) return;
    const match = repos.data.find((r) => r.id === repoParam);
    if (!match) return;
    appliedRepoParam.current = repoParam;
    setSelected(match);
    const el = document.querySelector(
      `[data-testid="repos-list"] [data-repo-id="${repoParam}"]`,
    );
    el?.scrollIntoView?.({ behavior: 'smooth', block: 'center' });
  }, [repoParam, repos.isSuccess, repos.data]);

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
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('repos.title')}</h1>
          <p className="text-xs text-text-muted">
            {t('repos.subtitle')}
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={openAdd}
          data-testid="repos-add-btn"
        >
          {t('repos.addRepo')}
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
          title={t('repos.empty.title')}
          body={t('repos.empty.body')}
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
        title={t('repos.delete.title')}
        message={
          deleting
            ? deleting.reference_count
              ? t('repos.delete.messageRefs', {
                  label: deleting.label,
                  count: deleting.reference_count,
                })
              : t('repos.delete.message', { label: deleting.label })
            : ''
        }
        confirmLabel={t('repos.delete.confirm')}
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
  const { t } = useTranslation('admin');
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
              {t('repos.card.usedBy', { count: repo.reference_count })}
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
          {selected ? t('repos.card.hideRemote') : t('repos.card.viewRemote')}
        </button>
        <button
          type="button"
          className="rounded border border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
          onClick={onEdit}
          data-testid="repo-card-edit"
        >
          {t('repos.card.edit')}
        </button>
        <button
          type="button"
          className="rounded border border-danger px-2 py-0.5 text-xs text-danger hover:bg-bg-subtle"
          onClick={onDelete}
          data-testid="repo-card-delete"
        >
          {t('repos.card.delete')}
        </button>
      </div>
    </li>
  );
}

// RemoteViewerPanel — read-only Commits / Branches for the selected repo, served
// live by BE-2 (go-github / git ls-remote, no clone). When BE-2 isn't wired the
// requests fail; we degrade to a friendly "unavailable" notice rather than error.
function RemoteViewerPanel({ repo }: { repo: WorkspaceRepo }): React.ReactElement {
  const { t } = useTranslation('admin');
  const [tab, setTab] = useState<'commits' | 'branches'>('commits');
  const [branch, setBranch] = useState(repo.default_branch || '');

  const branches = useRepoBranches(repo.id);
  const commits = useRepoCommits(repo.id, branch, tab === 'commits');

  return (
    <div className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1" data-testid="repo-remote-viewer">
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-semibold text-text-primary">
          {t('repos.remote.heading')} <span className="font-mono">{repo.label}</span>
        </h2>
        <span className="inline-flex items-center gap-1 rounded bg-status-green-bg px-1.5 py-0.5 text-[0.625rem] font-semibold text-status-green-fg">
          <span className="h-1.5 w-1.5 rounded-full bg-status-green-fg" aria-hidden="true" /> {t('repos.remote.liveBadge')}
        </span>
      </div>

      <div className="mb-3 flex items-center justify-between border-b border-border-base">
        <div className="flex gap-1" role="tablist">
          <TabBtn id="commits" active={tab === 'commits'} onClick={() => setTab('commits')}>{t('repos.remote.tabs.commits')}</TabBtn>
          <TabBtn id="branches" active={tab === 'branches'} onClick={() => setTab('branches')}>{t('repos.remote.tabs.branches')}</TabBtn>
        </div>
        {tab === 'commits' && (
          <select
            className="mb-1 rounded border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            value={branch}
            onChange={(e) => setBranch(e.target.value)}
            data-testid="repo-remote-branch-select"
            aria-label={t('repos.remote.branchLabel')}
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
          empty={t('repos.remote.noCommits')}
          render={(data) => <CommitTimeline commits={data.commits ?? []} repo={repo} />}
        />
      ) : (
        <RemoteList
          query={branches}
          empty={t('repos.remote.noBranches')}
          render={(data) => (
            <ul className="flex flex-wrap gap-2" data-testid="repo-remote-branches">
              {(data.branches ?? []).map((b) => (
                <li
                  key={b.name}
                  className="inline-flex items-center gap-1 rounded border border-border-base bg-bg-subtle px-2 py-1 font-mono text-xs text-text-secondary"
                >
                  {b.name}
                  {b.is_default && <span className="text-[0.5625rem] uppercase text-text-muted">{t('repos.remote.defaultBranch')}</span>}
                </li>
              ))}
            </ul>
          )}
        />
      )}

      <p className="mt-3 text-[0.6875rem] text-text-muted">
        {t('repos.remote.footer')}
      </p>
    </div>
  );
}

// CommitTimeline — GitHub-style commit list (@oopslink mockup): commits grouped by
// LOCAL calendar day under a "Commits on {day}" header with a left timeline rail,
// each commit a bordered card (subject + optional body toggle, author line with a
// relative "committed … ago", and a right-aligned short SHA + copy + browse-code).
// Data note: the BE-2 /commits contract exposes only {sha,message,author,date}, so
// the mockup's "N people / committer / real avatar" can't be sourced yet — we show
// the single author + a generic avatar glyph rather than fabricate them.
function CommitTimeline({ commits, repo }: { commits: RepoCommit[]; repo: WorkspaceRepo }): React.ReactElement {
  const { t } = useTranslation('admin');
  // Group by local day, preserving the API's (newest-first) order both across and
  // within groups — a Map keeps first-seen key insertion order.
  const groups = new Map<string, RepoCommit[]>();
  for (const c of commits) {
    const key = localDayKey(c.date) || 'unknown';
    const bucket = groups.get(key);
    if (bucket) bucket.push(c);
    else groups.set(key, [c]);
  }

  return (
    <div className="flex flex-col" data-testid="repo-remote-commits">
      {[...groups.entries()].map(([key, items]) => (
        <section key={key} className="relative pl-6">
          {/* timeline rail + node */}
          <span className="absolute left-[5px] top-2 bottom-0 w-px bg-border-base" aria-hidden="true" />
          <h3 className="relative mb-2 mt-1 flex items-center text-xs font-medium text-text-muted">
            <span className="absolute -left-[1.4rem] grid h-[11px] w-[11px] place-items-center rounded-full border-2 border-border-strong bg-bg-elevated" aria-hidden="true">
              <span className="h-1 w-1 rounded-full bg-text-muted" />
            </span>
            {t('repos.commits.dayHeader', { day: items[0]?.date ? formatDayLabel(items[0].date) : t('repos.commits.unknownDate') })}
          </h3>
          <ul className="mb-3 divide-y divide-border-base overflow-hidden rounded-lg border border-border-base bg-bg-elevated">
            {items.map((c) => (
              <CommitRow key={c.sha} commit={c} repo={repo} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

// CommitRow — one commit card. Splits the message into a SUBJECT (first line) and an
// optional BODY (the rest); the body collapses behind a "…" toggle (GitHub idiom).
function CommitRow({ commit, repo }: { commit: RepoCommit; repo: WorkspaceRepo }): React.ReactElement {
  const { t } = useTranslation('admin');
  const [open, setOpen] = useState(false);
  const nl = commit.message.indexOf('\n');
  const subject = (nl === -1 ? commit.message : commit.message.slice(0, nl)).trim();
  const body = nl === -1 ? '' : commit.message.slice(nl + 1).trim();
  const short = commit.sha.slice(0, 7);
  const href = commitUrl(repo, commit.sha);

  return (
    <li className="flex items-start gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex items-start gap-2">
          <span className="break-words text-sm font-semibold leading-snug text-text-primary">{subject}</span>
          {body && (
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              aria-label={open ? t('repos.commit.hideDescriptionAria') : t('repos.commit.showDescriptionAria')}
              title={open ? t('repos.commit.hideDescription') : t('repos.commit.showDescription')}
              data-testid="repo-commit-body-toggle"
              className={`mt-0.5 shrink-0 rounded px-1.5 leading-none text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent ${open ? 'bg-bg-subtle text-text-primary' : 'bg-bg-subtle'}`}
            >
              <span className="text-sm font-bold tracking-tight" aria-hidden="true">…</span>
            </button>
          )}
        </div>
        {body && open && (
          <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap rounded border border-border-base bg-bg-subtle px-3 py-2 font-mono text-xs text-text-secondary" data-testid="repo-commit-body">
            {body}
          </pre>
        )}
        <p className="mt-1 flex items-center gap-1.5 text-xs text-text-muted">
          <AvatarGlyph />
          <span className="font-medium text-text-secondary">{commit.author || t('repos.commit.unknownAuthor')}</span>
          {commit.date && (
            <span title={formatLocalTime(commit.date)}>{t('repos.commit.committed', { time: formatRelativeTime(commit.date) })}</span>
          )}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <span className="font-mono text-xs text-text-muted">{short}</span>
        <CopyShaButton sha={commit.sha} />
        {href && (
          <a
            href={href}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={t('repos.commit.browseAria', { short })}
            title={t('repos.commit.browseTitle')}
            data-testid="repo-commit-browse"
            className="rounded p-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent"
          >
            <CodeIcon />
          </a>
        )}
      </div>
    </li>
  );
}

// CopyShaButton — copy the FULL sha (not the short form) with a brief "copied" swap,
// mirroring the MessageCopyButton idiom (icon-only chrome; name on aria-label/title).
function CopyShaButton({ sha }: { sha: string }): React.ReactElement {
  const { t } = useTranslation('admin');
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard?.writeText(sha);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 2000);
  };
  return (
    <button
      type="button"
      onClick={copy}
      aria-label={t('repos.commit.copyAria')}
      title={copied ? t('repos.commit.copied') : t('repos.commit.copyTitle')}
      data-testid="repo-commit-copy"
      className="rounded p-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent motion-safe:transition-colors"
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </button>
  );
}

// commitUrl — best-effort browse link to a commit on the remote host. We only have
// the repo's clone url; normalise it (drop a trailing ".git" / user@host scp form)
// and append the GitHub/GitLab "/commit/<sha>" path. Returns '' for non-http remotes
// so the caller omits the affordance rather than emit a broken link.
function commitUrl(repo: WorkspaceRepo, sha: string): string {
  const raw = (repo.url || '').trim();
  if (!/^https?:\/\//i.test(raw)) return '';
  const base = raw.replace(/\.git$/i, '').replace(/\/+$/, '');
  return `${base}/commit/${sha}`;
}

function AvatarGlyph(): React.ReactElement {
  return (
    <span className="grid h-4 w-4 shrink-0 place-items-center rounded-full bg-bg-subtle text-text-muted" aria-hidden="true">
      <svg viewBox="0 0 20 20" fill="currentColor" className="h-3 w-3">
        <path d="M10 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm0 1.5c-2.7 0-5 1.4-5 3.2V16h10v-1.3c0-1.8-2.3-3.2-5-3.2Z" />
      </svg>
    </span>
  );
}

function CodeIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M7 6 3.5 10 7 14M13 6l3.5 4-3.5 4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function CopyIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="7" y="7" width="9" height="9" rx="1.5" strokeLinejoin="round" />
      <path d="M13 4.5H5.5A1.5 1.5 0 0 0 4 6v7.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function CheckIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="2" aria-hidden="true">
      <path d="M4.5 10.5l3.5 3.5 7.5-8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
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
  const { t } = useTranslation('admin');
  if (query.isLoading) return <Skeleton height="3rem" />;
  if (query.isError || !query.data) {
    return (
      <p className="text-xs italic text-text-muted" data-testid="repo-remote-unavailable">
        {t('repos.remote.unavailable')}
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

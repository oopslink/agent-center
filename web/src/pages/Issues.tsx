import type React from 'react';
import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useIssues } from '@/api/issues';
import { useProjects } from '@/api/projects';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import type { IssueStatus } from '@/api/types';

// Issues page (/issues). Lists Discussion BC Issues with a status
// filter row + a project filter chip row.
//
// v2.3-5b cutover (per § 0.6): this page now reads from the Discussion
// BC-native `GET /api/issues?project_id=...` endpoint (was previously
// `useConversations({kind:'issue'})`, a cross-BC reach). The project
// chip filter is now REAL — the backend rejects requests without a
// project_id, so the page is gated on "pick a project" and shows a
// nudge in the all-projects state. Status enum here is Discussion BC's
// 6-value Issue.Status (different from ConversationStatus).
const STATUS_TABS: Array<{ label: string; value: IssueStatus | 'all' }> = [
  { label: 'All', value: 'all' },
  { label: 'Open', value: 'open' },
  { label: 'Discussing', value: 'under_discussion' },
  { label: 'Concluded', value: 'concluded' },
  { label: 'Closed (tasks)', value: 'closed_with_tasks' },
  { label: 'Closed (no-op)', value: 'closed_no_action' },
  { label: 'Withdrawn', value: 'withdrawn' },
];

export default function Issues(): React.ReactElement {
  const [filter, setFilter] = useState<IssueStatus | 'all'>('all');
  const [searchParams, setSearchParams] = useSearchParams();
  const projectFilter = searchParams.get('project') ?? 'all';
  const projects = useProjects();
  // Status filter is server-side now (backend accepts optional `status`
  // query param mapped to discussion.IssueFilter.Status).
  const issues = useIssues({
    projectId: projectFilter === 'all' ? undefined : projectFilter,
    status: filter === 'all' ? undefined : filter,
  });
  const data = issues.data ?? [];

  const setProject = (id: string) => {
    const next = new URLSearchParams(searchParams);
    if (id === 'all') next.delete('project');
    else next.set('project', id);
    setSearchParams(next, { replace: true });
  };

  return (
    <section className="space-y-4" data-testid="page-Issues">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Issues</h2>
      </header>

      <div
        className="flex flex-wrap items-center gap-1"
        role="tablist"
        aria-label="project filter"
        data-testid="issues-project-filter"
      >
        <span className="mr-1 text-[0.6875rem] uppercase tracking-wider text-text-muted">
          Project:
        </span>
        <ProjectChip
          label="All"
          value="all"
          selected={projectFilter === 'all'}
          onClick={() => setProject('all')}
        />
        {(projects.data ?? []).map((p) => (
          <ProjectChip
            key={p.id}
            label={p.name}
            value={p.id}
            selected={projectFilter === p.id}
            onClick={() => setProject(p.id)}
          />
        ))}
      </div>

      <div className="flex flex-wrap gap-1" role="tablist" aria-label="status filter">
        {STATUS_TABS.map((t) => (
          <button
            key={t.value}
            type="button"
            role="tab"
            aria-selected={filter === t.value}
            onClick={() => setFilter(t.value)}
            className={[
              'rounded px-3 py-1 text-xs uppercase tracking-wide',
              filter === t.value
                ? 'bg-text-primary text-bg-elevated'
                : 'bg-bg-subtle text-text-secondary hover:bg-border-base',
            ].join(' ')}
            data-testid="issues-status-tab"
            data-status={t.value}
          >
            {t.label}
          </button>
        ))}
      </div>

      {projectFilter === 'all' && (
        <EmptyState
          testId="issues-pick-project"
          title="Pick a project"
          body="Issues live inside a project — choose one from the chip row above to see its issues."
        />
      )}
      {projectFilter !== 'all' && issues.isLoading && (
        <div className="space-y-2" data-testid="issues-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {projectFilter !== 'all' && issues.isError && (
        <p className="text-sm text-danger" data-testid="issues-error">
          {(issues.error as Error).message}
        </p>
      )}
      {projectFilter !== 'all' && issues.isSuccess && data.length === 0 && (
        <EmptyState
          testId="issues-empty"
          title={filter === 'all' ? 'No issues yet' : `No ${filter.replace(/_/g, ' ')} issues`}
          body={
            filter === 'all'
              ? 'Issues capture decisions or problems that need resolution. Open one from a conversation via the Derive menu.'
              : 'Switch the filter above or open a new issue from a conversation.'
          }
        />
      )}
      {projectFilter !== 'all' && data.length > 0 && (
        <ul className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-text-primary">
          {data.map((iss) => (
            <li key={iss.id} data-testid="issue-row" data-issue-id={iss.id}>
              <Link
                to={`/issues/${encodeURIComponent(iss.id)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-bg-subtle"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{iss.title || iss.id}</span>
                  <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
                    {iss.status.replace(/_/g, ' ')}
                  </span>
                </span>
                <span className="flex items-center gap-3 text-xs text-text-muted">
                  <span className="font-mono">{iss.opener}</span>
                  <span>{formatRelative(iss.opened_at)}</span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function ProjectChip({
  label,
  value,
  selected,
  onClick,
}: {
  label: string;
  value: string;
  selected: boolean;
  onClick: () => void;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      onClick={onClick}
      data-testid="issues-project-chip"
      data-project={value}
      className={[
        'rounded-full px-3 py-0.5 text-xs',
        selected
          ? 'bg-brand text-white'
          : 'bg-bg-subtle text-text-secondary hover:bg-border-base',
      ].join(' ')}
    >
      {label}
    </button>
  );
}

// Tiny relative-time helper (mirrors Home.tsx — kept inline so this
// page has zero extra deps).
function formatRelative(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const delta = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (delta < 60) return `${delta}s ago`;
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
  return `${Math.floor(delta / 86400)}d ago`;
}

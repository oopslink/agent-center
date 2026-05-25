import type React from 'react';
import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import { useProjects } from '@/api/projects';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import type { ConversationStatus } from '@/api/types';

// Issues page (/issues). Lists kind=issue conversations with a status
// filter chip row + a project filter chip row.
//
// PROJECT FILTER: the Conversation projection does NOT carry a
// project_id field today — the Issue/Task AR holds the project link in
// its own BC. The chip row is rendered for UX continuity with the
// v2.3-4 project surface and writes the selection back to the URL
// (?project=…). Filtering is COSMETIC for now — the row visually
// scopes the page without altering the result set. Wiring the actual
// filter requires projecting project_id onto the conversation read
// model (follow-up pass).
const STATUS_TABS: Array<{ label: string; value: ConversationStatus | 'all' }> = [
  { label: 'All', value: 'all' },
  { label: 'Active', value: 'active' },
  { label: 'Closed', value: 'closed' },
  { label: 'Archived', value: 'archived' },
];

export default function Issues(): React.ReactElement {
  const [filter, setFilter] = useState<ConversationStatus | 'all'>('all');
  const [searchParams, setSearchParams] = useSearchParams();
  const projectFilter = searchParams.get('project') ?? 'all';
  const projects = useProjects();
  const all = useConversations({ kind: 'issue' });
  // Status filtering is real; project filtering is cosmetic (see
  // module docstring) — we still slice by status so the UI behaves.
  const data = (all.data ?? []).filter((c) => filter === 'all' || c.status === filter);

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

      <div className="flex gap-1" role="tablist" aria-label="status filter">
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
                ? 'bg-slate-900 text-white'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200',
            ].join(' ')}
            data-testid="issues-status-tab"
            data-status={t.value}
          >
            {t.label}
          </button>
        ))}
      </div>

      {all.isLoading && (
        <div className="space-y-2" data-testid="issues-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {all.isError && (
        <p className="text-sm text-danger" data-testid="issues-error">
          {(all.error as Error).message}
        </p>
      )}
      {all.isSuccess && data.length === 0 && (
        <EmptyState
          testId="issues-empty"
          title={filter === 'all' ? 'No issues yet' : `No ${filter} issues`}
          body={
            filter === 'all'
              ? 'Issues capture decisions or problems that need resolution. Open one from a conversation via the Derive menu.'
              : 'Switch the filter above or open a new issue from a conversation.'
          }
        />
      )}
      {data.length > 0 && (
        <ul className="divide-y divide-slate-200 rounded border border-slate-200 bg-white">
          {data.map((c) => (
            <li key={c.id} data-testid="issue-row" data-issue-id={c.id}>
              <Link
                to={`/issues/${encodeURIComponent(c.id)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-slate-50"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{c.name || c.id}</span>
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs uppercase text-slate-600">
                    {c.status}
                  </span>
                </span>
                <span className="max-w-[40ch] truncate text-xs text-slate-500">
                  {c.description}
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
          : 'bg-bg-subtle text-text-secondary hover:bg-bg-elevated',
      ].join(' ')}
    >
      {label}
    </button>
  );
}

import type React from 'react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import type { ConversationStatus } from '@/api/types';

// Issues page (/issues). Lists kind=issue conversations with a status
// filter chip row.
const STATUS_TABS: Array<{ label: string; value: ConversationStatus | 'all' }> = [
  { label: 'All', value: 'all' },
  { label: 'Active', value: 'active' },
  { label: 'Closed', value: 'closed' },
  { label: 'Archived', value: 'archived' },
];

export default function Issues(): React.ReactElement {
  const [filter, setFilter] = useState<ConversationStatus | 'all'>('all');
  const all = useConversations({ kind: 'issue' });
  const data = (all.data ?? []).filter((c) => filter === 'all' || c.status === filter);

  return (
    <section className="space-y-4" data-testid="page-Issues">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Issues</h2>
      </header>

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
        <p className="text-sm text-slate-500" data-testid="issues-loading">
          Loading…
        </p>
      )}
      {all.isError && (
        <p className="text-sm text-red-600" data-testid="issues-error">
          {(all.error as Error).message}
        </p>
      )}
      {all.isSuccess && data.length === 0 && (
        <p
          className="rounded border border-dashed border-slate-300 bg-white p-6 text-center text-sm text-slate-500"
          data-testid="issues-empty"
        >
          No issues for this filter.
        </p>
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

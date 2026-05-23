import type React from 'react';
import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import { DMStartModal } from '@/components/DMStartModal';

// DMList page (/dms). Lists kind=dm conversations + "Start a DM" button.
//
// Unread badges are intentionally noop here — the backend doesn't track
// per-user read-state yet (see docs/plans/v2.1-backlog.md "Unread
// message tracking"). When that lands, surface it inside the row.
export default function DMs(): React.ReactElement {
  const dms = useConversations({ kind: 'dm' });
  const [startOpen, setStartOpen] = useState(false);
  const navigate = useNavigate();

  return (
    <section className="space-y-4" data-testid="page-DMs">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">DMs</h2>
        <button
          type="button"
          className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800"
          onClick={() => setStartOpen(true)}
          data-testid="dms-new-button"
        >
          Start a DM
        </button>
      </header>

      {dms.isLoading && (
        <p className="text-sm text-slate-500" data-testid="dms-loading">
          Loading…
        </p>
      )}
      {dms.isError && (
        <p className="text-sm text-red-600" data-testid="dms-error">
          {(dms.error as Error).message}
        </p>
      )}
      {dms.isSuccess && dms.data.length === 0 && (
        <div
          className="rounded border border-dashed border-slate-300 bg-white p-6 text-center text-sm text-slate-500"
          data-testid="dms-empty"
        >
          No DMs yet.{' '}
          <button
            type="button"
            className="font-medium text-blue-600 hover:underline"
            onClick={() => setStartOpen(true)}
          >
            Start one
          </button>
          .
        </div>
      )}
      {dms.isSuccess && dms.data.length > 0 && (
        <ul className="divide-y divide-slate-200 rounded border border-slate-200 bg-white">
          {dms.data.map((c) => (
            <li key={c.id} data-testid="dm-row" data-dm-id={c.id}>
              <Link
                to={`/dms/${encodeURIComponent(c.id)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-slate-50"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{c.name || c.id}</span>
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs uppercase text-slate-600">
                    {c.status}
                  </span>
                </span>
                {/*
                 * Unread badge — v2.1-backlog "Unread message tracking".
                 * Backend has no per-user read-state model yet; render
                 * blank for now.
                 */}
                <span className="text-xs text-slate-500"></span>
              </Link>
            </li>
          ))}
        </ul>
      )}

      <DMStartModal
        open={startOpen}
        onClose={() => setStartOpen(false)}
        onCreated={(id) => navigate(`/dms/${encodeURIComponent(id)}`)}
      />
    </section>
  );
}

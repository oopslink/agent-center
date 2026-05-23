import type React from 'react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import { ChannelCreateModal } from '@/components/ChannelCreateModal';

// ChannelList page (/channels). Lists kind=channel conversations + a
// "New channel" button. Empty state offers the same button inline.
export default function Channels(): React.ReactElement {
  const channels = useConversations({ kind: 'channel' });
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <section className="space-y-4" data-testid="page-Channels">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Channels</h2>
        <button
          type="button"
          className="rounded bg-slate-900 px-3 py-1.5 text-sm font-medium text-white hover:bg-slate-800"
          onClick={() => setCreateOpen(true)}
          data-testid="channels-new-button"
        >
          New channel
        </button>
      </header>

      {channels.isLoading && (
        <p className="text-sm text-slate-500" data-testid="channels-loading">
          Loading…
        </p>
      )}
      {channels.isError && (
        <p className="text-sm text-red-600" data-testid="channels-error">
          {(channels.error as Error).message}
        </p>
      )}
      {channels.isSuccess && channels.data.length === 0 && (
        <div
          className="rounded border border-dashed border-slate-300 bg-white p-6 text-center text-sm text-slate-500"
          data-testid="channels-empty"
        >
          No channels yet.{' '}
          <button
            type="button"
            className="font-medium text-blue-600 hover:underline"
            onClick={() => setCreateOpen(true)}
          >
            Create one
          </button>
          .
        </div>
      )}
      {channels.isSuccess && channels.data.length > 0 && (
        <ul className="divide-y divide-slate-200 rounded border border-slate-200 bg-white">
          {channels.data.map((c) => (
            <li key={c.id} data-testid="channel-row" data-channel-name={c.name}>
              <Link
                to={`/channels/${encodeURIComponent(c.name)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-slate-50"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{c.name}</span>
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

      <ChannelCreateModal open={createOpen} onClose={() => setCreateOpen(false)} />
    </section>
  );
}

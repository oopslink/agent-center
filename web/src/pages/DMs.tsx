import type React from 'react';
import { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import { DMStartModal } from '@/components/DMStartModal';
import { UnreadBadge } from '@/components/UnreadBadge';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';

// DMList page (/dms). Lists kind=dm conversations + "Start a DM" button.
export default function DMs(): React.ReactElement {
  const dms = useConversations({ kind: 'dm' });
  const [startOpen, setStartOpen] = useState(false);
  const navigate = useNavigate();
  useSSEConversationSubscribe(dms.data?.map((c) => c.id));

  return (
    <section className="space-y-4" data-testid="page-DMs">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">DMs</h2>
        <button
          type="button"
          className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90"
          onClick={() => setStartOpen(true)}
          data-testid="dms-new-button"
        >
          Start a DM
        </button>
      </header>

      {dms.isLoading && (
        <div className="space-y-2" data-testid="dms-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {dms.isError && (
        <p className="text-sm text-danger" data-testid="dms-error">
          {(dms.error as Error).message}
        </p>
      )}
      {dms.isSuccess && dms.data.length === 0 && (
        <EmptyState
          testId="dms-empty"
          title="No DMs yet"
          body="DMs are private conversations between two parties (human or agent). Start one to message someone directly."
          action={{ label: 'Start a DM', onClick: () => setStartOpen(true) }}
        />
      )}
      {dms.isSuccess && dms.data.length > 0 && (
        <ul className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-text-primary">
          {dms.data.map((c) => (
            <li key={c.id} data-testid="dm-row" data-dm-id={c.id}>
              <Link
                to={`/dms/${encodeURIComponent(c.id)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-bg-subtle"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{c.name || c.id}</span>
                  <UnreadBadge conversationId={c.id} />
                  <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
                    {c.status}
                  </span>
                </span>
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

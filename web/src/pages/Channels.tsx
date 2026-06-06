import type React from 'react';
import { OrgLink, useOptionalOrgContext } from '@/OrgContext';
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';

import { useConversations } from '@/api/conversations';
import { ChannelCreateModal } from '@/components/ChannelCreateModal';
import { UnreadBadge } from '@/components/UnreadBadge';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';

// ChannelList page (/channels). Lists kind=channel conversations + a
// "New channel" button. Empty state offers the same button inline.
export default function Channels(): React.ReactElement {
  const channels = useConversations({ kind: 'channel' });
  const [createOpen, setCreateOpen] = useState(false);
  // v2.7.1 #247: after create, navigate to the new channel by id.
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  // Subscribe to every visible channel so badge auto-ticks via SSE.
  useSSEConversationSubscribe(channels.data?.map((c) => c.id));

  return (
    <section className="space-y-4" data-testid="page-Channels">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Channels</h2>
        <button
          type="button"
          className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90"
          onClick={() => setCreateOpen(true)}
          data-testid="channels-new-button"
        >
          New channel
        </button>
      </header>

      {channels.isLoading && (
        <div className="space-y-2" data-testid="channels-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {channels.isError && (
        <p className="text-sm text-danger" data-testid="channels-error">
          {(channels.error as Error).message}
        </p>
      )}
      {channels.isSuccess && channels.data.length === 0 && (
        <EmptyState
          testId="channels-empty"
          title="No channels yet"
          body="Channels group humans + agents around a topic. Create one to start a conversation that anyone in this server can join."
          action={{ label: 'New channel', onClick: () => setCreateOpen(true) }}
        />
      )}
      {channels.isSuccess && channels.data.length > 0 && (
        <ul className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-text-primary">
          {channels.data.map((c) => (
            <li key={c.id} data-testid="channel-row" data-channel-name={c.name} data-channel-id={c.id}>
              <OrgLink
                to={`/channels/${encodeURIComponent(c.id)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-bg-subtle"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{c.name}</span>
                  <UnreadBadge unreadCount={c.unread_count} mentionCount={c.mention_count} />
                  <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
                    {c.status}
                  </span>
                </span>
                <span className="max-w-[40ch] truncate text-xs text-text-muted">
                  {c.description}
                </span>
              </OrgLink>
            </li>
          ))}
        </ul>
      )}

      <ChannelCreateModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(id) => navigate(`${org ? `/organizations/${org.slug}` : ''}/channels/${encodeURIComponent(id)}`)}
      />
    </section>
  );
}

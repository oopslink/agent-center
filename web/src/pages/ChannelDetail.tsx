import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useConversation, useMessages } from '@/api/conversations';
import { useMarkSeen } from '@/api/readState';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { MessageList } from '@/components/MessageList';
import { TypeChip } from '@/components/TypeChip';
import { MessageComposer } from '@/components/MessageComposer';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';
import { Breadcrumb } from '@/components/Breadcrumb';

// ChannelDetail page (/channels/:channelId). v2.7.1 #247: the URL carries the
// channel's hash id (conversation_id), consistent with project/task/issue URLs
// — no more by-name lookup. The detail GET (by id) provides name/description/
// participants. The channel NAME stays the visible chrome (header/breadcrumb);
// the hash id is only the URL segment (#195 name-uniqueness unaffected).
export default function ChannelDetail(): React.ReactElement {
  const { channelId = '' } = useParams<{ channelId: string }>();
  const conv = useConversation(channelId);
  const messages = useMessages(channelId);
  const markSeen = useMarkSeen();
  useSSEConversationSubscribe(channelId ? [channelId] : undefined);

  // Fire-and-forget: bump read cursor to the latest message whenever a
  // new message list arrives (mount + SSE-driven refetch). Server-side
  // only-forward guard makes redundant POSTs cheap (no event, no row
  // write past the conditional UPSERT early-return).
  const latestMessageId = messages.data?.[messages.data.length - 1]?.id;
  useEffect(() => {
    if (!channelId || !latestMessageId) return;
    markSeen.mutate({ conversationId: channelId, lastSeenMessageId: latestMessageId });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channelId, latestMessageId]);

  if (conv.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-ChannelDetail">
        Loading channel…
      </section>
    );
  }

  if (conv.isError) {
    return (
      <section className="space-y-3 text-sm text-text-muted" data-testid="page-ChannelDetail">
        <p data-testid="channel-not-found">{(conv.error as Error).message}</p>
        <OrgLink to="/channels" className="text-accent hover:underline">
          Back to channels
        </OrgLink>
      </section>
    );
  }

  if (!conv.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-ChannelDetail">
        Channel lookup failed.
      </section>
    );
  }

  const ch = conv.data;
  const participants = ch.participants ?? [];
  const activeCount = participants.filter((p) => !p.left_at).length;

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-ChannelDetail"
      data-channel-id={ch.id}
    >
      <div className="mb-2">
        {/* #192/#247: leaf shows the channel NAME (chrome), URL carries the id. */}
        <Breadcrumb items={[{ label: 'Channels', to: '/channels' }, { label: ch.name }]} />
      </div>
      <header className="flex items-center justify-between border-b border-border-base pb-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-xl font-semibold">{ch.name}</h2>
            <TypeChip kind="channel" />
          </div>
          {ch.description && (
            <p className="text-sm text-text-muted">{ch.description}</p>
          )}
        </div>
        <div className="flex items-center gap-3">
          <span className="text-xs text-text-muted">
            {activeCount} {activeCount === 1 ? 'participant' : 'participants'}
          </span>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden">
          {messages.isLoading && (
            <p className="p-4 text-sm text-text-muted" data-testid="messages-loading">
              Loading messages…
            </p>
          )}
          {messages.isError && (
            <p className="p-4 text-sm text-danger" data-testid="messages-error">
              {(messages.error as Error).message}
            </p>
          )}
          {messages.isSuccess && <MessageList messages={messages.data} />}
          <MessageComposer conversationId={ch.id} />
        </div>
        <ParticipantsPanel conversationId={ch.id} participants={participants} />
      </div>
    </section>
  );
}

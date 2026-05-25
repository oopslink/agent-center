import type React from 'react';
import { useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  useConversation,
  useConversations,
  useMessages,
} from '@/api/conversations';
import { useMarkSeen } from '@/api/readState';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';
import { ConversationDeriveControls } from '@/components/ConversationDeriveControls';
import { useSelection } from '@/components/useSelection';

// ChannelDetail page (/channels/:name).
//
// Backend exposes show by ID, not by name. We hit the channels list
// (small + cached) and find the channel by name; this gives us the ID
// for the participants + messages queries. SSE invalidation in
// dispatchToQueryClient already targets these query keys, so live
// updates work without any extra wiring here.
export default function ChannelDetail(): React.ReactElement {
  const { name = '' } = useParams<{ name: string }>();
  const channels = useConversations({ kind: 'channel' });
  const channel = channels.data?.find((c) => c.name === name);
  const conv = useConversation(channel?.id);
  const messages = useMessages(channel?.id);
  const selection = useSelection();
  const markSeen = useMarkSeen();
  useSSEConversationSubscribe(channel?.id ? [channel.id] : undefined);

  // Fire-and-forget: bump read cursor to the latest message whenever a
  // new message list arrives (mount + SSE-driven refetch). Server-side
  // only-forward guard makes redundant POSTs cheap (no event, no row
  // write past the conditional UPSERT early-return).
  const latestMessageId = messages.data?.[messages.data.length - 1]?.id;
  useEffect(() => {
    if (!channel?.id || !latestMessageId) return;
    markSeen.mutate({ conversationId: channel.id, lastSeenMessageId: latestMessageId });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [channel?.id, latestMessageId]);

  if (channels.isLoading || (channel && conv.isLoading)) {
    return (
      <section className="text-sm text-slate-500" data-testid="page-ChannelDetail">
        Loading channel…
      </section>
    );
  }

  if (channels.isSuccess && !channel) {
    return (
      <section
        className="space-y-3 text-sm text-slate-500"
        data-testid="page-ChannelDetail"
      >
        <p data-testid="channel-not-found">
          Channel <span className="font-mono">{name}</span> not found.
        </p>
        <Link to="/channels" className="text-blue-600 hover:underline">
          Back to channels
        </Link>
      </section>
    );
  }

  if (!channel || !conv.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-ChannelDetail">
        Channel lookup failed.
      </section>
    );
  }

  const participants = conv.data.participants ?? [];
  const activeCount = participants.filter((p) => !p.left_at).length;

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-ChannelDetail"
      data-channel-id={channel.id}
    >
      <header className="flex items-center justify-between border-b border-slate-200 pb-3">
        <div>
          <h2 className="text-xl font-semibold">{channel.name}</h2>
          {channel.description && (
            <p className="text-sm text-slate-500">{channel.description}</p>
          )}
        </div>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={selection.toggleSelectMode}
            className={[
              'rounded px-2.5 py-1 text-xs font-medium',
              selection.selectMode
                ? 'bg-slate-900 text-white'
                : 'bg-slate-100 text-slate-700 hover:bg-slate-200',
            ].join(' ')}
            data-testid="select-mode-toggle"
            aria-pressed={selection.selectMode}
          >
            {selection.selectMode ? 'Cancel select' : 'Select messages'}
          </button>
          <span className="text-xs text-slate-500">
            {activeCount} {activeCount === 1 ? 'participant' : 'participants'}
          </span>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden">
          {messages.isLoading && (
            <p className="p-4 text-sm text-slate-500" data-testid="messages-loading">
              Loading messages…
            </p>
          )}
          {messages.isError && (
            <p className="p-4 text-sm text-danger" data-testid="messages-error">
              {(messages.error as Error).message}
            </p>
          )}
          {messages.isSuccess && (
            <MessageList
              messages={messages.data}
              selectable={selection.selectMode}
              isSelected={selection.isSelected}
              onToggle={selection.toggle}
            />
          )}
          <ConversationDeriveControls
            conversationId={channel.id}
            selection={selection}
          />
          <MessageComposer conversationId={channel.id} />
        </div>
        <ParticipantsPanel conversationId={channel.id} participants={participants} />
      </div>
    </section>
  );
}

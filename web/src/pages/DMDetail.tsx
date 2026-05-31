import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useConversation, useMessages } from '@/api/conversations';
import { useMarkSeen } from '@/api/readState';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { useAppStore } from '@/store/app';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';

// DMDetail page (/dms/:id). Mirrors ChannelDetail layout but skips the
// ParticipantsPanel — DM membership is fixed at create time (per
// ADR-0032 § 6) and not mutable from the UI.
//
// SSE live updates flow through F5's dispatchToQueryClient, same as
// channels — `conversation.message_added` invalidates the messages
// query.
export default function DMDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const me = useAppStore((s) => s.currentUserId);
  const conv = useConversation(id);
  const messages = useMessages(id);
  const markSeen = useMarkSeen();
  useSSEConversationSubscribe(id ? [id] : undefined);

  // See ChannelDetail for rationale: fire-and-forget auto-mark-seen
  // bumps the cursor whenever a fresh message list lands (mount or SSE
  // refetch). Server-side only-forward guard keeps this cheap.
  const latestMessageId = messages.data?.[messages.data.length - 1]?.id;
  useEffect(() => {
    if (!id || !latestMessageId) return;
    markSeen.mutate({ conversationId: id, lastSeenMessageId: latestMessageId });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, latestMessageId]);

  if (conv.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-DMDetail">
        Loading DM…
      </section>
    );
  }
  if (conv.isError) {
    return (
      <section className="space-y-3 text-sm" data-testid="page-DMDetail">
        <p className="text-danger" data-testid="dm-not-found">
          {(conv.error as Error).message}
        </p>
        <OrgLink to="/dms" className="text-accent hover:underline">
          Back to DMs
        </OrgLink>
      </section>
    );
  }
  if (!conv.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-DMDetail">
        DM lookup failed.
      </section>
    );
  }

  // Peer label = active participants other than the current user, joined
  // by " · ". For group DMs this lists everyone.
  const peers = (conv.data.participants ?? [])
    .filter((p) => !p.left_at && p.identity_id !== me)
    .map((p) => p.identity_id);
  const heading = conv.data.name || peers.join(' · ') || conv.data.id;

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-DMDetail"
      data-dm-id={conv.data.id}
    >
      <header className="flex items-center justify-between border-b border-border-base pb-3">
        <div>
          <h2 className="text-xl font-semibold" data-testid="dm-heading">
            {heading}
          </h2>
          <p className="text-xs text-text-muted">
            {peers.length === 0
              ? 'You — solo DM'
              : `with ${peers.length} ${peers.length === 1 ? 'peer' : 'peers'}`}
          </p>
        </div>
      </header>

      <div className="flex flex-1 flex-col overflow-hidden">
        {messages.isLoading && (
          <p className="p-4 text-sm text-text-muted" data-testid="dm-messages-loading">
            Loading messages…
          </p>
        )}
        {messages.isError && (
          <p className="p-4 text-sm text-danger" data-testid="dm-messages-error">
            {(messages.error as Error).message}
          </p>
        )}
        {messages.isSuccess && <MessageList messages={messages.data} />}
        <MessageComposer conversationId={conv.data.id} />
      </div>
    </section>
  );
}

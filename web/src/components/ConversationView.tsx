import type React from 'react';
import { useEffect } from 'react';
import { useConversationTimeline } from '@/api/conversations';
import { useMarkSeen } from '@/api/readState';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ThreadSidebarProvider } from '@/components/ThreadSidebarContext';

// v2.8 #264 P1: the surface-agnostic conversation shell. channel / DM /
// task-thread / issue-thread all render through ONE <ConversationView> — the
// body (message fetch → loading/error/MessageList + Composer) AND the shared
// conversation side-effects (SSE live-subscribe + read-cursor bump) live here;
// only the per-surface header chrome + optional side panel are injected.
//
// This is the §5.1 "surface-agnostic conversation component" axis: upgrade the
// conversation surface once, every surface benefits. Notably this brings the
// SSE live-update + mark-seen read-cursor (previously only in ChannelDetail/
// DMDetail) to the task-thread / issue-thread surfaces too, uniformly.
export type ConversationSurface = 'channel' | 'dm' | 'task-thread' | 'issue-thread';

interface Props {
  surface: ConversationSurface;
  conversationId: string;
  /** surface-specific header chrome (name/breadcrumb/TypeChip), rendered above the message body. */
  header?: React.ReactNode;
  /** optional right-side panel (e.g. channel ParticipantsPanel); rendered beside the message body. */
  sidePanel?: React.ReactNode;
}

export function ConversationView({
  surface,
  conversationId,
  header,
  sidePanel,
}: Props): React.ReactElement {
  // T189 phase 2: the timeline = the live latest window + an on-demand older-history
  // buffer (scroll-up pagination). The latest window stays SSE-driven; older pages
  // load via the `before` keyset cursor and merge in chronologically.
  const messages = useConversationTimeline(conversationId);
  const markSeen = useMarkSeen();

  // Live updates: subscribe to the conversation's SSE stream (new messages /
  // read_state.changed) — query invalidation drives the refetch.
  useSSEConversationSubscribe(conversationId ? [conversationId] : undefined);

  // Fire-and-forget: bump the read cursor to the latest message whenever a new
  // message list arrives (mount + SSE-driven refetch). The server's only-forward
  // rule makes redundant POSTs cheap (no-op past the conditional UPSERT).
  const latestMessageId = messages.messages[messages.messages.length - 1]?.id;
  useEffect(() => {
    if (!conversationId || !latestMessageId) return;
    markSeen.mutate({ conversationId, lastSeenMessageId: latestMessageId });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [conversationId, latestMessageId]);

  const body = (
    <div className="flex flex-1 flex-col overflow-hidden">
      {messages.isLoading && (
        <p className="p-4 text-sm text-text-muted" role="status" data-testid="conversation-loading">
          Loading messages…
        </p>
      )}
      {messages.isError && (
        <p className="p-4 text-sm text-danger" role="alert" data-testid="conversation-error">
          {(messages.error as Error).message}
        </p>
      )}
      {messages.isSuccess && (
        <MessageList
          messages={messages.messages}
          surface={surface}
          onLoadOlder={messages.loadOlder}
          hasOlder={messages.hasOlder}
          isLoadingOlder={messages.isLoadingOlder}
        />
      )}
      <MessageComposer conversationId={conversationId} />
    </div>
  );

  return (
    // v2.9.1 Threads P2: one ThreadSidebarProvider at the surface root so the
    // message list (body) AND the side panel's thread list both open the SAME
    // single ThreadSidebar (shared instance, no double-render).
    <ThreadSidebarProvider>
      <div
        className="flex flex-1 flex-col overflow-hidden"
        data-testid="conversation-view"
        data-surface={surface}
      >
        {header}
        {sidePanel ? (
          <div className="flex flex-1 overflow-hidden">
            {body}
            {sidePanel}
          </div>
        ) : (
          body
        )}
      </div>
    </ThreadSidebarProvider>
  );
}

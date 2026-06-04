import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useEffect } from 'react';
import { useParams } from 'react-router-dom';
import { useConversation, useMessages } from '@/api/conversations';
import { useMarkSeen } from '@/api/readState';
import { useDisplayNameResolver, normalizeIdentityRef } from '@/api/members';
import { useAppStore } from '@/store/app';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { TypeChip } from '@/components/TypeChip';
import { Breadcrumb } from '@/components/Breadcrumb';

// DMDetail page (/dms/:id). Mirrors ChannelDetail layout but skips the
// ParticipantsPanel — DM membership is fixed at create time (per
// ADR-0032 § 6) and not mutable from the UI.
//
// SSE live updates flow through F5's dispatchToQueryClient, same as
// channels — `conversation.message_added` invalidates the messages
// query.
export default function DMDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const conv = useConversation(id);
  const messages = useMessages(id);
  const markSeen = useMarkSeen();
  // v2.7.1 #238 fix: the DM detail GET doesn't enrich peer_display_name (only the
  // list does), so resolve the peer from participants − self for direct loads.
  const me = useAppStore((s) => s.currentUserId);
  const resolveName = useDisplayNameResolver();
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

  // v2.7.1 #215 + #238 fix: DMs are strict 1:1; heading = the resolved peer as
  // @name. Prefer the backend-enriched peer fields (present when navigated from
  // the list), else derive the peer from participants − self and resolve its
  // display name — so a DIRECT load (detail GET, no peer enrichment) still shows
  // @peer instead of the generic "Direct message". Deleted/unresolved → fallback.
  const meBare = me ? normalizeIdentityRef(me) : '';
  const peerRef =
    conv.data.peer_identity_id ||
    (conv.data.participants ?? [])
      .filter((p) => !p.left_at)
      .map((p) => p.identity_id)
      .find((pid) => normalizeIdentityRef(pid) !== meBare) ||
    '';
  const resolvedPeer = conv.data.peer_display_name || (peerRef ? resolveName(peerRef) : '');
  // resolveName returns the ref itself on a miss → treat that as unresolved.
  const peerName = resolvedPeer && resolvedPeer !== peerRef ? resolvedPeer : '';
  const heading = peerName ? `@${peerName}` : peerRef ? '(deleted)' : 'Direct message';

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-DMDetail"
      data-dm-id={conv.data.id}
    >
      <div className="mb-2">
        <Breadcrumb items={[{ label: 'DMs', to: '/dms' }, { label: heading }]} />
      </div>
      <header className="flex items-center justify-between border-b border-border-base pb-3">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-xl font-semibold" data-testid="dm-heading" title={peerRef || undefined}>
              {heading}
            </h2>
            <TypeChip kind="dm" />
          </div>
          <p className="text-xs text-text-muted">Direct message</p>
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

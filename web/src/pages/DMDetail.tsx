import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useConversation } from '@/api/conversations';
import { useDisplayNameResolver, normalizeIdentityRef } from '@/api/members';
import { useAppStore } from '@/store/app';
import { ConversationView } from '@/components/ConversationView';
import { FollowToggle } from '@/components/FollowToggle';
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
  // v2.7.1 #238 fix: the DM detail GET doesn't enrich peer_display_name (only the
  // list does), so resolve the peer from participants − self for direct loads.
  const me = useAppStore((s) => s.currentUserId);
  const resolveName = useDisplayNameResolver();

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
        {/* #264 P1 / #176 §4: follow/unfollow this DM. */}
        <FollowToggle conversationId={conv.data.id} followed={conv.data.followed ?? false} />
      </header>

      {/* #264 P1: message body + read-cursor + SSE live updates flow through
          the surface-agnostic shell (no ParticipantsPanel for DMs). */}
      <ConversationView surface="dm" conversationId={conv.data.id} />
    </section>
  );
}

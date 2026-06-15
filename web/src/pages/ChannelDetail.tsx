import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useConversation } from '@/api/conversations';
import { ConversationView } from '@/components/ConversationView';
import { SenderSidebarProvider } from '@/components/SenderSidebarContext';
import { FollowToggle } from '@/components/FollowToggle';
import { TypeChip } from '@/components/TypeChip';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';
import { SharedFilesPanel } from '@/components/SharedFilesPanel';
import { ContextPanel } from '@/shell/contextPanel';
import { Breadcrumb } from '@/components/Breadcrumb';
import { Avatar } from '@/components/Avatar';
import { useDisplayNameResolver } from '@/api/members';
import type { Participant } from '@/api/types';

// ChannelDetail page (/channels/:channelId). v2.7.1 #247: the URL carries the
// channel's hash id (conversation_id), consistent with project/task/issue URLs
// — no more by-name lookup. The detail GET (by id) provides name/description/
// participants. The channel NAME stays the visible chrome (header/breadcrumb);
// the hash id is only the URL segment (#195 name-uniqueness unaffected).
export default function ChannelDetail(): React.ReactElement {
  const { channelId = '' } = useParams<{ channelId: string }>();
  const conv = useConversation(channelId);
  const displayName = useDisplayNameResolver();

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
  const active = participants.filter((p) => !p.left_at);
  const activeCount = active.length;

  return (
    // #281 entry ②: the channel message surface needs a SenderSidebarProvider so the
    // @mention tokens in message content become clickable (and message-sender clicks
    // drive the one provider sidebar). DM has its own; channel/work-item each wrap too.
    <SenderSidebarProvider>
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
          {/* 8th channel redesign: overlapping avatar group of participants
              (reuses the shared Avatar #211) + the count text. */}
          <div className="flex items-center gap-2">
            <AvatarStack participants={active} resolve={displayName} />
            <span className="text-xs text-text-muted">
              {activeCount} {activeCount === 1 ? 'participant' : 'participants'}
            </span>
          </div>
          {/* #264 P1 / #176 §4: follow/unfollow this channel. */}
          <FollowToggle conversationId={ch.id} followed={ch.followed ?? false} />
        </div>
      </header>

      {/* #264 P1: message body + read-cursor + SSE live updates all flow
          through the surface-agnostic shell (col③). */}
      <ConversationView surface="channel" conversationId={ch.id} />
      {/* v2.10.0 [T64] col④ (例1): participants + shared files render into the
          shell's on-demand fourth column via <ContextPanel> (portals in;
          collapses when this page unmounts). Outside the shell (no provider)
          ContextPanel is a no-op, so the page still renders standalone. */}
      <ContextPanel>
        <ParticipantsPanel conversationId={ch.id} participants={participants} />
        <SharedFilesPanel conversationId={ch.id} />
      </ContextPanel>
    </section>
    </SenderSidebarProvider>
  );
}

// AvatarStack — a stacked/overlapping group of participant avatars for the
// channel header (8th channel redesign, mockup-locked). Reuses the shared
// Avatar (#211): up to MAX_SHOWN discs with a negative-overlap (`-space-x-2`)
// and a bg-token ring so the overlap reads cleanly in both light + dark;
// a `+N` chip absorbs the overflow. The kind (human/agent) comes from the
// identity-ref prefix (matches MessageList's avatar wiring).
const MAX_SHOWN = 3;
function AvatarStack({
  participants,
  resolve,
}: {
  participants: Participant[];
  resolve: (ref: string) => string;
}): React.ReactElement | null {
  if (participants.length === 0) return null;
  const shown = participants.slice(0, MAX_SHOWN);
  const overflow = participants.length - shown.length;
  const countLabel = `${participants.length} ${participants.length === 1 ? 'participant' : 'participants'}`;
  return (
    <div
      className="flex items-center -space-x-2"
      data-testid="channel-avatar-stack"
      role="group"
      aria-label={countLabel}
    >
      {shown.map((p) => {
        const resolved = resolve(p.identity_id);
        const name = resolved === p.identity_id ? '?' : resolved;
        return (
          <span
            key={p.identity_id}
            className="rounded-full ring-2 ring-bg-elevated"
            data-testid="channel-avatar-stack-item"
          >
            <Avatar
              name={name}
              kind={p.identity_id.startsWith('agent:') ? 'agent' : 'human'}
              size="sm"
            />
          </span>
        );
      })}
      {overflow > 0 && (
        <span
          className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-bg-subtle text-[0.625rem] font-semibold text-text-secondary ring-2 ring-bg-elevated"
          data-testid="channel-avatar-stack-overflow"
          aria-label={`${overflow} more`}
        >
          +{overflow}
        </span>
      )}
    </div>
  );
}

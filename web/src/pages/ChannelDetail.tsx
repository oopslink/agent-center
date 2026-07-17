import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useConversation } from '@/api/conversations';
import { ConversationView } from '@/components/ConversationView';
import { SenderSidebarProvider } from '@/components/SenderSidebarContext';
import { FollowToggle } from '@/components/FollowToggle';
import { TypeChip } from '@/components/TypeChip';
import { ConversationSidebar } from '@/components/ConversationSidebar';
import { ConversationSurfaceMobile } from '@/components/ConversationSurfaceMobile';
import { ConversationInfoButton, ConversationInfoSheet } from '@/components/ConversationInfoPanel';
import { useIsMobile } from '@/components/WorkItemMobileMeta';
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
  const { t } = useTranslation('chat');
  const { channelId = '' } = useParams<{ channelId: string }>();
  const conv = useConversation(channelId);
  const displayName = useDisplayNameResolver();
  // T184: mobile collapses the col③/col④ split into one chat/threads/files tab bar.
  const isMobile = useIsMobile();

  if (conv.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-ChannelDetail">
        {t('channels.detail.loading')}
      </section>
    );
  }

  if (conv.isError) {
    return (
      <section className="space-y-3 text-sm text-text-muted" data-testid="page-ChannelDetail">
        <p data-testid="channel-not-found">{(conv.error as Error).message}</p>
        <OrgLink to="/channels" className="text-accent hover:underline">
          {t('channels.detail.backToChannels')}
        </OrgLink>
      </section>
    );
  }

  if (!conv.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-ChannelDetail">
        {t('channels.detail.lookupFailed')}
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
      className="-mx-4 -mt-2 flex h-full flex-col px-4 pt-2 md:mx-0 md:mt-0 md:px-0 md:pt-0"
      data-testid="page-ChannelDetail"
      data-channel-id={ch.id}
    >
      <div className="mb-2">
        {/* #192/#247: leaf shows the channel NAME (chrome), URL carries the id. */}
        <Breadcrumb items={[{ label: t('channels.title'), to: '/channels' }, { label: ch.name }]} />
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
              (reuses the shared Avatar #211) + the count text. Mobile hides it —
              the redesigned mobile header is back + title + star + ⓘ only
              (mobile-redesign-conversations.md §3.5, mockup frame ④); the People
              segment + the ⓘ sheet's Members preview carry this instead. */}
          <div className="hidden items-center gap-2 md:flex">
            <AvatarStack participants={active} resolve={displayName} />
            <span className="text-xs text-text-muted">
              {t('channels.participantCount', { count: activeCount })}
            </span>
          </div>
          {/* #264 P1 / #176 §4: follow/unfollow this channel. */}
          <FollowToggle conversationId={ch.id} followed={ch.followed ?? false} />
          {/* Mobile-only ⓘ → the shell's Context Panel bottom sheet (batch 1). */}
          <ConversationInfoButton />
        </div>
      </header>

      {/* Mobile (<768px): the redesigned surface — Chat / Threads / Files / People
          segment pills (mobile-redesign-conversations.md §3.5), plus the ⓘ sheet
          content portalled into the shell's Context Panel bottom sheet.
          Desktop: the message stream (col③) + the shared col④ sidebar. */}
      {isMobile ? (
        <>
          <ConversationSurfaceMobile surface="channel" conversationId={ch.id} participants={participants} />
          <ContextPanel>
            <ConversationInfoSheet
              conversationId={ch.id}
              title={ch.name}
              description={ch.description}
              participants={participants}
            />
          </ContextPanel>
        </>
      ) : (
        <>
          {/* #264 P1: message body + read-cursor + SSE live updates all flow
              through the surface-agnostic shell (col③). */}
          <ConversationView surface="channel" conversationId={ch.id} />
          {/* col④ via <ContextPanel> (portals into the shell's fourth column;
              collapses when this page unmounts). T184: the SHARED ConversationSidebar. */}
          <ContextPanel>
            <ConversationSidebar conversationId={ch.id} participants={participants} />
          </ContextPanel>
        </>
      )}
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
  const { t } = useTranslation('chat');
  if (participants.length === 0) return null;
  const shown = participants.slice(0, MAX_SHOWN);
  const overflow = participants.length - shown.length;
  const countLabel = t('channels.participantCount', { count: participants.length });
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
          aria-label={t('channels.moreOverflow', { count: overflow })}
        >
          +{overflow}
        </span>
      )}
    </div>
  );
}

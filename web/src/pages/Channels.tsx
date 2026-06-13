import type React from 'react';
import { OrgLink, useOptionalOrgContext } from '@/OrgContext';
import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import { useConversations } from '@/api/conversations';
import type { Participant, RecentMessageSummary } from '@/api/types';
import {
  displayNameFallback,
  isResolvedName,
  useDisplayNameResolver,
} from '@/api/members';
import { Avatar } from '@/components/Avatar';
import { ChannelCreateModal } from '@/components/ChannelCreateModal';
import { UnreadBadge } from '@/components/UnreadBadge';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { formatLocalTime } from '@/utils/time';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';

// v2.8.1 list-enrichment: how many participant avatars we show before the "+N"
// overflow chip (mirrors the ChannelDetail header AvatarStack, MAX_SHOWN=3).
const MAX_AVATARS = 3;
const MAX_PREVIEWS = 3;

// ChannelList page (/channels). Lists kind=channel conversations + a
// "New channel" button. Empty state offers the same button inline.
// v2.8.1 #list-enrich: each row is enriched with created_at (local tz), a
// participant avatar-stack + count, and ≤3 recent-message plain-text previews.
export default function Channels(): React.ReactElement {
  const channels = useConversations({ kind: 'channel' });
  const [createOpen, setCreateOpen] = useState(false);
  // v2.7.1 #247: after create, navigate to the new channel by id.
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  // One org-scoped resolver for the whole list (members are cached, NOT per
  // row → no N+1); rows just call resolve(ref). Reused by the recent-message
  // sender names so a deleted sender renders "(deleted)" not the raw ref.
  const resolve = useDisplayNameResolver();
  // Subscribe to every visible channel so badge auto-ticks via SSE.
  useSSEConversationSubscribe(channels.data?.map((c) => c.id));

  return (
    <section className="space-y-4" data-testid="page-Channels">
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">Channels</h1>
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
                className="block px-4 py-3 hover:bg-bg-subtle"
              >
                <div className="flex items-center justify-between gap-3">
                  <span className="flex min-w-0 items-center gap-3">
                    <span className="truncate font-medium">{c.name}</span>
                    <UnreadBadge unreadCount={c.unread_count} mentionCount={c.mention_count} />
                    <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
                      {c.status}
                    </span>
                  </span>
                  <span className="flex shrink-0 items-center gap-3">
                    <ParticipantStack
                      participants={c.participants}
                      participantCount={c.participant_count}
                      resolve={resolve}
                    />
                    {c.created_at && (
                      <span
                        className="hidden text-xs text-text-muted sm:inline"
                        data-testid="channel-created-at"
                        title={formatLocalTime(c.created_at)}
                      >
                        {formatLocalTime(c.created_at)}
                      </span>
                    )}
                  </span>
                </div>
                {c.description && (
                  <p className="mt-1 max-w-full truncate text-xs text-text-muted">{c.description}</p>
                )}
                <RecentMessages messages={c.recent_messages} resolve={resolve} />
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

// ParticipantStack — overlapping avatar group for a channel row (v2.8.1
// list-enrich). Reuses the shared Avatar (#211) exactly like ChannelDetail's
// header AvatarStack: up to MAX_AVATARS discs with a -space-x-2 overlap + a
// bg-token ring (reads in both modes), then a "+N" overflow chip. The group has
// an aria-label ("5 participants") so the whole stack has an accessible name.
// participant_count drives the count text + overflow even when only a few
// summary participants are embedded. The name fed to Avatar is the resolved
// display name (NEVER the raw prefixed ref): prefer the inline display_name from
// the summary, else the list resolver, else a clean tail handle.
function ParticipantStack({
  participants,
  participantCount,
  resolve,
}: {
  participants: Participant[] | undefined;
  participantCount: number | undefined;
  resolve: (ref: string) => string;
}): React.ReactElement | null {
  const shown = participants ?? [];
  // Total = explicit participant_count when present, else the summary length.
  const total = participantCount ?? shown.length;
  if (shown.length === 0 && total === 0) return null;
  const visible = shown.slice(0, MAX_AVATARS);
  const overflow = Math.max(0, total - visible.length);
  const countLabel = `${total} ${total === 1 ? 'participant' : 'participants'}`;
  return (
    <span
      className="flex items-center gap-1.5"
      data-testid="channel-participants"
      data-participant-count={total}
    >
      <span className="flex items-center -space-x-2" role="group" aria-label={countLabel}>
        {visible.map((p) => {
          // Prefer the inline summary display_name; else resolve via the list
          // resolver; else a clean handle. Avatar's accessible name is never the
          // raw prefixed ref (#192 chrome rule).
          const resolved = p.display_name || resolve(p.identity_id);
          const name = isResolvedName(p.identity_id, resolved)
            ? resolved
            : displayNameFallback(p.identity_id);
          return (
            <span
              key={p.identity_id}
              className="rounded-full ring-2 ring-bg-elevated"
              data-testid="channel-participant-avatar"
            >
              <Avatar
                name={name}
                kind={
                  (p.kind === 'agent' || p.kind === 'human'
                    ? p.kind
                    : p.identity_id.startsWith('agent:') ? 'agent' : 'human')
                }
                size="sm"
              />
            </span>
          );
        })}
        {overflow > 0 && (
          <span
            className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-bg-subtle text-[0.625rem] font-semibold text-text-secondary ring-2 ring-bg-elevated"
            data-testid="channel-participants-overflow"
            aria-label={`${overflow} more`}
          >
            +{overflow}
          </span>
        )}
      </span>
      <span className="text-xs text-text-muted" data-testid="channel-participant-count">
        {total}
      </span>
    </span>
  );
}

// RecentMessages — ≤3 compact single-line previews for a channel row (v2.8.1
// list-enrich). Each preview is PLAIN TEXT (rendered into a text node, NOT a
// markdown block / code / image), TRUNCATED with ellipsis (truncate; long
// messages never grow the row height) and carries a `title` with the full
// "sender: content" for hover. A deleted/unresolved sender shows a friendly
// "(deleted)" (the #246 F1 isResolvedName pattern), NEVER the raw agent:<id>
// ref. Empty channel → a friendly "No messages yet" placeholder, never blank.
function RecentMessages({
  messages,
  resolve,
}: {
  messages: RecentMessageSummary[] | undefined;
  resolve: (ref: string) => string;
}): React.ReactElement | null {
  // Memoize the per-row derivation (sender resolution + plain-text flattening)
  // so re-renders (SSE badge ticks) don't re-walk every preview.
  const previews = useMemo(() => {
    return (messages ?? []).slice(0, MAX_PREVIEWS).map((m, i) => {
      // contract LOCK + soft-ref reconcile (a): the backend resolves the sender
      // name (sender_display_name) and returns it EMPTY "" for a deleted/unresolved
      // sender (backend-authoritative, the #246 F1 miss-sentinel pattern). So when
      // the field is PRESENT (even empty) use it directly — empty → "(deleted)";
      // fall back to the FE resolver ONLY when the field is absent (legacy payload).
      const backendName =
        m.sender_display_name !== undefined ? m.sender_display_name : resolve(m.sender_identity_id);
      const senderResolved = !!backendName && isResolvedName(m.sender_identity_id, backendName);
      const senderLabel = senderResolved ? backendName : '(deleted)';
      // Flatten any newlines so a multi-line message stays a single-line
      // preview; the row's `truncate` + title carry the rest. PLAIN TEXT only.
      const content = m.content.replace(/\s+/g, ' ').trim();
      return {
        key: m.id || `${m.sender_identity_id}-${m.posted_at}-${i}`,
        senderLabel,
        senderResolved,
        content,
      };
    });
  }, [messages, resolve]);

  // recent_messages absent → the field wasn't enriched; render nothing (the
  // row's name/participants still show). An explicit empty array → "No messages
  // yet" placeholder (a real, known-empty channel).
  if (messages === undefined) return null;
  if (previews.length === 0) {
    return (
      <p className="mt-1 text-xs italic text-text-muted" data-testid="channel-no-messages">
        No messages yet
      </p>
    );
  }
  return (
    <ul className="mt-1 space-y-0.5" data-testid="channel-recent-messages">
      {previews.map((p) => (
        <li
          key={p.key}
          className="truncate text-xs text-text-muted"
          data-testid="channel-recent-message"
          title={`${p.senderResolved ? p.senderLabel : 'deleted'}: ${p.content}`}
        >
          <span
            className={p.senderResolved ? 'font-medium text-text-secondary' : 'font-medium italic text-text-secondary'}
            data-testid="channel-recent-sender"
            data-sender-resolved={p.senderResolved ? 'true' : 'false'}
          >
            {p.senderLabel}
          </span>
          <span aria-hidden="true">: </span>
          {p.content}
        </li>
      ))}
    </ul>
  );
}

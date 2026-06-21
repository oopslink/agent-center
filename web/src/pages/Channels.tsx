import type React from 'react';
import { OrgLink, useOptionalOrgContext } from '@/OrgContext';
import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import {
  useConversations,
  useArchivedChannels,
  useArchiveConversation,
} from '@/api/conversations';
import type { Conversation, Participant, RecentMessageSummary } from '@/api/types';
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
import { formatLocalTime, formatLocalDateTimeSeconds } from '@/utils/time';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { CONVERSATION_SEGMENTS } from './conversationSegments';

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
  // v2.9.1 (task-169c598d): archive a channel (active→archived). Single hook for
  // the whole list (hooks can't run inside the row .map); rows call archive.mutate.
  const archive = useArchiveConversation();
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
      {/* v2.10.2 [T129] Mobile (<md): Conversations module 二级段控 (Channels |
          DMs) — desktop keeps the col② nav. */}
      <SegmentedNav items={CONVERSATION_SEGMENTS} ariaLabel="Conversations sections" />
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
            <li
              key={c.id}
              data-testid="channel-row"
              data-channel-name={c.name}
              data-channel-id={c.id}
              className="flex items-stretch"
            >
              <OrgLink
                to={`/channels/${encodeURIComponent(c.id)}`}
                className="block min-w-0 flex-1 px-4 py-3 hover:bg-bg-subtle"
              >
                <div className="flex items-center justify-between gap-3">
                  <span className="flex min-w-0 flex-1 items-center gap-3">
                    <span className="min-w-0 truncate font-medium">{c.name}</span>
                    <UnreadBadge unreadCount={c.unread_count} mentionCount={c.mention_count} />
                    <span className="shrink-0 rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
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
              <div className="flex shrink-0 items-center pr-3">
                <button
                  type="button"
                  data-testid="channel-archive-btn"
                  aria-label={`Archive ${c.name}`}
                  className="rounded px-2 py-1 text-xs font-medium text-text-secondary motion-safe:transition-colors hover:bg-bg-subtle hover:text-text-primary disabled:opacity-50"
                  disabled={archive.isPending}
                  onClick={() => archive.mutate({ id: c.id, version: 0 })}
                >
                  Archive
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {/* v2.9.1 (task-169c598d): collapsed Archived group, lazy-loaded, read-only —
          mirrors the Projects Archived group (#317). Active list above default-
          excludes archived (backend). */}
      <ArchivedChannelsGroup />

      <ChannelCreateModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(id) => navigate(`${org ? `/organizations/${org.slug}` : ''}/channels/${encodeURIComponent(id)}`)}
      />
    </section>
  );
}

// ArchivedChannelsGroup — the collapsed "Archived" disclosure (v2.9.1
// task-169c598d), mirroring the Projects archived group (#317). Fetches the
// archived-only channel list (useArchivedChannels) ONLY when first expanded so the
// active page load stays a single request. Rows are READ-ONLY (no Archive action;
// the ARCHIVED badge shows the state, and the backend rejects mutations with 409).
// Empty → a quiet note.
function ArchivedChannelsGroup(): React.ReactElement {
  const [open, setOpen] = useState(false);
  const archived = useArchivedChannels(open);

  return (
    <section className="space-y-2" data-testid="archived-channels-group">
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded px-1 py-1.5 text-left text-sm font-medium text-text-secondary motion-safe:transition-colors hover:text-text-primary"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        data-testid="archived-channels-toggle"
      >
        <svg
          viewBox="0 0 24 24"
          className={['h-3.5 w-3.5 motion-safe:transition-transform', open ? 'rotate-90' : ''].join(' ')}
          fill="none"
          stroke="currentColor"
          strokeWidth="2.4"
          aria-hidden="true"
        >
          <path d="M9 6l6 6-6 6" />
        </svg>
        <span>Archived / 已归档</span>
      </button>

      {open && (
        <div data-testid="archived-channels-body">
          {archived.isLoading && (
            <div className="space-y-2" data-testid="archived-channels-loading">
              <Skeleton height="2.5rem" />
              <Skeleton height="2.5rem" />
            </div>
          )}
          {archived.isError && (
            <p className="text-sm text-danger" data-testid="archived-channels-error">
              {(archived.error as Error).message}
            </p>
          )}
          {archived.isSuccess && archived.data.length === 0 && (
            <p className="px-1 text-xs italic text-text-muted" data-testid="archived-channels-empty">
              No archived channels.
            </p>
          )}
          {archived.isSuccess && archived.data.length > 0 && (
            <ul
              className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-text-primary"
              data-testid="archived-channels-list"
            >
              {archived.data.map((c) => (
                <ArchivedChannelRow key={c.id} channel={c} />
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}

// ArchivedChannelRow — a read-only archived-channel row: name + ARCHIVED badge +
// description, linking to the (read-only) channel detail. No Archive action.
function ArchivedChannelRow({ channel: c }: { channel: Conversation }): React.ReactElement {
  return (
    <li data-testid="archived-channel-row" data-channel-id={c.id} data-channel-name={c.name}>
      <OrgLink
        to={`/channels/${encodeURIComponent(c.id)}`}
        className="block px-4 py-3 hover:bg-bg-subtle"
      >
        <div className="flex items-center justify-between gap-3">
          <span className="flex min-w-0 flex-1 items-center gap-3">
            <span className="min-w-0 truncate font-medium">{c.name}</span>
            <ChannelStatusBadge status={c.status} />
          </span>
          {c.created_at && (
            <span
              className="hidden text-xs text-text-muted sm:inline"
              title={formatLocalTime(c.created_at)}
            >
              {formatLocalTime(c.created_at)}
            </span>
          )}
        </div>
        {c.description && (
          <p className="mt-1 max-w-full truncate text-xs text-text-muted">{c.description}</p>
        )}
      </OrgLink>
    </li>
  );
}

// ChannelStatusBadge — active/archived status chip (mirrors ProjectStatusBadge).
// The uppercase label is the primary distinguisher (never color alone).
function ChannelStatusBadge({ status }: { status: Conversation['status'] }): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        status === 'archived' ? 'bg-bg-subtle text-text-muted' : 'bg-success/10 text-success',
      ].join(' ')}
      data-testid={`channel-status-${status}`}
    >
      {status}
    </span>
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
// list-enrich). T234 display format: `[yyyy-MM-dd HH:mm:ss] [{User Name}]: {content}`
// where the `[timestamp] [User Name]` meta is BOLD and the content is plain text,
// TRUNCATED with ellipsis and capped at ≤3/4 of the card width so a long message
// never grows the row height nor crowds out the meta. Each preview is PLAIN TEXT
// (rendered into a text node, NOT a markdown block / code / image) and carries a
// `title` with the full "[time] [sender]: content" for hover. A deleted/unresolved
// sender shows a friendly "(deleted)" (the #246 F1 isResolvedName pattern), NEVER
// the raw agent:<id> ref. Empty channel → a friendly "No messages yet"
// placeholder, never blank.
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
      // T234: a compact absolute "yyyy-MM-dd HH:mm:ss" local-tz timestamp prefix.
      const timestamp = formatLocalDateTimeSeconds(m.posted_at);
      return {
        key: m.id || `${m.sender_identity_id}-${m.posted_at}-${i}`,
        senderLabel,
        senderResolved,
        timestamp,
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
          className="flex min-w-0 items-baseline gap-1 text-xs text-text-muted"
          data-testid="channel-recent-message"
          title={`[${p.timestamp}] [${p.senderResolved ? p.senderLabel : 'deleted'}]: ${p.content}`}
        >
          {/* `[yyyy-MM-dd HH:mm:ss] [{User Name}]` — BOLD meta. The timestamp is
              fixed (shrink-0); the sender truncates so the meta never overflows a
              narrow (mobile) row. The trailing `:` + content stay normal weight. */}
          <span className="flex min-w-0 items-baseline gap-1 font-semibold text-text-secondary">
            <span className="shrink-0" data-testid="channel-recent-time">[{p.timestamp}]</span>
            <span
              className={`min-w-0 max-w-[10rem] truncate ${p.senderResolved ? '' : 'italic'}`}
              data-testid="channel-recent-sender"
              data-sender-resolved={p.senderResolved ? 'true' : 'false'}
            >
              [{p.senderLabel}]
            </span>
          </span>
          <span className="shrink-0" aria-hidden="true">:</span>
          {/* content fills the remaining row width and ellipsizes on a single line
              so a long message never grows the row height nor overflows the card. */}
          <span className="min-w-0 flex-1 truncate" data-testid="channel-recent-content">
            {p.content}
          </span>
        </li>
      ))}
    </ul>
  );
}

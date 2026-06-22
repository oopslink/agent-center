import type React from 'react';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import type { Message } from '@/api/types';
import { withOrgSlug } from '@/api/client';
import { useDisplayNameResolver, isResolvedName, normalizeIdentityRef, isSystemSender } from '@/api/members';
import { useAppStore } from '@/store/app';
import { Avatar } from './Avatar';
import { formatChatTime } from '@/utils/time';
import { MarkdownMessage } from './MarkdownMessage';
import { MessageCopyButton } from './MessageCopyButton';
import type { ConversationSurface } from './ConversationView';
import { SenderDetailSidebar } from './SenderDetailSidebar';
import { useSenderSidebar } from './SenderSidebarContext';
import { ThreadButton } from './ThreadButton';
import { ThreadSidebar } from './ThreadSidebar';
import { useThreadSidebar } from './ThreadSidebarContext';

// v2.7 #133: a short text type label for an attachment (no emoji icons — a11y
// no-emoji-icons rule). Derived from the mime category for the metadata chip.
export function attachmentKind(mime: string): string {
  const slash = mime.indexOf('/');
  const top = slash > 0 ? mime.slice(0, slash) : mime;
  switch (top) {
    case 'image':
      return 'IMG';
    case 'video':
      return 'VID';
    case 'audio':
      return 'AUD';
    case 'text':
      return 'TXT';
    default:
      return 'FILE';
  }
}

// formatBytes renders a human-readable size for an attachment chip.
export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

// attachmentHref maps an ac://files/{ulid} URI to the gated download endpoint,
// carrying the current org scope (path-routed /api/orgs/{slug}/files/{ulid}) the
// same way the api client does — the endpoint runs requireOrgMember, which
// rejects requests without an org scope.
export function attachmentHref(uri: string): string {
  const prefix = 'ac://files/';
  if (!uri.startsWith(prefix)) return '#';
  return `/api${withOrgSlug(`/files/${encodeURIComponent(uri.slice(prefix.length))}`)}`;
}

interface Props {
  messages: Message[];
  /**
   * Conversation surface. v2.8.1 7th-DM increment (Dev/Dev2 split): the DM surface
   * renders RECEIVED messages as bordered content cards (per the DM mockup) instead
   * of the channel's gray pill bubble. Own bubble (#D1E3FF) + channel are unchanged.
   * Defaults to channel styling so every other caller is unaffected.
   */
  surface?: ConversationSurface;
  /**
   * v2.9.1 Threads: render the per-message ThreadButton + own the thread sidebar.
   * Defaults true (the main conversation surface). ThreadSidebar passes false so
   * a reply rendered INSIDE a thread never grows its own thread affordance — P1
   * is single-level (no thread-in-thread).
   */
  showThreads?: boolean;
  /**
   * T189 phase 2 — scroll-up history pagination. When the user scrolls near the
   * TOP and there is older history, onLoadOlder() fetches the previous page; the
   * list preserves the scroll position across the prepend so the view doesn't jump.
   * Omitted (e.g. inside a thread) → no pagination affordance.
   */
  onLoadOlder?: () => void;
  hasOlder?: boolean;
  isLoadingOlder?: boolean;
}

// MessageList — render messages chronologically. Sender id + posted_at
// + content. No virtual scrolling yet (deferred to M3 per F6 oversight
// #2 — happy path doesn't need it).
//
// Auto-scroll behavior (v2.5.6 #60): when a new message arrives, scroll
// to bottom — but only if the user is already near the bottom. If they
// scrolled up to read history, we don't yank them back.
export function MessageList({
  messages,
  surface = 'channel',
  showThreads = true,
  onLoadOlder,
  hasOlder = false,
  isLoadingOlder = false,
}: Props): React.ReactElement {
  const displayName = useDisplayNameResolver();
  // v2.8.1 chat-rightalign: the viewer's own messages render right-aligned
  // (iMessage/Slack style). `currentUserId` is a prefixed identity ref
  // (e.g. "user:hayang"); normalize BOTH sides so the user:/agent: prefix
  // never breaks the compare. Empty `me` (not yet bound) => nothing is "own".
  const me = useAppStore((s) => s.currentUserId);
  const meKey = me ? normalizeIdentityRef(me) : '';
  const containerRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);
  // T189 phase 2: scroll-anchor for prepending older history. When a load is
  // triggered we snapshot (scrollHeight, scrollTop); after the older page renders
  // and the content grows at the top, we restore the offset so the view stays put.
  const pendingRestoreRef = useRef<{ height: number; top: number } | null>(null);
  const latestId = messages[messages.length - 1]?.id;
  const prevLatestIdRef = useRef<string | undefined>(undefined);
  // Re-render trigger so the "New messages ↓" pill appears when a new
  // message arrives while the user is scrolled up; cleared on click or
  // when the user scrolls back to the bottom.
  const [hasNewBelow, setHasNewBelow] = useState(false);
  // v2.8.1 7th DM increment 2: the sender-detail sidebar. Holds the clicked
  // message's sender identity ref (prefixed, e.g. "agent:A-1"); null = closed.
  // #281: PREFER the surface-level provider opener when present (so the header
  // peer + @mention tokens + message-sender clicks all drive the ONE provider
  // sidebar). Standalone (no provider, e.g. channel surface / unit tests) → keep
  // this local state + the local sidebar instance below.
  const providerOpen = useSenderSidebar();
  const [sidebarSender, setSidebarSender] = useState<string | null>(null);
  const openSender = providerOpen ?? setSidebarSender;

  // v2.9.1 Threads: the per-message ThreadButton opens the ONE thread sidebar.
  // PREFER the surface-level provider (ConversationView mounts it) so the single
  // sidebar is shared; fall back to a local instance when standalone (matches the
  // sender-sidebar pattern above, keeping MessageList self-contained in tests).
  const providerOpenThread = useThreadSidebar();
  const [localThreadRoot, setLocalThreadRoot] = useState<Message | null>(null);
  const openThread = providerOpenThread ?? setLocalThreadRoot;

  useEffect(() => {
    if (latestId === prevLatestIdRef.current) return;
    prevLatestIdRef.current = latestId;
    const el = containerRef.current;
    if (!el) return;
    if (stickToBottomRef.current) {
      el.scrollTop = el.scrollHeight;
      setHasNewBelow(false);
    } else {
      setHasNewBelow(true);
    }
  }, [latestId]);

  // On first mount with messages, snap to bottom so the initial render
  // starts at the latest message (Slack-style).
  useEffect(() => {
    const el = containerRef.current;
    if (el && messages.length > 0) {
      el.scrollTop = el.scrollHeight;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // T189 phase 2: trigger an older-history load, snapshotting the scroll metrics so
  // the post-prepend layout effect can restore the view position.
  const triggerLoadOlder = () => {
    const el = containerRef.current;
    if (!el || !onLoadOlder || !hasOlder || isLoadingOlder || pendingRestoreRef.current) return;
    pendingRestoreRef.current = { height: el.scrollHeight, top: el.scrollTop };
    onLoadOlder();
  };

  // After an older page prepends, the container grows at the top — shift scrollTop
  // by the height delta so the previously-visible message stays under the cursor
  // (no jump). Runs before paint (useLayoutEffect) to avoid a flicker.
  useLayoutEffect(() => {
    const el = containerRef.current;
    const pend = pendingRestoreRef.current;
    if (!el || !pend) return;
    if (el.scrollHeight > pend.height) {
      el.scrollTop = el.scrollHeight - pend.height + pend.top;
      pendingRestoreRef.current = null;
    }
  });

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distFromBottom < 40;
    stickToBottomRef.current = atBottom;
    if (atBottom && hasNewBelow) setHasNewBelow(false);
    // Near the top → pull the previous (older) page.
    if (el.scrollTop < 80) triggerLoadOlder();
  };

  const jumpToLatest = () => {
    const el = containerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    stickToBottomRef.current = true;
    setHasNewBelow(false);
  };

  if (messages.length === 0) {
    return (
      <div
        className="flex flex-1 items-center justify-center text-sm text-text-muted"
        data-testid="message-list-empty"
      >
        No messages yet.
      </div>
    );
  }
  const renderRow = (m: Message): React.ReactElement => {
    // @oopslink: system messages (e.g. agent converse failures) are
    // de-emphasized — a centered hint with the raw text collapsed behind
    // [Details], not a full sender bubble dumping the raw API error inline.
    if (m.content_kind === 'system') {
      return <SystemMessageRow key={m.id} content={m.content} />;
    }
    // T308: a message authored by the SYSTEM sender (reminders, scheduler/plan
    // dispatch notices — content_kind='text', sender='system') renders as a
    // de-emphasized NOTIFICATION (centered notice card with a bell + "System ·
    // time"), NOT a peer chat bubble with a "System" avatar (@oopslink). The
    // content still renders as markdown so task/issue/plan refs stay clickable.
    if (isSystemSender(m.sender_identity_id)) {
      return <SystemNotificationRow key={m.id} m={m} />;
    }
    // v2.8.1 chat-rightalign: own = the viewer's own message. Normalize both
    // sides so the user:/agent: prefix never breaks the compare.
    const isOwn = meKey !== '' && normalizeIdentityRef(m.sender_identity_id) === meKey;
    const hasCodeBlock = m.content.includes('```');
    // v2.10.1 [M2] mobile: bubbles fill ~86% of the full-screen conversation
    // (mockup v2.10.1-mobile .msg max-width:86%) for comfortable reading on a
    // narrow viewport, narrowing back to 75% at the desktop 3-column breakpoint
    // (≥768, where col③ is itself a column). Code-block bubbles already go
    // full-width on mobile (horizontal scroll guard), so they are left as-is.
    const bubbleWidthClass = hasCodeBlock
      ? 'w-full max-w-full sm:w-2/3 sm:max-w-[66.666667%]'
      : 'max-w-[86%] md:max-w-[75%]';

    // Chat UX 2 (#3 + #5): the sender NAME (+ work-item tag) and the TIME move
    // OUT of the bubble into a small header line ABOVE the bubble; the bubble is
    // now content-ONLY (markdown body + attachments). The header line is right-
    // aligned for own messages, left-aligned for others. Header is muted theme-
    // adaptive text on BOTH sides (it sits on the page surface, never inside the
    // fixed light-blue bubble) — so it uses normal theme tokens.
    // F1 (v2.8.1 #192): resolve the sender name. An UNRESOLVED ref (e.g. a
    // force-deleted agent — member row gone, messages soft-ref retained) must
    // NEVER show the raw `agent:agent-xxx` prefixed form. We render a muted
    // "(deleted)" label instead, keeping the clean handle + raw ref on hover
    // (title=) for debugging per the #192 chrome rule. Tradeoff: an unresolved
    // ref could also be a not-yet-loaded member, but the members list IS loaded
    // in the message-list surface (useMembers is org-scoped + cached), so an
    // unresolved sender here is effectively gone — "(deleted)" is acceptable.
    const senderName = displayName(m.sender_identity_id);
    const senderResolved = isResolvedName(m.sender_identity_id, senderName);
    // Clean handle (prefix stripped) for the title/hover when unresolved.
    const senderHandle = normalizeIdentityRef(m.sender_identity_id);
    // Name fed to the Avatar (initials/hash + aria-label): resolved name, else
    // the CLEAN handle — NEVER the raw prefixed ref (which displayName returns on
    // a miss). Keeps the avatar's accessible name free of "agent:agent-xxx".
    const avatarName = senderResolved ? senderName : senderHandle;
    const headerLine = (
      <div
        className={`mb-0.5 flex items-center gap-2 text-xs font-medium text-text-secondary ${
          isOwn ? 'flex-row-reverse' : ''
        }`}
        data-testid="message-header"
      >
        {/* increment 2: the sender name opens the sender-detail sidebar. A real
            <button> (Tab + Enter/Space) with an aria-label; own messages stay
            clickable too (own = the viewer's own profile). */}
        <button
          type="button"
          onClick={() => openSender(m.sender_identity_id)}
          aria-label={
            senderResolved
              ? `View ${senderName} detail`
              : `View ${senderHandle} detail (deleted sender)`
          }
          title={senderResolved ? m.sender_identity_id : `${senderHandle} (${m.sender_identity_id})`}
          data-testid="message-sender-button"
          data-sender-resolved={senderResolved ? 'true' : 'false'}
          className={`rounded font-medium hover:underline focus-visible:ring-2 focus-visible:ring-accent ${
            senderResolved ? '' : 'italic text-text-secondary'
          }`}
        >
          {senderResolved ? senderName : '(deleted)'}
        </button>
        {/* #219: per-message work-item tag (only when the message carries one);
            the raw ref stays on hover (#192 chrome rule). Now always on the page
            surface (header is outside the bubble), so one both-mode treatment. */}
        {m.context_refs?.work_item_ref && (
          <span
            className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-medium uppercase tracking-wide text-text-secondary"
            data-testid="message-workitem-tag"
            data-work-item-ref={m.context_refs.work_item_ref}
            title={m.context_refs.work_item_ref}
          >
            Work item
          </span>
        )}
        {/* Chat UX 2 #4: timestamp in the header line (outside the bubble), as a
            <time dateTime title>. @oopslink locked: visible text is 24-hr local
            "HH:MM" (formatChatTime); the dateTime attr keeps the raw ISO. */}
        <time
          className="text-[0.625rem] font-normal text-text-muted"
          dateTime={m.posted_at}
          title={m.posted_at}
          data-testid="message-time"
        >
          {formatChatTime(m.posted_at)}
        </time>
        {/* T246: per-message copy — copies the raw content to the clipboard.
            Lives in the header line so it shares the own/other alignment (the
            line reverses for own messages). */}
        <MessageCopyButton content={m.content} />
      </div>
    );

    // Content-only bubble body: markdown + attachments (no name/time). On the OWN
    // bubble the surface is the FIXED light-blue #D1E3FF with FIXED dark text, so
    // the attachment chrome uses the SAME both-mode treatment as the non-own side
    // (theme tokens) — no more white-alpha-on-indigo variants.
    const bubbleBody = (
      <div className="min-w-0">
        {/* #276: message content renders as markdown (GFM + strict-escape);
            long fenced code collapses via the shared CollapsibleCodeBlock. */}
        {/* both-mode命门: the own bubble is a FIXED light #D1E3FF that does NOT
            flip per theme, so its markdown body must use FIXED dark text
            (text-chatbubble-fg, a light==dark token = the old slate-900) in both
            modes — the default theme token would flip light in dark mode =
            light-on-light-blue FAIL. Other bubble is theme-adaptive
            (bg-bg-subtle), so it keeps the default token. */}
        <MarkdownMessage
          content={m.content}
          textClass={isOwn ? 'text-chatbubble-fg' : 'text-text-primary'}
          linkClass={isOwn ? 'text-chatbubble-link' : 'text-accent'}
        />
        {/* #142: attachments download through the same gated /api/files/{id}
            endpoint used by the backend reachability checks. */}
        {m.attachments && m.attachments.length > 0 && (
          <ul
            className={`mt-1 flex flex-wrap gap-2 ${isOwn ? 'justify-end' : ''}`}
            data-testid="message-attachments"
          >
            {m.attachments.map((att) => (
              <li
                key={att.uri}
                className="flex min-w-0 max-w-full items-center gap-2 rounded border border-border-base bg-bg-base px-2 py-1 text-xs"
                data-testid="message-attachment"
                data-mime={att.mime_type}
              >
                {att.mime_type.startsWith('image/') && (
                  <a href={attachmentHref(att.uri)} target="_blank" rel="noreferrer" aria-label={`Open ${att.filename}`}>
                    <img
                      src={attachmentHref(att.uri)}
                      alt={att.filename}
                      className="h-10 w-10 rounded object-cover"
                      data-testid="attachment-preview"
                    />
                  </a>
                )}
                <a
                  href={attachmentHref(att.uri)}
                  target="_blank"
                  rel="noreferrer"
                  className="flex min-w-0 items-center gap-2 text-text-primary hover:underline"
                  data-testid="attachment-link"
                >
                  <span
                    className="shrink-0 rounded bg-bg-base px-1 font-mono uppercase text-text-muted"
                    data-testid="attachment-type"
                  >
                    {attachmentKind(att.mime_type)}
                  </span>
                  {/* T149: a long filename truncates (ellipsis) instead of widening
                      the chip past the viewport; full name on hover/aria via the link. */}
                  <span className="truncate" title={att.filename}>{att.filename}</span>
                </a>
                <span className="text-text-muted">{formatBytes(att.size)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    );

    // v2.9.1 Threads: the per-message thread affordance (count chip + activity
    // dot), aligned to the message's side. Opens the thread sidebar for THIS
    // message. Omitted when showThreads=false (i.e. inside a thread itself).
    const threadAffordance = showThreads ? (
      <ThreadButton
        replyCount={m.reply_count}
        // v2.9.1 P3: dot = NEW activity since last viewed (server-derived), not
        // merely "has any replies".
        hasActivity={!!m.has_new_activity}
        onClick={() => openThread(m)}
      />
    ) : null;

    // Chat UX 2 (#1+#2): own = right-aligned LIGHT-BLUE bubble (#D1E3FF), no
    // avatar (#225). bg-chatuserbubble + FIXED dark text (text-chatbubble-fg, a
    // light==dark token) — NOT a theme token. The bubble surface is a fixed light
    // color in BOTH modes, so text-text-primary (which flips light in dark mode)
    // would be light-on-light-blue = FAIL. The fixed token stays dark on #D1E3FF
    // in both modes (Tester2
    // 13.72 AAA). The header line (name + work-item tag + time) sits ABOVE the
    // bubble, right-aligned; the bubble itself is content-only.
    if (isOwn) {
      return (
        <article
          key={m.id}
          className="flex flex-col items-end text-sm"
          data-testid="message-row"
          data-message-id={m.id}
          data-own="true"
        >
          {headerLine}
          <div className={`${bubbleWidthClass} rounded-2xl bg-chatuserbubble px-3 py-2 text-chatbubble-fg shadow-sm`}>
            {bubbleBody}
          </div>
          {threadAffordance}
        </article>
      );
    }

    // Chat UX 2: other people's messages — avatar + a left-aligned content-only
    // bubble. `bg-bg-subtle` (浅灰, both-mode theme token) + `text-text-primary`
    // stay theme-adaptive (fine — both flip together). The header line (name +
    // work-item tag + time) sits ABOVE the bubble, left-aligned (sharing the
    // avatar column gutter via the same gap).
    return (
      <article
        key={m.id}
        className="flex items-start gap-3 text-sm"
        data-testid="message-row"
        data-message-id={m.id}
        data-own="false"
      >
        {/* 7th/8th redesign: sender avatar (name-hashed gradient + shape
            discriminator). kind from the identity-ref prefix (agent:/user:).
            increment 2: the avatar is a button that opens the sender-detail
            sidebar (keyboard-accessible; aria-label on the button). */}
        <button
          type="button"
          onClick={() => openSender(m.sender_identity_id)}
          aria-label={`View ${avatarName} detail`}
          data-testid="message-sender-avatar-button"
          className="mt-5 shrink-0 rounded-full focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Avatar
            name={avatarName}
            kind={m.sender_identity_id.startsWith('agent:') ? 'agent' : 'human'}
          />
        </button>
        <div className="flex min-w-0 flex-1 flex-col items-start">
          {headerLine}
          {/* DM surface renders received messages as a bordered content card (per
              the 7th-DM mockup); channel/thread surfaces keep the gray pill bubble.
              Both use theme tokens (bg-bg-elevated/bg-bg-subtle + text-text-primary)
              so they read AA in both modes. Own bubble (#D1E3FF) is unchanged. */}
          <div
            className={`${bubbleWidthClass} px-3 py-2 text-text-primary ${
              surface === 'dm'
                ? 'rounded-lg border border-border-base bg-bg-elevated'
                : 'rounded-2xl bg-bg-subtle shadow-sm'
            }`}
            data-surface={surface}
          >
            {bubbleBody}
          </div>
          {threadAffordance}
        </div>
      </article>
    );
  };

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={containerRef}
        onScroll={onScroll}
        // T149: overflow-x-hidden is the page-level guarantee — the message stream
        // scrolls only vertically, never horizontally. Long content wraps
        // (.markdown-body overflow-wrap) or scrolls INSIDE its own block (code /
        // tables), so nothing escapes to push a whole-page horizontal scroll.
        className="min-w-0 flex-1 space-y-3 overflow-y-auto overflow-x-hidden p-4"
        data-testid="message-list"
      >
        {/* T189 phase 2: older-history affordance at the top of the stream. Shown
            only on a paginating surface (onLoadOlder wired) with more history. */}
        {onLoadOlder && hasOlder && (
          <div className="flex justify-center py-1" data-testid="message-list-older">
            <button
              type="button"
              onClick={triggerLoadOlder}
              disabled={isLoadingOlder}
              data-testid="message-list-load-older"
              className="rounded-full bg-bg-subtle px-3 py-1 text-xs font-medium text-text-secondary hover:bg-bg-base disabled:opacity-60"
            >
              {isLoadingOlder ? 'Loading earlier…' : 'Load earlier messages'}
            </button>
          </div>
        )}
        {/* v2.7.1 #219: flat chronological stream (Slack-like); work-item
            provenance shows as a per-message tag, not a grouping header. */}
        {messages.map(renderRow)}
      </div>
      {hasNewBelow && (
        <button
          type="button"
          onClick={jumpToLatest}
          data-testid="message-list-new-pill"
          className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-text-primary px-3 py-1 text-xs font-medium text-bg-elevated shadow-2 hover:opacity-90"
        >
          New messages ↓
        </button>
      )}
      {/* v2.8.1 increment 2: a single sidebar instance at the MessageList root.
          #281: rendered ONLY when there's no surface-level provider — under a
          SenderSidebarProvider (e.g. DMDetail) the provider owns the one sidebar,
          so we don't double-render. */}
      {!providerOpen && (
        <SenderDetailSidebar
          open={sidebarSender !== null}
          senderRef={sidebarSender}
          onClose={() => setSidebarSender(null)}
        />
      )}
      {/* v2.9.1 Threads: local thread sidebar fallback — rendered ONLY when this
          list owns threads (showThreads) AND there is no surface-level provider.
          On real conversation surfaces ConversationView mounts ThreadSidebarProvider
          (added in P2), so providerOpenThread is set and this fallback does NOT
          render (the provider owns the single sidebar — no double-render). The
          fallback covers standalone use (e.g. unit tests with no provider). */}
      {showThreads && !providerOpenThread && (
        <ThreadSidebar
          open={localThreadRoot !== null}
          rootMessage={localThreadRoot}
          onClose={() => setLocalThreadRoot(null)}
        />
      )}
    </div>
  );
}

// SystemNotificationRow (T308) — a SYSTEM-authored message (reminder / scheduler
// / plan-dispatch notice; sender='system', content_kind='text') shown as a
// de-emphasized NOTIFICATION rather than a peer chat bubble: a centered, subtle
// notice card with a bell + "System · time" header and the content rendered as
// markdown (so task/issue/plan refs stay clickable). Distinct from
// SystemMessageRow, which is the terse "Message failed" notice for content_kind
// ='system'. Full-width-ish + centered so it reads as an out-of-band notice.
function SystemNotificationRow({ m }: { m: Message }): React.ReactElement {
  return (
    <div className="my-2 flex justify-center" data-testid="message-system-notice" data-message-system="true">
      <div className="w-full max-w-2xl rounded-md border border-border-base bg-bg-subtle/60 px-3 py-2">
        <div className="mb-1 flex items-center gap-1.5 text-[0.625rem] font-medium uppercase tracking-wide text-text-muted">
          <svg
            viewBox="0 0 24 24"
            className="h-3 w-3 shrink-0"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
            <path d="M13.73 21a2 2 0 0 1-3.46 0" />
          </svg>
          <span>System</span>
          <span className="text-text-muted/70">·</span>
          <time dateTime={m.posted_at} title={m.posted_at} className="font-normal normal-case tracking-normal">
            {formatChatTime(m.posted_at)}
          </time>
        </div>
        <div className="text-xs text-text-secondary">
          <MarkdownMessage content={m.content} textClass="text-text-secondary" linkClass="text-accent" />
        </div>
      </div>
    </div>
  );
}

// SystemMessageRow — de-emphasized rendering for content_kind='system' messages
// (e.g. agent converse failures). A centered hint; the raw message text (which
// may carry an API error) is collapsed behind [Details] so it never dumps into
// the main conversation flow uninvited (@oopslink convention). The warning is an
// SVG (no emoji-icon per the a11y guardrail) on a both-mode-safe warning token.
function SystemMessageRow({ content }: { content: string }): React.ReactElement {
  const [expanded, setExpanded] = useState(false);
  const detailId = useId();
  return (
    <div className="my-2 flex justify-center" data-testid="message-system" data-message-system="true">
      <div className="max-w-md rounded-md border border-warning/30 bg-warning/10 px-3 py-1.5 text-xs text-text-secondary">
        <div className="flex items-center justify-center gap-1.5">
          <svg
            viewBox="0 0 24 24"
            className="h-3.5 w-3.5 shrink-0 text-warning"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
          <span>Message failed</span>
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-accent hover:underline"
            data-testid="message-system-details-toggle"
            aria-expanded={expanded}
            aria-controls={detailId}
          >
            {expanded ? 'Hide' : 'Details'}
          </button>
        </div>
        {expanded && (
          <pre
            id={detailId}
            className="mt-1.5 max-h-48 overflow-auto whitespace-pre-wrap break-words text-left text-[0.625rem] text-text-muted"
            data-testid="message-system-detail"
          >
            {content}
          </pre>
        )}
      </div>
    </div>
  );
}

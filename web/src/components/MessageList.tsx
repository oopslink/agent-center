import type React from 'react';
import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Message, MessageAttachment, QuotedMessagePreview } from '@/api/types';
import { withOrgSlug } from '@/api/client';
import { useDisplayNameResolver, isResolvedName, normalizeIdentityRef, isSystemSender } from '@/api/members';
import { useAppStore } from '@/store/app';
import { Avatar } from './Avatar';
import { formatChatTime, formatLocalTime } from '@/utils/time';
import { IconClose, IconCopy, IconDownload, IconZoomIn, IconZoomOut } from './icons';
import { MarkdownMessage } from './MarkdownMessage';
import { MessageCopyButton } from './MessageCopyButton';
import { MessageQuoteButton } from './MessageQuoteButton';
import { useQuote } from './QuoteContext';
import type { ConversationSurface } from './ConversationView';
import { SenderDetailSidebar } from './SenderDetailSidebar';
import { useSenderSidebar } from './SenderSidebarContext';
import { ThreadButton } from './ThreadButton';
import { ThreadSidebar } from './ThreadSidebar';
import { useThreadSidebar } from './ThreadSidebarContext';
import { ThreadPreview } from './ThreadPreview';
import { useModalA11y } from './useModalA11y';

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
  const { t } = useTranslation('chat');
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
  // T500: whether the stream is scrolled to (near) the bottom. Drives a
  // persistent "jump to bottom" affordance: any time the user has scrolled up to
  // read history — NOT only when a new message arrived (that is the hasNewBelow
  // pill above) — a corner chevron lets them snap back to the latest message.
  // Mirrors stickToBottomRef but as state so the button can re-render on scroll.
  const [atBottom, setAtBottom] = useState(true);
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
  const [viewerAttachment, setViewerAttachment] = useState<MessageAttachment | null>(null);

  // 引用 (quote): the shared quote target (owned by QuoteContext, mounted by
  // ConversationView). Null when rendered standalone (no provider) — the Quote
  // action is then omitted so the list keeps working in tests / inside threads.
  const quote = useQuote();
  // Transient highlight for the message a quote card just scrolled to. Holds the
  // target message id for ~1.5s, then clears; drives a brief ring on the row.
  const [highlightedId, setHighlightedId] = useState<string | null>(null);
  const highlightTimerRef = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(highlightTimerRef.current), []);

  // scrollToMessage — jump to the quoted original and briefly highlight it. Looks
  // the row up by its data-message-id inside the scroll container; a no-op when
  // the target is not in the currently-loaded window.
  const scrollToMessage = (id: string) => {
    const el = containerRef.current?.querySelector<HTMLElement>(`[data-message-id="${CSS.escape(id)}"]`);
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    setHighlightedId(id);
    window.clearTimeout(highlightTimerRef.current);
    highlightTimerRef.current = window.setTimeout(() => setHighlightedId(null), 1500);
  };

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
    const nowAtBottom = distFromBottom < 40;
    stickToBottomRef.current = nowAtBottom;
    setAtBottom(nowAtBottom); // T500: toggle the jump-to-bottom chevron
    if (nowAtBottom && hasNewBelow) setHasNewBelow(false);
    // Near the top → pull the previous (older) page.
    if (el.scrollTop < 80) triggerLoadOlder();
  };

  const jumpToLatest = () => {
    const el = containerRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    stickToBottomRef.current = true;
    setAtBottom(true); // T500
    setHasNewBelow(false);
  };

  if (messages.length === 0) {
    return (
      <div
        className="flex flex-1 items-center justify-center text-sm text-text-muted"
        data-testid="message-list-empty"
      >
        {t('message.empty')}
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
    // 引用: transient highlight ring applied to the row a quote card just jumped
    // to (cleared after ~1.5s by scrollToMessage's timer).
    const rowHighlight =
      highlightedId === m.id
        ? 'rounded-xl ring-2 ring-accent ring-offset-2 ring-offset-bg-base motion-safe:transition-shadow'
        : '';
    const hasCodeBlock = m.content.includes('```');
    // v2.10.1 [M2] mobile: bubbles fill the FULL width of the full-screen
    // conversation (@oopslink: 移动端聊天气泡宽度应该占 100%) — the narrow
    // viewport has no room to waste on side gutters — narrowing back to 75% at
    // the desktop 3-column breakpoint (≥768, where col③ is itself a column).
    // Code-block bubbles already go full-width on mobile (horizontal scroll
    // guard), so they are left as-is.
    const bubbleWidthClass = hasCodeBlock
      ? 'w-full max-w-full sm:w-2/3 sm:max-w-[66.666667%]'
      : 'max-w-full md:max-w-[75%]';

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
              ? t('message.viewSenderDetail', { name: senderName })
              : t('message.viewSenderDetailDeleted', { name: senderHandle })
          }
          title={senderResolved ? m.sender_identity_id : `${senderHandle} (${m.sender_identity_id})`}
          data-testid="message-sender-button"
          data-sender-resolved={senderResolved ? 'true' : 'false'}
          className={`rounded font-medium hover:underline focus-visible:ring-2 focus-visible:ring-accent ${
            senderResolved ? '' : 'italic text-text-secondary'
          }`}
        >
          {senderResolved ? senderName : t('message.deletedSender')}
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
            {t('message.workItem')}
          </span>
        )}
        {/* Chat UX 2 #4 / T751: timestamp in the header line (outside the bubble),
            as a <time dateTime title>. Visible text is formatChatTime (same-day →
            "HH:MM", cross-day → date+time+tz); the tooltip is the full local
            timezone-aware time (formatLocalTime); dateTime keeps the raw ISO. */}
        <time
          className="text-[0.625rem] font-normal text-text-muted"
          dateTime={m.posted_at}
          title={formatLocalTime(m.posted_at)}
          data-testid="message-time"
        >
          {formatChatTime(m.posted_at)}
        </time>
        {/* T246: per-message copy — copies the raw content to the clipboard.
            Lives in the header line so it shares the own/other alignment (the
            line reverses for own messages). */}
        <MessageCopyButton content={m.content} />
        {/* 引用: per-message Quote action — queues this message as the composer's
            quote target. Only shown under a QuoteProvider (the real conversation
            surface); omitted standalone so the list keeps working without it. */}
        {quote && <MessageQuoteButton onQuote={() => quote.setTarget(m)} />}
      </div>
    );

    const openAttachment = (att: MessageAttachment, e?: React.MouseEvent<HTMLElement>) => {
      e?.preventDefault();
      setViewerAttachment(att);
    };

    // Content-only bubble body: markdown + attachments (no name/time). On the OWN
    // bubble the surface is the FIXED light-blue #D1E3FF with FIXED dark text, so
    // the attachment chrome uses the SAME both-mode treatment as the non-own side
    // (theme tokens) — no more white-alpha-on-indigo variants.
    const bubbleBody = (
      <div className="min-w-0">
        {/* 引用: when this message quotes an earlier one, render the quoted
            preview card ABOVE the content (left-bar indented block). Clicking a
            live card scrolls to + highlights the original; a removed target
            degrades to a muted, non-clickable "original unavailable" placeholder. */}
        {m.quoted_message && (
          <QuotedPreviewCard
            quoted={m.quoted_message}
            displayName={displayName}
            onJump={scrollToMessage}
            isOwn={isOwn}
          />
        )}
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
            {m.attachments.map((att) => {
              const href = attachmentHref(att.uri);
              return (
                <li
                  key={att.uri}
                  className="flex min-w-0 max-w-full items-center gap-2 rounded border border-border-base bg-bg-base px-2 py-1 text-xs"
                  data-testid="message-attachment"
                  data-mime={att.mime_type}
                >
                  {att.mime_type.startsWith('image/') && (
                    <button
                      type="button"
                      onClick={(e) => openAttachment(att, e)}
                      aria-label={t('message.openAttachment', { filename: att.filename })}
                      className="shrink-0 rounded focus-visible:ring-2 focus-visible:ring-accent"
                      data-testid="attachment-preview-button"
                    >
                      <img
                        src={href}
                        alt={att.filename}
                        className="h-10 w-10 rounded object-cover"
                        data-testid="attachment-preview"
                      />
                    </button>
                  )}
                  <a
                    href={href}
                    onClick={(e) => openAttachment(att, e)}
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
              );
            })}
          </ul>
        )}
      </div>
    );

    // v2.9.1 Threads: the per-message thread affordance (count chip + activity
    // dot), aligned to the message's side. Opens the thread sidebar for THIS
    // message. Omitted when showThreads=false (i.e. inside a thread itself).
    const threadAffordance = showThreads ? (
      (m.reply_count ?? 0) > 0 ? (
        <ThreadPreview
          rootMessage={m}
          align={isOwn ? 'right' : 'left'}
          // v2.9.1 P3: dot = NEW activity since last viewed (server-derived), not
          // merely "has any replies".
          hasActivity={!!m.has_new_activity}
          onOpenThread={() => openThread(m)}
        />
      ) : (
        <ThreadButton
          replyCount={m.reply_count}
          hasActivity={!!m.has_new_activity}
          onClick={() => openThread(m)}
        />
      )
    ) : null;

    // Hex-inspired: own = right-aligned eggplant bubble (light: #31263B,
    // dark: #a78bfa). bg-chatuserbubble + FIXED text (text-chatbubble-fg).
    // The bubble surface is a fixed branded color — text tokens stay per-mode.
    if (isOwn) {
      return (
        <article
          key={m.id}
          className={`flex flex-col items-end text-sm ${rowHighlight}`}
          data-testid="message-row"
          data-message-id={m.id}
          data-own="true"
        >
          {headerLine}
          <div className={`${bubbleWidthClass} rounded-xl bg-chatuserbubble px-3.5 py-2.5 text-chatbubble-fg shadow-sm`}>
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
        className={`flex items-start gap-3 text-sm ${rowHighlight}`}
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
          aria-label={t('message.viewSenderDetail', { name: avatarName })}
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
          {/* Hex-inspired: all "other" bubbles use bordered card style
              (bg-subtle + border) for both DM and channel surfaces. */}
          <div
            className={`${bubbleWidthClass} rounded-xl border border-border-base bg-bg-subtle px-3.5 py-2.5 text-text-primary`}
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
        className="min-w-0 flex-1 space-y-4 overflow-y-auto overflow-x-hidden p-4 md:px-6 md:py-5"
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
              {isLoadingOlder ? t('message.loadingEarlier') : t('message.loadEarlier')}
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
          className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-btn-primary-bg px-3 py-1 text-xs font-medium text-btn-primary-fg shadow-2 hover:opacity-90"
        >
          {t('message.newMessages')}
        </button>
      )}
      {/* T500: persistent "jump to bottom" affordance. Shown whenever the user
          has scrolled up away from the latest message AND there is no new-message
          pill already inviting them down (mutually exclusive so the two never
          overlap). A compact corner chevron — clicking snaps to the latest. */}
      {!atBottom && !hasNewBelow && (
        <button
          type="button"
          onClick={jumpToLatest}
          data-testid="message-list-jump-bottom"
          title={t('message.jumpToLatest')}
          aria-label={t('message.jumpToLatest')}
          className="absolute bottom-3 right-3 flex h-8 w-8 items-center justify-center rounded-full bg-bg-subtle text-text-secondary shadow-2 hover:bg-bg-base hover:text-text-primary"
        >
          <svg
            viewBox="0 0 16 16"
            className="h-4 w-4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.75"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M4 6l4 4 4-4" />
          </svg>
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
      {viewerAttachment && (
        <AttachmentViewerModal
          attachment={viewerAttachment}
          onClose={() => setViewerAttachment(null)}
        />
      )}
    </div>
  );
}

const ATTACHMENT_ZOOM_MIN = 0.5;
const ATTACHMENT_ZOOM_MAX = 3;
const ATTACHMENT_ZOOM_STEP = 0.25;

function AttachmentViewerModal({
  attachment,
  onClose,
}: {
  attachment: MessageAttachment;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('chat');
  const [zoom, setZoom] = useState(1);
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const titleId = useId();
  const copyTimerRef = useRef<number | undefined>(undefined);
  const dialogRef = useModalA11y({ open: true, onClose });
  const href = attachmentHref(attachment.uri);
  const zoomPct = Math.round(zoom * 100);

  useEffect(() => {
    setZoom(1);
    setCopyState('idle');
  }, [attachment.uri]);

  useEffect(() => () => window.clearTimeout(copyTimerRef.current), []);

  const markCopyState = (state: 'copied' | 'failed') => {
    setCopyState(state);
    window.clearTimeout(copyTimerRef.current);
    copyTimerRef.current = window.setTimeout(() => setCopyState('idle'), 1800);
  };

  const copyLink = () => {
    const text = href === '#' ? attachment.filename : new URL(href, window.location.href).toString();
    if (!navigator.clipboard?.writeText) {
      markCopyState('failed');
      return;
    }
    void navigator.clipboard.writeText(text).then(
      () => markCopyState('copied'),
      () => markCopyState('failed'),
    );
  };

  const zoomOut = () => setZoom((z) => Math.max(ATTACHMENT_ZOOM_MIN, z - ATTACHMENT_ZOOM_STEP));
  const zoomIn = () => setZoom((z) => Math.min(ATTACHMENT_ZOOM_MAX, z + ATTACHMENT_ZOOM_STEP));
  const resetZoom = () => setZoom(1);

  const closeFromBackdrop = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) onClose();
  };

  const copyStatus =
    copyState === 'copied'
      ? t('message.attachmentViewer.copied')
      : copyState === 'failed'
        ? t('message.attachmentViewer.copyFailed')
        : '';

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-3"
      onMouseDown={closeFromBackdrop}
      data-testid="attachment-viewer-backdrop"
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="flex max-h-[92vh] w-full max-w-6xl flex-col overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-2"
        data-testid="attachment-viewer"
      >
        <div className="flex min-h-14 flex-wrap items-center gap-3 border-b border-border-base px-3 py-2">
          <div className="min-w-0 flex-1 basis-48">
            <h2 id={titleId} className="truncate text-sm font-semibold text-text-primary" title={attachment.filename}>
              {attachment.filename}
            </h2>
            <div className="mt-0.5 flex items-center gap-2 text-xs text-text-muted">
              <span>{attachmentKind(attachment.mime_type)}</span>
              <span>{formatBytes(attachment.size)}</span>
            </div>
          </div>
          <div className="flex max-w-full shrink-0 items-center gap-1 overflow-x-auto">
            <AttachmentViewerIconButton
              label={t('message.attachmentViewer.zoomOut')}
              testId="attachment-viewer-zoom-out"
              onClick={zoomOut}
              disabled={zoom <= ATTACHMENT_ZOOM_MIN}
            >
              <IconZoomOut />
            </AttachmentViewerIconButton>
            <button
              type="button"
              onClick={resetZoom}
              aria-label={t('message.attachmentViewer.resetZoom')}
              title={t('message.attachmentViewer.resetZoom')}
              className="h-8 min-w-12 rounded border border-border-base bg-bg-base px-2 text-xs font-medium tabular-nums text-text-secondary hover:bg-bg-subtle focus-visible:ring-2 focus-visible:ring-accent"
              data-testid="attachment-viewer-zoom-reset"
            >
              <span data-testid="attachment-viewer-zoom-pct">{zoomPct}%</span>
            </button>
            <AttachmentViewerIconButton
              label={t('message.attachmentViewer.zoomIn')}
              testId="attachment-viewer-zoom-in"
              onClick={zoomIn}
              disabled={zoom >= ATTACHMENT_ZOOM_MAX}
            >
              <IconZoomIn />
            </AttachmentViewerIconButton>
            <AttachmentViewerIconButton
              label={t('message.attachmentViewer.copyLink')}
              testId="attachment-viewer-copy"
              onClick={copyLink}
            >
              <IconCopy />
            </AttachmentViewerIconButton>
            <a
              href={href}
              download={attachment.filename}
              onClick={(e) => {
                if (href === '#') e.preventDefault();
              }}
              aria-label={t('message.attachmentViewer.download')}
              title={t('message.attachmentViewer.download')}
              className="flex h-8 w-8 items-center justify-center rounded border border-border-base bg-bg-base text-text-secondary hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent"
              data-testid="attachment-viewer-download"
            >
              <IconDownload />
            </a>
            <AttachmentViewerIconButton
              label={t('message.attachmentViewer.close')}
              testId="attachment-viewer-close"
              onClick={onClose}
            >
              <IconClose />
            </AttachmentViewerIconButton>
          </div>
        </div>
        <div className="min-h-0 flex-1 bg-bg-base">
          <AttachmentPreview attachment={attachment} href={href} zoom={zoom} />
        </div>
        <div className="min-h-6 border-t border-border-base px-3 py-1 text-xs text-text-muted" aria-live="polite" data-testid="attachment-viewer-copy-status">
          {copyStatus}
        </div>
      </div>
    </div>
  );
}

function AttachmentPreview({
  attachment,
  href,
  zoom,
}: {
  attachment: MessageAttachment;
  href: string;
  zoom: number;
}): React.ReactElement {
  const { t } = useTranslation('chat');
  const mode = attachmentPreviewMode(attachment, href);

  if (mode === 'markdown') {
    return <MarkdownAttachmentPreview href={href} zoom={zoom} />;
  }

  if (mode === 'image') {
    return (
      <div className="flex h-[70vh] min-h-0 items-start justify-center overflow-auto p-4" data-testid="attachment-viewer-preview">
        <img
          src={href}
          alt={attachment.filename}
          className="h-auto max-h-none object-contain"
          style={{ width: `${zoom * 100}%`, maxWidth: 'none' }}
          data-testid="attachment-viewer-image"
        />
      </div>
    );
  }

  if (mode === 'video') {
    return (
      <div className="flex h-[70vh] items-center justify-center overflow-auto p-4" data-testid="attachment-viewer-preview">
        <video
          src={href}
          controls
          className="max-h-full max-w-full"
          style={{ width: `${zoom * 100}%`, maxWidth: 'none' }}
          data-testid="attachment-viewer-video"
        />
      </div>
    );
  }

  if (mode === 'audio') {
    return (
      <div className="flex h-[70vh] items-center justify-center overflow-auto p-4" data-testid="attachment-viewer-preview">
        <audio src={href} controls className="w-full max-w-2xl" data-testid="attachment-viewer-audio" />
      </div>
    );
  }

  if (mode === 'frame') {
    return (
      <div className="h-[70vh] overflow-auto p-4" data-testid="attachment-viewer-preview">
        <div
          className="origin-top-left rounded border border-border-base bg-white"
          style={{ width: `${zoom * 100}%`, height: `${zoom * 68}vh` }}
        >
          <iframe
            src={href}
            title={attachment.filename}
            sandbox=""
            className="h-full w-full border-0 bg-white"
            style={{ width: `${100 / zoom}%`, height: '68vh', transform: `scale(${zoom})`, transformOrigin: 'top left' }}
            data-testid="attachment-viewer-frame"
          />
        </div>
      </div>
    );
  }

  return (
    <div className="flex h-[70vh] items-center justify-center p-4" data-testid="attachment-viewer-preview">
      <div className="max-w-md text-center">
        <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded border border-border-base bg-bg-subtle font-mono text-xs font-semibold text-text-muted">
          {attachmentKind(attachment.mime_type)}
        </div>
        <p className="break-words text-sm font-medium text-text-primary">{attachment.filename}</p>
        <p className="mt-1 text-xs text-text-muted">{t('message.attachmentViewer.previewUnavailable')}</p>
      </div>
    </div>
  );
}

function MarkdownAttachmentPreview({
  href,
  zoom,
}: {
  href: string;
  zoom: number;
}): React.ReactElement {
  const { t } = useTranslation('chat');
  const [state, setState] = useState<
    | { status: 'loading'; content: string; error: string }
    | { status: 'loaded'; content: string; error: string }
    | { status: 'error'; content: string; error: string }
  >({ status: 'loading', content: '', error: '' });

  useEffect(() => {
    const controller = new AbortController();
    setState({ status: 'loading', content: '', error: '' });
    void fetch(href, { credentials: 'same-origin', signal: controller.signal }).then(
      async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`.trim());
        const text = await res.text();
        setState({ status: 'loaded', content: text, error: '' });
      },
      (err: unknown) => {
        if (controller.signal.aborted) return;
        setState({
          status: 'error',
          content: '',
          error: err instanceof Error ? err.message : t('message.attachmentViewer.previewUnavailable'),
        });
      },
    );
    return () => controller.abort();
  }, [href, t]);

  return (
    <div className="h-[70vh] overflow-auto p-4" data-testid="attachment-viewer-preview">
      <div
        className="origin-top-left rounded border border-border-base bg-bg-elevated p-5 shadow-1"
        style={{ width: `${zoom * 100}%`, maxWidth: 'none' }}
        data-testid="attachment-viewer-markdown"
      >
        {state.status === 'loading' && (
          <p className="text-sm text-text-muted" data-testid="attachment-viewer-markdown-loading">
            {t('message.attachmentViewer.loadingPreview')}
          </p>
        )}
        {state.status === 'error' && (
          <div className="text-sm text-danger" data-testid="attachment-viewer-markdown-error">
            {state.error || t('message.attachmentViewer.previewUnavailable')}
          </div>
        )}
        {state.status === 'loaded' && (
          <MarkdownMessage content={state.content} textClass="text-text-primary" linkClass="text-accent" />
        )}
      </div>
    </div>
  );
}

function AttachmentViewerIconButton({
  label,
  testId,
  onClick,
  disabled = false,
  children,
}: {
  label: string;
  testId: string;
  onClick: () => void;
  disabled?: boolean;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      className="flex h-8 w-8 items-center justify-center rounded border border-border-base bg-bg-base text-text-secondary hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-45"
      data-testid={testId}
    >
      {children}
    </button>
  );
}

function attachmentPreviewMode(
  attachment: MessageAttachment,
  href: string,
): 'image' | 'video' | 'audio' | 'markdown' | 'frame' | 'none' {
  if (href === '#') return 'none';
  const mime = attachment.mime_type.toLowerCase();
  const name = attachment.filename.toLowerCase();
  if (mime.startsWith('image/')) return 'image';
  if (mime.startsWith('video/')) return 'video';
  if (mime.startsWith('audio/')) return 'audio';
  if (mime === 'text/markdown' || mime === 'text/x-markdown' || /\.(md|markdown|mdown|mkdn)$/i.test(name)) {
    return 'markdown';
  }
  if (
    mime === 'application/pdf' ||
    mime === 'text/html' ||
    mime.startsWith('text/') ||
    mime.includes('json') ||
    mime.includes('xml') ||
    /\.(html?|pdf|txt|md|json|csv|log|xml|svg)$/i.test(name)
  ) {
    return 'frame';
  }
  return 'none';
}

// SystemNotificationRow (T308) — a SYSTEM-authored message (reminder / scheduler
// / plan-dispatch notice; sender='system', content_kind='text') shown as a
// de-emphasized NOTIFICATION rather than a peer chat bubble: a centered, subtle
// notice card with a bell + "System · time" header and the content rendered as
// markdown (so task/issue/plan refs stay clickable). Distinct from
// SystemMessageRow, which is the terse "Message failed" notice for content_kind
// ='system'. Full-width-ish + centered so it reads as an out-of-band notice.
function SystemNotificationRow({ m }: { m: Message }): React.ReactElement {
  const { t } = useTranslation('chat');
  // T316: system notices can be long (reminder/scheduler dumps) — render them
  // COLLAPSED by default (@oopslink), header + one-line preview, with the header
  // as the expand/collapse toggle (chevron rotates). Full markdown on expand.
  const [expanded, setExpanded] = useState(false);
  const bodyId = useId();
  const preview = m.content.replace(/\s+/g, ' ').trim();
  return (
    <div className="my-2 flex justify-center" data-testid="message-system-notice" data-message-system="true">
      <div className="w-full max-w-2xl rounded-md border border-border-base bg-bg-subtle/60 px-3 py-2">
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
          aria-controls={bodyId}
          className="flex w-full items-center gap-1.5 min-h-[36px] px-2 text-left text-[0.625rem] font-medium uppercase tracking-wide text-text-muted"
          data-testid="message-system-toggle"
        >
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
          <span>{t('message.system')}</span>
          <span className="text-text-muted/70">·</span>
          <time dateTime={m.posted_at} title={formatLocalTime(m.posted_at)} className="font-normal normal-case tracking-normal">
            {formatChatTime(m.posted_at)}
          </time>
          <svg
            viewBox="0 0 12 12"
            aria-hidden="true"
            className={`ml-auto h-3 w-3 shrink-0 transition-transform ${expanded ? 'rotate-90' : ''}`}
          >
            <path d="M4 2l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
        {expanded ? (
          <div id={bodyId} className="mt-1 text-xs text-text-secondary" data-testid="message-system-body">
            <MarkdownMessage content={m.content} textClass="text-text-secondary" linkClass="text-accent" />
          </div>
        ) : (
          <div
            className="mt-0.5 truncate text-xs text-text-muted"
            data-testid="message-system-preview"
            title={preview}
          >
            {preview}
          </div>
        )}
      </div>
    </div>
  );
}

// QuotedPreviewCard (引用) — the inlined preview of the message a quoting message
// points at, rendered as a left-bar indented block ABOVE the quoting message's
// content. A LIVE target is a <button> that scrolls to + highlights the original
// (onJump); a REMOVED target (is_deleted) degrades to a muted, non-clickable
// "original unavailable" placeholder. On the own bubble the surface is the FIXED
// light-blue #D1E3FF, so the bar/text use the same fixed dark chatbubble tokens
// as the body; the other bubble is theme-adaptive.
function QuotedPreviewCard({
  quoted,
  displayName,
  onJump,
  isOwn,
}: {
  quoted: QuotedMessagePreview;
  displayName: (ref: string) => string;
  onJump: (id: string) => void;
  isOwn: boolean;
}): React.ReactElement {
  const { t } = useTranslation('chat');
  const barClass = isOwn ? 'border-chatbubble-fg/40' : 'border-accent';
  const nameClass = isOwn ? 'text-chatbubble-fg' : 'text-text-secondary';
  const snippetClass = isOwn ? 'text-chatbubble-fg/80' : 'text-text-muted';

  // Removed target: a muted, non-interactive placeholder (no scroll target).
  if (quoted.is_deleted) {
    return (
      <div
        className={`mb-1.5 border-l-2 pl-2 text-xs italic ${barClass} ${snippetClass}`}
        data-testid="message-quote-card"
        data-quote-deleted="true"
      >
        {t('message.quote.originalUnavailable')}
      </div>
    );
  }

  const senderName = quoted.sender_identity_id ? displayName(quoted.sender_identity_id) : '';
  const senderHandle = quoted.sender_identity_id
    ? normalizeIdentityRef(quoted.sender_identity_id)
    : '';
  const resolved = quoted.sender_identity_id
    ? isResolvedName(quoted.sender_identity_id, senderName)
    : false;
  return (
    <button
      type="button"
      onClick={() => onJump(quoted.id)}
      aria-label={t('message.quote.jumpToOriginal')}
      title={t('message.quote.jumpToOriginal')}
      data-testid="message-quote-card"
      data-quote-target={quoted.id}
      className={`mb-1.5 flex w-full flex-col items-start rounded-r border-l-2 pl-2 text-left hover:opacity-80 focus-visible:ring-2 focus-visible:ring-accent motion-safe:transition-opacity ${barClass}`}
    >
      {/* Resolved name, else the CLEAN handle (prefix stripped) — never the raw
          `agent:agent-xxx` ref (#192); the full ref stays on the title below. */}
      <span
        className={`text-xs font-medium ${nameClass}`}
        title={quoted.sender_identity_id ?? undefined}
      >
        {resolved ? senderName : senderHandle}
      </span>
      <span className={`max-w-full truncate text-xs ${snippetClass}`} title={quoted.content_snippet}>
        {quoted.content_snippet}
      </span>
    </button>
  );
}

// SystemMessageRow — de-emphasized rendering for content_kind='system' messages
// (e.g. agent converse failures). A centered hint; the raw message text (which
// may carry an API error) is collapsed behind [Details] so it never dumps into
// the main conversation flow uninvited (@oopslink convention). The warning is an
// SVG (no emoji-icon per the a11y guardrail) on a both-mode-safe warning token.
function SystemMessageRow({ content }: { content: string }): React.ReactElement {
  const { t } = useTranslation('chat');
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
          <span>{t('message.messageFailed')}</span>
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-accent hover:underline"
            data-testid="message-system-details-toggle"
            aria-expanded={expanded}
            aria-controls={detailId}
          >
            {expanded ? t('message.hide') : t('message.details')}
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

import type React from 'react';
import { useEffect, useId, useRef, useState } from 'react';
import type { Message } from '@/api/types';
import { withOrgSlug } from '@/api/client';
import { useDisplayNameResolver, normalizeIdentityRef } from '@/api/members';
import { useAppStore } from '@/store/app';
import { Avatar } from './Avatar';
import { formatChatTime } from '@/utils/time';
import { MarkdownMessage } from './MarkdownMessage';
import { SenderDetailSidebar } from './SenderDetailSidebar';

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
// carrying the current org scope (?org_slug=) the same way the api client does —
// GET /api/files/{ulid} runs requireOrgMember, which 400s without an org scope.
export function attachmentHref(uri: string): string {
  const prefix = 'ac://files/';
  if (!uri.startsWith(prefix)) return '#';
  return `/api${withOrgSlug(`/files/${encodeURIComponent(uri.slice(prefix.length))}`)}`;
}

interface Props {
  messages: Message[];
}

// MessageList — render messages chronologically. Sender id + posted_at
// + content. No virtual scrolling yet (deferred to M3 per F6 oversight
// #2 — happy path doesn't need it).
//
// Auto-scroll behavior (v2.5.6 #60): when a new message arrives, scroll
// to bottom — but only if the user is already near the bottom. If they
// scrolled up to read history, we don't yank them back.
export function MessageList({ messages }: Props): React.ReactElement {
  const displayName = useDisplayNameResolver();
  // v2.8.1 chat-rightalign: the viewer's own messages render right-aligned
  // (iMessage/Slack style). `currentUserId` is a prefixed identity ref
  // (e.g. "user:hayang"); normalize BOTH sides so the user:/agent: prefix
  // never breaks the compare. Empty `me` (not yet bound) => nothing is "own".
  const me = useAppStore((s) => s.currentUserId);
  const meKey = me ? normalizeIdentityRef(me) : '';
  const containerRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);
  const latestId = messages[messages.length - 1]?.id;
  const prevLatestIdRef = useRef<string | undefined>(undefined);
  // Re-render trigger so the "New messages ↓" pill appears when a new
  // message arrives while the user is scrolled up; cleared on click or
  // when the user scrolls back to the bottom.
  const [hasNewBelow, setHasNewBelow] = useState(false);
  // v2.8.1 7th DM increment 2: the sender-detail sidebar. Holds the clicked
  // message's sender identity ref (prefixed, e.g. "agent:A-1"); null = closed.
  const [sidebarSender, setSidebarSender] = useState<string | null>(null);

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

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    const distFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distFromBottom < 40;
    stickToBottomRef.current = atBottom;
    if (atBottom && hasNewBelow) setHasNewBelow(false);
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
    // v2.8.1 chat-rightalign: own = the viewer's own message. Normalize both
    // sides so the user:/agent: prefix never breaks the compare.
    const isOwn = meKey !== '' && normalizeIdentityRef(m.sender_identity_id) === meKey;

    // Chat UX 2 (#3 + #5): the sender NAME (+ work-item tag) and the TIME move
    // OUT of the bubble into a small header line ABOVE the bubble; the bubble is
    // now content-ONLY (markdown body + attachments). The header line is right-
    // aligned for own messages, left-aligned for others. Header is muted theme-
    // adaptive text on BOTH sides (it sits on the page surface, never inside the
    // fixed light-blue bubble) — so it uses normal theme tokens.
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
          onClick={() => setSidebarSender(m.sender_identity_id)}
          aria-label={`View ${displayName(m.sender_identity_id)} detail`}
          title={m.sender_identity_id}
          data-testid="message-sender-button"
          className="rounded font-medium hover:underline focus-visible:ring-2 focus-visible:ring-accent"
        >
          {displayName(m.sender_identity_id)}
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
            (text-slate-900) in both modes — the default theme token would flip
            light in dark mode = light-on-light-blue FAIL. Other bubble is
            theme-adaptive (bg-bg-subtle), so it keeps the default token. */}
        <MarkdownMessage
          content={m.content}
          textClass={isOwn ? 'text-slate-900' : 'text-text-primary'}
          linkClass={isOwn ? 'text-blue-700' : 'text-accent'}
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
                className="flex items-center gap-2 rounded border border-border-base bg-bg-base px-2 py-1 text-xs"
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
                  className="flex items-center gap-2 text-text-primary hover:underline"
                  data-testid="attachment-link"
                >
                  <span
                    className="rounded bg-bg-base px-1 font-mono uppercase text-text-muted"
                    data-testid="attachment-type"
                  >
                    {attachmentKind(att.mime_type)}
                  </span>
                  <span>{att.filename}</span>
                </a>
                <span className="text-text-muted">{formatBytes(att.size)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    );

    // Chat UX 2 (#1+#2): own = right-aligned LIGHT-BLUE bubble (#D1E3FF), no
    // avatar (#225). bg-chatuserbubble + FIXED dark text (text-slate-900) — NOT a
    // theme token. The bubble surface is a fixed light color in BOTH modes, so
    // text-text-primary (which flips light in dark mode) would be light-on-light-
    // blue = FAIL. text-slate-900 stays dark on #D1E3FF in both modes (Tester2
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
          <div className="max-w-[75%] rounded-2xl bg-chatuserbubble px-3 py-2 text-slate-900 shadow-sm">
            {bubbleBody}
          </div>
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
          onClick={() => setSidebarSender(m.sender_identity_id)}
          aria-label={`View ${displayName(m.sender_identity_id)} detail`}
          data-testid="message-sender-avatar-button"
          className="mt-5 shrink-0 rounded-full focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Avatar
            name={displayName(m.sender_identity_id)}
            kind={m.sender_identity_id.startsWith('agent:') ? 'agent' : 'human'}
          />
        </button>
        <div className="flex min-w-0 flex-col items-start">
          {headerLine}
          <div className="max-w-[75%] rounded-2xl bg-bg-subtle px-3 py-2 text-text-primary shadow-sm">
            {bubbleBody}
          </div>
        </div>
      </article>
    );
  };

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={containerRef}
        onScroll={onScroll}
        className="flex-1 space-y-3 overflow-y-auto p-4"
        data-testid="message-list"
      >
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
      {/* v2.8.1 increment 2: a single sidebar instance at the MessageList root. */}
      <SenderDetailSidebar
        open={sidebarSender !== null}
        senderRef={sidebarSender}
        onClose={() => setSidebarSender(null)}
      />
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

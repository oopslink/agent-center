import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import type { Message } from '@/api/types';
import { withOrgSlug } from '@/api/client';
import { useDisplayNameResolver } from '@/api/members';
import { Avatar } from './Avatar';
import { formatLocalTime } from '@/utils/time';
import { MarkdownMessage } from './MarkdownMessage';

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
  const containerRef = useRef<HTMLDivElement | null>(null);
  const stickToBottomRef = useRef(true);
  const latestId = messages[messages.length - 1]?.id;
  const prevLatestIdRef = useRef<string | undefined>(undefined);
  // Re-render trigger so the "New messages ↓" pill appears when a new
  // message arrives while the user is scrolled up; cleared on click or
  // when the user scrolls back to the bottom.
  const [hasNewBelow, setHasNewBelow] = useState(false);

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
    return (
      <article
        key={m.id}
        className="flex gap-3 rounded border border-border-base bg-bg-elevated p-3 text-sm shadow-sm"
        data-testid="message-row"
        data-message-id={m.id}
      >
        {/* 7th/8th redesign: sender avatar (name-hashed gradient + shape
            discriminator). kind from the identity-ref prefix (agent:/user:). */}
        <Avatar
          name={displayName(m.sender_identity_id)}
          kind={m.sender_identity_id.startsWith('agent:') ? 'agent' : 'human'}
        />
        <div className="min-w-0 flex-1">
          <header className="mb-1 flex items-center justify-between text-xs text-text-muted">
            <span className="flex items-center gap-2">
              <span title={m.sender_identity_id}>{displayName(m.sender_identity_id)}</span>
              {/* v2.7.1 #219: per-message work-item tag (only when the message
                  carries one); the raw ref stays on hover (#192 chrome rule). */}
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
            </span>
            <time dateTime={m.posted_at} title={m.posted_at}>{formatLocalTime(m.posted_at)}</time>
          </header>
          {/* #276: message content renders as markdown (GFM + strict-escape);
              long fenced code collapses via the shared CollapsibleCodeBlock. */}
          <MarkdownMessage content={m.content} />
          {/* v2.7 #142: attachments download through the same gated /api/files/{id}
              endpoint used by the backend reachability checks. */}
          {m.attachments && m.attachments.length > 0 && (
            <ul className="mt-1 flex flex-wrap gap-2" data-testid="message-attachments">
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
                      className="rounded bg-bg-elevated px-1 font-mono uppercase text-text-muted"
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
    </div>
  );
}

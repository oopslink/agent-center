import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import type { Message } from '@/api/types';
import { groupMessagesByWorkItem } from './messageSegments';

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

interface Props {
  messages: Message[];
  // Optional selection mode (per F9 derive UI). When `selectable` is
  // true each row prepends a checkbox driven by `isSelected` / `onToggle`.
  selectable?: boolean;
  isSelected?: (id: string) => boolean;
  onToggle?: (id: string) => void;
  // v2.7 #137: when true (task/issue conversations) the list is split into
  // labeled segments by work_item_ref. Off for channels/DMs, which carry no
  // work-item provenance and render as a flat list.
  segmentByWorkItem?: boolean;
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
  selectable = false,
  isSelected,
  onToggle,
  segmentByWorkItem = false,
}: Props): React.ReactElement {
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
    const checked = selectable && !!isSelected?.(m.id);
    return (
      <article
        key={m.id}
        className={[
          'flex gap-3 rounded border bg-bg-elevated p-3 text-sm shadow-sm',
          checked ? 'border-accent ring-1 ring-accent/40' : 'border-border-base',
        ].join(' ')}
        data-testid="message-row"
        data-message-id={m.id}
        data-selected={checked}
      >
        {selectable && (
          <label className="flex items-start pt-0.5">
            <input
              type="checkbox"
              checked={checked}
              onChange={() => onToggle?.(m.id)}
              className="h-4 w-4 cursor-pointer"
              data-testid="message-select"
              aria-label={`select message ${m.id}`}
            />
          </label>
        )}
        <div className="flex-1">
          <header className="mb-1 flex items-center justify-between text-xs text-text-muted">
            <span className="font-mono">{m.sender_identity_id}</span>
            <time>{m.posted_at}</time>
          </header>
          <div className="whitespace-pre-wrap text-text-primary">{m.content}</div>
          {/* v2.7 #133: render message attachments as METADATA chips only
              (filename / type / size). No download affordance here — download +
              attach-time authz land in #142; until then a download link would 403
              (gated, leak-safe) and read as a false affordance. Only when present,
              so the #137 work_item segmentation below is unaffected. */}
          {m.attachments && m.attachments.length > 0 && (
            <ul className="mt-1 flex flex-wrap gap-2" data-testid="message-attachments">
              {m.attachments.map((att) => (
                <li
                  key={att.uri}
                  className="flex items-center gap-2 rounded border border-border-base bg-bg-base px-2 py-1 text-xs"
                  data-testid="message-attachment"
                  data-mime={att.mime_type}
                  title="Download coming soon (#142)"
                >
                  <span
                    className="rounded bg-bg-elevated px-1 font-mono uppercase text-text-muted"
                    data-testid="attachment-type"
                  >
                    {attachmentKind(att.mime_type)}
                  </span>
                  <span className="text-text-primary">{att.filename}</span>
                  <span className="text-text-muted">{formatBytes(att.size)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </article>
    );
  };

  const segments = segmentByWorkItem ? groupMessagesByWorkItem(messages) : null;

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={containerRef}
        onScroll={onScroll}
        className="flex-1 space-y-3 overflow-y-auto p-4"
        data-testid="message-list"
      >
        {segments
          ? segments.map((seg) => (
              <section
                key={seg.key}
                className="space-y-3"
                data-testid="message-segment"
                data-work-item-ref={seg.workItemRef}
              >
                <h3
                  className="sticky top-0 z-10 bg-bg-base/95 py-1 text-xs font-semibold uppercase tracking-wide text-text-muted"
                  data-testid="message-segment-header"
                >
                  {seg.label}
                </h3>
                {seg.messages.map(renderRow)}
              </section>
            ))
          : messages.map(renderRow)}
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

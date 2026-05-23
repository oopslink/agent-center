import type React from 'react';
import type { Message } from '@/api/types';

interface Props {
  messages: Message[];
  // Optional selection mode (per F9 derive UI). When `selectable` is
  // true each row prepends a checkbox driven by `isSelected` / `onToggle`.
  selectable?: boolean;
  isSelected?: (id: string) => boolean;
  onToggle?: (id: string) => void;
}

// MessageList — render messages chronologically. Sender id + posted_at
// + content. No virtual scrolling yet (deferred to M3 per F6 oversight
// #2 — happy path doesn't need it).
export function MessageList({
  messages,
  selectable = false,
  isSelected,
  onToggle,
}: Props): React.ReactElement {
  if (messages.length === 0) {
    return (
      <div
        className="flex flex-1 items-center justify-center text-sm text-slate-400"
        data-testid="message-list-empty"
      >
        No messages yet.
      </div>
    );
  }
  return (
    <div
      className="flex-1 space-y-3 overflow-y-auto p-4"
      data-testid="message-list"
    >
      {messages.map((m) => {
        const checked = selectable && !!isSelected?.(m.id);
        return (
          <article
            key={m.id}
            className={[
              'flex gap-3 rounded border bg-white p-3 text-sm shadow-sm',
              checked ? 'border-blue-400 ring-1 ring-blue-200' : 'border-slate-200',
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
              <header className="mb-1 flex items-center justify-between text-xs text-slate-500">
                <span className="font-mono">{m.sender_identity_id}</span>
                <time>{m.posted_at}</time>
              </header>
              <div className="whitespace-pre-wrap text-slate-900">{m.content}</div>
            </div>
          </article>
        );
      })}
    </div>
  );
}

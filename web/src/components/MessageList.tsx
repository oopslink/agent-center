import type React from 'react';
import type { Message } from '@/api/types';

interface Props {
  messages: Message[];
}

// MessageList — render messages chronologically. Sender id + posted_at
// + content. No virtual scrolling yet (deferred to M3 per F6 oversight
// #2 — happy path doesn't need it).
export function MessageList({ messages }: Props): React.ReactElement {
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
      {messages.map((m) => (
        <article
          key={m.id}
          className="rounded border border-slate-200 bg-white p-3 text-sm shadow-sm"
          data-testid="message-row"
          data-message-id={m.id}
        >
          <header className="mb-1 flex items-center justify-between text-xs text-slate-500">
            <span className="font-mono">{m.sender_identity_id}</span>
            <time>{m.posted_at}</time>
          </header>
          <div className="whitespace-pre-wrap text-slate-900">{m.content}</div>
        </article>
      ))}
    </div>
  );
}

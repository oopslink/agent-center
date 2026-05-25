import type React from 'react';
import { useState } from 'react';
import { useSendMessage } from '@/api/conversations';

interface Props {
  conversationId: string;
}

// MessageComposer — single-line textarea + Send button. Per F6 oversight
// #3: Enter sends; Shift+Enter inserts a newline; submit is disabled
// while the mutation is pending; clears on success.
//
// Owns its own draft state (component-local — not server, not Zustand).
export function MessageComposer({ conversationId }: Props): React.ReactElement {
  const [draft, setDraft] = useState('');
  const send = useSendMessage();
  const disabled = !draft.trim() || send.isPending;

  const submit = async () => {
    if (disabled) return;
    const content = draft.trim();
    try {
      await send.mutateAsync({ conversationId, content });
      setDraft('');
    } catch {
      // Error surfaces in send.error; leave draft intact so user can retry.
    }
  };

  const handleKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void submit();
    }
  };

  return (
    <form
      className="flex items-end gap-2 border-t border-slate-200 bg-white p-3"
      data-testid="message-composer"
      onSubmit={(e) => {
        e.preventDefault();
        void submit();
      }}
    >
      <textarea
        className="min-h-[2.5rem] flex-1 resize-none rounded border border-border-strong px-3 py-2 text-sm focus:border-accent"
        rows={1}
        aria-label="Message"
        placeholder="Type a message — Enter to send, Shift+Enter for newline"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={handleKey}
        data-testid="composer-textarea"
        disabled={send.isPending}
      />
      <button
        type="submit"
        disabled={disabled}
        className="rounded bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:bg-slate-300"
        data-testid="composer-send"
      >
        {send.isPending ? 'Sending…' : 'Send'}
      </button>
      {send.isError && (
        <span className="text-xs text-danger" data-testid="composer-error">
          {(send.error as Error).message}
        </span>
      )}
    </form>
  );
}

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { MessageList } from './MessageList';
import { MessageComposer } from './MessageComposer';
import { QuoteProvider } from './QuoteContext';
import type { Message } from '@/api/types';

// 引用 (quote) end-to-end flow: clicking Quote on a message drives the
// composer's quoting bar (shared QuoteContext), and submitting sends
// quoted_message_id + clears the queued target. The list + composer render under
// ONE QuoteProvider — the same wiring ConversationView mounts.
function Harness({ messages }: { messages: Message[] }) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <QuoteProvider>
        <MessageList messages={messages} />
        <MessageComposer conversationId="C1" />
      </QuoteProvider>
    </QueryClientProvider>
  );
}

const msg = (id: string, content: string, sender = 'agent:arch1'): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: sender,
  content_kind: 'text',
  content,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

describe('MessageComposer quoting (引用)', () => {
  beforeEach(() => {
    server.use(
      http.post('/api/conversations/:id/messages', () =>
        HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 }),
      ),
    );
  });
  afterEach(() => cleanup());

  it('clicking Quote on a message puts the composer into the quoting state (preview bar)', () => {
    render(<Harness messages={[msg('M1', 'hello world')]} />);
    // No quoting bar yet.
    expect(screen.queryByTestId('composer-quote-bar')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('message-quote-btn'));
    const bar = screen.getByTestId('composer-quote-bar');
    // sender (unresolved → clean handle) + one-line snippet show in the bar.
    expect(screen.getByTestId('composer-quote-sender')).toHaveTextContent('Quoting arch1');
    expect(screen.getByTestId('composer-quote-snippet')).toHaveTextContent('hello world');
    expect(bar).toBeInTheDocument();
  });

  it('the ✕ cancels the quote without sending', () => {
    render(<Harness messages={[msg('M1', 'hello world')]} />);
    fireEvent.click(screen.getByTestId('message-quote-btn'));
    expect(screen.getByTestId('composer-quote-bar')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('composer-quote-cancel'));
    expect(screen.queryByTestId('composer-quote-bar')).not.toBeInTheDocument();
  });

  it('submitting while quoting sends quoted_message_id and clears the quote after success', async () => {
    let seen: { quoted_message_id?: string; content?: string } | null = null;
    server.use(
      http.post('/api/conversations/:id/messages', async ({ request }) => {
        seen = (await request.json()) as { quoted_message_id?: string; content?: string };
        return HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 });
      }),
    );
    render(<Harness messages={[msg('M1', 'hello world')]} />);
    fireEvent.click(screen.getByTestId('message-quote-btn'));
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'a quoting reply');
    fireEvent.keyDown(ta, { key: 'Enter' });
    await waitFor(() => expect(ta.value).toBe(''));
    expect(seen).not.toBeNull();
    expect(seen!.quoted_message_id).toBe('M1');
    expect(seen!.content).toBe('a quoting reply');
    // quote cleared after a successful send.
    expect(screen.queryByTestId('composer-quote-bar')).not.toBeInTheDocument();
  });

  it('keeps the quote queued when the send fails (retry without re-selecting)', async () => {
    server.use(
      http.post('/api/conversations/:id/messages', () =>
        HttpResponse.json({ error: 'too_long', message: 'message too long' }, { status: 400 }),
      ),
    );
    render(<Harness messages={[msg('M1', 'hello world')]} />);
    fireEvent.click(screen.getByTestId('message-quote-btn'));
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'oops');
    fireEvent.click(screen.getByTestId('composer-send'));
    await waitFor(() => expect(screen.getByTestId('composer-error')).toBeInTheDocument());
    // draft + quote both preserved for retry.
    expect(ta.value).toBe('oops');
    expect(screen.getByTestId('composer-quote-bar')).toBeInTheDocument();
  });
});

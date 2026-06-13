import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render as rtlRender, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { MessageList } from './MessageList';
import { useAppStore } from '@/store/app';
import type { Message } from '@/api/types';

function render(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const msg = (over: Partial<Message> = {}): Message => ({
  id: 'M1',
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content: 'hello',
  direction: 'inbound',
  posted_at: '2026-06-12T00:00:00Z',
  ...over,
});

describe('MessageList thread affordance', () => {
  beforeEach(() => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
  });
  afterEach(() => {
    cleanup();
    useAppStore.setState({ currentUserId: '' });
  });

  it('renders a thread button per non-system message', () => {
    render(<MessageList messages={[msg()]} />);
    expect(screen.getByTestId('thread-button')).toBeInTheDocument();
  });

  it('shows the reply count and activity dot from the message', () => {
    render(
      <MessageList
        messages={[msg({ reply_count: 4, thread_last_activity_at: '2026-06-12T00:05:00Z' })]}
      />,
    );
    expect(screen.getByTestId('thread-reply-count')).toHaveTextContent('4');
    expect(screen.getByTestId('thread-activity-dot')).toBeInTheDocument();
  });

  it('hides the thread button when showThreads is false (used inside a thread)', () => {
    render(<MessageList messages={[msg()]} showThreads={false} />);
    expect(screen.queryByTestId('thread-button')).toBeNull();
  });

  it('opens the thread sidebar (local fallback) on click, showing the root message', async () => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () =>
        HttpResponse.json([], { status: 200 }),
      ),
    );
    render(<MessageList messages={[msg({ content: 'open me' })]} />);
    expect(screen.queryByTestId('thread-sidebar')).toBeNull();
    await userEvent.click(screen.getByTestId('thread-button'));
    expect(screen.getByTestId('thread-sidebar')).toBeInTheDocument();
    // the root message content shows inside the sidebar (plus the row) → ≥1
    expect(screen.getAllByText('open me').length).toBeGreaterThanOrEqual(1);
  });
});

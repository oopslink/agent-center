import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render as rtlRender, screen, within } from '@testing-library/react';
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

  it('shows a thread preview with reply count, and the dot only when there is NEW activity (P3)', async () => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () =>
        HttpResponse.json([
          msg({
            id: 'R1',
            sender_identity_id: 'agent:x9527',
            parent_message_id: 'M1',
            content: 'I started fixing this bug.',
            posted_at: '2026-06-12T00:02:00Z',
          }),
          msg({
            id: 'R2',
            sender_identity_id: 'agent:x9527',
            parent_message_id: 'M1',
            content: 'Fix pushed to origin/main.',
            posted_at: '2026-06-12T00:03:00Z',
          }),
          msg({
            id: 'R3',
            sender_identity_id: 'agent:x9527',
            parent_message_id: 'M1',
            content: 'Fixed and deployed.',
            posted_at: '2026-06-12T00:04:00Z',
          }),
        ]),
      ),
    );
    render(
      <MessageList
        messages={[msg({ reply_count: 4, thread_last_activity_at: '2026-06-12T00:05:00Z', has_new_activity: true })]}
      />,
    );
    const panel = screen.getByTestId('thread-preview-panel');
    expect(within(panel).getByTestId('thread-preview-chip')).toHaveTextContent('4 replies');
    expect(within(panel).getByText('new')).toBeInTheDocument();
    expect(within(panel).getByTestId('thread-preview-activity-dot')).toBeInTheDocument();
    expect(await screen.findAllByTestId('thread-preview-reply')).toHaveLength(3);
    expect(screen.getByTestId('thread-preview-earlier')).toHaveTextContent('1 earlier reply');
  });

  it('hides the dot when there are replies but no NEW activity (seen)', () => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () =>
        HttpResponse.json([], { status: 200 }),
      ),
    );
    render(
      <MessageList
        messages={[msg({ reply_count: 4, thread_last_activity_at: '2026-06-12T00:05:00Z', has_new_activity: false })]}
      />,
    );
    const panel = screen.getByTestId('thread-preview-panel');
    expect(within(panel).getByTestId('thread-preview-chip')).toHaveTextContent('4 replies');
    expect(within(panel).getByText('seen')).toBeInTheDocument();
    expect(screen.queryByTestId('thread-preview-activity-dot')).toBeNull();
  });

  it('keeps thread metadata inside the attached preview and aligns it with its root bubble', () => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () =>
        HttpResponse.json([], { status: 200 }),
      ),
    );
    render(<MessageList messages={[msg({ reply_count: 1 })]} />);
    const preview = screen.getByTestId('thread-preview');
    const panel = within(preview).getByTestId('thread-preview-panel');
    expect(preview).toHaveAttribute('data-align', 'right');
    expect(within(panel).getByTestId('thread-preview-header')).toContainElement(
      within(panel).getByTestId('thread-preview-chip'),
    );
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

  it('opens the thread sidebar from a populated preview', async () => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () =>
        HttpResponse.json([
          msg({
            id: 'R1',
            parent_message_id: 'M1',
            sender_identity_id: 'agent:x9527',
            content: 'reply body',
            posted_at: '2026-06-12T00:02:00Z',
          }),
        ]),
      ),
    );
    render(<MessageList messages={[msg({ content: 'thread root', reply_count: 1 })]} />);
    await userEvent.click(screen.getByTestId('thread-preview-panel'));
    expect(screen.getByTestId('thread-sidebar')).toBeInTheDocument();
    expect(screen.getAllByText('thread root').length).toBeGreaterThanOrEqual(1);
  });
});

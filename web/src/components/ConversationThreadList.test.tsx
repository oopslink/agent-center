import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render as rtlRender, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { ConversationThreadList } from './ConversationThreadList';
import { ThreadSidebarProvider } from './ThreadSidebarContext';
import type { Message, ThreadSummary } from '@/api/types';

function render(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function root(id: string, content: string, posted: string): Message {
  return {
    id,
    conversation_id: 'C1',
    sender_identity_id: 'user:hayang',
    content_kind: 'text',
    content,
    direction: 'inbound',
    posted_at: posted,
  };
}

function mockThreads(summaries: ThreadSummary[]) {
  server.use(
    http.get('/api/conversations/:id/threads', () => HttpResponse.json(summaries, { status: 200 })),
  );
}

describe('ConversationThreadList', () => {
  afterEach(() => cleanup());

  it('renders a row per thread with a content preview and reply count', async () => {
    mockThreads([
      { root: root('M1', 'design discussion', '2026-06-12T00:00:00Z'), reply_count: 3 },
    ]);
    render(<ConversationThreadList conversationId="C1" />);
    const row = await screen.findByTestId('thread-list-row');
    expect(within(row).getByText(/design discussion/)).toBeInTheDocument();
    expect(within(row).getByTestId('thread-list-reply-count')).toHaveTextContent('3');
  });

  it('shows a has-activity dot only when the thread has recent activity', async () => {
    mockThreads([
      { root: root('M1', 'active one', '2026-06-12T00:00:00Z'), reply_count: 1, thread_last_activity_at: '2026-06-12T03:00:00Z' },
      { root: root('M2', 'quiet one', '2026-06-12T00:00:00Z'), reply_count: 0 },
    ]);
    render(<ConversationThreadList conversationId="C1" />);
    await screen.findAllByTestId('thread-list-row');
    expect(screen.getAllByTestId('thread-list-activity-dot')).toHaveLength(1);
  });

  it('sorts by most recent activity first', async () => {
    mockThreads([
      { root: root('M-old', 'older activity', '2026-06-12T00:00:00Z'), reply_count: 1, thread_last_activity_at: '2026-06-12T01:00:00Z' },
      { root: root('M-new', 'newer activity', '2026-06-12T00:00:00Z'), reply_count: 1, thread_last_activity_at: '2026-06-12T05:00:00Z' },
    ]);
    render(<ConversationThreadList conversationId="C1" />);
    const rows = await screen.findAllByTestId('thread-list-row');
    expect(within(rows[0]).getByText(/newer activity/)).toBeInTheDocument();
    expect(within(rows[1]).getByText(/older activity/)).toBeInTheDocument();
  });

  it('renders an empty state when there are no threads', async () => {
    mockThreads([]);
    render(<ConversationThreadList conversationId="C1" />);
    expect(await screen.findByTestId('thread-list-empty')).toBeInTheDocument();
  });

  it('opens the matching ThreadSidebar when a thread row is clicked', async () => {
    mockThreads([{ root: root('M1', 'click into me', '2026-06-12T00:00:00Z'), reply_count: 2 }]);
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () => HttpResponse.json([])),
    );
    render(
      <ThreadSidebarProvider>
        <ConversationThreadList conversationId="C1" />
      </ThreadSidebarProvider>,
    );
    const row = await screen.findByTestId('thread-list-row');
    expect(screen.queryByTestId('thread-sidebar')).toBeNull();
    await userEvent.click(row);
    expect(screen.getByTestId('thread-sidebar')).toBeInTheDocument();
    // the clicked thread's root content shows inside the opened sidebar
    const sidebar = screen.getByTestId('thread-sidebar');
    expect(within(sidebar).getByText('click into me')).toBeInTheDocument();
  });
});

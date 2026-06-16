import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import type { Participant } from '@/api/types';
import { ConversationMobileTabs } from './ConversationMobileTabs';

const participants: Participant[] = [
  { identity_id: 'agent:dev', role: 'member', joined_at: '2026-05-01T00:00:00Z' } as Participant,
];

function mockApi({ threads = [], messages = [] }: { threads?: unknown[]; messages?: unknown[] } = {}) {
  server.use(
    http.get('/api/conversations/:id/threads', () => HttpResponse.json(threads)),
    http.get('/api/conversations/:id/messages/:rootId/replies', () => HttpResponse.json([])),
    http.get('/api/conversations/:id/messages', () => HttpResponse.json(messages)),
    http.post('/api/conversations/:id/read-state', () => HttpResponse.json({ ok: true })),
    http.post('/api/sse/subscribe', () => HttpResponse.json({ subscribed: true })),
    http.post('/api/sse/unsubscribe', () => HttpResponse.json({ unsubscribed: true })),
  );
}

function wrap(props: Partial<React.ComponentProps<typeof ConversationMobileTabs>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ConversationMobileTabs surface="channel" conversationId="C-1" participants={participants} {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ConversationMobileTabs (T184 mobile chat/threads/files)', () => {
  afterEach(() => cleanup());

  it('defaults to the Chat tab and shows the message stream', () => {
    mockApi();
    wrap();
    expect(screen.getByTestId('conversation-mtab-chat')).toHaveAttribute('data-active', 'true');
    // chat panel is visible (not hidden) by default.
    expect(screen.getByTestId('conversation-mpanel-chat')).not.toHaveAttribute('hidden');
    // the other tabs exist (channel → participants present).
    expect(screen.getByTestId('conversation-mtab-participants')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-mtab-threads')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-mtab-files')).toBeInTheDocument();
  });

  it('switches to the Threads tab; chat stays mounted-but-hidden', async () => {
    mockApi({
      threads: [
        {
          root: { id: 'R1', conversation_id: 'C-1', sender_identity_id: 'user:x', content_kind: 'text', content: 'hi', direction: 'inbound', posted_at: '2026-05-24T01:00:00Z' },
          reply_count: 1,
          thread_last_activity_at: '2026-05-24T02:00:00Z',
        },
      ],
    });
    wrap();
    fireEvent.click(screen.getByTestId('conversation-mtab-threads'));
    expect(screen.getByTestId('conversation-mtab-threads')).toHaveAttribute('data-active', 'true');
    const panel = screen.getByTestId('conversation-mpanel-threads');
    expect(await within(panel).findByTestId('thread-list')).toBeInTheDocument();
    // chat panel is still in the DOM (mounted) but hidden — SSE/scroll/draft survive.
    expect(screen.getByTestId('conversation-mpanel-chat')).toHaveAttribute('hidden');
  });

  it('opens the ThreadSidebar when a thread row in the Threads tab is clicked (regression: rows were inert without a provider)', async () => {
    mockApi({
      threads: [
        {
          root: { id: 'R1', conversation_id: 'C-1', sender_identity_id: 'user:x', content_kind: 'text', content: 'hi', direction: 'inbound', posted_at: '2026-05-24T01:00:00Z' },
          reply_count: 1,
          thread_last_activity_at: '2026-05-24T02:00:00Z',
        },
      ],
    });
    wrap();
    fireEvent.click(screen.getByTestId('conversation-mtab-threads'));
    const panel = screen.getByTestId('conversation-mpanel-threads');
    const row = await within(panel).findByTestId('thread-list-row');
    // before the click there is no sidebar.
    expect(screen.queryByTestId('thread-sidebar')).not.toBeInTheDocument();
    fireEvent.click(row);
    // clicking the row must open the shared (overlay) ThreadSidebar.
    expect(await screen.findByTestId('thread-sidebar')).toBeInTheDocument();
  });

  it('DM (showParticipants=false) shows chat / threads / files only — no Participants tab', () => {
    mockApi();
    wrap({ surface: 'dm', showParticipants: false });
    expect(screen.getByTestId('conversation-mtab-chat')).toHaveAttribute('data-active', 'true');
    expect(screen.queryByTestId('conversation-mtab-participants')).not.toBeInTheDocument();
    expect(screen.getByTestId('conversation-mtab-threads')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-mtab-files')).toBeInTheDocument();
  });
});

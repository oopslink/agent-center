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

function optionValues(select: HTMLElement): string[] {
  return within(select)
    .queryAllByRole('option')
    .map((o) => (o as HTMLOptionElement).value);
}

describe('ConversationMobileTabs (T184 mobile chat/threads/files dropdown)', () => {
  afterEach(() => cleanup());

  it('defaults to the Chat panel and offers the panels in the dropdown', () => {
    mockApi();
    wrap();
    const select = screen.getByTestId('conversation-mtab-select') as HTMLSelectElement;
    expect(select.value).toBe('chat');
    // chat panel is visible (not hidden) by default.
    expect(screen.getByTestId('conversation-mpanel-chat')).not.toHaveAttribute('hidden');
    // the other panels are options (channel → participants present).
    expect(optionValues(select)).toEqual(['chat', 'participants', 'threads', 'files']);
  });

  it('switches to the Threads panel via the dropdown; chat stays mounted-but-hidden', async () => {
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
    fireEvent.change(screen.getByTestId('conversation-mtab-select'), { target: { value: 'threads' } });
    expect((screen.getByTestId('conversation-mtab-select') as HTMLSelectElement).value).toBe('threads');
    const panel = screen.getByTestId('conversation-mpanel-threads');
    expect(await within(panel).findByTestId('thread-list')).toBeInTheDocument();
    // chat panel is still in the DOM (mounted) but hidden — SSE/scroll/draft survive.
    expect(screen.getByTestId('conversation-mpanel-chat')).toHaveAttribute('hidden');
  });

  it('opens the ThreadSidebar when a thread row in the Threads panel is clicked (regression: rows were inert without a provider)', async () => {
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
    fireEvent.change(screen.getByTestId('conversation-mtab-select'), { target: { value: 'threads' } });
    const panel = screen.getByTestId('conversation-mpanel-threads');
    const row = await within(panel).findByTestId('thread-list-row');
    // before the click there is no sidebar.
    expect(screen.queryByTestId('thread-sidebar')).not.toBeInTheDocument();
    fireEvent.click(row);
    // clicking the row must open the shared (overlay) ThreadSidebar.
    expect(await screen.findByTestId('thread-sidebar')).toBeInTheDocument();
  });

  it('DM (showParticipants=false) offers chat / threads / files only — no Participants', () => {
    mockApi();
    wrap({ surface: 'dm', showParticipants: false });
    const select = screen.getByTestId('conversation-mtab-select') as HTMLSelectElement;
    expect(select.value).toBe('chat');
    expect(optionValues(select)).toEqual(['chat', 'threads', 'files']);
  });

  it('maximize toggle promotes the tabs to a full-viewport overlay and restores', () => {
    mockApi();
    wrap();
    const root = screen.getByTestId('conversation-mobile-tabs');
    const toggle = screen.getByTestId('conversation-maximize-toggle-mobile');
    expect(root).toHaveAttribute('data-maximized', 'false');
    expect(toggle).toHaveAttribute('aria-label', 'Maximize conversation');
    fireEvent.click(toggle);
    expect(root).toHaveAttribute('data-maximized', 'true');
    expect(root.className).toContain('fixed');
    expect(toggle).toHaveAttribute('aria-label', 'Restore conversation');
    fireEvent.click(toggle);
    expect(root).toHaveAttribute('data-maximized', 'false');
  });
});

import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import type { Participant } from '@/api/types';
import { ConversationSurfaceMobile } from './ConversationSurfaceMobile';

// mobile-redesign-conversations.md §3.5 / mockup frames ④ + ⑤ — the redesigned
// mobile conversation surface that replaces the pre-redesign
// ConversationMobileTabs (a <select> dropdown + a maximize toggle).

const participants: Participant[] = [
  { identity_id: 'agent:dev', role: 'member', joined_at: '2026-05-01T00:00:00Z' } as Participant,
  { identity_id: 'user:alice', role: 'member', joined_at: '2026-05-01T00:00:00Z' } as Participant,
  // A departed participant must not inflate the People count.
  {
    identity_id: 'user:gone',
    role: 'member',
    joined_at: '2026-05-01T00:00:00Z',
    left_at: '2026-05-02T00:00:00Z',
  } as Participant,
];

const threadFixture = [
  {
    root: {
      id: 'R1',
      conversation_id: 'C-1',
      sender_identity_id: 'user:x',
      content_kind: 'text',
      content: 'hi',
      direction: 'inbound',
      posted_at: '2026-05-24T01:00:00Z',
    },
    reply_count: 1,
    thread_last_activity_at: '2026-05-24T02:00:00Z',
  },
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

function wrap(props: Partial<React.ComponentProps<typeof ConversationSurfaceMobile>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <ConversationSurfaceMobile
          surface="channel"
          conversationId="C-1"
          participants={participants}
          {...props}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function segmentIds(): string[] {
  return screen
    .getAllByRole('tab')
    .map((el) => el.getAttribute('data-testid')?.replace('conversation-mseg-', '') ?? '');
}

describe('ConversationSurfaceMobile', () => {
  afterEach(() => cleanup());

  it('defaults to Chat and offers Chat / Threads / Files / People segment pills (channel)', () => {
    mockApi();
    wrap();
    // Segment pills, not a <select> — the old dropdown must be gone.
    expect(screen.queryByTestId('conversation-mtab-select')).not.toBeInTheDocument();
    expect(segmentIds()).toEqual(['chat', 'threads', 'files', 'people']);
    expect(screen.getByTestId('conversation-mseg-chat')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('conversation-mpanel-chat')).not.toHaveAttribute('hidden');
  });

  it('DM (showParticipants=false) drops the People segment — a DM is a fixed 1:1', () => {
    mockApi();
    wrap({ surface: 'dm', showParticipants: false });
    expect(segmentIds()).toEqual(['chat', 'threads', 'files']);
    expect(screen.queryByTestId('conversation-mpanel-people')).not.toBeInTheDocument();
  });

  it('tapping Threads switches panels; chat stays mounted-but-hidden so SSE/scroll/draft survive', async () => {
    mockApi({ threads: threadFixture });
    wrap();
    fireEvent.click(screen.getByTestId('conversation-mseg-threads'));
    expect(screen.getByTestId('conversation-mseg-threads')).toHaveAttribute('aria-selected', 'true');
    const panel = screen.getByTestId('conversation-mpanel-threads');
    expect(await within(panel).findByTestId('thread-list')).toBeInTheDocument();
    // Chat is still in the DOM, just hidden.
    expect(screen.getByTestId('conversation-mpanel-chat')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-mpanel-chat')).toHaveAttribute('hidden');
  });

  it('opens the ThreadSidebar when a thread row is tapped (regression: rows were inert without a provider)', async () => {
    mockApi({ threads: threadFixture });
    wrap();
    fireEvent.click(screen.getByTestId('conversation-mseg-threads'));
    const row = await within(screen.getByTestId('conversation-mpanel-threads')).findByTestId('thread-list-row');
    expect(screen.queryByTestId('thread-sidebar')).not.toBeInTheDocument();
    fireEvent.click(row);
    expect(await screen.findByTestId('thread-sidebar')).toBeInTheDocument();
  });

  it('shows the People panel with the participants, counting active members only', async () => {
    mockApi();
    wrap();
    // 3 participants, 1 with left_at → count reads 2.
    expect(screen.getByTestId('conversation-mseg-people-count')).toHaveTextContent('2');
    fireEvent.click(screen.getByTestId('conversation-mseg-people'));
    expect(screen.getByTestId('conversation-mpanel-people')).not.toHaveAttribute('hidden');
  });

  it('carries a thread count on the Threads pill and omits a zero count', async () => {
    mockApi({ threads: threadFixture });
    const { unmount } = wrap();
    expect(await screen.findByTestId('conversation-mseg-threads-count')).toHaveTextContent('1');
    unmount();
    cleanup();

    mockApi({ threads: [] });
    wrap();
    // A zero count renders no badge — the pill row stays quiet when empty.
    expect(screen.queryByTestId('conversation-mseg-threads-count')).not.toBeInTheDocument();
  });

  it('renders the Files empty state when the conversation has no attachments', () => {
    mockApi();
    wrap();
    fireEvent.click(screen.getByTestId('conversation-mseg-files'));
    expect(screen.getByTestId('conversation-mobile-files-empty')).toBeInTheDocument();
  });

  it('does NOT render a mobile maximize toggle (dropped by explicit decision, spec §4/§7)', () => {
    mockApi();
    wrap();
    expect(screen.queryByTestId('conversation-maximize-toggle-mobile')).not.toBeInTheDocument();
    // ...and the surface never promotes itself to a fixed-inset overlay.
    expect(screen.getByTestId('conversation-surface-mobile').className).not.toContain('fixed');
  });

  it('gives every segment pill a ≥44px touch target (v2.10.1 touch baseline)', () => {
    mockApi();
    wrap();
    for (const tab of screen.getAllByRole('tab')) {
      expect(tab.className).toContain('min-h-[44px]');
    }
  });
});

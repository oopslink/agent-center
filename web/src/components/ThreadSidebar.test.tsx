import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render as rtlRender, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { ThreadSidebar } from './ThreadSidebar';
import type { Message } from '@/api/types';

function render(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const root: Message = {
  id: 'M-root',
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content: 'root question',
  direction: 'inbound',
  posted_at: '2026-06-12T00:00:00Z',
  reply_count: 1,
};

function reply(id: string, content: string): Message {
  return {
    id,
    conversation_id: 'C1',
    sender_identity_id: 'agent:agent-1',
    content_kind: 'text',
    content,
    direction: 'outbound',
    posted_at: '2026-06-12T00:01:00Z',
    parent_message_id: 'M-root',
  };
}

describe('ThreadSidebar', () => {
  beforeEach(() => {
    server.use(
      http.get('/api/conversations/:id/messages/:mid/replies', () =>
        HttpResponse.json([reply('R1', 'first reply')], { status: 200 }),
      ),
    );
  });
  afterEach(() => cleanup());

  it('renders nothing when closed', () => {
    render(<ThreadSidebar open={false} rootMessage={null} onClose={() => {}} />);
    expect(screen.queryByTestId('thread-sidebar')).toBeNull();
  });

  it('marks the latest reply seen on open (P3 — clears the has-activity dot)', async () => {
    let seenId: string | undefined;
    server.use(
      http.post('/api/conversations/:id/seen', async ({ request }) => {
        const body = (await request.json()) as { last_seen_message_id: string };
        seenId = body.last_seen_message_id;
        return HttpResponse.json(
          { last_seen_message_id: body.last_seen_message_id, version: 1, bumped: true, event_id: 'E-seen' },
          { status: 200 },
        );
      }),
    );
    render(<ThreadSidebar open rootMessage={root} onClose={() => {}} />);
    // once the replies load, the latest reply id (R1) is marked seen
    await waitFor(() => expect(seenId).toBe('R1'));
  });

  it('renders the root message and its replies when open', async () => {
    render(<ThreadSidebar open rootMessage={root} onClose={() => {}} />);
    expect(screen.getByTestId('thread-sidebar')).toBeInTheDocument();
    expect(screen.getByText('root question')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('first reply')).toBeInTheDocument());
  });

  it('does not render nested thread buttons inside the thread (single-level)', async () => {
    render(<ThreadSidebar open rootMessage={root} onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText('first reply')).toBeInTheDocument());
    expect(screen.queryAllByTestId('thread-button')).toHaveLength(0);
  });

  // 引用 (quote): the thread body is wrapped in a QuoteProvider, so the per-message
  // Quote action is available INSIDE a thread (previously hidden — useQuote() was
  // null with no provider). Regression guard for "can't quote in a thread".
  it('exposes the per-message Quote action inside a thread', async () => {
    render(<ThreadSidebar open rootMessage={root} onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText('first reply')).toBeInTheDocument());
    expect(screen.getAllByTestId('message-quote-btn').length).toBeGreaterThan(0);
  });

  it('sends a reply carrying parent_message_id', async () => {
    let seenParent: string | undefined;
    server.use(
      http.post('/api/conversations/:id/messages', async ({ request }) => {
        const body = (await request.json()) as { parent_message_id?: string };
        seenParent = body.parent_message_id;
        return HttpResponse.json({ message_id: 'M-NEW', event_id: 'E-1' }, { status: 201 });
      }),
    );
    render(<ThreadSidebar open rootMessage={root} onClose={() => {}} />);
    const ta = screen.getByTestId('composer-textarea') as HTMLTextAreaElement;
    await userEvent.type(ta, 'my reply');
    fireEvent.keyDown(ta, { key: 'Enter' });
    await waitFor(() => expect(seenParent).toBe('M-root'));
  });

  it('calls onClose from the close button', async () => {
    const onClose = vi.fn();
    render(<ThreadSidebar open rootMessage={root} onClose={onClose} />);
    await userEvent.click(screen.getByTestId('thread-sidebar-close'));
    expect(onClose).toHaveBeenCalled();
  });

  it('renders a left-edge resize handle and a left-drag widens the panel (persisted)', () => {
    // jsdom's localStorage has no methods; install a real Map-backed stub so the
    // width persists. Unstubbed after the test so other tests keep the no-op store.
    const store = new Map<string, string>();
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => (store.has(k) ? (store.get(k) as string) : null),
      setItem: (k: string, v: string) => void store.set(k, String(v)),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    });
    render(<ThreadSidebar open rootMessage={root} onClose={() => {}} />);
    const panel = screen.getByTestId('thread-sidebar');
    expect(panel.style.getPropertyValue('--thread-w')).toBe('448px'); // default
    const handle = screen.getByTestId('thread-sidebar-resize');
    expect(handle).toHaveAttribute('aria-orientation', 'vertical');
    fireEvent.mouseDown(handle, { clientX: 800 });
    fireEvent.mouseMove(window, { clientX: 750 }); // 50px left -> +50
    fireEvent.mouseUp(window, { clientX: 750 });
    expect(panel.style.getPropertyValue('--thread-w')).toBe('498px');
    // width persisted as number
    expect(localStorage.getItem('ac.thread.panel.width')).toBe('498');
    vi.unstubAllGlobals();
  });
});

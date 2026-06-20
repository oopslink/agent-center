import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Channels from './Channels';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement, route = '/channels') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Channels page', () => {
  afterEach(() => cleanup());

  it('renders the channels list from the API', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: 'plan' },
          { id: 'C2', kind: 'channel', name: 'ops', status: 'active', description: '' },
        ]),
      ),
    );
    wrap(<Channels />);
    await waitFor(() => expect(screen.getAllByTestId('channel-row')).toHaveLength(2));
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('ops')).toBeInTheDocument();
  });

  it('shows the empty state when there are no channels', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<Channels />);
    await waitFor(() => expect(screen.getByTestId('channels-empty')).toBeInTheDocument());
  });

  it('opens the create modal from the header button', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<Channels />);
    await waitFor(() => expect(screen.getByTestId('channels-empty')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('channels-new-button'));
    expect(screen.getByTestId('channel-create-modal')).toBeInTheDocument();
  });

  it('surfaces the API error', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Channels />);
    await waitFor(() =>
      expect(screen.getByTestId('channels-error')).toHaveTextContent(/db down/),
    );
  });
});

// v2.8.1 list-enrich: each channel row is enriched with created_at (local tz),
// a participant avatar-stack + count, and ≤3 recent plain-text previews.
describe('Channels list-enrichment (v2.8.1)', () => {
  afterEach(() => cleanup());

  // The list resolver resolves user:hayang (seeded member) but NOT agent:gone.
  const membersHandler = http.get('/api/members', () =>
    HttpResponse.json([
      {
        id: 'mem-1', organization_id: 'org-test', identity_id: 'user:hayang',
        role: 'owner', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
        display_name: 'Hayang',
      },
    ]),
  );

  it('renders created_at via formatLocalTime (local tz, not raw Z)', async () => {
    server.use(
      membersHandler,
      http.get('/api/conversations', () =>
        HttpResponse.json([
          {
            id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '',
            created_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
    );
    wrap(<Channels />);
    const at = await screen.findByTestId('channel-created-at');
    // formatLocalTime emits a localized "Mon D, YYYY, h:MM AM/PM GMT±N" shape —
    // never the raw ISO with a trailing Z.
    expect(at.textContent).not.toMatch(/\d{4}-\d{2}-\d{2}T.*Z/);
    expect(at.textContent).toMatch(/2026/);
  });

  it('renders the participant avatar-stack + count with +N overflow and aria-label', async () => {
    server.use(
      membersHandler,
      http.get('/api/conversations', () =>
        HttpResponse.json([
          {
            id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '',
            participant_count: 5,
            participants: [
              { identity_id: 'user:hayang', display_name: 'Hayang' },
              { identity_id: 'agent:bot', display_name: 'Bot' },
            ],
          },
        ]),
      ),
    );
    wrap(<Channels />);
    await screen.findByTestId('channel-participants');
    // two avatar discs shown
    expect(screen.getAllByTestId('channel-participant-avatar')).toHaveLength(2);
    // overflow chip = 5 total - 2 shown = +3
    expect(screen.getByTestId('channel-participants-overflow')).toHaveTextContent('+3');
    // count text reflects participant_count
    expect(screen.getByTestId('channel-participant-count')).toHaveTextContent('5');
    // accessible group name
    expect(screen.getByRole('group', { name: '5 participants' })).toBeInTheDocument();
  });

  it('renders the recent-3 previews as truncated plain text', async () => {
    const long = 'x'.repeat(400);
    server.use(
      membersHandler,
      http.get('/api/conversations', () =>
        HttpResponse.json([
          {
            id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '',
            recent_messages: [
              { sender_identity_id: 'user:hayang', content: '# heading\n```code```', posted_at: '2026-05-24T01:00:00Z' },
              { sender_identity_id: 'user:hayang', content: long, posted_at: '2026-05-24T01:01:00Z' },
              { sender_identity_id: 'user:hayang', content: 'line one\nline two', posted_at: '2026-05-24T01:02:00Z' },
              { sender_identity_id: 'user:hayang', content: 'fourth dropped', posted_at: '2026-05-24T01:03:00Z' },
            ],
          },
        ]),
      ),
    );
    wrap(<Channels />);
    await screen.findByTestId('channel-recent-messages');
    const rows = screen.getAllByTestId('channel-recent-message');
    // ≤3 previews (the 4th is dropped)
    expect(rows).toHaveLength(3);
    // T234: content span is truncate-capped at ≤3/4 card width (single-line
    // ellipsis, no row-break). The truncate moved off the <li> onto the content.
    const contents = screen.getAllByTestId('channel-recent-content');
    expect(contents[1].className).toContain('truncate');
    expect(contents[1].className).toContain('max-w-[75%]');
    // T234: each row carries a "[yyyy-MM-dd HH:mm:ss]" local-tz timestamp prefix.
    const times = screen.getAllByTestId('channel-recent-time');
    expect(times[0].textContent).toMatch(/^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\]$/);
    // the [timestamp] [sender] meta is bold (font-semibold)
    expect(times[0].parentElement?.className).toContain('font-semibold');
    // markdown is rendered as plain text — the raw markup is present as text,
    // NOT parsed into a heading/code block element.
    expect(rows[0].textContent).toContain('# heading');
    expect(rows[0].querySelector('h1, pre, code, img')).toBeNull();
    // multi-line content is flattened to a single line (no newline)
    expect(rows[2].textContent).not.toContain('\n');
    // full text on the title for hover
    expect(rows[1]).toHaveAttribute('title', expect.stringContaining('xxxx'));
  });

  it('renders a deleted sender as "(deleted)" not the raw agent ref', async () => {
    server.use(
      membersHandler,
      http.get('/api/conversations', () =>
        HttpResponse.json([
          {
            id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '',
            recent_messages: [
              { sender_identity_id: 'agent:gone', content: 'last words', posted_at: '2026-05-24T01:00:00Z' },
            ],
          },
        ]),
      ),
    );
    wrap(<Channels />);
    const sender = await screen.findByTestId('channel-recent-sender');
    expect(sender).toHaveTextContent('(deleted)');
    expect(sender).toHaveAttribute('data-sender-resolved', 'false');
    // never the raw prefixed ref
    expect(screen.queryByText(/agent:gone/)).not.toBeInTheDocument();
  });

  it('renders a deleted sender as "(deleted)" when the backend returns EMPTY sender_display_name (reconcile a)', async () => {
    // soft-ref reconcile (a): #255 returns sender_display_name="" for a deleted/
    // unresolved sender (backend-authoritative). FE: present-but-empty → "(deleted)"
    // (does NOT re-resolve to a cleaned handle), consistent with #246 F1.
    server.use(
      membersHandler,
      http.get('/api/conversations', () =>
        HttpResponse.json([
          {
            id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '',
            recent_messages: [
              { id: 'm1', sender_identity_id: 'agent:gone', sender_display_name: '', content: 'last words', posted_at: '2026-05-24T01:00:00Z' },
            ],
          },
        ]),
      ),
    );
    wrap(<Channels />);
    const sender = await screen.findByTestId('channel-recent-sender');
    expect(sender).toHaveTextContent('(deleted)');
    expect(sender).toHaveAttribute('data-sender-resolved', 'false');
    expect(screen.queryByText(/agent:gone/)).not.toBeInTheDocument();
  });

  it('shows a friendly placeholder for a channel with no messages', async () => {
    server.use(
      membersHandler,
      http.get('/api/conversations', () =>
        HttpResponse.json([
          {
            id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '',
            recent_messages: [],
          },
        ]),
      ),
    );
    wrap(<Channels />);
    expect(await screen.findByTestId('channel-no-messages')).toHaveTextContent('No messages yet');
  });
});

// v2.9.1 task-169c598d: archived channels are excluded from the default list and
// surfaced via a lazy collapsed "Archived" group; active rows carry an Archive
// action. Mirrors the Projects archived-group tests (#317).
describe('Channels archive (v2.9.1)', () => {
  afterEach(() => cleanup());

  it('archived group is collapsed by default and does not fetch archived', async () => {
    let archivedFetched = false;
    server.use(
      http.get('/api/conversations', ({ request }) => {
        const status = new URL(request.url).searchParams.get('status');
        if (status === 'archived') {
          archivedFetched = true;
          return HttpResponse.json([]);
        }
        return HttpResponse.json([
          { id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '' },
        ]);
      }),
    );
    wrap(<Channels />);
    await waitFor(() => expect(screen.getAllByTestId('channel-row')).toHaveLength(1));
    expect(screen.getByTestId('archived-channels-toggle')).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTestId('archived-channels-body')).not.toBeInTheDocument();
    expect(archivedFetched).toBe(false);
  });

  it('expanding the archived group fetches and lists archived channels with the ARCHIVED badge', async () => {
    server.use(
      http.get('/api/conversations', ({ request }) => {
        const status = new URL(request.url).searchParams.get('status');
        if (status === 'archived') {
          return HttpResponse.json([
            { id: 'C9', kind: 'channel', name: 'zeta', status: 'archived', description: 'old room' },
          ]);
        }
        return HttpResponse.json([
          { id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '' },
        ]);
      }),
    );
    wrap(<Channels />);
    await waitFor(() => expect(screen.getAllByTestId('channel-row')).toHaveLength(1));
    fireEvent.click(screen.getByTestId('archived-channels-toggle'));
    await waitFor(() => expect(screen.getByTestId('archived-channels-list')).toBeInTheDocument());
    expect(screen.getByText('zeta')).toBeInTheDocument();
    expect(screen.getByTestId('channel-status-archived')).toBeInTheDocument();
    // archived row is read-only — no Archive action on it.
    expect(screen.getByTestId('archived-channel-row').querySelector('[data-testid="channel-archive-btn"]')).toBeNull();
  });

  it('shows the empty note when there are no archived channels', async () => {
    server.use(
      http.get('/api/conversations', ({ request }) => {
        const status = new URL(request.url).searchParams.get('status');
        return HttpResponse.json(
          status === 'archived'
            ? []
            : [{ id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '' }],
        );
      }),
    );
    wrap(<Channels />);
    await waitFor(() => expect(screen.getAllByTestId('channel-row')).toHaveLength(1));
    fireEvent.click(screen.getByTestId('archived-channels-toggle'));
    await waitFor(() => expect(screen.getByTestId('archived-channels-empty')).toBeInTheDocument());
  });

  it('archiving an active channel POSTs to the archive endpoint', async () => {
    let archivedId = '';
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'C1', kind: 'channel', name: 'alpha', status: 'active', description: '' },
        ]),
      ),
      http.post('/api/conversations/:id/archive', ({ params }) => {
        archivedId = params.id as string;
        return HttpResponse.json({ event_id: 'ev-1' });
      }),
    );
    wrap(<Channels />);
    const btn = await screen.findByTestId('channel-archive-btn');
    fireEvent.click(btn);
    await waitFor(() => expect(archivedId).toBe('C1'));
  });

  // v2.10.2 [T129] Mobile 二级段控 (Channels | DMs) — on mobile col② is hidden,
  // so this page surfaces the Channels↔DMs switch (the DM-reachability fix).
  it('renders the mobile Conversations segmented nav (Channels active, DMs reachable)', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<Channels />, '/channels');
    const nav = await screen.findByTestId('segmented-nav');
    const channels = within(nav).getByTestId('conv-seg-channels');
    const dms = within(nav).getByTestId('conv-seg-dms');
    expect(channels).toHaveAttribute('data-active', 'true');
    expect(dms).toHaveAttribute('data-active', 'false');
    // The DMs segment links to /dms so a mobile user can switch to private chats.
    expect(dms).toHaveAttribute('href', '/dms');
  });
});

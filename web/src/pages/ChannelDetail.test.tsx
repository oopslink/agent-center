import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { useContextPanelController } from '@/shell/contextPanel';
import { BottomSheet } from '@/shell/BottomSheet';
import ChannelDetail from './ChannelDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

// v2.10.0 [T64]: ChannelDetail now renders its participants + shared-files into
// the shell's col④ via <ContextPanel> (a portal). Provide a host so the panel
// mounts in this isolated page test (mirrors AppLayout's controller).
function WithCol4Host({ children }: { children: React.ReactNode }): React.ReactElement {
  const { Provider, value, setHost } = useContextPanelController();
  return (
    <Provider value={value}>
      {children}
      <div ref={setHost} data-testid="col4-host" />
    </Provider>
  );
}

// The MOBILE shell: mirrors AppLayout's real wiring — the col④ host lives inside
// the Context Panel BottomSheet, which only renders while `mobileSheetOpen`. So
// panel content is genuinely absent until the page's ⓘ opens the sheet (that is
// what the mobile tests below assert; a permanently-mounted host would make the
// "opens the sheet" assertion vacuous).
function WithMobileSheetHost({ children }: { children: React.ReactNode }): React.ReactElement {
  const { Provider, value, setHost, mobileSheetOpen, closeMobileSheet } = useContextPanelController();
  return (
    <Provider value={value}>
      {children}
      <BottomSheet open={mobileSheetOpen} onClose={closeMobileSheet} ariaLabel="context" testId="context-sheet">
        <div ref={setHost} />
      </BottomSheet>
    </Provider>
  );
}

function wrap(path: string, { mobile = false }: { mobile?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Host = mobile ? WithMobileSheetHost : WithCol4Host;
  return render(
    <QueryClientProvider client={qc}>
      <Host>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/channels/:channelId" element={<ChannelDetail />} />
          </Routes>
        </MemoryRouter>
      </Host>
    </QueryClientProvider>,
  );
}

// v2.7.1 #247: ChannelDetail loads by channel_id (URL segment) via the detail
// GET — no more by-name list lookup.
const channelShowHandler = http.get('/api/conversations/:id', ({ params }) =>
  HttpResponse.json({
    id: params.id,
    kind: 'channel',
    name: 'alpha',
    status: 'active',
    description: 'plan',
    participants: [
      {
        identity_id: 'user:hayang',
        role: 'owner',
        joined_at: '2026-05-24T00:00:00Z',
        joined_by: 'user:hayang',
      },
    ],
  }),
);
const messagesHandler = http.get('/api/conversations/:id/messages', () =>
  HttpResponse.json([
    {
      id: 'M1',
      conversation_id: 'C-alpha',
      sender_identity_id: 'user:hayang',
      content_kind: 'text',
      content: 'hello world',
      direction: 'inbound',
      posted_at: '2026-05-24T01:00:00Z',
    },
  ]),
);

describe('ChannelDetail page', () => {
  afterEach(() => cleanup());

  it('loads the channel by id (URL = hash) + renders name in chrome (#247)', async () => {
    server.use(channelShowHandler, messagesHandler);
    wrap('/channels/C-alpha');
    await waitFor(() =>
      expect(screen.getByText('hello world')).toBeInTheDocument(),
    );
    // URL carries the id; the channel NAME still shows as chrome (heading + breadcrumb leaf).
    expect(screen.getByRole('heading', { name: 'alpha' })).toBeInTheDocument();
    expect(screen.getByTestId('breadcrumb')).toHaveTextContent('alpha');
    expect(screen.getByTestId('page-ChannelDetail')).toHaveAttribute('data-channel-id', 'C-alpha');
    // #264 P1: the message body now renders through the surface-agnostic shell.
    expect(screen.getByTestId('conversation-view')).toHaveAttribute('data-surface', 'channel');
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    expect(screen.getByTestId('participants-panel')).toBeInTheDocument();
    expect(screen.getByText(/1 participant/)).toBeInTheDocument();
    // 8th channel redesign: header shows an overlapping participant avatar stack.
    const stack = screen.getByTestId('channel-avatar-stack');
    expect(stack).toHaveAttribute('aria-label', '1 participant');
    expect(screen.getAllByTestId('channel-avatar-stack-item')).toHaveLength(1);
  });

  it('shows not-found state when the channel id does not resolve', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such channel' }, { status: 404 }),
      ),
    );
    wrap('/channels/channel-ghost');
    await waitFor(() =>
      expect(screen.getByTestId('channel-not-found')).toHaveTextContent(/no such channel/),
    );
  });

  // ── Mobile (<768px), mobile-redesign-conversations.md §3.5 / mockup frame ④.
  // These assert the ROUTE actually wires the redesigned components — the /channels/:id
  // page is the production caller of ConversationSurfaceMobile + ConversationInfoButton.
  describe('mobile', () => {
    function stubMobile() {
      vi.stubGlobal('matchMedia', (query: string) => ({
        matches: true, media: query, onchange: null,
        addEventListener: () => {}, removeEventListener: () => {},
        addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false,
      }));
    }
    afterEach(() => vi.unstubAllGlobals());

    it('renders the redesigned segment surface (Chat/Threads/Files/People) instead of the old dropdown', async () => {
      stubMobile();
      server.use(channelShowHandler, messagesHandler);
      wrap('/channels/C-alpha', { mobile: true });
      await screen.findByTestId('conversation-surface-mobile');
      expect(screen.getByTestId('conversation-mseg-chat')).toHaveAttribute('aria-selected', 'true');
      expect(screen.getByTestId('conversation-mseg-people')).toBeInTheDocument();
      // The pre-redesign dropdown + maximize are gone from the real page.
      expect(screen.queryByTestId('conversation-mtab-select')).not.toBeInTheDocument();
      expect(screen.queryByTestId('conversation-maximize-toggle-mobile')).not.toBeInTheDocument();
    });

    it('header exposes the ⓘ button, which opens the Context Panel sheet holding the channel info card', async () => {
      stubMobile();
      server.use(channelShowHandler, messagesHandler);
      wrap('/channels/C-alpha', { mobile: true });
      const info = await screen.findByTestId('conversation-info-button');
      // The sheet (and therefore the info card) is closed until ⓘ is tapped.
      expect(screen.queryByTestId('context-sheet')).not.toBeInTheDocument();
      expect(screen.queryByTestId('conversation-info-sheet')).not.toBeInTheDocument();
      fireEvent.click(info);
      // The sheet content (portalled through <ContextPanel>) carries name + description.
      expect(await screen.findByTestId('context-sheet')).toBeInTheDocument();
      const sheet = await screen.findByTestId('conversation-info-sheet');
      expect(within(sheet).getByTestId('conversation-info-title')).toHaveTextContent('alpha');
      expect(within(sheet).getByTestId('conversation-info-description')).toHaveTextContent('plan');
      expect(within(sheet).getByTestId('conversation-info-members')).toHaveTextContent('Members (1)');
    });

    it('hides the desktop avatar stack on mobile (header is back + title + star + ⓘ)', async () => {
      stubMobile();
      server.use(channelShowHandler, messagesHandler);
      wrap('/channels/C-alpha', { mobile: true });
      await screen.findByTestId('conversation-surface-mobile');
      // Still in the DOM for desktop, but md:-gated so it is not visible at phone width.
      expect(screen.getByTestId('channel-avatar-stack').parentElement?.className).toContain('hidden');
    });
  });

  it('surfaces a messages error via the shared shell when the messages query fails', async () => {
    server.use(
      channelShowHandler,
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap('/channels/C-alpha');
    // #264 P1: error now renders inside ConversationView (shared `conversation-error`).
    await waitFor(() =>
      expect(screen.getByTestId('conversation-error')).toHaveTextContent(/db down/),
    );
  });
});

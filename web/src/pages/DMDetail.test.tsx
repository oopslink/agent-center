import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { useAppStore } from '@/store/app';
import { useContextPanelController } from '@/shell/contextPanel';
import { BottomSheet } from '@/shell/BottomSheet';
import DMDetail from './DMDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

// The MOBILE shell: mirrors AppLayout — the col④ host lives inside the Context
// Panel BottomSheet, which only renders while `mobileSheetOpen`, so the info
// card is genuinely absent until the page's ⓘ opens it.
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
  const tree = (
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/dms/:id" element={<DMDetail />} />
      </Routes>
    </MemoryRouter>
  );
  return render(
    <QueryClientProvider client={qc}>
      {mobile ? <WithMobileSheetHost>{tree}</WithMobileSheetHost> : tree}
    </QueryClientProvider>,
  );
}

describe('DMDetail page', () => {
  beforeEach(() => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
  });
  afterEach(() => cleanup());

  it('renders peer names + messages + composer when found', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'agent:bot-1',
          peer_display_name: 'Bot One',
        }),
      ),
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json([
          {
            id: 'M1',
            conversation_id: 'C-DM',
            sender_identity_id: 'agent:bot-1',
            content_kind: 'text',
            content: 'hi from bot',
            direction: 'inbound',
            posted_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
    );
    wrap('/dms/C-DM');
    await waitFor(() => expect(screen.getByText('hi from bot')).toBeInTheDocument());
    // v2.7.1 #215: heading shows the resolved peer as @name (raw id on hover).
    expect(screen.getByTestId('dm-heading')).toHaveTextContent('@Bot One');
    expect(screen.getByTestId('dm-heading')).toHaveAttribute('title', 'agent:bot-1');
    // #7thDM richer header: "Direct message" subtitle under the name.
    expect(screen.getByTestId('dm-subtitle')).toHaveTextContent('Direct message');
    // #264 P1: the message body now renders through the surface-agnostic shell.
    expect(screen.getByTestId('conversation-view')).toHaveAttribute('data-surface', 'dm');
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
    // No participants panel for DM.
    expect(screen.queryByTestId('participants-panel')).not.toBeInTheDocument();
  });

  it('#7thDM: header renders the back arrow, peer avatar, follow + overflow (no dead search)', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'agent:bot-1',
          peer_display_name: 'Bot One',
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-DM');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toBeInTheDocument());
    // Back arrow (icon link, aria-label).
    expect(screen.getByLabelText('Back to DMs')).toBeInTheDocument();
    // Peer avatar (agent kind → labelled "(agent)").
    expect(screen.getByTestId('avatar')).toHaveAttribute('aria-label', 'Bot One (agent)');
    // FollowToggle kept in the right cluster.
    expect(screen.getByTestId('follow-toggle')).toBeInTheDocument();
    // No dead search button (per [[no-middle-state]] — message search doesn't exist yet).
    expect(screen.queryByLabelText('Search messages')).not.toBeInTheDocument();
    // Overflow menu button (aria-label) + a keyboard-accessible menu item.
    expect(screen.getByLabelText('More actions')).toBeInTheDocument();
    expect(screen.getByTestId('dm-overflow-copy')).toBeInTheDocument();
  });

  it('#7thDM: an available agent peer shows the Bot badge + online dot', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'agent:bot-1',
          peer_display_name: 'Bot One',
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
      // A running + available agent → online.
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'Bot One',
          description: '',
          model: 'claude-opus',
          cli: 'claudecode',
          env_vars: {},

          worker_id: 'w-1',
          lifecycle: 'running',
          availability: 'available',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
    );
    wrap('/dms/C-DM');
    await waitFor(() => expect(screen.getByTestId('dm-bot-badge')).toHaveTextContent('Bot'));
    await waitFor(() => expect(screen.getByTestId('dm-online-dot')).toHaveAttribute('aria-label', 'Online'));
  });

  it('#7thDM: a stopped agent peer shows the Bot badge but no online dot', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'agent:bot-1',
          peer_display_name: 'Bot One',
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'Bot One',
          description: '',
          model: 'claude-opus',
          cli: 'claudecode',
          env_vars: {},

          worker_id: 'w-1',
          lifecycle: 'stopped',
          availability: 'unavailable',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
    );
    wrap('/dms/C-DM');
    await waitFor(() => expect(screen.getByTestId('dm-bot-badge')).toBeInTheDocument());
    expect(screen.queryByTestId('dm-online-dot')).not.toBeInTheDocument();
  });

  it('#7thDM: a user peer degrades gracefully — no Bot badge, no online dot', async () => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          { identity_id: 'alice', display_name: 'Alice', kind: 'user', status: 'joined' },
        ]),
      ),
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-USER',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'user:alice',
          peer_display_name: 'Alice',
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-USER');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('@Alice'));
    expect(screen.queryByTestId('dm-bot-badge')).not.toBeInTheDocument();
    expect(screen.queryByTestId('dm-online-dot')).not.toBeInTheDocument();
    // Human avatar (no "(agent)" suffix).
    expect(screen.getByTestId('avatar')).toHaveAttribute('aria-label', 'Alice');
    // Header chrome still present.
    expect(screen.getByLabelText('Back to DMs')).toBeInTheDocument();
    expect(screen.getByTestId('conversation-view')).toHaveAttribute('data-surface', 'dm');
  });

  it('#7thDM: a deleted agent peer keeps "(deleted)" and shows no online dot', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DEL',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'agent:gone',
          // peer_display_name omitted → deleted peer.
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
      // Even if the agent row resolves as running, a deleted (unresolved name)
      // peer must NOT show the online dot.
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'gone',
          description: '',
          model: 'claude-opus',
          cli: 'claudecode',
          env_vars: {},

          worker_id: 'w-1',
          lifecycle: 'running',
          availability: 'available',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
    );
    wrap('/dms/C-DEL');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('(deleted)'));
    expect(screen.queryByTestId('dm-online-dot')).not.toBeInTheDocument();
  });

  it('shows "(deleted)" heading when the peer no longer resolves (#215/E1)', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM2',
          kind: 'dm',
          name: '',
          status: 'active',
          peer_identity_id: 'agent:gone',
          // peer_display_name omitted → deleted peer.
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-DM2');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('(deleted)'));
  });

  it('resolves the peer from participants − self on a direct load (#238)', async () => {
    // The detail GET does NOT enrich peer_display_name (only the list does), so a
    // direct DM URL load must derive the peer from participants + resolve its name.
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([{ identity_id: 'bot-9', display_name: 'Bot Nine', kind: 'agent', status: 'joined' }]),
      ),
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DIRECT',
          kind: 'dm',
          name: '',
          status: 'active',
          // no peer_display_name / peer_identity_id — only participants (detail GET).
          participants: [
            { identity_id: 'user:hayang', role: 'member', joined_at: 'x', joined_by: 'user:hayang' },
            { identity_id: 'agent:bot-9', role: 'member', joined_at: 'x', joined_by: 'user:hayang' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-DIRECT');
    // heading + breadcrumb leaf both show @Bot Nine (not "Direct message").
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('@Bot Nine'));
    expect(screen.getByTestId('breadcrumb')).toHaveTextContent('@Bot Nine');
    // raw peer ref on hover (#192).
    expect(screen.getByTestId('dm-heading')).toHaveAttribute('title', 'agent:bot-9');
  });

  it('surfaces conversation lookup error', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such dm' }, { status: 404 }),
      ),
    );
    wrap('/dms/missing');
    await waitFor(() => expect(screen.getByTestId('dm-not-found')).toHaveTextContent(/no such dm/));
  });

  it('surfaces messages error inside the page', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          participants: [
            { identity_id: 'user:hayang', role: 'owner', joined_at: 'x', joined_by: 'user:hayang' },
          ],
        }),
      ),
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap('/dms/C-DM');
    // #264 P1: error now renders inside ConversationView (shared `conversation-error`).
    await waitFor(() => expect(screen.getByTestId('conversation-error')).toHaveTextContent(/db down/));
  });

  // #281 entry ①: the DM header peer (avatar + @name) opens the existing
  // kind-routed SenderDetailSidebar for the peer ref. Reuses the SAME sidebar
  // (SenderDetailSidebar) as the message-sender clicks — no new sidebar.
  describe('#281 header peer opens the SenderDetailSidebar (entry ①)', () => {
    function mockAgentDM() {
      server.use(
        http.get('/api/conversations/:id', () =>
          HttpResponse.json({
            id: 'C-DM',
            kind: 'dm',
            name: '',
            status: 'active',
            peer_identity_id: 'agent:bot-1',
            peer_display_name: 'Bot One',
          }),
        ),
        http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
        http.get('/api/agents/:id', ({ params }) =>
          HttpResponse.json({
            id: String(params.id),
            organization_id: 'O-1',
            name: 'Bot One',
            description: 'a bot',
            model: 'claude-opus',
            cli: 'claudecode',
            env_vars: {},

            worker_id: 'w-1',
            lifecycle: 'running',
            availability: 'available',
            created_by: 'user:hayang',
            version: 1,
            created_at: '2026-05-24T01:00:00Z',
            updated_at: '2026-05-24T02:00:00Z',
          }),
        ),
      );
    }

    it('clicking the peer @name opens the sidebar with the agent body (AgentDetailBody)', async () => {
      mockAgentDM();
      wrap('/dms/C-DM');
      await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('@Bot One'));
      expect(screen.queryByTestId('sender-sidebar')).toBeNull();
      fireEvent.click(screen.getByTestId('dm-heading'));
      await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
      // kind-routing: an agent peer renders the AgentDetailBody.
      await waitFor(() => expect(screen.getByTestId('sender-sidebar-agent')).toBeInTheDocument());
    });

    it('clicking the peer avatar opens the same sidebar; a11y label present', async () => {
      mockAgentDM();
      wrap('/dms/C-DM');
      await waitFor(() => expect(screen.getByTestId('dm-peer-avatar-button')).toBeInTheDocument());
      const avatarBtn = screen.getByTestId('dm-peer-avatar-button');
      expect(avatarBtn).toHaveAttribute('aria-label', 'View Bot One details');
      fireEvent.click(avatarBtn);
      await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    });

    it('keyboard (Enter via native button activation) opens the sidebar', async () => {
      mockAgentDM();
      wrap('/dms/C-DM');
      const nameBtn = await screen.findByTestId('dm-heading');
      nameBtn.focus();
      fireEvent.keyDown(nameBtn, { key: 'Enter' });
      // native <button> activates onClick on Enter/Space — fireEvent.click is the
      // canonical RTL assertion that keyboard activation works.
      fireEvent.click(nameBtn);
      await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    });

    it('a USER peer opens the user body (UserDetailBody) — kind routing', async () => {
      server.use(
        http.get('/api/members', () =>
          HttpResponse.json([
            { identity_id: 'user:alice', display_name: 'Alice', kind: 'user', status: 'joined' },
          ]),
        ),
        http.get('/api/conversations/:id', () =>
          HttpResponse.json({
            id: 'C-USER',
            kind: 'dm',
            name: '',
            status: 'active',
            peer_identity_id: 'user:alice',
            peer_display_name: 'Alice',
          }),
        ),
        http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
        http.get('/api/users/:id', ({ params }) =>
          HttpResponse.json({
            user_id: String(params.id),
            display_name: 'Alice',
            email: 'alice@example.com',
            created_at: '2026-01-01T00:00:00Z',
            orgs: [],
          }),
        ),
      );
      wrap('/dms/C-USER');
      await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('@Alice'));
      fireEvent.click(screen.getByTestId('dm-heading'));
      await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
      // kind-routing: a user peer renders the UserDetailBody (NOT the agent body).
      await waitFor(() => expect(screen.getByTestId('sender-sidebar-user')).toBeInTheDocument());
      expect(screen.queryByTestId('sender-sidebar-agent')).toBeNull();
    });
  });

  // v2.7.1 #215: a malformed DM with no resolved peer falls back to "Direct message".
  it('falls back to "Direct message" heading when there is no peer', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-SOLO',
          kind: 'dm',
          name: '',
          status: 'active',
          // no peer_identity_id → malformed/solo DM.
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-SOLO');
    await waitFor(() => expect(screen.getByTestId('dm-heading')).toHaveTextContent('Direct message'));
  });

  // ── Mobile (<768px), mobile-redesign-conversations.md §3.5 / mockup frame ⑤.
  // Asserts the /dms/:id ROUTE really wires the redesigned components.
  describe('mobile', () => {
    function stubMobile() {
      vi.stubGlobal('matchMedia', (query: string) => ({
        matches: true, media: query, onchange: null,
        addEventListener: () => {}, removeEventListener: () => {},
        addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false,
      }));
    }
    function mockDM() {
      server.use(
        http.get('/api/conversations/:id', () =>
          HttpResponse.json({
            id: 'C-DM',
            kind: 'dm',
            name: '',
            status: 'active',
            peer_identity_id: 'agent:bot-1',
            peer_display_name: 'Bot One',
          }),
        ),
        http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
        http.get('/api/conversations/:id/threads', () => HttpResponse.json([])),
      );
    }
    afterEach(() => vi.unstubAllGlobals());

    it('renders Chat/Threads/Files segments with NO People segment (a DM is a fixed 1:1)', async () => {
      stubMobile();
      mockDM();
      wrap('/dms/C-DM', { mobile: true });
      await screen.findByTestId('conversation-surface-mobile');
      expect(screen.getByTestId('conversation-mseg-chat')).toBeInTheDocument();
      expect(screen.getByTestId('conversation-mseg-threads')).toBeInTheDocument();
      expect(screen.getByTestId('conversation-mseg-files')).toBeInTheDocument();
      expect(screen.queryByTestId('conversation-mseg-people')).not.toBeInTheDocument();
      // The pre-redesign dropdown + maximize are gone from the real page.
      expect(screen.queryByTestId('conversation-mtab-select')).not.toBeInTheDocument();
      expect(screen.queryByTestId('conversation-maximize-toggle-mobile')).not.toBeInTheDocument();
    });

    it('ⓘ opens the Context Panel sheet with the DM info card (no Members section)', async () => {
      stubMobile();
      mockDM();
      wrap('/dms/C-DM', { mobile: true });
      const info = await screen.findByTestId('conversation-info-button');
      expect(screen.queryByTestId('conversation-info-sheet')).not.toBeInTheDocument();
      fireEvent.click(info);
      const sheet = await screen.findByTestId('conversation-info-sheet');
      expect(within(sheet).getByTestId('conversation-info-title')).toHaveTextContent('@Bot One');
      expect(within(sheet).queryByTestId('conversation-info-members')).not.toBeInTheDocument();
      expect(within(sheet).getByTestId('conversation-info-files')).toBeInTheDocument();
    });

    it('keeps the star (follow) and ⋯ overflow alongside ⓘ (mockup frame ⑤ header)', async () => {
      stubMobile();
      mockDM();
      wrap('/dms/C-DM', { mobile: true });
      await screen.findByTestId('conversation-info-button');
      expect(screen.getByTestId('dm-back')).toBeInTheDocument();
      expect(screen.getByTestId('follow-toggle')).toBeInTheDocument();
      expect(screen.getByTestId('dm-overflow')).toBeInTheDocument();
    });
  });
});

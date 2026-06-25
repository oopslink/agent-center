import React, { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { SenderDetailSidebar } from './SenderDetailSidebar';

// Probe the current router location so a test can assert a navigation happened.
function LocationProbe(): React.ReactElement {
  const loc = useLocation();
  return <div data-testid="location-probe">{loc.pathname}</div>;
}

afterEach(() => cleanup());

// Fresh QueryClient per render so cached agent/user responses don't leak.
// MemoryRouter: T136's header "Open DM" button uses useOpenDm → useNavigate,
// which requires a Router ancestor (the live app always renders within one).
function render(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return rtlRender(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const noop = () => {};

describe('SenderDetailSidebar', () => {
  it('renders nothing when closed', () => {
    render(<SenderDetailSidebar open={false} senderRef={'agent:A-1'} onClose={noop} />);
    expect(screen.queryByTestId('sender-sidebar')).toBeNull();
  });

  it('renders nothing when senderRef is null', () => {
    render(<SenderDetailSidebar open senderRef={null} onClose={noop} />);
    expect(screen.queryByTestId('sender-sidebar')).toBeNull();
  });

  it('dispatches an agent: ref to the agent branch (name + lifecycle)', async () => {
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'builder-bot',
          description: '',
          model: 'claude-opus',
          cli: 'claudecode',
          env_vars: {},
          skills: [],
          worker_id: 'w-9',
          lifecycle: 'running',
          availability: 'busy',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:A-9'} onClose={noop} />);
    expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-agent')).toBeInTheDocument());
    // Name now lives only in the header (no duplicate body row); the compact
    // body keeps CLI / Model / Worker / Description.
    expect(screen.getByTestId('sender-sidebar-agent-info')).toBeInTheDocument();
    expect(screen.getByTestId('agent-lifecycle-badge')).toHaveTextContent('running');
    expect(screen.getByTestId('agent-availability-badge')).toHaveTextContent('busy');
    expect(screen.getByText('claude-opus')).toBeInTheDocument();
    expect(screen.getByText('claudecode')).toBeInTheDocument();
    // dialog a11y: role + aria-modal.
    const panel = screen.getByTestId('sender-sidebar');
    expect(panel).toHaveAttribute('role', 'dialog');
    expect(panel).toHaveAttribute('aria-modal', 'true');
  });

  // T346: a BARE agent id ("agent-<id>", no "agent:" prefix) must ALSO route to the
  // agent branch — the activity sidebar passed the bare ref, refKind mis-read it as
  // a user, and the card showed "This user is unavailable (deleted)" for a live agent.
  it('dispatches a BARE agent-<id> ref to the agent branch (not the user/deleted path)', async () => {
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'agent-center-pd',
          description: '',
          model: 'claude-opus',
          cli: 'claudecode',
          env_vars: {},
          skills: [],
          worker_id: 'w-9',
          lifecycle: 'running',
          availability: 'available',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent-b5036ea8'} onClose={noop} />);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-agent')).toBeInTheDocument());
    expect(screen.queryByText(/This user is unavailable/i)).toBeNull();
    expect(screen.getByTestId('agent-lifecycle-badge')).toHaveTextContent('running');
  });

  it('shows the agent activity feed in the sidebar', async () => {
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'builder-bot',
          description: 'does builds',
          model: 'claude-opus-4-8',
          cli: 'claude-code',
          env_vars: {},
          skills: [],
          worker_id: 'worker001',
          lifecycle: 'running',
          availability: 'busy',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
      http.get('/api/agents/:id/activity', ({ params }) =>
        HttpResponse.json({
          activity: [
            {
              id: 'AC-9',
              agent_id: String(params.id),
              event_type: 'system_init',
              payload: '{}',
              occurred_at: '2026-05-24T01:00:00Z',
            },
          ],
          next_cursor: null,
        }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:A-9'} onClose={noop} />);
    // compact basic info dl is present (CLI / Model / Worker / Description).
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-agent-info')).toBeInTheDocument());
    expect(screen.getByText('does builds')).toBeInTheDocument();
    expect(screen.getByText('worker001')).toBeInTheDocument();
    // activity feed renders its events.
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-activity-list')).toBeInTheDocument());
  });

  it('shows an empty-activity state when the agent has no activity', async () => {
    server.use(
      http.get('/api/agents/:id/activity', () =>
        HttpResponse.json({ activity: [], next_cursor: null }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:A-1'} onClose={noop} />);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-activity-empty')).toBeInTheDocument());
  });

  it('dispatches a user: ref to the user branch (name + User label)', async () => {
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({
          user_id: String(params.id),
          display_name: 'Hayang Li',
          email: 'hey@example.com',
          created_at: '2026-05-24T01:00:00Z',
          orgs: [],
        }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'user:hayang'} onClose={noop} />);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-user')).toBeInTheDocument());
    expect(screen.getByText('Hayang Li')).toBeInTheDocument();
    expect(screen.getByText('hey@example.com')).toBeInTheDocument();
    // T136: the "Open DM" header button is agent-only (no DM button for a user).
    expect(screen.queryByTestId('sender-sidebar-dm')).not.toBeInTheDocument();
  });

  // T136: the agent header has a no-text "Open DM" icon button that opens/creates
  // the 1:1 DM with this agent and closes the sidebar (navigation happens on the
  // DM create success). The button carries a tooltip + aria-label "Open DM".
  it('agent header shows an "Open DM" icon button; clicking it creates the DM and closes', async () => {
    let postedBody: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id), organization_id: 'O-1', name: 'builder-bot', description: '',
          model: 'claude-opus', cli: 'claudecode', env_vars: {}, skills: [], worker_id: 'w-9',
          lifecycle: 'running', availability: 'busy', created_by: 'user:hayang', version: 1,
          created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
      http.post('/api/conversations', async ({ request }) => {
        postedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ conversation_id: 'dm-1' });
      }),
    );
    const onClose = vi.fn();
    render(<SenderDetailSidebar open senderRef={'agent:A-9'} onClose={onClose} />);
    const dm = await screen.findByTestId('sender-sidebar-dm');
    expect(dm).toHaveAttribute('aria-label', 'Open DM');
    expect(dm).toHaveAttribute('title', 'Open DM');
    expect(dm).not.toHaveTextContent(/dm|message/i); // icon-only, no text label.

    fireEvent.click(dm);
    // Opens/creates the 1:1 DM with this agent…
    await waitFor(() => expect(postedBody).toEqual({ kind: 'dm', members: ['agent:A-9'] }));
    // …and closes the sidebar so the operator lands in the conversation.
    expect(onClose).toHaveBeenCalled();
  });

  // v2.10.2 [T159] regression (Tester2 run-real FAIL): clicking "Open DM" calls
  // onClose() synchronously, which UNMOUNTS the button before the POST resolves.
  // The navigate must STILL happen (it used to be a mutate() per-call onSuccess
  // that React Query discarded on the unmounted observer → DM created but never
  // opened). Here onClose actually unmounts the sidebar (open→false), unlike the
  // vi.fn() above, so it reproduces the real flow.
  it('T159: navigates to the DM even though onClose unmounts the button first', async () => {
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id), organization_id: 'O-1', name: 'builder-bot', description: '',
          model: 'claude-opus', cli: 'claudecode', env_vars: {}, skills: [], worker_id: 'w-9',
          lifecycle: 'running', availability: 'busy', created_by: 'user:hayang', version: 1,
          created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
      http.post('/api/conversations', () => HttpResponse.json({ conversation_id: 'dm-1' })),
    );
    function Harness(): React.ReactElement {
      const [open, setOpen] = useState(true);
      return <SenderDetailSidebar open={open} senderRef={'agent:A-9'} onClose={() => setOpen(false)} />;
    }
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    rtlRender(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <Harness />
          <LocationProbe />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    fireEvent.click(await screen.findByTestId('sender-sidebar-dm'));
    // The button unmounts immediately (onClose → open=false), yet the DM opens.
    await waitFor(() =>
      expect(screen.getByTestId('location-probe')).toHaveTextContent('/dms/dm-1'),
    );
    expect(screen.queryByTestId('sender-sidebar-dm')).not.toBeInTheDocument();
  });

  // T230: the resolved agent name in the header is a link into the Agent detail
  // page (/agents/:id). Clicking it navigates there AND closes the sidebar so the
  // operator lands on the detail page rather than leaving the modal panel open.
  it('T230: the resolved agent name links to the agent detail page and closes the sidebar', async () => {
    server.use(
      // members data is what lets the resolver turn agent:A-9 into a real name
      // (without it the header degrades to "(deleted)" and stays plain text).
      http.get('/api/members', () =>
        HttpResponse.json([
          { identity_id: 'agent:A-9', kind: 'agent', display_name: 'builder-bot', role: 'member' },
        ]),
      ),
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id), organization_id: 'O-1', name: 'builder-bot', description: '',
          model: 'claude-opus', cli: 'claudecode', env_vars: {}, skills: [], worker_id: 'w-9',
          lifecycle: 'running', availability: 'busy', created_by: 'user:hayang', version: 1,
          created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
    );
    const onClose = vi.fn();
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    rtlRender(
      <QueryClientProvider client={qc}>
        <MemoryRouter>
          <SenderDetailSidebar open senderRef={'agent:A-9'} onClose={onClose} />
          <LocationProbe />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    // wait for the name to resolve into the link affordance (the div is replaced
    // by an <a> once /api/members resolves the name — re-query each tick).
    await waitFor(() =>
      expect(screen.getByTestId('sender-sidebar-name').tagName).toBe('A'),
    );
    const link = screen.getByTestId('sender-sidebar-name');
    expect(link).toHaveAttribute('data-name-link', 'true');
    expect(link).toHaveAttribute('href', '/agents/A-9');
    expect(link).toHaveTextContent('builder-bot');

    fireEvent.click(link);
    // navigates to the agent detail page …
    await waitFor(() =>
      expect(screen.getByTestId('location-probe')).toHaveTextContent('/agents/A-9'),
    );
    // … and closes the sidebar so the operator lands on the detail page.
    expect(onClose).toHaveBeenCalled();
  });

  // T230: a deleted/unresolved agent name has no detail page to link to — it must
  // stay plain text (no link), keeping the "(deleted)" affordance.
  it('T230: an unresolved (deleted) agent name is NOT a link', async () => {
    server.use(
      http.get('/api/agents/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'gone' }, { status: 404 }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:agent-8d1126f6'} onClose={noop} />);
    const nameEl = screen.getByTestId('sender-sidebar-name');
    expect(nameEl.tagName).not.toBe('A');
    expect(nameEl).not.toHaveAttribute('data-name-link');
  });

  // F2 (v2.8.1): a force-deleted agent's GET /api/agents/{id} returns 404. The
  // panel must show a FRIENDLY "unavailable (deleted)" message — NOT a blank
  // panel, NOT a bare "not found", NOT the generic "couldn't load".
  it('shows a FRIENDLY deleted message (not blank) for an agent that 404s', async () => {
    server.use(
      http.get('/api/agents/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'gone' }, { status: 404 }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:agent-8d1126f6'} onClose={noop} />);
    // the panel itself renders (never blank) ...
    expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument();
    // ... and the body shows the friendly deleted message.
    await waitFor(() =>
      expect(screen.getByText(/this agent is unavailable \(deleted\)/i)).toBeInTheDocument(),
    );
    // not the generic / bare wording.
    expect(screen.queryByText(/couldn't load this agent/i)).toBeNull();
    expect(screen.queryByText(/^Agent not found\.$/i)).toBeNull();
  });

  // F1 consistency: the header for a deleted (unresolved) sender shows an AA-muted (text-text-secondary)
  // "(deleted)" label, never the raw `agent:agent-xxx` prefixed ref. With no
  // /api/members data the resolver cannot resolve the name.
  it('header degrades to muted "(deleted)" for an unresolved sender (no raw ref)', async () => {
    server.use(
      http.get('/api/agents/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'gone' }, { status: 404 }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:agent-8d1126f6'} onClose={noop} />);
    const nameEl = screen.getByTestId('sender-sidebar-name');
    expect(nameEl.textContent).toBe('(deleted)');
    expect(nameEl).toHaveAttribute('data-name-resolved', 'false');
    expect(nameEl.className).toContain('text-text-secondary');
    expect(nameEl.textContent).not.toContain('agent:');
    // raw ref kept on title for debugging (#192 chrome rule).
    expect(nameEl.getAttribute('title')).toContain('agent:agent-8d1126f6');
  });

  // A NON-404 error (e.g. 500) keeps the generic load-failure message — only a
  // 404 maps to the deleted wording.
  it('shows the generic load-failure message for a non-404 error', async () => {
    server.use(
      http.get('/api/agents/:id', () =>
        HttpResponse.json({ error: 'server_error', message: 'boom' }, { status: 500 }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:A-9'} onClose={noop} />);
    await waitFor(() => expect(screen.getByText(/couldn't load this agent/i)).toBeInTheDocument());
    expect(screen.queryByText(/unavailable \(deleted\)/i)).toBeNull();
  });

  // F2 user branch: a deleted user's GET 404 shows the friendly message too.
  it('shows a FRIENDLY deleted message for a user that 404s', async () => {
    server.use(
      http.get('/api/users/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'gone' }, { status: 404 }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'user:user-deleted'} onClose={noop} />);
    expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByText(/this user is unavailable \(deleted\)/i)).toBeInTheDocument(),
    );
  });

  it('fires onClose from the close button, the overlay, and Escape', async () => {
    const onClose = vi.fn();
    render(<SenderDetailSidebar open senderRef={'agent:A-1'} onClose={onClose} />);
    fireEvent.click(screen.getByTestId('sender-sidebar-close'));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId('sender-sidebar-overlay'));
    expect(onClose).toHaveBeenCalledTimes(2);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it('close button uses an ASCII glyph, not an emoji (a11y guardrail)', () => {
    render(<SenderDetailSidebar open senderRef={'agent:A-1'} onClose={noop} />);
    const close = screen.getByTestId('sender-sidebar-close');
    // The glyph is a plain ASCII "X" (per the #208 lesson — NOT ✕/U+2715, which
    // is in the a11y guardrail's pictograph range; the component was corrected to
    // ASCII but this assertion was left stale and merged in #233).
    expect(close.textContent).toBe('X');
    // No pictographic/emoji codepoints (Misc Symbols & Pictographs, Emoticons,
    // Transport, Supplemental Symbols, regional indicators, variation selectors).
    expect(
      /[\u{1F300}-\u{1FAFF}\u{1F1E6}-\u{1F1FF}\u{FE00}-\u{FE0F}]/u.test(close.textContent ?? ''),
    ).toBe(false);
  });

  // T474 — the agent-only header "Create reminder" button.
  it('renders a "Create reminder" button for an agent and opens the create modal preselecting it', async () => {
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'builder-bot',
          description: '',
          model: 'claude-opus-4-8',
          cli: 'claude-code',
          env_vars: {},
          skills: [],
          worker_id: 'w-1',
          lifecycle: 'running',
          availability: 'busy',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
      http.get('/api/agents/:id/activity', () =>
        HttpResponse.json({ activity: [], next_cursor: null }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:A-9'} onClose={noop} />);
    const btn = await screen.findByTestId('sender-sidebar-reminder');
    expect(btn.getAttribute('aria-label')).toBe('Create reminder');
    fireEvent.click(btn);
    // Modal opens with this agent preselected as a remindee chip.
    expect(await screen.findByTestId('reminder-create-modal')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('reminder-remindee-chip')).toBeInTheDocument());
  });

  it('does NOT render the reminder button for a user sender', async () => {
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({
          user_id: String(params.id),
          display_name: 'Ada',
          email: 'ada@x.io',
        }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'user:U-1'} onClose={noop} />);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-user')).toBeInTheDocument());
    expect(screen.queryByTestId('sender-sidebar-reminder')).toBeNull();
  });

  it('prefills a one-shot reset time + content when the agent recently hit a session limit', async () => {
    const recent = new Date(Date.now() - 3600_000).toISOString(); // ~1h ago
    const limitText = "You've hit your session limit · resets 12:10pm (Asia/Shanghai)";
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          organization_id: 'O-1',
          name: 'builder-bot',
          description: '',
          model: 'claude-opus-4-8',
          cli: 'claude-code',
          env_vars: {},
          skills: [],
          worker_id: 'w-1',
          lifecycle: 'running',
          availability: 'busy',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        }),
      ),
      http.get('/api/agents/:id/activity', ({ params }) =>
        HttpResponse.json({
          activity: [
            {
              id: 'AC-rl',
              agent_id: String(params.id),
              event_type: 'assistant_text',
              payload: JSON.stringify({ text: limitText, raw: { text: limitText } }),
              occurred_at: recent,
            },
          ],
          next_cursor: null,
        }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:A-9'} onClose={noop} />);
    // Wait for the activity query to settle before clicking (the button reads its
    // cached events at click time).
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-activity')).toBeInTheDocument());
    await waitFor(() =>
      expect(screen.queryByTestId('sender-sidebar-activity-loading')).toBeNull(),
    );
    fireEvent.click(screen.getByTestId('sender-sidebar-reminder'));
    expect(await screen.findByTestId('reminder-create-modal')).toBeInTheDocument();
    // Once mode is active with a (computed) date filled in, and the content
    // references the limit reset.
    const date = await screen.findByTestId('reminder-once-date');
    expect((date as HTMLInputElement).value).not.toBe('');
    expect((screen.getByTestId('reminder-content') as HTMLTextAreaElement).value).toMatch(/limit reset/i);
  });
});

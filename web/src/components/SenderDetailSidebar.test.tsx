import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { SenderDetailSidebar } from './SenderDetailSidebar';

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
});

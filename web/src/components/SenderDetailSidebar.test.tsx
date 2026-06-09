import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { SenderDetailSidebar } from './SenderDetailSidebar';

afterEach(() => cleanup());

// Fresh QueryClient per render so cached agent/user responses don't leak.
function render(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return rtlRender(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
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
  });

  it('shows a graceful not-found / error state for an agent that fails to load', async () => {
    server.use(
      http.get('/api/agents/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'gone' }, { status: 404 }),
      ),
    );
    render(<SenderDetailSidebar open senderRef={'agent:nope'} onClose={noop} />);
    await waitFor(() => expect(screen.getByText(/couldn't load this agent/i)).toBeInTheDocument());
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

import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { MarkdownMessage } from './MarkdownMessage';
import { SenderSidebarProvider } from './SenderSidebarContext';

// #281 entry ②: @mention tokens in message content open the existing kind-routed
// SenderDetailSidebar. Mentions weren't previously distinct tokens — this asserts
// the new detection: an @handle that resolves to a known member becomes a
// clickable, keyboard-accessible token routed to the right detail body.
function renderInProvider(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SenderSidebarProvider>{ui}</SenderSidebarProvider>
    </QueryClientProvider>,
  );
}

// members: one agent (Bot One / bot-1) + one human (Alice / alice). Used to
// resolve @mention handles → prefixed identity refs for kind routing.
function mockMembers() {
  server.use(
    http.get('/api/members', () =>
      HttpResponse.json([
        { id: 'm1', organization_id: 'O', identity_id: 'agent:bot-1', display_name: 'Bot One', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x' },
        { id: 'm2', organization_id: 'O', identity_id: 'user:alice', display_name: 'Alice', kind: 'user', role: 'member', status: 'joined', joined_at: 'x' },
      ]),
    ),
  );
}

describe('MentionText (#281 entry ②)', () => {
  afterEach(() => cleanup());

  it('renders a known @agent mention as a clickable token; click opens the agent body', async () => {
    mockMembers();
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id), organization_id: 'O', name: 'Bot One', description: '',
          model: 'claude-opus', cli: 'claudecode', env_vars: {}, skills: [], worker_id: 'w-1',
          lifecycle: 'running', availability: 'available', created_by: 'user:hayang',
          version: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
        }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'hey @bot-1 please check this'} />);
    // mention is tokenized once the members query resolves.
    const token = await screen.findByTestId('mention-token');
    expect(token.tagName).toBe('BUTTON');
    expect(token).toHaveTextContent('@bot-1');
    // a11y: aria-label present.
    expect(token).toHaveAttribute('aria-label', 'View bot-1 details');
    // kind-routing: ref carries the agent: prefix.
    expect(token).toHaveAttribute('data-mention-ref', 'agent:bot-1');
    expect(screen.queryByTestId('sender-sidebar')).toBeNull();
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    // agent ref → AgentDetailBody.
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-agent')).toBeInTheDocument());
  });

  it('resolves an @agent by display name too (case/space-insensitive)', async () => {
    mockMembers();
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id), organization_id: 'O', name: 'Bot One', description: '',
          model: 'm', cli: 'c', env_vars: {}, skills: [], worker_id: 'w', lifecycle: 'running',
          availability: 'available', created_by: 'user:hayang', version: 1,
          created_at: 'x', updated_at: 'x',
        }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'cc @BotOne'} />);
    const token = await screen.findByTestId('mention-token');
    expect(token).toHaveAttribute('data-mention-ref', 'agent:bot-1');
  });

  it('a @human mention opens the user body (UserDetailBody) — kind routing', async () => {
    mockMembers();
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({
          user_id: String(params.id), display_name: 'Alice', email: 'alice@example.com',
          created_at: 'x', orgs: [],
        }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'thanks @alice'} />);
    const token = await screen.findByTestId('mention-token');
    expect(token).toHaveAttribute('data-mention-ref', 'user:alice');
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    // user ref → UserDetailBody, NOT the agent body.
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-user')).toBeInTheDocument());
    expect(screen.queryByTestId('sender-sidebar-agent')).toBeNull();
  });

  it('keyboard activation (Enter on the native button) opens the sidebar', async () => {
    mockMembers();
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({ user_id: String(params.id), display_name: 'Alice', created_at: 'x', orgs: [] }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'ping @alice'} />);
    const token = await screen.findByTestId('mention-token');
    token.focus();
    fireEvent.keyDown(token, { key: 'Enter' });
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
  });

  it('a mention click does NOT bubble to an outer (message-row) handler', async () => {
    mockMembers();
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({ user_id: String(params.id), display_name: 'Alice', created_at: 'x', orgs: [] }),
      ),
    );
    const outer = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <SenderSidebarProvider>
          <div
            data-testid="outer-row"
            role="button"
            tabIndex={0}
            onClick={outer}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') outer();
            }}
          >
            <MarkdownMessage content={'hi @alice'} />
          </div>
        </SenderSidebarProvider>
      </QueryClientProvider>,
    );
    const token = await screen.findByTestId('mention-token');
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    // stopPropagation: the outer row handler must NOT have fired.
    expect(outer).not.toHaveBeenCalled();
  });

  it('leaves an UNKNOWN @handle as plain text (no token, no wrong-kind body)', async () => {
    mockMembers();
    renderInProvider(<MarkdownMessage content={'hello @nobody here'} />);
    // members resolve, but @nobody matches no member → stays plain text.
    await waitFor(() => expect(screen.getByTestId('markdown-message')).toHaveTextContent('hello @nobody here'));
    expect(screen.queryByTestId('mention-token')).toBeNull();
  });
});

import type React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render as rtlRender, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { MessageList } from './MessageList';
import { useAppStore } from '@/store/app';
import type { Message } from '@/api/types';
import { OrgContext } from '@/OrgContext';
import { server } from '@/test/mswServer';

// jsdom does not implement scrollIntoView — the quote card's jump-to-original
// calls it, so stub it (per the harness contract) and spy on invocations.
beforeEach(() => {
  Element.prototype.scrollIntoView = vi.fn();
});

function renderFresh(ui: React.ReactElement) {
  const c = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return rtlRender(<QueryClientProvider client={c}>{ui}</QueryClientProvider>);
}

function renderFreshInOrg(ui: React.ReactElement) {
  const c = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return rtlRender(
    <QueryClientProvider client={c}>
      <OrgContext.Provider value={{ slug: 'test-org', orgId: 'O', orgName: 'Test Org' }}>{ui}</OrgContext.Provider>
    </QueryClientProvider>,
  );
}

function mockRefs() {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/tasks', () =>
      HttpResponse.json({
        items: [{ id: 'task-5ea6a6e8', org_ref: 'T86', project: { id: 'proj-x', name: 'X' }, title: 't', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' }],
        total: 1,
      }),
    ),
    http.get('/api/plans', () =>
      HttpResponse.json({
        items: [{ id: 'plan-86', org_ref: 'P86', project: { id: 'proj-x', name: 'X' }, name: 'p', status: 'running', has_failed: false, progress: { done: 0, total: 0 }, created_at: 'x', updated_at: 'x' }],
        total: 1,
      }),
    ),
  );
}

const sample = (id: string, content: string): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

// 引用 (quote) — the rendered quote card above a quoting message: a live target
// scrolls to + highlights the original; a removed target is a muted, inert
// placeholder.
describe('MessageList quote card (引用)', () => {
  beforeEach(() => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
  });
  afterEach(() => {
    cleanup();
    useAppStore.setState({ currentUserId: '' });
  });

  it('renders the quote card with the sender + snippet above the quoting message', () => {
    const target = sample('M1', 'the original message');
    const quoting: Message = {
      ...sample('M2', 'a reply that quotes'),
      quoted_message_id: 'M1',
      quoted_message: {
        id: 'M1',
        is_deleted: false,
        sender_identity_id: 'agent:arch1',
        content_snippet: 'the original message',
      },
    };
    renderFresh(<MessageList messages={[target, quoting]} />);
    const card = screen.getByTestId('message-quote-card');
    expect(card).toHaveAttribute('data-quote-target', 'M1');
    expect(card).toHaveTextContent('the original message');
    // unresolved sender (no members loaded) → clean handle, never the raw ref.
    expect(card).toHaveTextContent('arch1');
    expect(card.textContent).not.toContain('agent:arch1');
  });

  it('linkifies entity refs inside quote snippets through the shared renderer', async () => {
    mockRefs();
    const quoting: Message = {
      ...sample('M2', 'a reply that quotes'),
      quoted_message_id: 'M1',
      quoted_message: {
        id: 'M1',
        is_deleted: false,
        sender_identity_id: 'user:hayang',
        content_snippet: 'Plan P86 dispatched task-5ea6a6e8',
      },
    };
    renderFreshInOrg(<MessageList messages={[quoting]} />);
    expect(await screen.findByTestId('plan-ref-token')).toHaveTextContent('P86');
    const task = await screen.findByTestId('task-ref-token');
    expect(task).toHaveTextContent('T86');
    expect(task).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-5ea6a6e8');
  });

  it('clicking the quote card scrolls to + highlights the original message', () => {
    const target = sample('M1', 'the original message');
    const quoting: Message = {
      ...sample('M2', 'a reply that quotes'),
      quoted_message_id: 'M1',
      quoted_message: { id: 'M1', is_deleted: false, sender_identity_id: 'user:hayang', content_snippet: 'the original message' },
    };
    renderFresh(<MessageList messages={[target, quoting]} />);
    fireEvent.click(screen.getByTestId('message-quote-card'));
    // jump scrolls the target into view…
    expect(Element.prototype.scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' });
    // …and briefly rings the TARGET row (data-message-id M1), not the quoting one.
    const rows = screen.getAllByTestId('message-row');
    const targetRow = rows.find((r) => r.getAttribute('data-message-id') === 'M1');
    expect(targetRow?.className).toContain('ring-2');
  });

  it('renders a muted, non-clickable placeholder when the original was removed', () => {
    const quoting: Message = {
      ...sample('M2', 'a reply to a deleted message'),
      quoted_message_id: 'gone',
      quoted_message: { id: 'gone', is_deleted: true },
    };
    renderFresh(<MessageList messages={[quoting]} />);
    const card = screen.getByTestId('message-quote-card');
    expect(card).toHaveAttribute('data-quote-deleted', 'true');
    expect(card).toHaveTextContent('Original message unavailable');
    // NOT clickable — a plain <div>, no scroll on click.
    expect(card.tagName).toBe('DIV');
    fireEvent.click(card);
    expect(Element.prototype.scrollIntoView).not.toHaveBeenCalled();
  });

  it('renders no quote card for a plain message', () => {
    renderFresh(<MessageList messages={[sample('M1', 'plain')]} />);
    expect(screen.queryByTestId('message-quote-card')).not.toBeInTheDocument();
  });

  // Without a QuoteProvider (standalone list) the per-message Quote ACTION is
  // omitted — but a rendered quote card still shows (read-side is provider-free).
  it('omits the per-message Quote action when there is no QuoteProvider', () => {
    renderFresh(<MessageList messages={[sample('M1', 'hi')]} />);
    const row = screen.getByTestId('message-row');
    expect(within(row).queryByTestId('message-quote-btn')).not.toBeInTheDocument();
  });
});

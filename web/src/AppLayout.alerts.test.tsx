// v2.26.0 I61 — the "Needs your attention" rail panel. Its source is now the
// unified GET /attention endpoint: stuck tasks (running + input_required/obstacle)
// UNIONed with the human's directed unread (DM + @mention). The panel must render
// BOTH kinds, deep-link each to its source, and let a mention be dismissed
// (mark_seen) — all visible from ANY page via the rail badge + auto-opening popout.
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { OrgContext } from './OrgContext';

vi.mock('@/shell/secondaryNav', () => ({ SECONDARY_NAV_REGISTRY: {} }));

import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
  const store: Record<string, string> = {};
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => { store[k] = String(v); },
      removeItem: (k: string) => { delete store[k]; },
      clear: () => { for (const k of Object.keys(store)) delete store[k]; },
    },
    configurable: true,
  });
});

// A unified attention list as the backend would return it: already deduped and
// severity-then-recency sorted (input_required task → obstacle task → mention).
function seedAttention(items: unknown[]) {
  server.use(http.get('/api/attention', () => HttpResponse.json({ items })));
}

const TASK_INPUT = {
  kind: 'task', severity: 'urgent', ref: 'task-inp', task_id: 'task-inp',
  reason_type: 'input_required', title: 'Confirm the schema', snippet: 'please confirm column names',
  actor: '', conversation_id: '', project_id: 'p2', project_name: 'Beta', org_ref: 'T21',
  ts: '2026-06-30T00:00:00Z', route: '/projects/p2/tasks/task-inp',
};
const TASK_OBSTACLE = {
  kind: 'task', severity: 'warning', ref: 'task-obs', task_id: 'task-obs',
  reason_type: 'obstacle', title: 'Needs a deploy key', snippet: 'waiting on infra to provision a key',
  actor: '', conversation_id: '', project_id: 'p1', project_name: 'Alpha', org_ref: 'T20',
  ts: '2026-06-29T00:00:00Z', route: '/projects/p1/tasks/task-obs',
};
// The I61 core: an agent @mentioned the human in a task conversation with NO
// human-owned task/block — surfaces as a kind=mention item, dismissable.
const MENTION = {
  kind: 'mention', severity: 'warning', ref: 'conv-9', conversation_id: 'conv-9',
  conversation_kind: 'task', title: 'Blocked integrate', snippet: 'the integrate node is stuck on SQLITE_BUSY',
  actor: 'agent:AG1', mention_count: 1, unread_count: 1, message_id: 'msg-42',
  ts: '2026-06-30T02:00:00Z', route: '/projects/p3/tasks/task-x',
};

function renderShell() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug: 'acme', orgId: 'org-1', orgName: 'Acme' }}>
        <MemoryRouter initialEntries={['/channels']}>
          <Routes>
            <Route element={<AppLayout />}>
              <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
            </Route>
          </Routes>
        </MemoryRouter>
      </OrgContext.Provider>
    </QueryClientProvider>,
  );
}

describe('rail Alerts — unified attention panel (tasks + directed mentions)', () => {
  beforeEach(() => { localStorage.clear(); });
  afterEach(() => cleanup());

  it('badge + auto-opened panel render both sources, urgent first, each deep-linked', async () => {
    seedAttention([TASK_INPUT, TASK_OBSTACLE, MENTION]);
    renderShell();

    const badge = await screen.findByTestId('rail-alerts-badge');
    expect(badge).toHaveTextContent('3');

    const panel = await screen.findByTestId('rail-alerts-panel');
    const items = within(panel).getAllByTestId('rail-alert-item');
    expect(items).toHaveLength(3);

    // Order preserved as delivered (backend already sorted urgent→…).
    expect(items[0]).toHaveAttribute('data-kind', 'task');
    expect(items[0]).toHaveAttribute('data-reason-type', 'input_required');
    expect(items[0]).toHaveAttribute('href', '/organizations/acme/projects/p2/tasks/task-inp');
    expect(within(items[0]).getByText('Awaiting your reply')).toBeInTheDocument();

    expect(items[1]).toHaveAttribute('data-reason-type', 'obstacle');
    expect(within(items[1]).getByText('Needs intervention')).toBeInTheDocument();

    // The directed @mention item — deep-links to the source conversation, shows
    // the @-count, and carries a dismiss affordance (tasks do not).
    const mention = items[2];
    expect(mention).toHaveAttribute('data-kind', 'mention');
    expect(mention).toHaveAttribute('href', '/organizations/acme/projects/p3/tasks/task-x');
    expect(within(mention).getByText('Mentioned you')).toBeInTheDocument();
    expect(within(panel).getAllByTestId('rail-alert-dismiss')).toHaveLength(1);
  });

  it('dismissing a mention marks it seen (mark_seen) and it drops off the panel', async () => {
    seedAttention([MENTION]);
    let seenBody: { last_seen_message_id?: string } | null = null;
    server.use(
      http.post('/api/conversations/:id/seen', async ({ request, params }) => {
        seenBody = (await request.json()) as { last_seen_message_id?: string };
        expect(params.id).toBe('conv-9');
        // After catch-up the panel source is empty.
        seedAttention([]);
        return HttpResponse.json({ last_seen_message_id: 'msg-42', version: 2, bumped: true, event_id: 'e1' });
      }),
    );
    renderShell();

    const dismiss = await screen.findByTestId('rail-alert-dismiss');
    fireEvent.click(dismiss);

    await waitFor(() => expect(seenBody).not.toBeNull());
    expect(seenBody!.last_seen_message_id).toBe('msg-42');
    // The item drops out once attention refetches empty.
    await waitFor(() => expect(screen.getByTestId('rail-alerts-empty')).toBeInTheDocument());
  });

  it('renders no badge when nothing needs attention', async () => {
    seedAttention([]);
    renderShell();

    const btn = await screen.findByTestId('rail-alerts');
    expect(btn).toHaveAttribute('data-count', '0');
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    expect(screen.queryByTestId('rail-alerts-badge')).not.toBeInTheDocument();
    expect(screen.queryByTestId('rail-alerts-panel')).not.toBeInTheDocument();
  });
});

import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act, cleanup, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import DMs from './DMs';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement, route = '/dms') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('DMs page', () => {
  afterEach(() => cleanup());

  it('renders DM rows from the API', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'C-D1', kind: 'dm', name: '', status: 'active', peer_identity_id: 'agent:bot-1', peer_display_name: 'Bot One' },
          { id: 'C-D2', kind: 'dm', name: '', status: 'active' },
        ]),
      ),
    );
    wrap(<DMs />);
    await waitFor(() => expect(screen.getAllByTestId('dm-row')).toHaveLength(2));
    // v2.7.1 #215/Rule 2a: peer as @name; a peer-less DM reads "Direct message".
    expect(screen.getByText('@Bot One')).toBeInTheDocument();
    expect(screen.getByText('Direct message')).toBeInTheDocument();
    expect(screen.queryByText('C-D2')).not.toBeInTheDocument();
  });

  it('labels the DM group tabs in English (per @oopslink): Mine / Agent-to-agent', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'C-D1', kind: 'dm', name: '', status: 'active', peer_identity_id: 'agent:bot-1', peer_display_name: 'Bot One' },
        ]),
      ),
    );
    wrap(<DMs />);
    const mine = await screen.findByTestId('dms-tab-mine');
    expect(mine).toHaveTextContent('Mine');
    const agent = screen.getByTestId('dms-tab-agent-agent');
    expect(agent).toHaveTextContent('Agent-to-agent');
    // no Chinese labels remain.
    expect(screen.queryByText('我的')).not.toBeInTheDocument();
    expect(screen.queryByText(/Agent 间/)).not.toBeInTheDocument();
  });

  it('adds a "System DMs" tab that lists system deliveries by their target', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([
          { id: 'C-D1', kind: 'dm', name: '', status: 'active', dm_type: 'my_dm', peer_identity_id: 'agent:bot-1', peer_display_name: 'Bot One' },
          { id: 'C-SYS', kind: 'dm', name: '', status: 'active', dm_type: 'system_dm', peer_identity_id: 'tester3', peer_display_name: 'tester3' },
        ]),
      ),
    );
    wrap(<DMs />);
    const systemTab = await screen.findByTestId('dms-tab-system');
    expect(systemTab).toHaveTextContent('System DMs');
    // The system DM is NOT in the default "Mine" view.
    expect(screen.queryByText('@tester3')).not.toBeInTheDocument();
    // Switching to the System tab reveals it, labeled by the target.
    fireEvent.click(systemTab);
    expect(await screen.findByText('@tester3')).toBeInTheDocument();
    expect(screen.getByTestId('system-dm-badge')).toHaveTextContent('SYSTEM');
  });

  it('shows the empty state when there are no DMs', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<DMs />);
    await waitFor(() => expect(screen.getByTestId('dms-empty')).toBeInTheDocument());
  });

  it('Start a DM header button opens the modal', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<DMs />);
    await waitFor(() => expect(screen.getByTestId('dms-empty')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('dms-new-button'));
    expect(screen.getByTestId('dm-start-modal')).toBeInTheDocument();
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<DMs />);
    await waitFor(() => expect(screen.getByTestId('dms-error')).toHaveTextContent(/db down/));
  });
});

// v2.7 #198: DM rows carry a delete action (hard-delete conv+messages+read-state);
// confirmed via the shared ConfirmModal; errors (403 authz) surface, never silent.
describe('DMs delete (#198)', () => {
  afterEach(() => cleanup());

  const oneDm = () =>
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json([{ id: 'C-D1', kind: 'dm', name: 'with bot-1', status: 'active' }]),
      ),
    );

  it('exposes a delete action per DM row', async () => {
    oneDm();
    wrap(<DMs />);
    const btn = await screen.findByTestId('dm-delete-button');
    expect(btn).toHaveAttribute('data-dm-id', 'C-D1');
  });

  it('confirms (naming the DM) before posting DELETE', async () => {
    let deleted: string | null = null;
    oneDm();
    server.use(
      http.delete('/api/conversations/C-D1', () => {
        deleted = 'C-D1';
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap(<DMs />);
    fireEvent.click(await screen.findByTestId('dm-delete-button'));
    const modal = await screen.findByTestId('confirm-modal');
    expect(modal).toHaveTextContent('with bot-1');
    await act(async () => {
      fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    });
    await waitFor(() => expect(deleted).toBe('C-D1'));
  });

  it('can be canceled without deleting', async () => {
    let deleted = false;
    oneDm();
    server.use(
      http.delete('/api/conversations/C-D1', () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap(<DMs />);
    fireEvent.click(await screen.findByTestId('dm-delete-button'));
    fireEvent.click(await screen.findByTestId('confirm-modal-cancel'));
    await waitFor(() => expect(screen.queryByTestId('confirm-modal')).not.toBeInTheDocument());
    expect(deleted).toBe(false);
  });

  it('surfaces a delete error instead of failing silently (Rule 9)', async () => {
    oneDm();
    server.use(
      http.delete('/api/conversations/C-D1', () =>
        HttpResponse.json({ error: 'not_a_participant', message: 'not a participant' }, { status: 403 }),
      ),
    );
    wrap(<DMs />);
    fireEvent.click(await screen.findByTestId('dm-delete-button'));
    await act(async () => {
      fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    });
    await waitFor(() =>
      expect(screen.getByTestId('dm-delete-error')).toHaveTextContent(/only a participant can delete this dm/i),
    );
  });

  // v2.10.2 [T129] Mobile 二级段控 (Channels | DMs) — on mobile col② is hidden,
  // so this page surfaces the Channels↔DMs switch (DMs active here).
  it('renders the mobile Conversations segmented nav (DMs active, Channels reachable)', async () => {
    server.use(http.get('/api/conversations', () => HttpResponse.json([])));
    wrap(<DMs />, '/dms');
    const nav = await screen.findByTestId('segmented-nav');
    const channels = within(nav).getByTestId('conv-seg-channels');
    const dms = within(nav).getByTestId('conv-seg-dms');
    expect(dms).toHaveAttribute('data-active', 'true');
    expect(channels).toHaveAttribute('data-active', 'false');
    expect(channels).toHaveAttribute('href', '/channels');
  });
});

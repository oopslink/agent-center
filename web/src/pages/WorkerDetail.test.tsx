import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import WorkerDetail from './WorkerDetail';

const worker = (extra: Record<string, unknown> = {}) => ({
  worker_id: 'worker-abc123',
  organization_id: 'O-1',
  name: 'Worker One',
  status: 'online',
  last_acked_offset: 0,
  last_heartbeat_at: '2026-06-06T12:00:00Z',
  created_at: '2026-06-06T10:00:00Z',
  updated_at: '2026-06-06T12:00:00Z',
  version: 1,
  ...extra,
});

function wrap(path = '/workers/worker-abc123') {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/workers/:id" element={<WorkerDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

describe('WorkerDetail (shell)', () => {
  it('renders header (name + status badge + #192 id handle) and 4 tabs', async () => {
    server.use(http.get('/api/workers/:id', () => HttpResponse.json(worker())));
    wrap();
    expect(await screen.findByRole('heading', { name: 'Worker One' })).toBeInTheDocument();
    // status badge not-color-only: has a text label, not just color
    expect(screen.getByTestId('worker-status-badge')).toHaveTextContent('Online');
    // #192: worker_id handle exposes the full id on hover (title), visible chrome is the handle
    expect(screen.getByTestId('worker-id-handle')).toHaveAttribute('title', 'worker-abc123');
    for (const k of ['profile', 'agents', 'management', 'activity']) {
      expect(screen.getByTestId(`worker-tab-${k}`)).toBeInTheDocument();
    }
    // default tab = profile
    expect(screen.getByTestId('worker-tab-profile')).toHaveAttribute('aria-selected', 'true');
  });

  it('clicking a tab switches the active panel', async () => {
    server.use(http.get('/api/workers/:id', () => HttpResponse.json(worker())));
    wrap();
    await screen.findByRole('heading', { name: 'Worker One' });
    fireEvent.click(screen.getByTestId('worker-tab-agents'));
    expect(screen.getByTestId('worker-tab-agents')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('worker-tabpanel-agents')).toBeInTheDocument();
  });

  it('Activity tab shows the v2.9 deferred-with-pointer placeholder', async () => {
    server.use(http.get('/api/workers/:id', () => HttpResponse.json(worker())));
    wrap();
    await screen.findByRole('heading', { name: 'Worker One' });
    fireEvent.click(screen.getByTestId('worker-tab-activity'));
    expect(screen.getByTestId('worker-activity-stub')).toHaveTextContent('Coming in v2.9');
  });

  it('manual activation: ArrowRight moves focus only, does NOT change the active tab', async () => {
    server.use(http.get('/api/workers/:id', () => HttpResponse.json(worker())));
    wrap();
    await screen.findByRole('heading', { name: 'Worker One' });
    fireEvent.keyDown(screen.getByTestId('worker-tabs'), { key: 'ArrowRight' });
    expect(screen.getByTestId('worker-tab-agents')).toHaveFocus();
    // selection unchanged (manual — arrow does not activate)
    expect(screen.getByTestId('worker-tab-profile')).toHaveAttribute('aria-selected', 'true');
  });

  it('offline worker → Offline badge', async () => {
    server.use(http.get('/api/workers/:id', () => HttpResponse.json(worker({ status: 'offline' }))));
    wrap();
    await screen.findByRole('heading', { name: 'Worker One' });
    expect(screen.getByTestId('worker-status-badge')).toHaveTextContent('Offline');
  });

  it('Management tab exposes the Force delete action which opens the typed-name modal', async () => {
    server.use(
      http.get('/api/workers/:id', () => HttpResponse.json(worker())),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
    );
    wrap();
    await screen.findByRole('heading', { name: 'Worker One' });
    fireEvent.click(screen.getByTestId('worker-tab-management'));
    fireEvent.click(await screen.findByTestId('worker-force-delete'));
    expect(screen.getByTestId('force-delete-modal')).toBeInTheDocument();
    // gated until the worker name is typed exactly
    expect(screen.getByTestId('force-delete-confirm')).toBeDisabled();
  });

  it('not-found → error + back-to-Environment link', async () => {
    server.use(
      http.get('/api/workers/:id', () =>
        HttpResponse.json({ error: 'worker_not_found', message: 'no worker' }, { status: 404 }),
      ),
    );
    wrap();
    expect(await screen.findByTestId('worker-not-found')).toBeInTheDocument();
  });
});

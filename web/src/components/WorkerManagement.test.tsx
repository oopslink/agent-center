import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { WorkerManagement } from './WorkerManagement';
import type { EnvWorker } from '@/api/types';

const worker = (extra: Partial<EnvWorker> = {}): EnvWorker => ({
  worker_id: 'w-1',
  organization_id: 'O',
  name: 'Worker One',
  status: 'online',
  last_acked_offset: 0,
  created_at: '2026-06-06T09:00:00Z',
  updated_at: '2026-06-06T12:00:00Z',
  version: 1,
  ...extra,
});

function wrap(w = worker(), agentsResp: { agents: unknown[] } = { agents: [] }) {
  server.use(http.get('/api/agents', () => HttpResponse.json(agentsResp)));
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <WorkerManagement worker={w} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(cleanup);

describe('WorkerManagement', () => {
  it('renders rename / install / re-mint / remove actions', () => {
    wrap();
    expect(screen.getByTestId('worker-rename-edit')).toBeInTheDocument();
    expect(screen.getByTestId('worker-install-show')).toBeInTheDocument();
    expect(screen.getByTestId('worker-install-remint')).toBeInTheDocument();
    expect(screen.getByTestId('worker-remove')).toBeInTheDocument();
  });

  it('rename PATCHes the name endpoint', async () => {
    let patched: string | null = null;
    server.use(
      http.patch('/api/workers/w-1/name', async ({ request }) => {
        const b = (await request.json()) as { name: string };
        patched = b.name;
        return HttpResponse.json({});
      }),
    );
    wrap();
    fireEvent.click(screen.getByTestId('worker-rename-edit'));
    fireEvent.change(screen.getByTestId('worker-rename-input'), { target: { value: 'New Name' } });
    fireEvent.click(screen.getByTestId('worker-rename-save'));
    await waitFor(() => expect(patched).toBe('New Name'));
  });

  it('Remove opens a ConfirmModal warning about bound agents (informed consent)', async () => {
    wrap(worker(), {
      agents: [{ id: 'A1', name: 'a', lifecycle: 'running', availability: 'available', worker_id: 'w-1' }],
    });
    fireEvent.click(screen.getByTestId('worker-remove'));
    // once useAgents resolves, the confirm message includes the bound-agent count
    await waitFor(() =>
      expect(screen.getByRole('dialog')).toHaveTextContent('1 agent(s) are bound'),
    );
  });

  it('Remove confirm hard-DELETEs the worker', async () => {
    let deleted = false;
    server.use(
      http.delete('/api/workers/w-1', () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap();
    fireEvent.click(screen.getByTestId('worker-remove'));
    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(deleted).toBe(true));
  });

  it('renders the Force delete action', () => {
    wrap();
    expect(screen.getByTestId('worker-force-delete')).toBeInTheDocument();
  });

  // v2.8.1: force-delete — typed-name confirm → DELETE ?force=true → 200 surfaces
  // the unbound-agents count.
  it('force-deletes the worker with ?force=true and notes N unbound agents', async () => {
    let forceQuery: string | null = null;
    server.use(
      http.delete('/api/workers/w-1', ({ request }) => {
        forceQuery = new URL(request.url).searchParams.get('force');
        return HttpResponse.json({ ok: true, unbound_agents: 2 });
      }),
    );
    wrap();
    fireEvent.click(screen.getByTestId('worker-force-delete'));
    const confirm = screen.getByTestId('force-delete-confirm');
    expect(confirm).toBeDisabled();
    // displayName falls back to name → "Worker One"
    fireEvent.change(screen.getByTestId('force-delete-input'), { target: { value: 'Worker One' } });
    expect(confirm).toBeEnabled();
    fireEvent.click(confirm);
    await waitFor(() => expect(forceQuery).toBe('true'));
    expect(await screen.findByTestId('worker-force-delete-note')).toHaveTextContent('2 agent(s) unbound');
  });

  it('keeps the force-delete modal open and surfaces a 409 error', async () => {
    server.use(
      http.delete('/api/workers/w-1', () =>
        HttpResponse.json({ error: 'worker_busy', message: 'worker is busy' }, { status: 409 }),
      ),
    );
    wrap();
    fireEvent.click(screen.getByTestId('worker-force-delete'));
    fireEvent.change(screen.getByTestId('force-delete-input'), { target: { value: 'Worker One' } });
    fireEvent.click(screen.getByTestId('force-delete-confirm'));
    expect(await screen.findByTestId('force-delete-error')).toHaveTextContent('worker is busy');
    expect(screen.getByTestId('force-delete-modal')).toBeInTheDocument();
  });
});

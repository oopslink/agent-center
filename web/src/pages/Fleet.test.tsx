import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Fleet from './Fleet';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Fleet page', () => {
  afterEach(() => cleanup());

  it('renders all segments when populated (v2.7: work_items, no input-requests segment)', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          work_items: [
            {
              work_item_id: 'WI-1',
              agent_id: 'AG-1',
              task_id: 'T-1',
              status: 'active',
              current_activity: 'edit',
              total_tool_calls: 2,
              total_tokens_input: 100,
              total_tokens_output: 50,
              working_seconds: 0,
              last_activity_at: '2026-05-24T01:00:00Z',
            },
          ],
          workers: [
            {
              worker_id: 'w-1',
              status: 'online',
              active_count: 1,
              last_heartbeat_at: '2026-05-24T01:00:01Z',
            },
          ],
          pending_issues: [
            {
              issue_id: 'I-1',
              project_id: 'P-1',
              title: 'fix login',
              status: 'open',
              opened_at: '2026-05-24T00:00:00Z',
              opener: 'user:hayang',
            },
          ],
          generated_at: '2026-05-24T01:00:03Z',
        }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-workers-table')).toBeInTheDocument());
    expect(screen.getByTestId('fleet-worker-row')).toHaveAttribute('data-worker-id', 'w-1');
    expect(screen.getByTestId('fleet-workitem-row')).toHaveAttribute('data-work-item-id', 'WI-1');
    expect(screen.getByText('fix login')).toBeInTheDocument();
    expect(screen.getByTestId('fleet-generated-at')).toHaveTextContent('2026-05-24T01:00:03Z');
  });

  it('empty fleet shows enroll-worker hint', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          work_items: [],
          workers: [],
          pending_issues: [],
        }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-workers-empty')).toHaveTextContent(/No workers connected yet/));
    expect(screen.getByTestId('fleet-workers-empty-cta')).toHaveTextContent(/Add your first worker/);
    expect(screen.getByTestId('fleet-workitem-empty')).toBeInTheDocument();
  });

  it('shows warnings banner when backend returns partial snapshot', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          work_items: [],
          workers: [],
          pending_issues: [],
          warnings: ['workers segment errored: db down'],
        }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-warnings')).toBeInTheDocument());
    expect(screen.getByTestId('fleet-warnings')).toHaveTextContent(/db down/);
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({ error: 'fleet_not_wired', message: 'down' }, { status: 501 }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-error')).toHaveTextContent(/down/));
  });

  // v2.4-D-F4: when a workforce.worker.enrolled event arrives, the
  // matching row picks up data-just-enrolled="true" for ~3s so the
  // user sees which row is the one they just connected.
  it('highlights a newly enrolled worker row when the DOM event fires', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          work_items: [],
          workers: [
            {
              worker_id: 'w-1',
              status: 'online',
              active_count: 0,
              last_heartbeat_at: '2026-05-24T01:00:01Z',
            },
          ],
          pending_issues: [],
        }),
      ),
    );
    wrap(<Fleet />);
    await waitFor(() => expect(screen.getByTestId('fleet-worker-row')).toBeInTheDocument());
    act(() =>
      window.dispatchEvent(
        new CustomEvent('agent-center:worker-enrolled', {
          detail: { worker_id: 'w-1' },
        }),
      ),
    );
    await waitFor(() =>
      expect(screen.getByTestId('fleet-worker-row')).toHaveAttribute('data-just-enrolled', 'true'),
    );
  });
});

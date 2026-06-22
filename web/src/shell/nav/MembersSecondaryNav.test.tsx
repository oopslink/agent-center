// T235 — Members col② secondary nav: each agent row carries status chips
// (Lifecycle + Availability) plus a derived Idle/Busy chip for running agents.
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { MembersSecondaryNav } from './MembersSecondaryNav';

// Offsets are computed from the REAL clock (the component reads the same
// Date.now() at render) so the 5-min idle threshold is exercised deterministically
// without fake timers — fake timers would stall react-query/msw async polling.
const NOW = Date.now();

function agentRow(over: Record<string, unknown>) {
  return {
    id: 'A-x', organization_id: 'O-1', name: 'agent-x', description: '',
    model: 'claude-opus', cli: 'claudecode', env_vars: {}, skills: [], worker_id: 'w-1',
    lifecycle: 'stopped', availability: 'available', created_by: 'user:hayang', version: 1,
    created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T02:00:00Z',
    ...over,
  };
}

function renderNav() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/agents']}>
        <MembersSecondaryNav orgBase="" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/agents', () =>
      HttpResponse.json({
        agents: [
          // running + recent activity → busy
          agentRow({ id: 'A-busy', name: 'busy-bot', lifecycle: 'running', availability: 'busy',
            last_activity_at: new Date(NOW - 60_000).toISOString() }),
          // running + stale (>5 min) activity → idle (green)
          agentRow({ id: 'A-idle', name: 'idle-bot', lifecycle: 'running', availability: 'available',
            last_activity_at: new Date(NOW - 10 * 60_000).toISOString() }),
          // stopped → no idle/busy chip, just lifecycle/availability
          agentRow({ id: 'A-stop', name: 'stopped-bot', lifecycle: 'stopped', availability: 'unavailable' }),
          // archived → excluded entirely
          agentRow({ id: 'A-arch', name: 'archived-bot', lifecycle: 'archived', availability: 'unavailable' }),
        ],
      }),
    ),
  );
});

afterEach(() => cleanup());

describe('MembersSecondaryNav agent status (T235)', () => {
  it('shows Availability + Activity for a running agent and hides the redundant RUNNING chip (T320)', async () => {
    renderNav();
    const busyLink = await screen.findByRole('link', { name: /busy-bot/ });
    const busyRow = busyLink.closest('li') as HTMLElement;
    // T320: a running agent's lifecycle is implied → no RUNNING chip; show the
    // two meaningful axes instead.
    expect(within(busyRow).queryByTestId('agent-lifecycle-badge')).toBeNull();
    expect(within(busyRow).getByTestId('agent-availability-badge')).toHaveTextContent('busy');
    expect(within(busyRow).getByTestId('agent-activity-status-badge')).toBeInTheDocument();
  });

  it('marks a running agent with recent activity as Active (T320 label), data busy', async () => {
    renderNav();
    const link = await screen.findByRole('link', { name: /busy-bot/ });
    const row = link.closest('li') as HTMLElement;
    const status = within(row).getByTestId('agent-activity-status-badge');
    expect(status).toHaveAttribute('data-activity-status', 'busy');
    // T320: the label disambiguates from Availability's "busy" — it reads "Active".
    expect(status).toHaveTextContent(/active/i);
  });

  it('marks a running agent idle (green) after 5 min without activity', async () => {
    renderNav();
    const link = await screen.findByRole('link', { name: /idle-bot/ });
    const row = link.closest('li') as HTMLElement;
    const status = within(row).getByTestId('agent-activity-status-badge');
    expect(status).toHaveAttribute('data-activity-status', 'idle');
    expect(status.className).toContain('text-success');
  });

  it('shows NO Idle/Busy chip for a non-running agent', async () => {
    renderNav();
    const link = await screen.findByRole('link', { name: /stopped-bot/ });
    const row = link.closest('li') as HTMLElement;
    expect(within(row).getByTestId('agent-lifecycle-badge')).toHaveTextContent('stopped');
    expect(within(row).queryByTestId('agent-activity-status-badge')).toBeNull();
  });

  it('still excludes archived agents from the list', async () => {
    renderNav();
    await screen.findByRole('link', { name: /busy-bot/ });
    expect(screen.queryByText('archived-bot')).toBeNull();
  });
});

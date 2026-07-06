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
    model: 'claude-opus', cli: 'claudecode', env_vars: {}, worker_id: 'w-1',
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

describe('MembersSecondaryNav agent status (T322 single status)', () => {
  it('shows ONE unified status pill per agent (not three chips)', async () => {
    renderNav();
    const busyLink = await screen.findByRole('link', { name: /busy-bot/ });
    const busyRow = busyLink.closest('li') as HTMLElement;
    // T322: a single status badge replaces the lifecycle/availability/activity trio.
    expect(within(busyRow).getByTestId('agent-status-badge')).toBeInTheDocument();
    expect(within(busyRow).queryByTestId('agent-lifecycle-badge')).toBeNull();
    expect(within(busyRow).queryByTestId('agent-availability-badge')).toBeNull();
    expect(within(busyRow).queryByTestId('agent-activity-status-badge')).toBeNull();
  });

  it('a running+busy agent reads "Busy"', async () => {
    renderNav();
    const link = await screen.findByRole('link', { name: /busy-bot/ });
    const status = within(link.closest('li') as HTMLElement).getByTestId('agent-status-badge');
    expect(status).toHaveAttribute('data-agent-status', 'busy');
    expect(status).toHaveTextContent(/busy/i);
  });

  it('a running+available+quiet agent reads "Idle"', async () => {
    renderNav();
    const link = await screen.findByRole('link', { name: /idle-bot/ });
    const status = within(link.closest('li') as HTMLElement).getByTestId('agent-status-badge');
    expect(status).toHaveAttribute('data-agent-status', 'idle');
    expect(status).toHaveTextContent(/idle/i);
  });

  it('a non-running agent reads "Stopped"', async () => {
    renderNav();
    const link = await screen.findByRole('link', { name: /stopped-bot/ });
    const status = within(link.closest('li') as HTMLElement).getByTestId('agent-status-badge');
    expect(status).toHaveAttribute('data-agent-status', 'stopped');
    expect(status).toHaveTextContent(/stopped/i);
  });

  it('still excludes archived agents from the list', async () => {
    renderNav();
    await screen.findByRole('link', { name: /busy-bot/ });
    expect(screen.queryByText('archived-bot')).toBeNull();
  });
});

// v2.10.0 [T7] Members — col② secondary nav. The Members module registers a
// custom col② (MembersSecondaryNav) via SECONDARY_NAV_REGISTRY: Humans + Agents
// sections, each with an "All …" row plus the individual members. This test
// drives it through the real AppLayout shell so it also pins the registry wiring.
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
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

beforeEach(() => {
  localStorage.clear();
  server.use(
    http.get('/api/agents', () =>
      HttpResponse.json({
        agents: [
          { id: 'A-run', organization_id: 'O-1', name: 'dev2', lifecycle: 'running', availability: 'available', version: 1, created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-01T00:00:00Z' },
          { id: 'A-stop', organization_id: 'O-1', name: 'tester2', lifecycle: 'stopped', availability: 'available', version: 1, created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-01T00:00:00Z' },
          { id: 'A-arch', organization_id: 'O-1', name: 'ghost', lifecycle: 'archived', availability: 'unavailable', version: 1, created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-01T00:00:00Z' },
        ],
      }),
    ),
    http.get('/api/members', () =>
      HttpResponse.json([
        { id: 'mem-1', organization_id: 'org-test', identity_id: 'user:hayang', kind: 'user', display_name: 'oopslink', role: 'owner', status: 'joined' },
        { id: 'mem-2', organization_id: 'org-test', identity_id: 'agent:A-run', kind: 'agent', display_name: 'dev2', role: 'member', status: 'joined' },
      ]),
    ),
  );
});

function renderShell(initial = '/members/humans') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/members/humans" element={<div data-testid="page-Humans">x</div>} />
            <Route path="/agents" element={<div data-testid="page-Agents">x</div>} />
            <Route path="/agents/:id" element={<div data-testid="page-AgentDetail">x</div>} />
            <Route path="/users/:id" element={<div data-testid="page-UserDetail">x</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('col② Members — registered custom nav (Humans/Agents sections)', () => {
  afterEach(() => cleanup());

  it('the Members module renders its custom nav (not the shell default group)', () => {
    renderShell('/members/humans');
    expect(screen.getByTestId('members-secondary-nav')).toBeInTheDocument();
    // The shell default NavGroup for Members must NOT render.
    expect(screen.queryByTestId('sidebar-group-toggle-Members')).not.toBeInTheDocument();
    // "All …" rows point at the list/table pages.
    expect(screen.getByTestId('members-all-humans')).toHaveAttribute('href', '/members/humans');
    expect(screen.getByTestId('members-all-agents')).toHaveAttribute('href', '/agents');
  });

  it('the Agents section lists agent names (archived dropped); a row opens AgentDetail', async () => {
    renderShell('/members/humans');
    await waitFor(() => {
      const list = screen.getByTestId('members-section-list-agents');
      expect(list.textContent).toContain('dev2');
      expect(list.textContent).toContain('tester2');
    });
    const list = screen.getByTestId('members-section-list-agents');
    expect(list.textContent).not.toContain('ghost'); // archived agent excluded
    const row = within(list).getByRole('link', { name: 'tester2' });
    expect(row).toHaveAttribute('href', '/agents/A-stop');
    fireEvent.click(row);
    expect(screen.getByTestId('page-AgentDetail')).toBeInTheDocument();
  });

  it('the Humans section lists human members linking to UserDetail (agent members excluded)', async () => {
    renderShell('/members/humans');
    await waitFor(() => {
      const list = screen.getByTestId('members-section-list-humans');
      expect(list.textContent).toContain('oopslink');
    });
    const list = screen.getByTestId('members-section-list-humans');
    const row = within(list).getByRole('link', { name: 'oopslink' });
    expect(row).toHaveAttribute('href', '/users/hayang');
  });

  it('a section collapses via its toggle', async () => {
    renderShell('/members/humans');
    await waitFor(() => expect(screen.getByTestId('members-section-list-agents')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('members-section-toggle-agents'));
    expect(screen.queryByTestId('members-section-list-agents')).not.toBeInTheDocument();
  });
});

// v2.5.x #63 — Sidebar collapsible groups + Channels/DMs sub-lists.
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

// Polyfill localStorage so the per-group / per-subitem persist effects
// work in the test env (matches AppLayout.p6.test.tsx setup).
beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
  const store: Record<string, string> = {};
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => {
        store[k] = String(v);
      },
      removeItem: (k: string) => {
        delete store[k];
      },
      clear: () => {
        for (const k of Object.keys(store)) delete store[k];
      },
    },
    configurable: true,
  });
});

beforeEach(() => {
  localStorage.clear();
  server.use(http.get('/api/input_requests', () => HttpResponse.json([])));
  // Three channels + two DMs to seed the sub-lists.
  server.use(
    http.get('/api/conversations', ({ request }) => {
      const url = new URL(request.url);
      const kind = url.searchParams.get('kind');
      if (kind === 'channel') {
        return HttpResponse.json([
          { id: 'C1', kind: 'channel', name: 'all', status: 'active', participants: [] },
          { id: 'C2', kind: 'channel', name: 'agent-center', status: 'active', participants: [] },
          { id: 'C3', kind: 'channel', name: 'general', status: 'active', participants: [] },
        ]);
      }
      if (kind === 'dm') {
        return HttpResponse.json([
          {
            id: 'D1',
            kind: 'dm',
            status: 'active',
            participants: [
              { identity_id: 'user:hayang' },
              { identity_id: 'user:other' },
            ],
          },
          {
            id: 'D2',
            kind: 'dm',
            status: 'active',
            participants: [
              { identity_id: 'user:hayang' },
              { identity_id: 'agent:Sam' },
            ],
          },
        ]);
      }
      return HttpResponse.json([]);
    }),
  );
});

function renderShell(initial = '/channels') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
            <Route path="/dms" element={<div data-testid="page-DMs">x</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppLayout sidebar — collapsible groups (v2.5.x #63)', () => {
  afterEach(() => cleanup());

  it('renders each group as a collapsible button + items expanded by default', () => {
    renderShell();
    expect(screen.getByTestId('sidebar-group-toggle-Conversations')).toBeInTheDocument();
    expect(screen.getByTestId('sidebar-group-toggle-Work')).toBeInTheDocument();
    expect(screen.getByTestId('sidebar-group-toggle-System')).toBeInTheDocument();
    // Conversations expanded by default → Channels + DMs links visible.
    expect(screen.getByRole('link', { name: /channels/i })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /dms/i })).toBeInTheDocument();
  });

  it('clicking a group toggle collapses its items', () => {
    renderShell();
    const toggle = screen.getByTestId('sidebar-group-toggle-Conversations');
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    // Channels/DMs links should be hidden now.
    expect(screen.queryByRole('link', { name: /channels/i })).not.toBeInTheDocument();
  });

  it('persists group state in localStorage', () => {
    renderShell();
    fireEvent.click(screen.getByTestId('sidebar-group-toggle-Conversations'));
    const stored = localStorage.getItem('ac.sidebar.groups');
    expect(stored).toBeTruthy();
    const parsed = JSON.parse(stored as string);
    expect(parsed.Conversations).toBe(false);
  });

  it('Channels item exposes a sub-list of channel names when expanded', async () => {
    renderShell();
    await waitFor(() => {
      const list = screen.getByTestId('sidebar-subitem-list-/channels');
      expect(list.textContent).toContain('# all');
    });
    const list = screen.getByTestId('sidebar-subitem-list-/channels');
    expect(list.textContent).toContain('# agent-center');
    expect(list.textContent).toContain('# general');
  });

  it('DMs item exposes a sub-list of DM peers when expanded', async () => {
    renderShell();
    await waitFor(() => {
      const list = screen.getByTestId('sidebar-subitem-list-/dms');
      // Peer label is the other participant's identity id since we don't
      // populate currentUserId in the test store.
      expect(list.textContent).toMatch(/@ /);
    });
  });

  it('clicking the sub-item toggle collapses the channel sub-list', async () => {
    renderShell();
    await waitFor(() => {
      expect(screen.getByTestId('sidebar-subitem-list-/channels')).toBeInTheDocument();
    });
    const subToggle = screen.getByTestId('sidebar-subitem-toggle-/channels');
    expect(subToggle).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(subToggle);
    expect(subToggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTestId('sidebar-subitem-list-/channels')).not.toBeInTheDocument();
  });

  it('sub-item state persists to localStorage', async () => {
    renderShell();
    await waitFor(() =>
      expect(screen.getByTestId('sidebar-subitem-toggle-/channels')).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId('sidebar-subitem-toggle-/channels'));
    await waitFor(() => {
      const stored = localStorage.getItem('ac.sidebar.subitems');
      expect(stored).toBeTruthy();
      const parsed = JSON.parse(stored as string);
      expect(parsed['/channels']).toBe(false);
    });
  });
});

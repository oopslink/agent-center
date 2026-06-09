// v2.5.x #63 — Sidebar collapsible groups + Channels/DMs sub-lists.
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
            peer_identity_id: 'user:other',
            peer_display_name: 'Other',
          },
          {
            id: 'D2',
            kind: 'dm',
            status: 'active',
            peer_identity_id: 'agent:Sam',
            peer_display_name: 'Sam',
          },
        ]);
      }
      return HttpResponse.json([]);
    }),
  );
  // v2.5.x #67 — Projects sub-list under Workspace group.
  server.use(
    http.get('/api/projects', () =>
      HttpResponse.json({
        projects: [
          {
            id: 'proj-abc',
            organization_id: 'org-test',
            name: 'agent-center',
            description: '',
            status: 'active',
            created_by: 'user:hayang',
            version: 1,
            created_at: '2026-05-27T00:00:00Z',
            updated_at: '2026-05-27T00:00:00Z',
          },
          {
            id: 'proj-def',
            organization_id: 'org-test',
            name: 'sandbox',
            description: '',
            status: 'active',
            created_by: 'user:hayang',
            version: 1,
            created_at: '2026-05-27T00:00:00Z',
            updated_at: '2026-05-27T00:00:00Z',
          },
        ],
      }),
    ),
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
            <Route path="/dms/:id" element={<div data-testid="page-DMDetail">x</div>} />
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
      // v2.7.1 #215: DM sidebar labels are the backend-resolved peer as @name.
      expect(list.textContent).toContain('@Sam');
      expect(list.textContent).toContain('@Other');
    });
  });

  it('shows a manual delete action for deleted-peer DMs in the sidebar', async () => {
    server.use(
      http.get('/api/conversations', ({ request }) => {
        const kind = new URL(request.url).searchParams.get('kind');
        if (kind === 'dm') {
          return HttpResponse.json([
            {
              id: 'D-DELETED',
              kind: 'dm',
              status: 'active',
              peer_identity_id: 'agent:gone',
              unread_count: 0,
              mention_count: 0,
            },
            {
              id: 'D-LIVE',
              kind: 'dm',
              status: 'active',
              peer_identity_id: 'agent:live',
              peer_display_name: 'Live',
              unread_count: 0,
              mention_count: 0,
            },
          ]);
        }
        return HttpResponse.json([]);
      }),
    );
    renderShell();
    const list = await screen.findByTestId('sidebar-subitem-list-/dms');
    await waitFor(() => expect(list.textContent).toContain('(deleted)'));
    expect(screen.getByText('@Live')).toBeInTheDocument();
    const deleteButtons = screen.getAllByTestId('sidebar-dm-delete-button');
    expect(deleteButtons).toHaveLength(1);
    expect(deleteButtons[0]).toHaveAttribute('aria-label', 'Delete DM (deleted)');
  });

  it('confirms before deleting a deleted-peer DM from the sidebar', async () => {
    let deleted: string | null = null;
    server.use(
      http.get('/api/conversations', ({ request }) => {
        const kind = new URL(request.url).searchParams.get('kind');
        if (kind === 'dm') {
          return HttpResponse.json([
            {
              id: 'D-DELETED',
              kind: 'dm',
              status: 'active',
              peer_identity_id: 'agent:gone',
              unread_count: 0,
              mention_count: 0,
            },
          ]);
        }
        return HttpResponse.json([]);
      }),
      http.delete('/api/conversations/D-DELETED', () => {
        deleted = 'D-DELETED';
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderShell('/dms/D-DELETED');
    fireEvent.click(await screen.findByTestId('sidebar-dm-delete-button'));
    expect(await screen.findByTestId('confirm-modal')).toHaveTextContent('(deleted)');
    await act(async () => {
      fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    });
    await waitFor(() => expect(deleted).toBe('D-DELETED'));
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

  // v2.5.x #67 — Projects mirrors the Channels/DMs sub-list pattern
  // under the Workspace group so the operator can see each project by
  // name without first navigating to /projects.
  it('Projects item exposes a sub-list of project names when expanded', async () => {
    renderShell();
    await waitFor(() => {
      const list = screen.getByTestId('sidebar-subitem-list-/projects');
      expect(list.textContent).toContain('agent-center');
    });
    const list = screen.getByTestId('sidebar-subitem-list-/projects');
    expect(list.textContent).toContain('sandbox');
    // Each sub-link routes to /projects/<id>.
    const links = list.querySelectorAll('a[data-testid="sidebar-subitem-link"]');
    const hrefs = Array.from(links).map((a) => a.getAttribute('href'));
    expect(hrefs).toContain('/projects/proj-abc');
    expect(hrefs).toContain('/projects/proj-def');
  });

  it('Projects sub-list toggle collapses + persists like Channels/DMs', async () => {
    renderShell();
    await waitFor(() =>
      expect(screen.getByTestId('sidebar-subitem-toggle-/projects')).toBeInTheDocument(),
    );
    const subToggle = screen.getByTestId('sidebar-subitem-toggle-/projects');
    expect(subToggle).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(subToggle);
    expect(subToggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTestId('sidebar-subitem-list-/projects')).not.toBeInTheDocument();
    await waitFor(() => {
      const stored = localStorage.getItem('ac.sidebar.subitems');
      expect(stored).toBeTruthy();
      const parsed = JSON.parse(stored as string);
      expect(parsed['/projects']).toBe(false);
    });
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

  // v2.8.1 #278: "Topbar→sidebar" chrome — org switcher + ⌘K search live in
  // the sidebar top; CAPS section labels + right-aligned real-count badges;
  // bottom area = live + segmented theme + user + Sign out.
  it('hosts the org switcher + ⌘K search trigger inside the sidebar', () => {
    renderShell();
    expect(screen.getByTestId('org-switcher')).toBeInTheDocument();
    const search = screen.getByTestId('open-palette');
    expect(search).toHaveAttribute('aria-label', expect.stringMatching(/search/i));
  });

  it('renders CAPS section labels as headings', () => {
    renderShell();
    const labels = screen.getAllByTestId('section-label').map((n) => n.textContent);
    expect(labels).toContain('Workspace');
    expect(labels).toContain('Conversations');
    expect(labels).toContain('System');
  });

  it('count badges reflect real hook counts with accessible labels', async () => {
    renderShell();
    // 3 channels + 2 DMs + 2 projects seeded in beforeEach.
    await waitFor(() => {
      expect(screen.getByTestId('count-badge-Channels')).toHaveTextContent('3');
    });
    expect(screen.getByTestId('count-badge-Channels')).toHaveAttribute('aria-label', '3 channels');
    expect(screen.getByTestId('count-badge-DMs')).toHaveAttribute('aria-label', '2 dms');
    expect(screen.getByTestId('count-badge-Projects')).toHaveAttribute('aria-label', '2 projects');
  });

  it('renders the bottom area: live + segmented theme + user + sign out', () => {
    renderShell();
    expect(screen.getByTestId('sidebar-live')).toBeInTheDocument();
    expect(screen.getByTestId('theme-segment-light')).toBeInTheDocument();
    expect(screen.getByTestId('theme-segment-dark')).toBeInTheDocument();
    expect(screen.getByTestId('sidebar-signout')).toBeInTheDocument();
  });

  // v2.8 #264 P1 / #176: channel/DM sidebar sub-items badge off the per-row
  // unread_count/mention_count embedded in GET /conversations (no N /unread).
  it('renders unread/mention badges in the channel sub-list from row counts', async () => {
    server.use(
      http.get('/api/conversations', ({ request }) => {
        const kind = new URL(request.url).searchParams.get('kind');
        if (kind === 'channel') {
          return HttpResponse.json([
            // mention → red number badge
            { id: 'C1', kind: 'channel', name: 'alerts', status: 'active', unread_count: 9, mention_count: 2 },
            // unread-only → neutral dot
            { id: 'C2', kind: 'channel', name: 'general', status: 'active', unread_count: 4, mention_count: 0 },
            // caught up → no badge
            { id: 'C3', kind: 'channel', name: 'quiet', status: 'active', unread_count: 0, mention_count: 0 },
          ]);
        }
        return HttpResponse.json([]);
      }),
    );
    renderShell();
    const list = await screen.findByTestId('sidebar-subitem-list-/channels');
    await waitFor(() => expect(list.textContent).toContain('# alerts'));
    // mention badge shows the precise mention number, SR announces both counts.
    const mention = screen.getByTestId('conversation-mention-badge');
    expect(mention).toHaveTextContent('2');
    expect(mention).toHaveAttribute('aria-label', '9 unread, 2 mentions');
    // exactly one unread-only dot (the unread-but-no-mention channel).
    const dots = screen.getAllByTestId('conversation-unread-dot');
    expect(dots).toHaveLength(1);
    expect(dots[0]).toHaveAttribute('aria-label', '4 unread');
  });
});

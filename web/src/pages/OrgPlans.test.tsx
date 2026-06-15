import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import OrgPlansPage from './OrgPlans';
import { useContextPanelController } from '@/shell/contextPanel';

// v2.10.0 [T6] — global cross-project Plan list + col④ summary. Mirrors the
// org Tasks/Issues test: a ShellHarness supplies the ContextPanel provider/host
// (the AppLayout role) so a selected plan's <ContextPanel> portals into col④.
function ShellHarness({ children }: { children: React.ReactNode }): React.ReactElement {
  const { Provider, value, setHost, open } = useContextPanelController();
  return (
    <Provider value={value}>
      {children}
      <aside data-testid="ctx-col" data-open={open}>
        <div ref={setHost} />
      </aside>
    </Provider>
  );
}

function wrap(path = '/organizations/acme/plans') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <ShellHarness>
          <Routes>
            <Route path="/organizations/:slug/plans" element={<OrgPlansPage />} />
          </Routes>
        </ShellHarness>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const planRow = (extra: Record<string, unknown> = {}) => ({
  id: 'plan-01KT9ABCDEF',
  project_id: 'proj-a',
  project: { id: 'proj-a', name: 'agent-center2' },
  name: 'v2.9.2 收尾',
  description: '',
  status: 'running',
  org_ref: 'P7',
  creator_ref: 'user:alice',
  conversation_id: 'conv-1',
  has_failed: false,
  progress: { done: 3, total: 5 },
  node_count: 5,
  created_at: '2026-06-01T02:00:00Z',
  updated_at: '2026-06-04T02:00:00Z',
  ...extra,
});

describe('OrgPlans — global cross-project Plan list (v2.10.0 [T6])', () => {
  afterEach(() => cleanup());

  it('renders the cross-project plan table (name / status / project / progress / updated)', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/plans', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [planRow()], total: 1 });
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('org-plan-row')).toBeInTheDocument());
    // v2.10.2 [T142]: the page title is "Plans" (plural), matching the Workspace
    // nav + the Projects/Issues/Tasks list-page convention.
    expect(screen.getByRole('heading', { name: 'Plans' })).toBeInTheDocument();
    // default view → no status/project params.
    expect(gotQuery).toBe('');
    const row = screen.getByTestId('org-plan-row');
    expect(row).toHaveAttribute('data-status', 'running');
    expect(screen.getByTestId('org-plan-name')).toHaveTextContent('v2.9.2 收尾');
    // v2.10.1 [T99]: the human Plan id (P7) shows next to the name.
    expect(within(row).getByTestId('org-plan-ref')).toHaveTextContent('P7');
    // name links into the Plan detail (project-scoped route).
    expect(screen.getByTestId('org-plan-name').getAttribute('href')).toContain(
      '/projects/proj-a/plans/plan-01KT9ABCDEF',
    );
    // progress label.
    expect(row).toHaveTextContent('3/5');
    // project link.
    expect(screen.getByTestId('org-plan-project-cell')).toHaveTextContent('agent-center2');
  });

  it('status filter chips send the status param', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/plans', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [planRow()], total: 1 });
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('org-plan-row')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('org-plan-status-done'));
    await waitFor(() => expect(gotQuery).toContain('status=done'));
  });

  it('archived chip sends status=archived and renders archived plans (T98)', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/plans', ({ request }) => {
        gotQuery = new URL(request.url).search;
        // Backend only returns archived plans once explicitly filtered.
        const wantArchived = gotQuery.includes('status=archived');
        return HttpResponse.json({
          items: wantArchived
            ? [planRow({ id: 'plan-arch', name: '已归档计划', status: 'archived' })]
            : [planRow()],
          total: 1,
        });
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('org-plan-row')).toBeInTheDocument());
    // default view excludes archived → no status param, running row shown.
    expect(gotQuery).toBe('');
    expect(screen.getByTestId('org-plan-row')).toHaveAttribute('data-status', 'running');
    // the archived chip exists in the filter bar...
    fireEvent.click(screen.getByTestId('org-plan-status-archived'));
    await waitFor(() => expect(gotQuery).toContain('status=archived'));
    // ...and the now-archived row surfaces.
    await waitFor(() =>
      expect(screen.getByTestId('org-plan-row')).toHaveAttribute('data-status', 'archived'),
    );
    expect(screen.getByTestId('org-plan-name')).toHaveTextContent('已归档计划');
  });

  it('client-side search narrows the list by name', async () => {
    server.use(
      http.get('/api/plans', () =>
        HttpResponse.json({
          items: [planRow(), planRow({ id: 'plan-2', name: '聊天框附件增强' })],
          total: 2,
        }),
      ),
    );
    wrap();
    await waitFor(() => expect(screen.getAllByTestId('org-plan-row')).toHaveLength(2));
    fireEvent.change(screen.getByTestId('org-plans-search'), { target: { value: '附件' } });
    await waitFor(() => expect(screen.getAllByTestId('org-plan-row')).toHaveLength(1));
    expect(screen.getByTestId('org-plan-name')).toHaveTextContent('聊天框附件增强');
  });

  it('selecting a plan opens the col④ summary with an Open-plan link', async () => {
    server.use(
      http.get('/api/plans', () => HttpResponse.json({ items: [planRow()], total: 1 })),
    );
    wrap();
    const row = await screen.findByTestId('org-plan-row');
    expect(screen.queryByTestId('org-plan-meta-panel')).not.toBeInTheDocument();
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'false');

    fireEvent.click(row);
    const panel = await screen.findByTestId('org-plan-meta-panel');
    expect(panel).toHaveAttribute('data-id', 'plan-01KT9ABCDEF');
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'true');
    expect(screen.getByTestId('org-plan-row')).toHaveAttribute('aria-selected', 'true');
    expect(panel).toHaveTextContent('agent-center2');
    expect(panel).toHaveTextContent('3/5');
    expect(screen.getByTestId('org-plan-meta-open').getAttribute('href')).toContain(
      '/projects/proj-a/plans/plan-01KT9ABCDEF',
    );

    // close clears it → col④ collapses.
    fireEvent.click(screen.getByTestId('org-plan-meta-close'));
    await waitFor(() =>
      expect(screen.queryByTestId('org-plan-meta-panel')).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'false');
  });

  it('empty state when the org has no plans', async () => {
    server.use(http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })));
    wrap();
    await waitFor(() => expect(screen.getByTestId('org-plans-empty')).toBeInTheDocument());
    expect(screen.getByTestId('org-plans-empty')).toHaveTextContent(/No plans yet/i);
  });

  // v2.10.1 [M4] Mobile (<md): the wide table reflows to a card flow (md:hidden).
  // jsdom renders both; these specs assert the card list mirrors the rows.
  it('renders a mobile card per plan, with a name link to the plan detail', async () => {
    server.use(
      http.get('/api/plans', () =>
        HttpResponse.json({ items: [planRow(), planRow({ id: 'plan-2', name: '聊天框附件增强' })], total: 2 }),
      ),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('org-plans-cards')).toBeInTheDocument());
    const cards = screen.getAllByTestId('org-plan-card');
    expect(cards).toHaveLength(2);
    // name links into the plan detail (mirrors the table row link).
    const name = within(cards[0]).getByTestId('org-plan-card-name');
    expect(name.getAttribute('href')).toContain('/projects/proj-a/plans/plan-01KT9ABCDEF');
    // status chip surfaces on the card.
    expect(within(cards[0]).getByTestId('plan-status-chip')).toHaveTextContent('running');
  });

  it('tapping a mobile card selects it → opens the col④ summary (M1 reflows to a sheet)', async () => {
    server.use(http.get('/api/plans', () => HttpResponse.json({ items: [planRow()], total: 1 })));
    wrap();
    const card = await screen.findByTestId('org-plan-card');
    expect(card).toHaveAttribute('aria-selected', 'false');
    fireEvent.click(card);
    expect(screen.getByTestId('org-plan-card')).toHaveAttribute('aria-selected', 'true');
    // the col④ ContextPanel summary mounts (becomes a bottom sheet on mobile).
    expect(screen.getByTestId('org-plan-meta-panel')).toBeInTheDocument();
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'true');
  });
});

import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import ProjectPlans from './ProjectPlans';
import PlanDetail from './PlanDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

const projectAlpha = {
  id: 'proj-a',
  organization_id: 'org-test',
  name: 'Project Alpha',
  description: 'the alpha project',
  status: 'active',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-20T01:00:00Z',
  updated_at: '2026-05-20T01:00:00Z',
};

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/projects/:id/plans" element={<ProjectPlans />} />
          <Route path="/projects/:id/plans/:planId" element={<PlanDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('ProjectPlans Work Board (#291 — Backlog + Plan columns + new-Plan)', () => {
  afterEach(() => cleanup());

  it('renders the board: Backlog column (unplanned tasks) + Plan columns + new-Plan column', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    // Backlog column first — the unplanned task from ?unplanned=1.
    const backlog = screen.getByTestId('backlog-column');
    expect(within(backlog).getByText('unplanned backlog task')).toBeInTheDocument();
    expect(within(backlog).getByTestId('backlog-count')).toHaveTextContent('1');

    // One column per Plan (from usePlans): PL-1 running + has_failed, PL-2 draft.
    const cols = screen.getAllByTestId('plan-column');
    expect(cols).toHaveLength(2);
    const running = screen.getByText('Onboarding flow').closest('[data-testid="plan-column"]')!;
    expect(within(running as HTMLElement).getByTestId('plan-status-chip')).toHaveTextContent('running');
    // failed-indicator from has_failed; progress from progress{done,total}.
    expect(within(running as HTMLElement).getByTestId('plan-failed-indicator')).toBeInTheDocument();
    expect(within(running as HTMLElement).getByTestId('plan-progress')).toHaveTextContent('2/5');

    // Cards from nodes_preview (capped 4 by the backend), NOT the detail `nodes`.
    const cards = within(running as HTMLElement).getAllByTestId('plan-task-card');
    expect(cards).toHaveLength(4);
    expect(within(running as HTMLElement).getByText('Design intake form')).toBeInTheDocument();
    // Card StatusChip reads node.task_status from the preview node (the 2nd-shape
    // seam closed by the enrich — task_status is present in nodes_preview).
    expect(within(cards[0]).getByTestId('status-chip')).toHaveTextContent('done');
    expect(within(cards[1]).getByTestId('status-chip')).toHaveTextContent('running');
    // Overflow "…and M more" from node_count − nodes_preview.length (6 − 4 = 2).
    expect(within(running as HTMLElement).getByTestId('plan-overflow-PL-1')).toHaveTextContent('…and 2 more');

    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    expect(within(draft as HTMLElement).getByTestId('plan-status-chip')).toHaveTextContent('draft');
    expect(within(draft as HTMLElement).queryByTestId('plan-failed-indicator')).not.toBeInTheDocument();
    // node_count 0 → no overflow hint, empty-state instead.
    expect(within(draft as HTMLElement).queryByTestId('plan-overflow-PL-2')).not.toBeInTheDocument();
    expect(within(draft as HTMLElement).getByTestId('plan-empty')).toBeInTheDocument();

    // Trailing new-Plan column.
    expect(screen.getByTestId('new-plan-column')).toBeInTheDocument();
  });

  // P2-4: a running Plan column communicates it self-progresses (auto-advance);
  // a draft column does not (draft is not being orchestrated).
  it('a running Plan column shows the auto-advancing indicator; a draft column does not', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const running = screen.getByText('Onboarding flow').closest('[data-testid="plan-column"]')!;
    const ind = within(running as HTMLElement).getByTestId('plan-col-auto-advancing');
    expect(ind).toHaveTextContent(/auto-advancing/i);
    // both-mode AA token, no alpha-tint
    expect(ind.className).toContain('text-text-secondary');
    expect(ind.className).not.toMatch(/\/\d+/);

    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    expect(within(draft as HTMLElement).queryByTestId('plan-col-auto-advancing')).not.toBeInTheDocument();
  });

  // DEFENSIVE regression guard for the run-real white-screen (PR #272 context):
  // the ORIGINAL bare GET /plans had no progress/nodes_preview/node_count, so the
  // board read plan.progress.done on undefined → "Cannot read properties of
  // undefined (reading 'done')" → ErrorBoundary white-screen. With the defensive
  // defaults a partial/bare row must degrade to an empty column, never throw.
  it('DEFENSIVE: a bare plan (progress / nodes_preview / node_count undefined) renders a degraded column and does NOT crash', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      // A deliberately BARE list row — the pre-enrich shape that crashed run-real.
      http.get('/api/projects/proj-a/plans', () =>
        HttpResponse.json({
          plans: [
            {
              id: 'PL-BARE',
              project_id: 'proj-a',
              name: 'Bare legacy plan',
              description: '',
              status: 'draft',
              creator_ref: 'user:owner',
              conversation_id: 'c1',
              target_date: null,
              created_at: '2026-06-01T01:00:00Z',
              // NO progress, NO has_failed, NO node_count, NO nodes_preview.
            },
          ],
        }),
      ),
    );
    wrap('/projects/proj-a/plans');
    // The board renders (no ErrorBoundary white-screen / thrown render).
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    const col = screen.getByText('Bare legacy plan').closest('[data-testid="plan-column"]')!;
    // progress ?? {0,0} → "0/0" (the crash site — reading 'done' off undefined).
    expect(within(col as HTMLElement).getByTestId('plan-progress')).toHaveTextContent('0/0');
    // has_failed ?? false → no indicator. nodes_preview ?? [] → empty column.
    expect(within(col as HTMLElement).queryByTestId('plan-failed-indicator')).not.toBeInTheDocument();
    expect(within(col as HTMLElement).queryAllByTestId('plan-task-card')).toHaveLength(0);
    expect(within(col as HTMLElement).getByTestId('plan-empty')).toBeInTheDocument();
    // node_count ?? 0 → no overflow hint.
    expect(within(col as HTMLElement).queryByTestId('plan-overflow-PL-BARE')).not.toBeInTheDocument();
  });

  it('the "Add to plan" button adds a Backlog task into a DRAFT plan (useAddTaskToPlan)', async () => {
    let posted: Record<string, unknown> | undefined;
    let postedTo: string | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.post('/api/projects/proj-a/plans/:planId/tasks', async ({ params, request }) => {
        postedTo = String(params.planId);
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: postedTo, project_id: 'proj-a', name: 'p', status: 'draft', has_failed: false, progress: { done: 0, total: 1 }, nodes: [] });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    // open the add-menu on the backlog card.
    fireEvent.click(screen.getByTestId('backlog-add-TS-BL1'));
    const menu = screen.getByTestId('add-menu-TS-BL1');
    // ONLY the draft plan (PL-2) is offered — the running plan (PL-1) is NOT.
    expect(within(menu).getByTestId('add-to-plan-TS-BL1-PL-2')).toBeInTheDocument();
    expect(within(menu).queryByTestId('add-to-plan-TS-BL1-PL-1')).not.toBeInTheDocument();
    await act(async () => {
      fireEvent.click(within(menu).getByTestId('add-to-plan-TS-BL1-PL-2'));
    });
    await waitFor(() => expect(posted).toEqual({ task_id: 'TS-BL1' }));
    expect(postedTo).toBe('PL-2'); // draft-only select-into-plan.
  });

  it('a running Plan column is NOT a drop target (draft-only §9.4)', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    const running = screen.getByText('Onboarding flow').closest('[data-testid="plan-column"]')!;
    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    expect(running).toHaveAttribute('data-droppable', 'false');
    expect(draft).toHaveAttribute('data-droppable', 'true');
  });

  it('"Open ▸" on a Plan column links to the Plan detail route (reachability)', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    expect(screen.getByTestId('plan-open-PL-1')).toHaveAttribute('href', '/projects/proj-a/plans/PL-1');
  });

  it('"New Plan" creates a Plan via POST', async () => {
    let posted: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.post('/api/projects/proj-a/plans', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'PL-NEW', project_id: 'proj-a', name: posted.name }, { status: 201 });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('new-plan-column'));
    expect(screen.getByTestId('plan-create-modal')).toBeInTheDocument();
    fireEvent.change(screen.getByTestId('plan-create-name'), { target: { value: 'Q3 plan' } });
    await act(async () => {
      fireEvent.click(screen.getByTestId('plan-create-submit'));
    });
    await waitFor(() => expect(posted).toMatchObject({ name: 'Q3 plan' }));
  });

  it('#218: a board load error renders a friendly message + hides the raw error behind [Details]', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such API route' }, { status: 404 }),
      ),
    );
    wrap('/projects/proj-a/plans');
    const friendly = await screen.findByTestId('board-error');
    expect(friendly).toHaveTextContent("Couldn't load the work board.");
    const primary = screen.getByText("Couldn't load the work board.");
    expect(primary.tagName).toBe('P');
    expect(primary).not.toHaveTextContent('no such API route');
    const raw = screen.getByTestId('board-error-raw');
    expect(raw).toHaveTextContent('[404 not_found] no such API route');
    const details = raw.closest('details');
    expect(details).not.toBeNull();
    expect(within(details!).getByText('Details')).toBeInTheDocument();
  });

  it('empty states: no plans → only Backlog + new-Plan; empty backlog → friendly message', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json({ plans: [] })),
      http.get('/api/projects/proj-a/tasks', ({ request }) => {
        const unplanned = new URL(request.url).searchParams.get('unplanned');
        return HttpResponse.json({ tasks: unplanned === '1' ? [] : [{ id: 'TS-1', project_id: 'proj-a', title: 't', description: '', status: 'open', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z' }] });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    expect(screen.queryAllByTestId('plan-column')).toHaveLength(0);
    expect(screen.getByTestId('backlog-empty')).toBeInTheDocument();
    expect(screen.getByTestId('new-plan-column')).toBeInTheDocument();
  });
});

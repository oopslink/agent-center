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
    expect(within(running as HTMLElement).getByTestId('plan-failed-indicator')).toBeInTheDocument();
    expect(within(running as HTMLElement).getByTestId('plan-progress')).toHaveTextContent('2/5');
    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    expect(within(draft as HTMLElement).getByTestId('plan-status-chip')).toHaveTextContent('draft');
    expect(within(draft as HTMLElement).queryByTestId('plan-failed-indicator')).not.toBeInTheDocument();

    // Trailing new-Plan column.
    expect(screen.getByTestId('new-plan-column')).toBeInTheDocument();
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

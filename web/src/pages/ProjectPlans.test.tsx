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

describe('ProjectPlans page (#286 parallel list + new plan)', () => {
  afterEach(() => cleanup());

  it('renders the parallel Plan list with status / progress / has_failed', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('plans-list')).toBeInTheDocument());
    const cards = screen.getAllByTestId('plan-card');
    expect(cards).toHaveLength(2);
    // PL-1 = running + has_failed indicator + progress 2/5
    const running = screen.getByText('Onboarding flow').closest('[data-testid="plan-card"]')!;
    expect(within(running as HTMLElement).getByTestId('plan-status-chip')).toHaveTextContent('running');
    expect(within(running as HTMLElement).getByTestId('plan-failed-indicator')).toBeInTheDocument();
    expect(within(running as HTMLElement).getByTestId('plan-progress')).toHaveTextContent('2/5');
    // PL-2 = draft, no failed indicator
    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-card"]')!;
    expect(within(draft as HTMLElement).getByTestId('plan-status-chip')).toHaveTextContent('draft');
    expect(within(draft as HTMLElement).queryByTestId('plan-failed-indicator')).not.toBeInTheDocument();
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
    await waitFor(() => expect(screen.getByTestId('plans-list')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-create-btn'));
    expect(screen.getByTestId('plan-create-modal')).toBeInTheDocument();
    fireEvent.change(screen.getByTestId('plan-create-name'), { target: { value: 'Q3 plan' } });
    await act(async () => {
      fireEvent.click(screen.getByTestId('plan-create-submit'));
    });
    await waitFor(() => expect(posted).toMatchObject({ name: 'Q3 plan' }));
  });

  it('#218: on an API error renders a friendly message + hides the raw error behind [Details]', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such API route' }, { status: 404 }),
      ),
    );
    wrap('/projects/proj-a/plans');
    // Friendly headline renders (NOT the raw API error string).
    const friendly = await screen.findByTestId('plans-error');
    expect(friendly).toHaveTextContent("Couldn't load plans.");
    // The friendly headline <p> is the PRIMARY text — it must not be the raw
    // API error. The summary "Details" affordance gates the raw string.
    const primary = screen.getByText("Couldn't load plans.");
    expect(primary.tagName).toBe('P');
    expect(primary).not.toHaveTextContent('no such API route');
    // The raw API error lives ONLY behind the [Details] expander.
    const raw = screen.getByTestId('plans-error-raw');
    expect(raw).toHaveTextContent('[404 not_found] no such API route');
    const details = raw.closest('details');
    expect(details).not.toBeNull();
    expect(within(details!).getByText('Details')).toBeInTheDocument();
  });

  it('a Plan card links to the Plan detail route (reachability)', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('plans-list')).toBeInTheDocument());
    const link = screen.getAllByTestId('plan-card-link')[0];
    expect(link).toHaveAttribute('href', '/projects/proj-a/plans/PL-1');
  });
});

describe('PlanDetail page (#286 backlog→Plan selection)', () => {
  afterEach(() => cleanup());

  it('renders the Plan header + selected tasks + backlog picker (work-items table reuse)', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans/PL-1');
    // selected table shows the Plan's node (TS-1); backlog excludes it.
    await waitFor(() => expect(screen.getByTestId('plan-selected-table')).toBeInTheDocument());
    // the default tasks mock returns TS-1, which is already in the plan → backlog empty.
    await waitFor(() => expect(screen.getByTestId('plan-backlog-empty')).toBeInTheDocument());
    // a status chip from the reused work-items table renders.
    expect(within(screen.getByTestId('plan-selected-table')).getByTestId('status-chip')).toBeInTheDocument();
  });

  it('adds a backlog task into the Plan via POST /:id/tasks', async () => {
    let posted: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      // plan with NO nodes → TS-1 from the task list is a backlog candidate.
      http.get('/api/projects/proj-a/plans/PL-1', () =>
        HttpResponse.json({
          id: 'PL-1',
          project_id: 'proj-a',
          name: 'Empty plan',
          description: '',
          status: 'draft',
          creator_ref: 'user:owner',
          conversation_id: 'c1',
          target_date: null,
          has_failed: false,
          progress: { done: 0, total: 0 },
          created_at: '2026-06-01T01:00:00Z',
          nodes: [],
        }),
      ),
      http.post('/api/projects/proj-a/plans/PL-1/tasks', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'PL-1', project_id: 'proj-a', name: 'Empty plan', status: 'draft', has_failed: false, progress: { done: 0, total: 1 }, nodes: [] });
      }),
    );
    wrap('/projects/proj-a/plans/PL-1');
    await waitFor(() => expect(screen.getByTestId('plan-backlog-table')).toBeInTheDocument());
    await act(async () => {
      fireEvent.click(screen.getByTestId('plan-add-task-TS-1'));
    });
    await waitFor(() => expect(posted).toEqual({ task_id: 'TS-1' }));
  });

  it('#218: on an API error renders a friendly message + hides the raw error behind [Details]', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans/PL-1', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such API route' }, { status: 404 }),
      ),
    );
    wrap('/projects/proj-a/plans/PL-1');
    const friendly = await screen.findByTestId('plan-not-found');
    expect(friendly).toHaveTextContent("Couldn't load this plan.");
    // Friendly headline <p> is the primary text, not the raw API error.
    const primary = screen.getByText("Couldn't load this plan.");
    expect(primary.tagName).toBe('P');
    expect(primary).not.toHaveTextContent('no such API route');
    // Raw error only behind the [Details] expander.
    const raw = screen.getByTestId('plan-not-found-raw');
    expect(raw).toHaveTextContent('[404 not_found] no such API route');
    const details = raw.closest('details');
    expect(details).not.toBeNull();
    expect(within(details!).getByText('Details')).toBeInTheDocument();
    // Recovery path still present.
    expect(screen.getByText('← Back to plans')).toBeInTheDocument();
  });

  it('hides the backlog picker when the Plan is not draft', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans/PL-1', () =>
        HttpResponse.json({
          id: 'PL-1',
          project_id: 'proj-a',
          name: 'Running plan',
          description: '',
          status: 'running',
          creator_ref: 'user:owner',
          conversation_id: 'c1',
          target_date: null,
          has_failed: false,
          progress: { done: 1, total: 2 },
          created_at: '2026-06-01T01:00:00Z',
          nodes: [{ task_id: 'TS-1', title: 't', assignee_ref: '', task_status: 'running', node_status: 'running', depends_on: [] }],
        }),
      ),
    );
    wrap('/projects/proj-a/plans/PL-1');
    await waitFor(() => expect(screen.getByTestId('page-PlanDetail')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-backlog-table')).not.toBeInTheDocument();
    // no Remove action on a non-draft plan
    expect(screen.queryByTestId('plan-remove-task-TS-1')).not.toBeInTheDocument();
  });
});

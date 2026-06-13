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

  // A2 (#291 inverse of add-to-plan): remove a task from a Plan → back to Backlog.
  // §9.4 draft-only — a DRAFT Plan column's card exposes the remove affordance;
  // a running/done column does NOT.
  const planNode = (taskId: string, title: string) => ({
    task_id: taskId,
    title,
    assignee_ref: 'agent:builder',
    task_status: 'open',
    node_status: 'ready',
    depends_on: [],
    dispatched_at: null,
  });
  const plansWithDraftNode = {
    plans: [
      {
        id: 'PL-1', project_id: 'proj-a', name: 'Onboarding flow', description: '',
        status: 'running', creator_ref: 'user:owner', conversation_id: 'c1', target_date: null,
        has_failed: false, progress: { done: 1, total: 2 }, created_at: '2026-06-01T01:00:00Z',
        node_count: 1, nodes_preview: [planNode('TS-RUN', 'Running task')],
      },
      {
        id: 'PL-2', project_id: 'proj-a', name: 'Billing rework', description: '',
        status: 'draft', creator_ref: 'user:owner', conversation_id: 'c2', target_date: null,
        has_failed: false, progress: { done: 0, total: 1 }, created_at: '2026-06-01T01:00:00Z',
        node_count: 1, nodes_preview: [planNode('TS-DR', 'Draft task')],
      },
    ],
  };

  it('A2 board: a DRAFT Plan column card shows the remove affordance; clicking it DELETEs by task_id (back to backlog)', async () => {
    let deletedTaskId: string | undefined;
    let deletedFromPlan: string | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(plansWithDraftNode)),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', ({ params }) => {
        deletedFromPlan = String(params.planId);
        deletedTaskId = String(params.taskId);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const removeBtn = within(draft as HTMLElement).getByTestId('plan-task-remove-TS-DR');
    expect(removeBtn).toHaveAttribute('aria-label', 'Remove Draft task from plan');
    await act(async () => {
      fireEvent.click(removeBtn);
    });
    await waitFor(() => expect(deletedTaskId).toBe('TS-DR'));
    expect(deletedFromPlan).toBe('PL-2');
  });

  it('A2 board: a running Plan column card has NO remove affordance (§9.4 draft-only)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(plansWithDraftNode)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    const running = screen.getByText('Onboarding flow').closest('[data-testid="plan-column"]')!;
    expect(within(running as HTMLElement).queryByTestId('plan-task-remove-TS-RUN')).not.toBeInTheDocument();
    // the draft column DOES have it (control assertion).
    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    expect(within(draft as HTMLElement).getByTestId('plan-task-remove-TS-DR')).toBeInTheDocument();
  });

  it('A2 board #218: a remove failure surfaces a friendly inline message (no raw API error)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(plansWithDraftNode)),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', () =>
        HttpResponse.json({ error: 'conflict', message: 'plan not draft' }, { status: 409 }),
      ),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    await act(async () => {
      fireEvent.click(within(draft as HTMLElement).getByTestId('plan-task-remove-TS-DR'));
    });
    const err = await within(draft as HTMLElement).findByTestId('plan-task-remove-error-TS-DR');
    expect(err).toHaveTextContent("Couldn't remove this task from the plan.");
    expect(err).not.toHaveTextContent('plan not draft');
  });

  // -------------------------------------------------------------------------
  // A7 — full task drag: move a Plan-task between Plans / back to the Backlog.
  // -------------------------------------------------------------------------

  // Two DRAFT plans (PL-2, PL-3) each holding one node + a running plan (PL-1),
  // so a cross-plan MOVE has a draft source AND a draft target, and the running
  // column is locked at both ends (no draggable card, not a drop target).
  const twoDraftPlans = {
    plans: [
      {
        id: 'PL-1', project_id: 'proj-a', name: 'Onboarding flow', description: '',
        status: 'running', creator_ref: 'user:owner', conversation_id: 'c1', target_date: null,
        has_failed: false, progress: { done: 1, total: 2 }, created_at: '2026-06-01T01:00:00Z',
        node_count: 1, nodes_preview: [planNode('TS-RUN', 'Running task')],
      },
      {
        id: 'PL-2', project_id: 'proj-a', name: 'Billing rework', description: '',
        status: 'draft', creator_ref: 'user:owner', conversation_id: 'c2', target_date: null,
        has_failed: false, progress: { done: 0, total: 1 }, created_at: '2026-06-01T01:00:00Z',
        node_count: 1, nodes_preview: [planNode('TS-DR', 'Draft task')],
      },
      {
        id: 'PL-3', project_id: 'proj-a', name: 'Search revamp', description: '',
        status: 'draft', creator_ref: 'user:owner', conversation_id: 'c3', target_date: null,
        has_failed: false, progress: { done: 0, total: 1 }, created_at: '2026-06-01T01:00:00Z',
        node_count: 1, nodes_preview: [planNode('TS-DR3', 'Other draft task')],
      },
    ],
  };

  // A DataTransfer stub (jsdom has none) so fireEvent.dragStart/drop carry data.
  // `types` mirrors the real DataTransfer: it lists the MIME keys that have been
  // setData'd — the Backlog reads `types.includes(FROM_PLAN_MIME)` to accept a
  // plan-task drag race-proof (without waiting on the React state commit).
  function dt() {
    const store: Record<string, string> = {};
    return {
      setData: (k: string, v: string) => {
        store[k] = v;
      },
      getData: (k: string) => store[k] ?? '',
      get types() {
        return Object.keys(store);
      },
      effectAllowed: '',
      dropEffect: '',
    } as unknown as DataTransfer;
  }

  it('A7: a DRAFT Plan task card is draggable; a running Plan task card is NOT', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(twoDraftPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const draftCard = within(draft as HTMLElement).getByTestId('plan-task-card');
    expect(draftCard).toHaveAttribute('draggable', 'true');
    expect(draftCard).toHaveAttribute('data-draggable', 'true');

    const running = screen.getByText('Onboarding flow').closest('[data-testid="plan-column"]')!;
    const runCard = within(running as HTMLElement).getByTestId('plan-task-card');
    expect(runCard).toHaveAttribute('draggable', 'false');
    expect(runCard).toHaveAttribute('data-draggable', 'false');
  });

  it('A7: drag a Plan-task onto the Backlog → DELETE (remove from its source plan)', async () => {
    let deletedTaskId: string | undefined;
    let deletedFromPlan: string | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(twoDraftPlans)),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', ({ params }) => {
        deletedFromPlan = String(params.planId);
        deletedTaskId = String(params.taskId);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const card = within(draft as HTMLElement).getByTestId('plan-task-card');
    const backlog = screen.getByTestId('backlog-column');
    const transfer = dt();
    fireEvent.dragStart(card, { dataTransfer: transfer });
    // Backlog now accepts a plan-task drag.
    expect(backlog).toHaveAttribute('data-droppable', 'true');
    fireEvent.dragOver(backlog, { dataTransfer: transfer });
    await act(async () => {
      fireEvent.drop(backlog, { dataTransfer: transfer });
    });
    await waitFor(() => expect(deletedTaskId).toBe('TS-DR'));
    expect(deletedFromPlan).toBe('PL-2'); // removed from its SOURCE plan.
  });

  // CRITICAL run-real regression guard (Tester2): the prior test missed the
  // bug because it only checked the handler path with the React state already
  // committed. The run-real failure was a STATE-COMMIT race — the browser's
  // first `dragover` on the Backlog fired before the `dragSource` state set on
  // dragStart had committed, so an acceptance computed PURELY from that state
  // read stale (false), the onDragOver never preventDefault'd, the browser
  // never registered the Backlog as a drop zone → data-droppable stuck false +
  // 0 RemoveTaskFromPlan. The fix carries the source in dataTransfer (set
  // synchronously on dragStart, readable on every dragover/drop). This test
  // pins BOTH halves: (1) dragStart stamps the FROM_PLAN_MIME marker + source
  // plan id into dataTransfer; (2) the Backlog accepts + REMOVES from
  // dataTransfer ALONE — proven by dropping with the dataTransfer marker while
  // the drop event has NO chance to rely on freshly-committed state.
  it('A7 CRITICAL (run-real race): dragStart stamps the source into dataTransfer; the Backlog accepts + removes from dataTransfer alone (not state-dependent)', async () => {
    let deletedTaskId: string | undefined;
    let deletedFromPlan: string | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(twoDraftPlans)),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', ({ params }) => {
        deletedFromPlan = String(params.planId);
        deletedTaskId = String(params.taskId);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const card = within(draft as HTMLElement).getByTestId('plan-task-card');
    const backlog = screen.getByTestId('backlog-column');

    // (1) dragStart must stamp the race-proof source into dataTransfer: the
    // FROM_PLAN_MIME marker (presence = "this is a plan-task") carrying the
    // SOURCE plan id, plus the task id on text/plain. Without these the Backlog
    // can't accept a plan-task before the React state commits → the run-real bug.
    const transfer = dt();
    fireEvent.dragStart(card, { dataTransfer: transfer });
    expect(transfer.types).toContain('application/x-slock-from-plan');
    expect(transfer.getData('application/x-slock-from-plan')).toBe('PL-2');
    expect(transfer.getData('text/plain')).toBe('TS-DR');

    // data-droppable flips true on the plan-task drag (the visual/state signal).
    expect(backlog).toHaveAttribute('data-droppable', 'true');

    // (2) onDragOver must preventDefault so the browser allows the drop — driven
    // by the dataTransfer marker, NOT only the React state. Assert via a real
    // dragover event whose preventDefault we can observe.
    const overEvt = new Event('dragover', { bubbles: true, cancelable: true });
    Object.defineProperty(overEvt, 'dataTransfer', { value: transfer });
    fireEvent(backlog, overEvt);
    expect(overEvt.defaultPrevented).toBe(true);

    // (3) The drop REMOVES using the source read from dataTransfer (PL-2) — the
    // half that 0-fired in run-real.
    await act(async () => {
      fireEvent.drop(backlog, { dataTransfer: transfer });
    });
    await waitFor(() => expect(deletedTaskId).toBe('TS-DR'));
    expect(deletedFromPlan).toBe('PL-2');
  });

  it('A7: drag a Plan-task onto ANOTHER draft plan → MOVE = DELETE source + POST target', async () => {
    let deletedFromPlan: string | undefined;
    let deletedTaskId: string | undefined;
    let postedTo: string | undefined;
    let postedBody: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(twoDraftPlans)),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', ({ params }) => {
        deletedFromPlan = String(params.planId);
        deletedTaskId = String(params.taskId);
        return new HttpResponse(null, { status: 204 });
      }),
      http.post('/api/projects/proj-a/plans/:planId/tasks', async ({ params, request }) => {
        postedTo = String(params.planId);
        postedBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: postedTo, project_id: 'proj-a', name: 'p', status: 'draft', has_failed: false, progress: { done: 0, total: 1 }, nodes: [] });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const sourceCol = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const card = within(sourceCol as HTMLElement).getByTestId('plan-task-card');
    const targetCol = screen.getByText('Search revamp').closest('[data-testid="plan-column"]')!;
    const transfer = dt();
    fireEvent.dragStart(card, { dataTransfer: transfer });
    fireEvent.dragOver(targetCol, { dataTransfer: transfer });
    await act(async () => {
      fireEvent.drop(targetCol, { dataTransfer: transfer });
    });
    // BOTH ops fired (the move): remove from source PL-2 + add to target PL-3.
    await waitFor(() => expect(postedTo).toBe('PL-3'));
    expect(postedBody).toEqual({ task_id: 'TS-DR' });
    expect(deletedFromPlan).toBe('PL-2');
    expect(deletedTaskId).toBe('TS-DR');
  });

  it('A7: drag a Plan-task onto a RUNNING plan column → rejected (no mutate, not droppable)', async () => {
    let mutated = false;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(twoDraftPlans)),
      http.post('/api/projects/proj-a/plans/:planId/tasks', () => {
        mutated = true;
        return HttpResponse.json({});
      }),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', () => {
        mutated = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const sourceCol = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const card = within(sourceCol as HTMLElement).getByTestId('plan-task-card');
    const running = screen.getByText('Onboarding flow').closest('[data-testid="plan-column"]')!;
    expect(running).toHaveAttribute('data-droppable', 'false');
    const transfer = dt();
    fireEvent.dragStart(card, { dataTransfer: transfer });
    await act(async () => {
      fireEvent.drop(running, { dataTransfer: transfer });
    });
    // give any (erroneous) mutation a tick to fire.
    await act(async () => {
      await Promise.resolve();
    });
    expect(mutated).toBe(false);
  });

  it('A7: dropping a Plan-task back onto its OWN plan is a no-op (no mutate)', async () => {
    let mutated = false;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(twoDraftPlans)),
      http.post('/api/projects/proj-a/plans/:planId/tasks', () => {
        mutated = true;
        return HttpResponse.json({});
      }),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', () => {
        mutated = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const sourceCol = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const card = within(sourceCol as HTMLElement).getByTestId('plan-task-card');
    const transfer = dt();
    fireEvent.dragStart(card, { dataTransfer: transfer });
    await act(async () => {
      fireEvent.drop(sourceCol, { dataTransfer: transfer });
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(mutated).toBe(false);
  });

  it('A7 regression: a Backlog card dropped on the Backlog is a no-op; Backlog→plan still works', async () => {
    let postedTo: string | undefined;
    let deleted = false;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(twoDraftPlans)),
      http.post('/api/projects/proj-a/plans/:planId/tasks', async ({ params }) => {
        postedTo = String(params.planId);
        return HttpResponse.json({ id: postedTo, project_id: 'proj-a', name: 'p', status: 'draft', has_failed: false, progress: { done: 0, total: 1 }, nodes: [] });
      }),
      http.delete('/api/projects/proj-a/plans/:planId/tasks/:taskId', () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const backlog = screen.getByTestId('backlog-column');
    const backlogCard = within(backlog).getByTestId('backlog-card');
    const transfer = dt();
    // Backlog card dragged → Backlog is NOT a drop target (fromPlanId null).
    fireEvent.dragStart(backlogCard, { dataTransfer: transfer });
    expect(backlog).toHaveAttribute('data-droppable', 'false');
    await act(async () => {
      fireEvent.drop(backlog, { dataTransfer: transfer });
    });
    expect(deleted).toBe(false); // no remove fired from a backlog→backlog drop.

    // Backlog card → draft plan still SELECTs (the existing #270 behavior).
    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    fireEvent.dragStart(backlogCard, { dataTransfer: transfer });
    fireEvent.dragOver(draft, { dataTransfer: transfer });
    await act(async () => {
      fireEvent.drop(draft, { dataTransfer: transfer });
    });
    await waitFor(() => expect(postedTo).toBe('PL-2'));
  });

  // -------------------------------------------------------------------------
  // A6 (§4.2 reachability): the task TITLE on a board card is a new-tab link to
  // the TaskDetail page (/projects/{pid}/tasks/{tid}, org-prefixed by OrgLink's
  // orgPath — here unprefixed since the test renders outside an OrgGuard). The
  // link opens in a NEW TAB (target=_blank + rel noopener) and the card's drag /
  // A2 remove affordances are NOT swallowed by the link (link is on title only).
  // -------------------------------------------------------------------------
  it('A6 §4.2: a BACKLOG card title is a new-tab link to TaskDetail (href + target=_blank + rel noopener)', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const link = screen.getByTestId('task-open-link-TS-BL1');
    expect(link).toHaveAttribute('href', '/projects/proj-a/tasks/TS-BL1');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link).toHaveTextContent('unplanned backlog task');
    // The link does NOT consume the whole card: the draggable card + the
    // add-to-plan button still render alongside the title link.
    const card = screen.getByTestId('backlog-card');
    expect(card).toHaveAttribute('draggable', 'true');
    expect(within(card).getByTestId('backlog-add-TS-BL1')).toBeInTheDocument();
  });

  it('A6 §4.2: a PLAN-column task card title is a new-tab link AND coexists with the A2 remove button', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(plansWithDraftNode)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    const link = within(draft as HTMLElement).getByTestId('task-open-link-TS-DR');
    expect(link).toHaveAttribute('href', '/projects/proj-a/tasks/TS-DR');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link).toHaveTextContent('Draft task');
    // Coexistence: the A2 remove button is still present on the SAME card.
    expect(within(draft as HTMLElement).getByTestId('plan-task-remove-TS-DR')).toBeInTheDocument();
  });

  // v2.9 Stage B (#283): a plan task card shows an Archived badge when
  // task.archived (orthogonal — coexists with the status chip).
  it('Stage B: an archived-plan card shows the Archived badge (coexists with status chip)', async () => {
    const archivedPlans = {
      plans: [
        {
          id: 'PL-A', project_id: 'proj-a', name: 'Shelved plan', description: '',
          status: 'archived', creator_ref: 'user:owner', conversation_id: 'cA', target_date: null,
          has_failed: false, progress: { done: 0, total: 1 }, created_at: '2026-06-01T01:00:00Z',
          node_count: 1,
          nodes_preview: [{ ...planNode('TS-AR', 'Archived task'), archived: true }],
        },
        {
          id: 'PL-D', project_id: 'proj-a', name: 'Live draft', description: '',
          status: 'draft', creator_ref: 'user:owner', conversation_id: 'cD', target_date: null,
          has_failed: false, progress: { done: 0, total: 1 }, created_at: '2026-06-01T01:00:00Z',
          node_count: 1, nodes_preview: [planNode('TS-LV', 'Live task')],
        },
      ],
    };
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(archivedPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    // archived column chip shows 'archived'; archived task card has the badge.
    const archivedCol = screen.getByText('Shelved plan').closest('[data-testid="plan-column"]')!;
    expect(within(archivedCol as HTMLElement).getByTestId('plan-status-chip')).toHaveTextContent('archived');
    const arBadge = within(archivedCol as HTMLElement).getByTestId('task-archived-badge-TS-AR');
    expect(arBadge).toHaveTextContent('Archived');
    expect(arBadge.className).toContain('bg-status-amber-bg');
    // non-archived task in the draft column → NO badge.
    const draftCol = screen.getByText('Live draft').closest('[data-testid="plan-column"]')!;
    expect(within(draftCol as HTMLElement).queryByTestId('task-archived-badge-TS-LV')).not.toBeInTheDocument();
  });

  // @oopslink WORK-BOARD bug + sweep: AssigneeBadge must show the resolved
  // DISPLAY NAME (via useDisplayNameResolver), NOT the raw ref handle-tail.
  describe('AssigneeBadge — resolved display name (the @oopslink bug)', () => {
    it('renders the agent DISPLAY NAME (not the id tail) when the member directory resolves the ref', async () => {
      server.use(
        http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
        http.get('/api/projects/proj-a/plans', () => HttpResponse.json(plansWithDraftNode)),
        // member directory loaded by the resolver's internal useMembers() query.
        http.get('/api/members', () =>
          HttpResponse.json([
            {
              id: 'mem-b', organization_id: 'org-test', identity_id: 'agent:builder',
              kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
              display_name: 'Builder Bot',
            },
          ]),
        ),
      );
      wrap('/projects/proj-a/plans');
      await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

      const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
      const badge = within(draft as HTMLElement).getAllByTestId('assignee')[0];
      // Resolved → the display name; the raw "agent:builder" ref stays on title only.
      await waitFor(() => expect(badge).toHaveTextContent('Builder Bot'));
      expect(badge).toHaveTextContent('Builder Bot');
      expect(badge).not.toHaveTextContent('builder');
      expect(badge).toHaveAttribute('title', 'agent:builder');
      expect(badge).toHaveAttribute('data-kind', 'agent');
    });

    it('falls back to the CLEAN handle (normalizeIdentityRef) for an unresolvable ref', async () => {
      server.use(
        http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
        http.get('/api/projects/proj-a/plans', () => HttpResponse.json(plansWithDraftNode)),
        // directory does NOT contain agent:builder → resolver misses.
        http.get('/api/members', () => HttpResponse.json([])),
      );
      wrap('/projects/proj-a/plans');
      await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

      const draft = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
      const badge = within(draft as HTMLElement).getAllByTestId('assignee')[0];
      // Unresolved → the clean handle "builder", NEVER the raw "agent:builder".
      expect(badge).toHaveTextContent('builder');
      expect(badge.textContent).not.toContain('agent:builder');
      expect(badge).toHaveAttribute('title', 'agent:builder');
    });

    it('shows the Unassigned empty state when there is no assignee', async () => {
      const noAssignee = {
        plans: [
          {
            id: 'PL-U', project_id: 'proj-a', name: 'No-assignee plan', description: '',
            status: 'draft', creator_ref: 'user:owner', conversation_id: 'cU', target_date: null,
            has_failed: false, progress: { done: 0, total: 1 }, created_at: '2026-06-01T01:00:00Z',
            node_count: 1,
            nodes_preview: [{ ...planNode('TS-NA', 'Unassigned task'), assignee_ref: null }],
          },
        ],
      };
      server.use(
        http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
        http.get('/api/projects/proj-a/plans', () => HttpResponse.json(noAssignee)),
      );
      wrap('/projects/proj-a/plans');
      await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

      const col = screen.getByText('No-assignee plan').closest('[data-testid="plan-column"]')!;
      const badge = within(col as HTMLElement).getAllByTestId('assignee')[0];
      expect(badge).toHaveTextContent('Unassigned');
    });
  });
});

// ---------------------------------------------------------------------------
// ADR-0047 — the THREE-segment Work Board: Backlog (unscheduled, not claimable)
// + built-in Assignment Pool (flat, claimable) + structured Plans (DAG). These
// exercise the default mock list (which now includes a PL-BUILTIN is_builtin
// row) and bespoke fixtures.
// ---------------------------------------------------------------------------
describe('ADR-0047 Work Board — 3 segments (backlog / built-in pool / structured plans)', () => {
  afterEach(() => cleanup());

  // poolNode — a built-in-pool node fixture (carries the ADR-0047 `claimable`).
  const poolNode = (
    taskId: string,
    title: string,
    extra: Record<string, unknown> = {},
  ) => ({
    task_id: taskId,
    title,
    assignee_ref: 'agent:builder',
    task_status: 'open',
    node_status: 'ready',
    depends_on: [],
    dispatched_at: null,
    ...extra,
  });

  const threeSegmentPlans = {
    plans: [
      {
        id: 'PL-BUILTIN', project_id: 'proj-a', name: '[Built-in]', description: '',
        status: 'running', creator_ref: 'user:owner', conversation_id: 'cb', target_date: null,
        has_failed: false, progress: { done: 1, total: 4 }, created_at: '2026-06-01T01:00:00Z',
        is_builtin: true, node_count: 4,
        nodes_preview: [
          poolNode('TS-CLAIM', 'Claimable pool task', { node_status: 'dispatched', claimable: true }),
          poolNode('TS-POOL2', 'Pending pool task', { claimable: false }),
          poolNode('TS-DONE', 'Done pool task', { task_status: 'completed', node_status: 'done' }),
          poolNode('TS-DISC', 'Discarded pool task', { task_status: 'discarded', node_status: 'done' }),
        ],
      },
      {
        id: 'PL-2', project_id: 'proj-a', name: 'Billing rework', description: '',
        status: 'draft', creator_ref: 'user:owner', conversation_id: 'c2', target_date: null,
        has_failed: false, progress: { done: 1, total: 2 }, created_at: '2026-06-01T01:00:00Z',
        is_builtin: false, node_count: 2,
        nodes_preview: [
          poolNode('TS-STRUCT-DONE', 'Structured done node', { task_status: 'completed', node_status: 'done' }),
          poolNode('TS-STRUCT-OPEN', 'Structured open node'),
        ],
      },
    ],
  };

  it('renders all THREE segments DISTINCTLY: backlog-column, builtin-pool-column, and a structured plan-column', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    // 1. Backlog — labelled "unscheduled — not claimable".
    const backlog = screen.getByTestId('backlog-column');
    expect(within(backlog).getByTestId('backlog-subtitle')).toHaveTextContent(/not claimable/i);

    // 2. Built-in pool — its OWN segment (not a plan-column), labelled Assignment Pool.
    const pool = screen.getByTestId('builtin-pool-column');
    expect(within(pool).getByText('Assignment Pool')).toBeInTheDocument();
    expect(pool).toHaveAttribute('data-builtin', 'true');
    // The is_builtin plan is NOT rendered as a generic plan-column.
    expect(screen.queryByText('[Built-in]')).not.toBeInTheDocument();

    // 3. Structured plan — the ONLY plan-column (the built-in is excluded).
    const cols = screen.getAllByTestId('plan-column');
    expect(cols).toHaveLength(1);
    expect(within(cols[0]).getByText('Billing rework')).toBeInTheDocument();
  });

  it('the built-in pool shows the CLAIMABLE affordance on a claimable node only', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const pool = screen.getByTestId('builtin-pool-column');
    // The claimable node carries the chip; the pending (claimable:false) does not.
    const chip = within(pool).getByTestId('claimable-chip-TS-CLAIM');
    expect(chip).toHaveTextContent(/claimable/i);
    // both-mode AA: SOLID emerald-100/emerald-800 pair, NO alpha-tint.
    expect(chip.className).toContain('bg-status-emerald-bg');
    expect(chip.className).toContain('text-status-emerald-fg');
    expect(chip.className).not.toMatch(/\/\d+/);
    expect(within(pool).queryByTestId('claimable-chip-TS-POOL2')).not.toBeInTheDocument();
  });

  it('HIDES completed/discarded in the Backlog (count + cards)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const backlog = screen.getByTestId('backlog-column');
    // The default mock returns 1 open + 1 completed + 1 discarded unplanned task.
    expect(within(backlog).getByTestId('backlog-count')).toHaveTextContent('1');
    expect(within(backlog).getByText('unplanned backlog task')).toBeInTheDocument();
    expect(within(backlog).queryByText('completed backlog task')).not.toBeInTheDocument();
    expect(within(backlog).queryByText('discarded backlog task')).not.toBeInTheDocument();
    expect(within(backlog).getAllByTestId('backlog-card')).toHaveLength(1);
  });

  it('HIDES completed/discarded in the built-in pool (count + cards)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const pool = screen.getByTestId('builtin-pool-column');
    // 4 nodes: claimable(open) + pending(open) shown; done + discarded hidden → 2.
    expect(within(pool).getByTestId('builtin-pool-count')).toHaveTextContent('2');
    expect(within(pool).getAllByTestId('pool-task-card')).toHaveLength(2);
    expect(within(pool).getByText('Claimable pool task')).toBeInTheDocument();
    expect(within(pool).getByText('Pending pool task')).toBeInTheDocument();
    expect(within(pool).queryByText('Done pool task')).not.toBeInTheDocument();
    expect(within(pool).queryByText('Discarded pool task')).not.toBeInTheDocument();
  });

  it('a structured plan KEEPS its done node (history), unlike the pool', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const structured = screen.getByText('Billing rework').closest('[data-testid="plan-column"]')!;
    // Both the done (history) AND open nodes render — done is NOT hidden here.
    expect(within(structured as HTMLElement).getByText('Structured done node')).toBeInTheDocument();
    expect(within(structured as HTMLElement).getByText('Structured open node')).toBeInTheDocument();
    expect(within(structured as HTMLElement).getAllByTestId('plan-task-card')).toHaveLength(2);
    // The structured column has NO claimable affordance (pool-only).
    expect(within(structured as HTMLElement).queryByTestId('claimable-chip-TS-STRUCT-OPEN')).not.toBeInTheDocument();
  });

  it('the built-in pool has NO DAG/edge/remove affordance (flat) — no remove button on a pool card', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    const pool = screen.getByTestId('builtin-pool-column');
    const card = within(pool).getAllByTestId('pool-task-card')[0];
    // Flat: no remove control, not draggable (no data-draggable).
    expect(within(pool).queryByTestId('plan-task-remove-TS-CLAIM')).not.toBeInTheDocument();
    expect(card).not.toHaveAttribute('draggable', 'true');
  });

  it('selecting a Backlog task INTO the pool is allowed: the add-menu offers Assignment Pool → POST to the built-in plan', async () => {
    let postedTo: string | undefined;
    let posted: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
      http.post('/api/projects/proj-a/plans/:planId/tasks', async ({ params, request }) => {
        postedTo = String(params.planId);
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: postedTo, project_id: 'proj-a', name: 'p', status: 'running', has_failed: false, progress: { done: 0, total: 1 }, nodes: [] });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    fireEvent.click(screen.getByTestId('backlog-add-TS-BL1'));
    const menu = screen.getByTestId('add-menu-TS-BL1');
    const poolItem = within(menu).getByTestId('add-to-plan-TS-BL1-PL-BUILTIN');
    expect(poolItem).toHaveTextContent('Assignment Pool');
    await act(async () => {
      fireEvent.click(poolItem);
    });
    await waitFor(() => expect(posted).toEqual({ task_id: 'TS-BL1' }));
    expect(postedTo).toBe('PL-BUILTIN');
  });

  it('dragging a Backlog task onto the pool selects it in (POST to the built-in plan)', async () => {
    let postedTo: string | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans', () => HttpResponse.json(threeSegmentPlans)),
      http.post('/api/projects/proj-a/plans/:planId/tasks', async ({ params }) => {
        postedTo = String(params.planId);
        return HttpResponse.json({ id: postedTo, project_id: 'proj-a', name: 'p', status: 'running', has_failed: false, progress: { done: 0, total: 1 }, nodes: [] });
      }),
    );
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());

    function dt() {
      const store: Record<string, string> = {};
      return {
        setData: (k: string, v: string) => { store[k] = v; },
        getData: (k: string) => store[k] ?? '',
        get types() { return Object.keys(store); },
        effectAllowed: '', dropEffect: '',
      } as unknown as DataTransfer;
    }

    const backlog = screen.getByTestId('backlog-column');
    const backlogCard = within(backlog).getByTestId('backlog-card');
    const pool = screen.getByTestId('builtin-pool-column');
    const transfer = dt();
    fireEvent.dragStart(backlogCard, { dataTransfer: transfer });
    // pool accepts a backlog-origin drag (fromPlanId null).
    expect(pool).toHaveAttribute('data-droppable', 'true');
    fireEvent.dragOver(pool, { dataTransfer: transfer });
    await act(async () => {
      fireEvent.drop(pool, { dataTransfer: transfer });
    });
    await waitFor(() => expect(postedTo).toBe('PL-BUILTIN'));
  });

  it('uses the DEFAULT mock list which already includes a built-in pool segment', async () => {
    server.use(http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)));
    wrap('/projects/proj-a/plans');
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    // The shared handlers.ts mock now ships PL-BUILTIN (is_builtin) + 2 structured.
    expect(screen.getByTestId('builtin-pool-column')).toBeInTheDocument();
    expect(screen.getByTestId('claimable-chip-TS-CLAIM')).toBeInTheDocument();
    expect(screen.getAllByTestId('plan-column')).toHaveLength(2);
  });
});

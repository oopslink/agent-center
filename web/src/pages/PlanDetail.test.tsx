import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import PlanDetail from './PlanDetail';

// PlanDetail — v2.9 #287 EXECUTION view (DAG + chat + task list; NO backlog).
// The #286 backlog→Plan SELECTION is removed; these tests assert the new
// header / tabs / DAG (6-state nodes + edges + Advance) / task-list /
// conversation-side, the lifecycle + draft-gating, and that no backlog UI
// remains. usePlan + the conversation are mocked via MSW.

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

const projectAlpha = {
  id: 'proj-a',
  organization_id: 'org-test',
  name: 'Project Alpha',
  description: '',
  status: 'active',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-20T01:00:00Z',
  updated_at: '2026-05-20T01:00:00Z',
};

// A 5-node plan covering all 6 node states across 3 dependency levels:
//   n1(done) ─▶ n2(done) ─▶ n3(running)
//                        ─▶ n4(failed)
//   n5(blocked) depends on n3 + n4   (level 2)
// Plus a standalone for ready/dispatched on the list.
function planWith(overrides: Record<string, unknown> = {}) {
  return {
    id: 'PL-1',
    project_id: 'proj-a',
    name: 'v3.0 release plan',
    description: '',
    status: 'running',
    creator_ref: 'user:owner',
    conversation_id: 'conv-plan-1',
    target_date: '2026-07-15T00:00:00Z',
    has_failed: true,
    progress: { done: 2, total: 6 },
    created_at: '2026-06-01T01:00:00Z',
    nodes: [
      { task_id: 'n1', title: 'design schema', assignee_ref: 'agent:dev', task_status: 'completed', node_status: 'done', depends_on: [] },
      { task_id: 'n2', title: 'backend api', assignee_ref: 'agent:dev', task_status: 'completed', node_status: 'done', depends_on: ['n1'] },
      { task_id: 'n3', title: 'frontend list', assignee_ref: 'agent:dev2', task_status: 'running', node_status: 'running', depends_on: ['n2'] },
      { task_id: 'n4', title: 'migration', assignee_ref: 'agent:dev', task_status: 'in_progress', node_status: 'failed', depends_on: ['n2'] },
      { task_id: 'n5', title: 'orchestrator wiring', assignee_ref: 'agent:dev', task_status: 'open', node_status: 'ready', depends_on: ['n3', 'n4'] },
      { task_id: 'n6', title: 'e2e accept', assignee_ref: 'user:you', task_status: 'open', node_status: 'blocked', depends_on: ['n5'] },
      { task_id: 'n7', title: 'docs', assignee_ref: 'agent:dev2', task_status: 'open', node_status: 'dispatched', depends_on: [] },
    ],
    ...overrides,
  };
}

function mockPlan(overrides: Record<string, unknown> = {}) {
  server.use(
    http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
    http.get('/api/projects/proj-a/plans/PL-1', () => HttpResponse.json(planWith(overrides))),
  );
}

function wrap(path = '/projects/proj-a/plans/PL-1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/projects/:id/plans/:planId" element={<PlanDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('PlanDetail — v2.9 #287 execution view', () => {
  afterEach(() => cleanup());

  it('renders the header: name + status + failed indicator + progress + creator', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    const hd = screen.getByTestId('plan-detail-header');
    expect(within(hd).getByText('v3.0 release plan')).toBeInTheDocument();
    expect(within(hd).getByTestId('plan-status-chip')).toHaveTextContent('running');
    expect(within(hd).getByTestId('plan-failed-indicator')).toBeInTheDocument();
    expect(screen.getByTestId('plan-progress')).toHaveTextContent('2/6');
    expect(screen.getByTestId('plan-creator')).toHaveTextContent('@owner');
  });

  it('shows Stop (→ draft) when running and calls useStopPlan', async () => {
    let stopped = false;
    mockPlan();
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/stop', () => {
        stopped = true;
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-stop-btn')).toBeInTheDocument());
    await act(async () => fireEvent.click(screen.getByTestId('plan-stop-btn')));
    await waitFor(() => expect(stopped).toBe(true));
  });

  it('shows Start when draft (not Stop) and calls useStartPlan', async () => {
    let started = false;
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/start', () => {
        started = true;
        return HttpResponse.json(planWith({ status: 'running' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-start-btn')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-stop-btn')).not.toBeInTheDocument();
    await act(async () => fireEvent.click(screen.getByTestId('plan-start-btn')));
    await waitFor(() => expect(started).toBe(true));
  });

  it('tabs switch between DAG and task list; task-list count = node count', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // tab label carries the node count (7)
    expect(screen.getByTestId('plan-tab-tasks')).toHaveTextContent('7');
    // default = DAG; task list not shown
    expect(screen.queryByTestId('plan-task-list')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    expect(screen.getByTestId('plan-task-list')).toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag')).not.toBeInTheDocument();
  });

  it('DAG renders a node per task with the 6-state chips (label + color) + Advance', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // one positioned node per task
    expect(screen.getAllByTestId('plan-dag-node')).toHaveLength(7);
    // every node_status surfaces as a chip (label + color); legend shows all 6
    const chips = screen.getAllByTestId('node-state-chip');
    const statuses = new Set(chips.map((c) => c.getAttribute('data-node-status')));
    for (const st of ['blocked', 'ready', 'dispatched', 'running', 'done', 'failed']) {
      expect(statuses.has(st)).toBe(true);
    }
    // locked palette: each state's literal class pair is applied
    const legend = screen.getByTestId('plan-dag-legend');
    expect(within(legend).getByText('done').className).toContain('bg-emerald-100');
    expect(within(legend).getByText('done').className).toContain('text-emerald-800');
    expect(within(legend).getByText('failed').className).toContain('bg-rose-100');
    expect(within(legend).getByText('dispatched').className).toContain('bg-violet-100');
    // Advance button present while running
    expect(screen.getByTestId('plan-advance-btn')).toBeInTheDocument();
  });

  it('DAG computes a layered left→right layout from depends_on', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    const lvl = (id: string) =>
      Number(screen.getByTestId('plan-dag').querySelector(`[data-task-id="${id}"]`)!.getAttribute('data-level'));
    // n1 has no deps → level 0; n2 depends on n1 → 1; n3/n4 on n2 → 2; n5 → 3; n6 → 4
    expect(lvl('n1')).toBe(0);
    expect(lvl('n2')).toBe(1);
    expect(lvl('n3')).toBe(2);
    expect(lvl('n4')).toBe(2);
    expect(lvl('n5')).toBe(3);
    expect(lvl('n6')).toBe(4);
  });

  it('DAG draws an edge per depends_on relation (upstream→downstream)', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // depends_on edges: n2←n1, n3←n2, n4←n2, n5←n3, n5←n4, n6←n5 = 6 edges
    const edges = screen.getAllByTestId('plan-dag-edge');
    expect(edges).toHaveLength(6);
    const keys = edges.map((e) => e.getAttribute('data-edge'));
    expect(keys).toContain('n1->n2');
    expect(keys).toContain('n2->n3');
    expect(keys).toContain('n3->n5');
    // arrow marker defined
    expect(screen.getByTestId('plan-dag-svg').querySelector('#plan-dag-arrow')).not.toBeNull();
  });

  it('Advance calls useAdvancePlan (idempotent advance-all-ready)', async () => {
    let advanced = false;
    mockPlan();
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/advance', () => {
        advanced = true;
        return HttpResponse.json(planWith());
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-advance-btn')).toBeInTheDocument());
    await act(async () => fireEvent.click(screen.getByTestId('plan-advance-btn')));
    await waitFor(() => expect(advanced).toBe(true));
  });

  it('DAG is display-only: note never claims an edge editor, and no edge-edit control exists', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-dag-note')).toBeInTheDocument());
    const note = screen.getByTestId('plan-dag-note');
    // derived/display-only is stated, but the note must NOT claim edges are editable here
    expect(note).toHaveTextContent('derived');
    expect(note.textContent ?? '').toMatch(/display-only/i);
    // must NOT claim an edge editor lives here (the old over-claim wording)
    expect(note.textContent ?? '').not.toMatch(/edges are editable|editable in draft|edges are locked/i);
    // the over-claiming affordance elements are gone (no editor is here)
    expect(screen.queryByTestId('plan-edge-edit-hint')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-edit-locked')).not.toBeInTheDocument();
    // no control to change a node's status (derived) and no edge editor control
    expect(screen.queryByTestId('node-status-select')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-add')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-remove')).not.toBeInTheDocument();
  });

  it('DAG is display-only in draft too: still no edge-edit affordance', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-dag-note')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-edge-edit-hint')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-edit-locked')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-add')).not.toBeInTheDocument();
  });

  it('lifecycle controls (Start/Stop/Advance) each render exactly once, status-gated', async () => {
    // running → Stop + Advance once each, no Start
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-advance-btn')).toBeInTheDocument());
    expect(screen.getAllByTestId('plan-advance-btn')).toHaveLength(1);
    expect(screen.getAllByTestId('plan-stop-btn')).toHaveLength(1);
    expect(screen.queryByTestId('plan-start-btn')).not.toBeInTheDocument();
    // no duplicated DAG-footer controls
    expect(screen.queryByTestId('plan-dag-start-btn')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag-stop-btn')).not.toBeInTheDocument();
    cleanup();

    // draft → Start once, no Stop/Advance
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-start-btn')).toBeInTheDocument());
    expect(screen.getAllByTestId('plan-start-btn')).toHaveLength(1);
    expect(screen.queryByTestId('plan-stop-btn')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-advance-btn')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag-start-btn')).not.toBeInTheDocument();
  });

  it('task-list tab lists nodes with task_status + node_status chips', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    const table = screen.getByTestId('plan-task-list-table');
    expect(within(table).getAllByTestId('plan-task-row')).toHaveLength(7);
    // task_status reuses StatusChip; node_status uses NodeStateChip
    expect(within(table).getAllByTestId('status-chip').length).toBeGreaterThan(0);
    expect(within(table).getAllByTestId('node-state-chip').length).toBe(7);
  });

  it('conversation side renders ConversationView with the plan conversation_id', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-conversation')).toBeInTheDocument());
    const cv = await screen.findByTestId('conversation-view');
    expect(cv).toBeInTheDocument();
    expect(screen.getByTestId('plan-conversation-body')).toBeInTheDocument();
  });

  it('empty conversation_id → friendly initializing state (no crash)', async () => {
    mockPlan({ conversation_id: '' });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-conversation')).toBeInTheDocument());
    expect(screen.getByTestId('plan-conversation-initializing')).toBeInTheDocument();
    expect(screen.queryByTestId('conversation-view')).not.toBeInTheDocument();
  });

  it('NO backlog-select UI remains (the #286 selection is removed)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('page-PlanDetail')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-task-selection')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-backlog-table')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-selected-table')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag-placeholder')).not.toBeInTheDocument();
  });

  // ── P2-4: auto-advancing indicator + Advance reframed as override ──────────
  it('running plan shows the auto-advancing indicator (near the status chip)', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    const ind = screen.getByTestId('plan-auto-advancing');
    expect(ind).toHaveTextContent(/auto-advancing/i);
    // both-mode AA token (text-text-secondary), NO alpha-tint bg
    expect(ind.className).toContain('text-text-secondary');
    expect(ind.className).not.toMatch(/\/\d+/); // no bg-{token}/{opacity}
    // informational hint present
    expect(ind.getAttribute('title') ?? '').toMatch(/dispatches ready nodes automatically/i);
  });

  it('draft plan does NOT show the auto-advancing indicator', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-auto-advancing')).not.toBeInTheDocument();
  });

  it('done plan does NOT show the auto-advancing indicator', async () => {
    mockPlan({ status: 'done', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-auto-advancing')).not.toBeInTheDocument();
  });

  it('Advance button is kept (running) reframed as a manual override and still calls useAdvancePlan', async () => {
    let advanced = false;
    mockPlan();
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/advance', () => {
        advanced = true;
        return HttpResponse.json(planWith());
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-advance-btn')).toBeInTheDocument());
    const btn = screen.getByTestId('plan-advance-btn');
    // reworded label + override affordance (title/aria-label)
    expect(btn).toHaveTextContent(/advance now/i);
    expect(btn.getAttribute('title') ?? '').toMatch(/the system already advances automatically/i);
    expect(btn.getAttribute('aria-label') ?? '').toMatch(/manually dispatch ready nodes/i);
    // Stop still present alongside it
    expect(screen.getByTestId('plan-stop-btn')).toBeInTheDocument();
    // function unchanged → still hits useAdvancePlan
    await act(async () => fireEvent.click(btn));
    await waitFor(() => expect(advanced).toBe(true));
  });

  it('#218: plan-load error → friendly ErrorState with raw error behind [Details]', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans/PL-1', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such API route' }, { status: 404 }),
      ),
    );
    wrap();
    const friendly = await screen.findByTestId('plan-not-found');
    expect(friendly).toHaveTextContent("Couldn't load this plan.");
    const primary = screen.getByText("Couldn't load this plan.");
    expect(primary.tagName).toBe('P');
    expect(primary).not.toHaveTextContent('no such API route');
    const raw = screen.getByTestId('plan-not-found-raw');
    expect(raw).toHaveTextContent('[404 not_found] no such API route');
    expect(raw.closest('details')).not.toBeNull();
    expect(screen.getByText('← Back to plans')).toBeInTheDocument();
  });
});

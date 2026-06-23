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
    org_ref: 'P9',
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
    // v2.10.1 [T99]: the human Plan id (P9) shows in the header.
    expect(within(hd).getByTestId('plan-detail-ref')).toHaveTextContent('P9');
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

  // T53: a paused node gets an operator Resume button (task-list tab); a
  // non-paused node does not. Clicking it POSTs the node resume.
  it('paused node shows a Resume button that POSTs the node resume (T53)', async () => {
    let resumedTask = '';
    mockPlan({
      nodes: [
        { task_id: 'np', title: 'paused node', assignee_ref: 'agent:dev', task_status: 'running', node_status: 'paused', depends_on: [] },
        { task_id: 'nr', title: 'running node', assignee_ref: 'agent:dev', task_status: 'running', node_status: 'running', depends_on: [] },
      ],
    });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/nodes/:taskId/resume', ({ params }) => {
        resumedTask = String(params.taskId);
        return HttpResponse.json(planWith({}));
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    await waitFor(() => expect(screen.getByTestId('plan-task-list')).toBeInTheDocument());
    // Resume only on the paused node.
    expect(screen.getByTestId('plan-node-resume-np')).toBeInTheDocument();
    expect(screen.queryByTestId('plan-node-resume-nr')).not.toBeInTheDocument();
    await act(async () => fireEvent.click(screen.getByTestId('plan-node-resume-np')));
    await waitFor(() => expect(resumedTask).toBe('np'));
  });

  // T101: when the node is paused because its agent set the item aside to switch
  // to ANOTHER task, the agent is now busy → the backend refuses (409 agent_busy).
  // The operator must see WHY (accurate hint), not a generic "try again".
  it('shows an accurate hint (not the generic error) when resume fails agent_busy (T101)', async () => {
    mockPlan({
      nodes: [
        { task_id: 'np', title: 'paused node', assignee_ref: 'agent:dev', task_status: 'running', node_status: 'paused', depends_on: [] },
      ],
    });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/nodes/:taskId/resume', () =>
        HttpResponse.json(
          { error: 'agent_busy', message: "the node's agent is busy on another work item; try again after it settles" },
          { status: 409 },
        ),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    await waitFor(() => expect(screen.getByTestId('plan-task-list')).toBeInTheDocument());
    await act(async () => fireEvent.click(screen.getByTestId('plan-node-resume-np')));
    const err = await screen.findByTestId('plan-node-resume-error-np');
    expect(err).toHaveTextContent(/busy on another/i);
    expect(err.textContent ?? '').not.toMatch(/Please try again/i);
  });

  it('shows a "nothing to resume" hint when resume fails node_not_paused (T101)', async () => {
    mockPlan({
      nodes: [
        { task_id: 'np', title: 'paused node', assignee_ref: 'agent:dev', task_status: 'running', node_status: 'paused', depends_on: [] },
      ],
    });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/nodes/:taskId/resume', () =>
        HttpResponse.json(
          { error: 'node_not_paused', message: 'the plan node has no paused work item to resume' },
          { status: 409 },
        ),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    await waitFor(() => expect(screen.getByTestId('plan-task-list')).toBeInTheDocument());
    await act(async () => fireEvent.click(screen.getByTestId('plan-node-resume-np')));
    const err = await screen.findByTestId('plan-node-resume-error-np');
    expect(err).toHaveTextContent(/no paused work item|already resumed/i);
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

  it('three tabs (chat/DAG/tasks); chat is default; switching shows DAG / task list; task-list count = node count', async () => {
    mockPlan();
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-tabs')).toBeInTheDocument());
    // all three tabs exist
    expect(screen.getByTestId('plan-tab-chat')).toBeInTheDocument();
    expect(screen.getByTestId('plan-tab-dag')).toBeInTheDocument();
    expect(screen.getByTestId('plan-tab-tasks')).toBeInTheDocument();
    // T132: tabs are English-only, no「(中文)」括注 — exactly Chat / DAG / Task List.
    expect(screen.getByTestId('plan-tab-chat')).toHaveTextContent('Chat');
    expect(screen.getByTestId('plan-tab-dag')).toHaveTextContent('DAG');
    expect(screen.getByTestId('plan-tab-tasks')).toHaveTextContent('Task List');
    // T134: the "← execution view (no backlog — planning is on the Board)" hint
    // is removed from the tab bar.
    expect(screen.queryByText(/execution view/i)).not.toBeInTheDocument();
    expect(screen.getByTestId('plan-tabs')).not.toHaveTextContent(/planning is on the Board/i);
    // default = chat: the chat panel + conversation are shown; DAG + task list are not
    expect(screen.getByTestId('plan-panel-chat')).toBeInTheDocument();
    expect(await screen.findByTestId('plan-conversation')).toBeInTheDocument();
    // per @oopslink: the conversation panel names the plan by its concrete id
    // (org_ref "P9") instead of the generic "Plan conversation" label.
    expect(screen.getByTestId('plan-conversation-code')).toHaveTextContent('P9');
    expect(screen.queryByTestId('plan-dag')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-task-list')).not.toBeInTheDocument();
    // clicking DAG shows the DAG (and not the task list)
    fireEvent.click(screen.getByTestId('plan-tab-dag'));
    expect(screen.getByTestId('plan-dag')).toBeInTheDocument();
    expect(screen.queryByTestId('plan-task-list')).not.toBeInTheDocument();
    // clicking Task list shows the task list (and not the DAG)
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    expect(screen.getByTestId('plan-task-list')).toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag')).not.toBeInTheDocument();
  });

  it('maximizes via the tab-row toggle / restores via the overlay button (T347)', async () => {
    mockPlan();
    wrap();
    const section = await screen.findByTestId('plan-conversation');
    expect(section).toHaveAttribute('data-maximized', 'false');
    // T347: the maximize toggle now lives on the tab row, not above the chat.
    fireEvent.click(screen.getByTestId('plan-chat-maximize'));
    expect(screen.getByTestId('plan-conversation')).toHaveAttribute('data-maximized', 'true');
    // restore from inside the maximized overlay.
    fireEvent.click(screen.getByTestId('plan-conversation-restore'));
    expect(screen.getByTestId('plan-conversation')).toHaveAttribute('data-maximized', 'false');
  });

  it('shows the plan goal (description) on the detail page (T347)', async () => {
    mockPlan({ description: 'ship the v3 orchestrator' });
    wrap();
    expect(await screen.findByTestId('plan-goal')).toHaveTextContent('ship the v3 orchestrator');
    // short goal: no collapse toggle.
    expect(screen.queryByTestId('plan-goal-toggle')).toBeNull();
  });

  it('collapses a long plan goal (clamped by default; Show more / less) — T349', async () => {
    const longGoal = 'A'.repeat(200);
    mockPlan({ description: longGoal });
    wrap();
    const goal = await screen.findByTestId('plan-goal');
    // clamped by default.
    expect(goal.className).toContain('line-clamp-2');
    const toggle = screen.getByTestId('plan-goal-toggle');
    expect(toggle).toHaveTextContent('Show more');
    fireEvent.click(toggle);
    expect(screen.getByTestId('plan-goal').className).not.toContain('line-clamp-2');
    expect(screen.getByTestId('plan-goal-toggle')).toHaveTextContent('Show less');
  });

  it('collapses the header actions into a mobile Actions dropdown (T341)', async () => {
    mockPlan();
    wrap();
    // The Actions toggle exists (mobile); the action buttons are still in the DOM
    // (md: always-shown on desktop, dropdown on mobile).
    expect(await screen.findByTestId('plan-actions-toggle')).toBeInTheDocument();
    expect(within(screen.getByTestId('plan-actions')).getByTestId('plan-edit-btn')).toBeInTheDocument();
  });

  it('DAG renders a node per task with the 6-state chips (label + color) + Advance', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
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
    expect(within(legend).getByText('done').className).toContain('bg-status-emerald-bg');
    expect(within(legend).getByText('done').className).toContain('text-status-emerald-fg');
    expect(within(legend).getByText('failed').className).toContain('bg-status-rose-bg');
    expect(within(legend).getByText('dispatched').className).toContain('bg-status-violet-bg');
    // Advance button present while running
    expect(screen.getByTestId('plan-advance-btn')).toBeInTheDocument();
  });

  // T53: `paused` is a first-class node state — its legend chip renders with the
  // distinct stone palette (not the default/blocked fallback), so a node whose
  // agent paused its work item reads truthfully instead of as phantom `running`.
  it('legend includes a paused chip with the stone palette (T53)', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    const legend = screen.getByTestId('plan-dag-legend');
    const paused = within(legend).getByText('paused');
    expect(paused.className).toContain('bg-status-stone-bg');
    expect(paused.className).toContain('text-status-stone-fg');
  });

  it('point 1 (T126): DAG nodes + task-list rows show the Task id from node.org_ref; no-org_ref → FULL id, never a #hash', async () => {
    // T126: org_ref rides on the plan NODE (api/plans PlanNode.org_ref) and is
    // used DIRECTLY — no FE task-list re-resolver (which missed completed tasks
    // → leaked #4e2e71). n1/n2 carry org_ref; n3 does not.
    mockPlan({
      nodes: [
        { task_id: 'n1', title: 'design schema', assignee_ref: 'agent:dev', task_status: 'completed', node_status: 'done', depends_on: [], org_ref: 'T101' },
        { task_id: 'n2', title: 'backend api', assignee_ref: 'agent:dev', task_status: 'completed', node_status: 'done', depends_on: ['n1'], org_ref: 'T102' },
        { task_id: 'n3', title: 'frontend list', assignee_ref: 'agent:dev2', task_status: 'running', node_status: 'running', depends_on: ['n2'] },
      ],
    });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // DAG node carries the node's own T-number (scope to the graph node; the
    // v2.10.1 [M4] mobile stepper <li> also carries data-task-id).
    const node1 = screen.getByTestId('plan-dag').querySelector('[data-testid="plan-dag-node"][data-task-id="n1"]') as HTMLElement;
    await waitFor(() => expect(within(node1).getByTestId('plan-node-taskid')).toHaveTextContent('T101'));
    // a node with NO org_ref shows the FULL task id (never a #id-tail hash) — the
    // exact T126 regression (completed tasks leaked a 6-char #hash).
    const node3 = screen.getByTestId('plan-dag').querySelector('[data-testid="plan-dag-node"][data-task-id="n3"]') as HTMLElement;
    const tag3 = within(node3).getByTestId('plan-node-taskid');
    expect(tag3).toHaveTextContent('n3');
    expect(tag3).not.toHaveTextContent('#');
    // task-list rows show the same Task id
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    const row1 = screen.getByTestId('plan-task-list').querySelector('[data-task-id="n1"]') as HTMLElement;
    expect(within(row1).getByTestId('plan-row-taskid')).toHaveTextContent('T101');
  });

  it('point 2: compact toggle zooms the DAG down so a long plan fits', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    const toggle = screen.getByTestId('plan-dag-compact-toggle');
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('plan-dag-canvas')).toHaveAttribute('data-compact', 'false');
    expect(screen.getByTestId('plan-dag-scaler').getAttribute('style') ?? '').not.toContain('scale(');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByTestId('plan-dag-canvas')).toHaveAttribute('data-compact', 'true');
    // compact applies a uniform downscale so a wide/long DAG fits in view
    expect(screen.getByTestId('plan-dag-scaler').getAttribute('style') ?? '').toContain('scale(0.7)');
  });

  it('DAG computes a layered left→right layout from depends_on', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
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
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
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
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
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
    // no control to change a node's status (derived); the OLD dropdown editor box
    // is gone entirely (point 3 §21 single entry).
    expect(screen.queryByTestId('node-status-select')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag-editor')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-add')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-remove')).not.toBeInTheDocument();
  });

  it('running plan is display-only: NO in-graph connect / edge-delete controls (and no old editor box)', async () => {
    mockPlan(); // default = running
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // old dropdown box gone
    expect(screen.queryByTestId('plan-dag-editor')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-add')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-remove')).not.toBeInTheDocument();
    // new in-graph affordances are NOT rendered on a running plan
    expect(screen.queryByTestId('plan-node-connect')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-delete')).not.toBeInTheDocument();
  });

  it('done plan is display-only: NO in-graph connect / edge-delete controls', async () => {
    mockPlan({ status: 'done', has_failed: false });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-dag-editor')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-node-connect')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-delete')).not.toBeInTheDocument();
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
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
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

  // ── v2.9.1 point 3: IN-GRAPH dependency editing (draft-only) ───────────────
  // The old PlanDagEditor two-select dropdown box is GONE (§21 single entry).
  // from/to semantics (verified vs backend plan_view.go + plan_flow.go):
  // AddPlanDependency(from, to) ⟺ "from depends_on to"; a node's depends_on
  // lists edge.ToTaskID where edge.FromTaskID == node. So edge "B depends on A"
  // → { from_task_id: B, to_task_id: A }. The keyboard/click connect path uses
  // validDropTargets (self/exists/cycle excluded at the UI layer).
  //
  // Helpers to find the in-graph controls inside a specific node.
  function dagNode(taskId: string): HTMLElement {
    // Scope to the SVG-graph node (plan-dag-node) — v2.10.1 [M4] added a mobile
    // stepper whose <li> also carries data-task-id.
    return screen
      .getByTestId('plan-dag')
      .querySelector(`[data-testid="plan-dag-node"][data-task-id="${taskId}"]`) as HTMLElement;
  }

  it('draft plan shows in-graph affordances: a connect button per node + a delete control per edge', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // the OLD dropdown editor box is gone entirely
    expect(screen.queryByTestId('plan-dag-editor')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-add')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-edge-remove')).not.toBeInTheDocument();
    // a connect control per node (7 nodes), each a real <button> with an aria-label
    const connects = screen.getAllByTestId('plan-node-connect');
    expect(connects).toHaveLength(7);
    for (const c of connects) expect(c.tagName).toBe('BUTTON');
    const n1Connect = within(dagNode('n1')).getByTestId('plan-node-connect');
    expect(n1Connect).toHaveAttribute('aria-label', 'Add dependency from design schema');
    // a delete control per existing edge (6 depends_on edges in the fixture)
    const dels = screen.getAllByTestId('plan-edge-delete');
    expect(dels).toHaveLength(6);
    for (const d of dels) expect(d.tagName).toBe('BUTTON');
    const n2n1del = dels.find((el) => el.getAttribute('data-edge') === 'n2->n1');
    expect(n2n1del).toBeDefined();
    expect(n2n1del!).toHaveAttribute('aria-label', 'Remove dependency: backend api depends on design schema');
  });

  it('draft: clicking a node connect button enters connect mode and lights up valid targets', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // not in connect mode yet
    expect(screen.queryByTestId('plan-connect-banner')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-connect-target')).not.toBeInTheDocument();
    // connect FROM n7 (docs, no deps). Valid targets exclude self + already-linked
    // + cycle-forming. n7 has no deps and nothing depends on it → every OTHER node
    // is a valid target (6 of them).
    fireEvent.click(within(dagNode('n7')).getByTestId('plan-node-connect'));
    expect(screen.getByTestId('plan-connect-banner')).toBeInTheDocument();
    const targets = screen.getAllByTestId('plan-connect-target');
    expect(targets).toHaveLength(6);
    for (const t of targets) expect(t.tagName).toBe('BUTTON');
    // the source node itself is NOT an activatable target (self blocked)
    expect(within(dagNode('n7')).queryByTestId('plan-connect-target')).not.toBeInTheDocument();
    // while in connect mode, the per-node connect buttons are hidden
    expect(screen.queryByTestId('plan-node-connect')).not.toBeInTheDocument();
  });

  it('draft: activating a valid target calls useAddDependency with { from_task_id, to_task_id } (from depends_on to)', async () => {
    let body: { from_task_id?: string; to_task_id?: string } | null = null;
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/dependencies', async ({ request }) => {
        body = (await request.json()) as { from_task_id: string; to_task_id: string };
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // "docs" (n7) depends on "design schema" (n1): connect from n7, activate n1.
    fireEvent.click(within(dagNode('n7')).getByTestId('plan-node-connect'));
    const n1Target = within(dagNode('n1')).getByTestId('plan-connect-target');
    await act(async () => fireEvent.click(n1Target));
    await waitFor(() => expect(body).not.toBeNull());
    expect(body).toEqual({ from_task_id: 'n7', to_task_id: 'n1' });
    // connect mode exits after activating
    await waitFor(() => expect(screen.queryByTestId('plan-connect-banner')).not.toBeInTheDocument());
  });

  it('draft: a cycle/self/existing target is NOT offered as an activatable target (UI-layer block)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // Connect FROM n1 (design schema, level 0). n2..n6 all transitively depend on
    // n1, so making n1 depend on any of them would CLOSE A CYCLE → none offered.
    // n7 (docs) is independent → the only valid target.
    fireEvent.click(within(dagNode('n1')).getByTestId('plan-node-connect'));
    const targets = screen.getAllByTestId('plan-connect-target');
    expect(targets).toHaveLength(1);
    // the only valid target is n7
    expect(targets[0].closest('[data-task-id]')!.getAttribute('data-task-id')).toBe('n7');
    // self (n1) and the cycle-forming nodes (n2, n6) are NOT activatable targets
    expect(within(dagNode('n1')).queryByTestId('plan-connect-target')).not.toBeInTheDocument();
    expect(within(dagNode('n2')).queryByTestId('plan-connect-target')).not.toBeInTheDocument();
    expect(within(dagNode('n6')).queryByTestId('plan-connect-target')).not.toBeInTheDocument();
  });

  it('draft: an already-existing edge target is NOT offered again (exists excluded)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // n2 already depends on n1. Connect FROM n2 → n1 must NOT be an activatable
    // target (exists). n3..n6 depend on n2 (cycle), so only the independent n7 is
    // offered.
    fireEvent.click(within(dagNode('n2')).getByTestId('plan-node-connect'));
    expect(within(dagNode('n1')).queryByTestId('plan-connect-target')).not.toBeInTheDocument();
    const targets = screen.getAllByTestId('plan-connect-target');
    expect(targets.map((t) => t.closest('[data-task-id]')!.getAttribute('data-task-id'))).toEqual(['n7']);
  });

  it('draft: Escape exits connect mode without adding; the Cancel affordance also exits', async () => {
    let addHit = false;
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/dependencies', () => {
        addHit = true;
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // enter connect mode, then Escape
    fireEvent.click(within(dagNode('n7')).getByTestId('plan-node-connect'));
    expect(screen.getByTestId('plan-connect-banner')).toBeInTheDocument();
    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('plan-connect-banner')).not.toBeInTheDocument());
    // the per-node connect controls return
    expect(screen.getAllByTestId('plan-node-connect').length).toBeGreaterThan(0);
    // re-enter, then click Cancel
    fireEvent.click(within(dagNode('n7')).getByTestId('plan-node-connect'));
    fireEvent.click(screen.getByTestId('plan-connect-cancel'));
    await waitFor(() => expect(screen.queryByTestId('plan-connect-banner')).not.toBeInTheDocument());
    // nothing was added
    await act(async () => { await Promise.resolve(); });
    expect(addHit).toBe(false);
  });

  it('draft: an edge delete control FIRES the DELETE request with the correct ids (real wiring)', async () => {
    // Mock at the api/http (MSW) layer — NOT the useRemoveDependency hook — so the
    // actual button→onClick→mutate→api.del path is exercised end-to-end.
    let delHit = false;
    let addHit = false;
    let params: { from: string | null; to: string | null } = { from: null, to: null };
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1/dependencies', ({ request }) => {
        const url = new URL(request.url);
        params = { from: url.searchParams.get('from_task_id'), to: url.searchParams.get('to_task_id') };
        delHit = true;
        return new HttpResponse(null, { status: 204 });
      }),
      // if clicking delete somehow fired an add, this POST would trip.
      http.post('/api/projects/proj-a/plans/PL-1/dependencies', () => {
        addHit = true;
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getAllByTestId('plan-edge-delete').length).toBeGreaterThan(0));
    const del = screen
      .getAllByTestId('plan-edge-delete')
      .find((el) => el.getAttribute('data-edge') === 'n2->n1')!;
    await act(async () => fireEvent.click(del));
    await waitFor(() => expect(delHit).toBe(true));
    // "n2 depends on n1" → { from_task_id: n2, to_task_id: n1 }
    expect(params).toEqual({ from: 'n2', to: 'n1' });
    expect(addHit).toBe(false);
  });

  it('#218: a cycle error from add surfaces the FRIENDLY message (not the raw API error)', async () => {
    // Force a target through despite the UI guard by mocking a 400 cycle response
    // on a LEGAL UI target (n7→n1): the friendly mapping still applies.
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/dependencies', () =>
        HttpResponse.json(
          { error: 'invalid_request', message: 'projectmanager: dependency would create a cycle' },
          { status: 400 },
        ),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(within(dagNode('n7')).getByTestId('plan-node-connect'));
    await act(async () => fireEvent.click(within(dagNode('n1')).getByTestId('plan-connect-target')));
    const err = await screen.findByTestId('plan-edge-error');
    expect(err).toHaveTextContent('That would create a cycle in the plan.');
    // raw backend error must NOT leak
    expect(err.textContent ?? '').not.toMatch(/projectmanager:|invalid_request/);
  });

  it('draft: plan-dag-note states dependencies ARE editable here (in-graph)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag-note')).toBeInTheDocument());
    const note = screen.getByTestId('plan-dag-note');
    expect(note.textContent ?? '').toMatch(/editable/i);
    expect(note.textContent ?? '').not.toMatch(/display-only/i);
  });

  it('error surface + in-graph controls use solid danger/semantic tokens (no raw red, no alpha-tint)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/dependencies', () =>
        HttpResponse.json(
          { error: 'invalid_request', message: 'projectmanager: dependency would create a cycle' },
          { status: 400 },
        ),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(within(dagNode('n7')).getByTestId('plan-node-connect'));
    await act(async () => fireEvent.click(within(dagNode('n1')).getByTestId('plan-connect-target')));
    const err = await screen.findByTestId('plan-edge-error');
    expect(err.className).toContain('text-danger');
    expect(err.className).not.toMatch(/text-red-|bg-red-/);
    // in-graph delete control: solid token bg, no bg-{token}/{opacity} alpha-tint,
    // ASCII glyph (no emoji)
    const del = screen.getAllByTestId('plan-edge-delete')[0];
    expect(del.className).not.toMatch(/\/\d+/);
    expect(del.className).not.toMatch(/text-red-|bg-red-/);
    // eslint-disable-next-line no-control-regex
    expect(del.textContent ?? '').not.toMatch(/[\u{1F000}-\u{1FAFF}\u{2600}-\u{27BF}]/u);
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

  // A2 (§9.4 draft-only): a DRAFT plan's task-list rows have a Remove button →
  // useRemoveTaskFromPlan(task_id) (task returns to the Backlog). A running/done
  // plan's rows have NO Remove control; a remove failure surfaces a friendly
  // inline message (#218).
  it('A2 task-list: DRAFT plan rows have a Remove button that DELETEs by task_id', async () => {
    let deletedTaskId: string | undefined;
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1/tasks/:taskId', ({ params }) => {
        deletedTaskId = String(params.taskId);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    const removeBtn = screen.getByTestId('plan-task-remove-n3');
    expect(removeBtn).toHaveAttribute('aria-label', 'Remove frontend list from plan');
    await act(async () => {
      fireEvent.click(removeBtn);
    });
    await waitFor(() => expect(deletedTaskId).toBe('n3'));
  });

  // A6 (§4.2 reachability): a DAG node's TITLE and a task-list row's TITLE are
  // each a new-tab link to TaskDetail (/projects/{pid}/tasks/{tid}, org-prefixed
  // by orgPath — unprefixed here as the test renders outside an OrgGuard), with
  // target=_blank + rel noopener. The title link must NOT swallow the A2 remove
  // control on a draft plan's row.
  it('A6 §4.2: a DAG node title is a new-tab link to TaskDetail (href + target=_blank + rel noopener)', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    const node = screen.getByTestId('plan-dag').querySelector('[data-task-id="n3"]')! as HTMLElement;
    const link = within(node).getByTestId('task-open-link-n3');
    expect(link).toHaveAttribute('href', '/projects/proj-a/tasks/n3');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link).toHaveTextContent('frontend list');
  });

  it('A6 §4.2: a task-list row title is a new-tab link AND coexists with the A2 remove button (draft)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    const row = screen
      .getByTestId('plan-task-list')
      .querySelector('[data-testid="plan-task-row"][data-task-id="n3"]')! as HTMLElement;
    const link = within(row).getByTestId('task-open-link-n3');
    expect(link).toHaveAttribute('href', '/projects/proj-a/tasks/n3');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link.getAttribute('rel')).toContain('noopener');
    expect(link).toHaveTextContent('frontend list');
    // Coexistence: the A2 Remove button is still present in the same row.
    expect(within(row).getByTestId('plan-task-remove-n3')).toBeInTheDocument();
  });

  it('A2 task-list: running plan rows have NO Remove control (§9.4 draft-only)', async () => {
    mockPlan({ status: 'running' });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    expect(screen.getAllByTestId('plan-task-row').length).toBeGreaterThan(0);
    expect(screen.queryByTestId('plan-task-remove-n3')).not.toBeInTheDocument();
  });

  it('A2 task-list: done plan rows have NO Remove control (§9.4 draft-only)', async () => {
    mockPlan({ status: 'done' });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    expect(screen.queryByTestId('plan-task-remove-n3')).not.toBeInTheDocument();
  });

  it('A2 task-list #218: a remove failure shows a friendly inline message (no raw API error)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1/tasks/:taskId', () =>
        HttpResponse.json({ error: 'conflict', message: 'plan not draft' }, { status: 409 }),
      ),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    await act(async () => {
      fireEvent.click(screen.getByTestId('plan-task-remove-n3'));
    });
    const err = await screen.findByTestId('plan-task-remove-error-n3');
    expect(err).toHaveTextContent("Couldn't remove this task from the plan.");
    expect(err).not.toHaveTextContent('plan not draft');
  });

  // ── v2.9 Stage A3 + T238: Plan-edit modal (name / goal / target_date) ───────
  // T238: name + goal are DESCRIPTIVE metadata, editable in any non-archived
  // status (draft/running/done); target_date stays draft-only and the modal
  // hides it off-draft. An archived plan is read-only (no Edit). The modal
  // pre-fills name/goal(description)/target_date and PATCHes via usePatchPlan.
  // PATCH body field names are name/description/target_date (the contract names
  // goal `description`). Cleared target_date → '' (clears); an unchanged field
  // is OMITTED (partial update). #218 friendly errors.
  it('T238: Edit button shows for draft/running/done; hidden only when archived', async () => {
    for (const status of ['draft', 'running', 'done'] as const) {
      mockPlan({ status, has_failed: false });
      wrap();
      await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
      expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument();
      cleanup();
    }
    mockPlan({ status: 'archived', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-edit-btn')).not.toBeInTheDocument();
  });

  it('T238: running plan opens the edit modal with name/goal but NO target-date field', async () => {
    mockPlan({ status: 'running', name: 'live plan', description: 'in progress' });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-edit-btn'));
    expect(screen.getByTestId('plan-edit-modal')).toBeInTheDocument();
    expect((screen.getByTestId('plan-edit-name') as HTMLInputElement).value).toBe('live plan');
    expect((screen.getByTestId('plan-edit-description') as HTMLTextAreaElement).value).toBe('in progress');
    // target_date is draft-only → the field must be absent off-draft.
    expect(screen.queryByTestId('plan-edit-target-date')).not.toBeInTheDocument();
  });

  it('A3: clicking Edit opens the modal pre-filled with name/goal/target_date', async () => {
    mockPlan({
      status: 'draft',
      has_failed: false,
      name: 'v3.0 release plan',
      description: 'ship the orchestrator',
      target_date: '2026-07-15T00:00:00Z',
    });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-edit-modal')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('plan-edit-btn'));
    expect(screen.getByTestId('plan-edit-modal')).toBeInTheDocument();
    expect((screen.getByTestId('plan-edit-name') as HTMLInputElement).value).toBe('v3.0 release plan');
    expect((screen.getByTestId('plan-edit-description') as HTMLTextAreaElement).value).toBe(
      'ship the orchestrator',
    );
    // RFC3339 instant → local YYYY-MM-DD in the picker (date part preserved)
    expect((screen.getByTestId('plan-edit-target-date') as HTMLInputElement).value).toMatch(
      /^2026-07-1[45]$/,
    );
  });

  it('A3: editing + submit PATCHes only the CHANGED fields (name/description), absolute target_date', async () => {
    let body: Record<string, unknown> | null = null;
    mockPlan({
      status: 'draft',
      has_failed: false,
      name: 'old name',
      description: 'old goal',
      target_date: '2026-07-15T00:00:00Z',
    });
    server.use(
      http.patch('/api/projects/proj-a/plans/PL-1', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-edit-btn'));
    fireEvent.change(screen.getByTestId('plan-edit-name'), { target: { value: 'new name' } });
    // leave description + target_date UNCHANGED → they must be omitted
    await act(async () => fireEvent.click(screen.getByTestId('plan-edit-submit')));
    await waitFor(() => expect(body).not.toBeNull());
    expect(body).toEqual({ name: 'new name' });
    // modal closes on success
    await waitFor(() => expect(screen.queryByTestId('plan-edit-modal')).not.toBeInTheDocument());
  });

  it('A3: clearing target_date sends target_date: "" (clear); unchanged name/goal omitted', async () => {
    let body: Record<string, unknown> | null = null;
    mockPlan({
      status: 'draft',
      has_failed: false,
      name: 'keep name',
      description: 'keep goal',
      target_date: '2026-07-15T00:00:00Z',
    });
    server.use(
      http.patch('/api/projects/proj-a/plans/PL-1', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-edit-btn'));
    fireEvent.change(screen.getByTestId('plan-edit-target-date'), { target: { value: '' } });
    await act(async () => fireEvent.click(screen.getByTestId('plan-edit-submit')));
    await waitFor(() => expect(body).not.toBeNull());
    expect(body).toEqual({ target_date: '' });
  });

  it('A3: setting a new target_date sends an absolute RFC3339 instant', async () => {
    let body: Record<string, unknown> | null = null;
    mockPlan({ status: 'draft', has_failed: false, name: 'p', description: '', target_date: null });
    server.use(
      http.patch('/api/projects/proj-a/plans/PL-1', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-edit-btn'));
    fireEvent.change(screen.getByTestId('plan-edit-target-date'), { target: { value: '2026-08-01' } });
    await act(async () => fireEvent.click(screen.getByTestId('plan-edit-submit')));
    await waitFor(() => expect(body).not.toBeNull());
    expect(typeof body!.target_date).toBe('string');
    // an ABSOLUTE RFC3339 instant (local offset) whose LOCAL date is the picked
    // 2026-08-01 — timezone-agnostic (don't assume the runner's tz is UTC).
    const sent = new Date(String(body!.target_date));
    expect(Number.isNaN(sent.getTime())).toBe(false);
    const localDate = `${sent.getFullYear()}-${String(sent.getMonth() + 1).padStart(2, '0')}-${String(sent.getDate()).padStart(2, '0')}`;
    expect(localDate).toBe('2026-08-01');
    expect(Object.keys(body!)).toEqual(['target_date']); // name/description unchanged → omitted
  });

  it('A3: Cancel closes the modal WITHOUT a PATCH', async () => {
    let patched = false;
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.patch('/api/projects/proj-a/plans/PL-1', () => {
        patched = true;
        return HttpResponse.json(planWith({ status: 'draft' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-edit-btn'));
    fireEvent.change(screen.getByTestId('plan-edit-name'), { target: { value: 'changed' } });
    fireEvent.click(screen.getByTestId('plan-edit-cancel'));
    expect(screen.queryByTestId('plan-edit-modal')).not.toBeInTheDocument();
    // give any (erroneous) request a tick to fire
    await act(async () => {
      await Promise.resolve();
    });
    expect(patched).toBe(false);
  });

  it('A3 #218: a PATCH failure surfaces a FRIENDLY inline message (no raw API error)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.patch('/api/projects/proj-a/plans/PL-1', () =>
        HttpResponse.json({ error: 'conflict', message: 'projectmanager: plan is not a draft' }, { status: 409 }),
      ),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-edit-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-edit-btn'));
    fireEvent.change(screen.getByTestId('plan-edit-name'), { target: { value: 'new' } });
    await act(async () => fireEvent.click(screen.getByTestId('plan-edit-submit')));
    const err = await screen.findByTestId('plan-edit-error');
    expect(err).toHaveTextContent('The target date can only be changed while the plan is a draft.');
    expect(err.textContent ?? '').not.toMatch(/projectmanager:|conflict/);
    // danger token, not raw red; scrim ok but surface no alpha-tint
    expect(err.className).toContain('text-danger');
    expect(err.className).not.toMatch(/text-red-|bg-red-/);
    // modal stays open on error
    expect(screen.getByTestId('plan-edit-modal')).toBeInTheDocument();
  });
});

// ── v2.9 Stage A5: synthetic Start/End flow anchors ──────────────────────────
// Even parallel/independent chains read left→right because every real node sits
// on a Start→…→End path. The anchors are layout/flow markers — NOT tasks: no
// node_status / 6-state chip, no assignee, not clickable, not counted, not in
// the task list. Distinct testids; their edges are on a separate testid so the
// real depends_on edge count is unaffected.
describe('PlanDetail — v2.9 A5 synthetic Start/End DAG anchors', () => {
  afterEach(() => cleanup());

  function mockNodes(nodes: unknown[], overrides: Record<string, unknown> = {}) {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/plans/PL-1', () =>
        HttpResponse.json(planWith({ nodes, ...overrides })),
      ),
    );
  }

  const node = (id: string, deps: string[] = [], extra: Record<string, unknown> = {}) => ({
    task_id: id,
    title: id,
    assignee_ref: 'agent:dev',
    task_status: 'open',
    node_status: 'ready',
    depends_on: deps,
    ...extra,
  });

  it('renders distinct Start + End anchors (default fixture)', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    const start = screen.getByTestId('plan-dag-synthetic-start');
    const end = screen.getByTestId('plan-dag-synthetic-end');
    expect(start).toHaveTextContent('Start');
    expect(end).toHaveTextContent('End');
    // distinct from real task nodes: NOT a plan-dag-node, NO node_status chip,
    // NO assignee tag inside the marker.
    expect(start.getAttribute('data-testid')).not.toBe('plan-dag-node');
    expect(within(start).queryByTestId('node-state-chip')).toBeNull();
    expect(within(end).queryByTestId('node-state-chip')).toBeNull();
    // marker content is just the label — no assignee/avatar/status rendered
    expect(start.textContent).toBe('Start');
    expect(end.textContent).toBe('End');
  });

  it('synthetic anchors are NOT counted as task nodes (real node count unchanged)', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // still exactly 7 real task nodes — the 2 anchors are excluded
    expect(screen.getAllByTestId('plan-dag-node')).toHaveLength(7);
    // anchors are not in the task-list tab either (T132: the tab label no longer
    // carries a count — the count is verified directly by the task-row count below).
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    expect(within(screen.getByTestId('plan-task-list-table')).getAllByTestId('plan-task-row')).toHaveLength(7);
    expect(screen.queryByTestId('plan-dag-synthetic-start')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag-synthetic-end')).not.toBeInTheDocument();
  });

  it('real depends_on edges are unchanged; synthetic edges are on a separate testid', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // the 6 real depends_on edges are still exactly 6 (no synthetic leakage)
    expect(screen.getAllByTestId('plan-dag-edge')).toHaveLength(6);
    // synthetic edges exist on their own testid: Start→{n1,n7} roots (2) and
    // {n6,n7}→End leaves (2). (n7 is both a root AND a leaf.)
    const synth = screen.getAllByTestId('plan-dag-synthetic-edge');
    const keys = synth.map((e) => e.getAttribute('data-edge'));
    expect(keys).toContain('start->n1');
    expect(keys).toContain('start->n7');
    expect(keys).toContain('n6->end');
    expect(keys).toContain('n7->end');
  });

  it('multi-parallel: 3 independent tasks → Start→all 3, all 3→End (left→right flow)', async () => {
    mockNodes([node('a'), node('b'), node('c')]);
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    const keys = screen
      .getAllByTestId('plan-dag-synthetic-edge')
      .map((e) => e.getAttribute('data-edge'));
    // Start connects to all 3 roots
    expect(keys).toEqual(expect.arrayContaining(['start->a', 'start->b', 'start->c']));
    // all 3 leaves connect to End
    expect(keys).toEqual(expect.arrayContaining(['a->end', 'b->end', 'c->end']));
    // no real dependency edges in a fully-parallel plan
    expect(screen.queryAllByTestId('plan-dag-edge')).toHaveLength(0);
    // both anchors present, all real nodes level 0
    expect(screen.getByTestId('plan-dag-synthetic-start')).toBeInTheDocument();
    expect(screen.getByTestId('plan-dag-synthetic-end')).toBeInTheDocument();
  });

  it('single chain A→B→C: Start→A only, C→End only', async () => {
    mockNodes([node('a'), node('b', ['a']), node('c', ['b'])]);
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    const keys = screen
      .getAllByTestId('plan-dag-synthetic-edge')
      .map((e) => e.getAttribute('data-edge'));
    expect(keys).toEqual(expect.arrayContaining(['start->a', 'c->end']));
    // only A is a root and only C is a leaf
    expect(keys).not.toContain('start->b');
    expect(keys).not.toContain('start->c');
    expect(keys).not.toContain('a->end');
    expect(keys).not.toContain('b->end');
  });

  it('single node: Start→node and node→End', async () => {
    mockNodes([node('solo')]);
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    expect(screen.getByTestId('plan-dag-synthetic-start')).toBeInTheDocument();
    expect(screen.getByTestId('plan-dag-synthetic-end')).toBeInTheDocument();
    const keys = screen
      .getAllByTestId('plan-dag-synthetic-edge')
      .map((e) => e.getAttribute('data-edge'));
    expect(keys).toEqual(expect.arrayContaining(['start->solo', 'solo->end']));
  });

  it('empty plan: no synthetic anchors, no crash', async () => {
    mockNodes([]);
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    expect(screen.getByTestId('plan-dag-empty')).toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag-synthetic-start')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-dag-synthetic-end')).not.toBeInTheDocument();
    expect(screen.queryAllByTestId('plan-dag-synthetic-edge')).toHaveLength(0);
  });

  it('anchors use solid tokens (no alpha-tint, no raw red, not the 6-state border)', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    for (const id of ['plan-dag-synthetic-start', 'plan-dag-synthetic-end']) {
      const el = screen.getByTestId(id);
      expect(el.className).not.toMatch(/\/\d+/); // no bg-{token}/{opacity}
      expect(el.className).not.toMatch(/text-red-|bg-red-/);
      // readable secondary text, not muted
      expect(el.className).toContain('text-text-secondary');
    }
  });
});

// ── v2.9 Stage B: Plan Delete + Archive (consequence-explaining modals) ───────
// Destructive lifecycle: Delete (DELETE /{id}, unloads tasks→backlog, deletes
// conv+plan, IRREVERSIBLE → navigate away) + Archive (POST /{id}/archive, plan +
// tasks → terminal archived, IRREVERSIBLE). Entry gated to NON-running, NON-
// archived (the real boundary is the backend 409). Each opens a consequence-
// explaining confirm modal; Cancel = no call; 409 = friendly inline, modal stays.
describe('PlanDetail — v2.9 Stage B delete + archive', () => {
  afterEach(() => cleanup());

  // A wrap that surfaces the current location so navigate-away is assertable.
  function wrapLoc(path = '/projects/proj-a/plans/PL-1') {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
    return render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="/projects/:id/plans/:planId" element={<PlanDetail />} />
            <Route
              path="/projects/:id/plans"
              element={<div data-testid="plans-board">Plans board</div>}
            />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
  }

  it('shows Delete + Archive entries for a DRAFT plan', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.getByTestId('plan-delete-btn')).toBeInTheDocument();
    expect(screen.getByTestId('plan-archive-btn')).toBeInTheDocument();
  });

  it('shows Delete + Archive entries for a DONE plan', async () => {
    mockPlan({ status: 'done', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.getByTestId('plan-delete-btn')).toBeInTheDocument();
    expect(screen.getByTestId('plan-archive-btn')).toBeInTheDocument();
  });

  it('HIDES Delete + Archive for a RUNNING plan (entry gate; real block is the 409)', async () => {
    mockPlan({ status: 'running' });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-delete-btn')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-archive-btn')).not.toBeInTheDocument();
  });

  it('an ARCHIVED plan is terminal: NO Delete / Archive entries (read-only)', async () => {
    mockPlan({ status: 'archived', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-delete-btn')).not.toBeInTheDocument();
    expect(screen.queryByTestId('plan-archive-btn')).not.toBeInTheDocument();
    // the archived plan still shows its status chip (read-only).
    expect(within(screen.getByTestId('plan-detail-header')).getByTestId('plan-status-chip')).toHaveTextContent(
      'archived',
    );
  });

  it('clicking Delete opens a CONSEQUENCE-explaining modal (not just "are you sure?")', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-delete-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-delete-btn'));
    const modal = screen.getByTestId('plan-delete-modal');
    expect(modal).toHaveTextContent(/unloads all this plan's tasks back to the Backlog/i);
    expect(modal).toHaveTextContent(/permanently deletes the plan's conversation/i);
    expect(modal).toHaveTextContent(/deletes the plan/i);
    expect(modal).toHaveTextContent(/cannot be undone/i);
  });

  it('Delete confirm DELETEs /{id} and navigates AWAY to the plans board', async () => {
    let method = '';
    let url = '';
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1', ({ request }) => {
        method = request.method;
        url = new URL(request.url).pathname;
        return HttpResponse.json({ deleted: true });
      }),
    );
    wrapLoc();
    await waitFor(() => expect(screen.getByTestId('plan-delete-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-delete-btn'));
    await act(async () => fireEvent.click(screen.getByTestId('plan-delete-confirm')));
    await waitFor(() => expect(screen.getByTestId('plans-board')).toBeInTheDocument());
    expect(method).toBe('DELETE');
    expect(url).toBe('/api/projects/proj-a/plans/PL-1');
  });

  it('Delete Cancel closes the modal WITHOUT a DELETE', async () => {
    let deleted = false;
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1', () => {
        deleted = true;
        return HttpResponse.json({ deleted: true });
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-delete-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-delete-btn'));
    fireEvent.click(screen.getByTestId('plan-delete-cancel'));
    expect(screen.queryByTestId('plan-delete-modal')).not.toBeInTheDocument();
    await act(async () => { await Promise.resolve(); });
    expect(deleted).toBe(false);
  });

  it('Delete #218: a 409 surfaces a FRIENDLY message and the modal STAYS OPEN', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.delete('/api/projects/proj-a/plans/PL-1', () =>
        HttpResponse.json({ error: 'plan_conflict', message: 'projectmanager: plan is running' }, { status: 409 }),
      ),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-delete-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-delete-btn'));
    await act(async () => fireEvent.click(screen.getByTestId('plan-delete-confirm')));
    const err = await screen.findByTestId('plan-delete-error');
    expect(err).toHaveTextContent(/This plan is running\. Stop it first/i);
    expect(err.textContent ?? '').not.toMatch(/projectmanager:|plan_conflict/);
    expect(err.className).toContain('text-danger');
    expect(err.className).not.toMatch(/text-red-|bg-red-/);
    expect(screen.getByTestId('plan-delete-modal')).toBeInTheDocument();
  });

  it('clicking Archive opens a CONSEQUENCE-explaining modal (terminal, cannot be undone)', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-archive-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-archive-btn'));
    const modal = screen.getByTestId('plan-archive-modal');
    expect(modal).toHaveTextContent(/archives the plan and all its tasks/i);
    expect(modal).toHaveTextContent(/terminal state/i);
    expect(modal).toHaveTextContent(/cannot be undone/i);
  });

  it('Archive confirm POSTs /{id}/archive (and stays on the detail view)', async () => {
    let method = '';
    let url = '';
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/archive', ({ request }) => {
        method = request.method;
        url = new URL(request.url).pathname;
        return HttpResponse.json(planWith({ status: 'archived' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-archive-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-archive-btn'));
    await act(async () => fireEvent.click(screen.getByTestId('plan-archive-confirm')));
    await waitFor(() => expect(method).toBe('POST'));
    expect(url).toBe('/api/projects/proj-a/plans/PL-1/archive');
    // modal closes on success; still on the detail page (plan is GET-able).
    await waitFor(() => expect(screen.queryByTestId('plan-archive-modal')).not.toBeInTheDocument());
    expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument();
  });

  it('Archive Cancel closes the modal WITHOUT an archive POST', async () => {
    let archived = false;
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/archive', () => {
        archived = true;
        return HttpResponse.json(planWith({ status: 'archived' }));
      }),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-archive-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-archive-btn'));
    fireEvent.click(screen.getByTestId('plan-archive-cancel'));
    expect(screen.queryByTestId('plan-archive-modal')).not.toBeInTheDocument();
    await act(async () => { await Promise.resolve(); });
    expect(archived).toBe(false);
  });

  it('Archive #218: a 409 already-archived surfaces a FRIENDLY message, modal stays open', async () => {
    mockPlan({ status: 'draft', has_failed: false });
    server.use(
      http.post('/api/projects/proj-a/plans/PL-1/archive', () =>
        HttpResponse.json({ error: 'plan_conflict', message: 'projectmanager: plan already archived' }, { status: 409 }),
      ),
    );
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-archive-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-archive-btn'));
    await act(async () => fireEvent.click(screen.getByTestId('plan-archive-confirm')));
    const err = await screen.findByTestId('plan-archive-error');
    expect(err).toHaveTextContent(/already archived/i);
    expect(err.textContent ?? '').not.toMatch(/projectmanager:|plan_conflict/);
    expect(screen.getByTestId('plan-archive-modal')).toBeInTheDocument();
  });

  it('task-list row shows the Archived badge when task.archived (coexists with status chips)', async () => {
    mockPlan({
      status: 'archived',
      has_failed: false,
      nodes: [
        {
          task_id: 'na',
          title: 'archived task',
          assignee_ref: 'agent:dev',
          task_status: 'completed',
          node_status: 'done',
          depends_on: [],
          archived: true,
        },
        {
          task_id: 'nb',
          title: 'live task',
          assignee_ref: 'agent:dev',
          task_status: 'open',
          node_status: 'ready',
          depends_on: [],
        },
      ],
    });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-tab-tasks')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    // archived task → badge present, AND its node-status chip still shows.
    expect(screen.getByTestId('task-archived-badge-na')).toHaveTextContent('Archived');
    const rowA = screen.getAllByTestId('plan-task-row').find((r) => r.getAttribute('data-task-id') === 'na')!;
    expect(within(rowA).getByTestId('node-state-chip')).toBeInTheDocument();
    // non-archived task → NO badge.
    expect(screen.queryByTestId('task-archived-badge-nb')).not.toBeInTheDocument();
  });

  it('archive badge uses a curated SOLID amber pair (no alpha-tint, no raw red, no emoji)', async () => {
    mockPlan({
      status: 'archived',
      has_failed: false,
      nodes: [
        { task_id: 'na', title: 't', assignee_ref: 'agent:dev', task_status: 'open', node_status: 'ready', depends_on: [], archived: true },
      ],
    });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-tab-tasks')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-tab-tasks'));
    const badge = screen.getByTestId('task-archived-badge-na');
    expect(badge.className).toContain('bg-status-amber-bg');
    expect(badge.className).toContain('text-status-amber-fg');
    expect(badge.className).not.toMatch(/\/\d+/); // no bg-{token}/{opacity}
    expect(badge.className).not.toMatch(/text-red-|bg-red-/);
    // eslint-disable-next-line no-control-regex
    expect(badge.textContent ?? '').not.toMatch(/[\u{1F000}-\u{1FAFF}\u{2600}-\u{27BF}]/u);
  });

  it('PlanStatusChip renders the archived status with a curated SOLID stone pair', async () => {
    mockPlan({ status: 'archived', has_failed: false });
    wrap();
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    const chip = within(screen.getByTestId('plan-detail-header')).getByTestId('plan-status-chip');
    expect(chip).toHaveTextContent('archived');
    expect(chip).toHaveAttribute('data-status', 'archived');
    expect(chip.className).toContain('bg-status-stone-bg');
    expect(chip.className).toContain('text-status-stone-fg');
    expect(chip.className).not.toMatch(/\/\d+/);
    expect(chip.className).not.toMatch(/text-red-|bg-red-/);
  });

  // ── T41 (v2.9.1 #291): big-plan Task-list = searchable + scrollable + inline
  // 分派. A big plan must render EVERY node (no cap), be filterable by title /
  // Task-id / assignee, and let you reassign per row regardless of plan status.
  // Build a ~12-node plan so "no silent truncation" is meaningful.
  function bigNodes() {
    const nodes = [];
    for (let i = 1; i <= 12; i++) {
      nodes.push({
        task_id: `b${i}`,
        title: `task number ${i}`,
        assignee_ref: i % 2 === 0 ? 'agent:dev' : 'agent:dev2',
        task_status: 'open',
        node_status: 'ready',
        depends_on: [],
      });
    }
    return nodes;
  }

  it('T41: Task tab renders ALL nodes of a big plan (no cap)', async () => {
    mockPlan({ nodes: bigNodes() });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    const table = await screen.findByTestId('plan-task-list-table');
    expect(within(table).getAllByTestId('plan-task-row')).toHaveLength(12);
    expect(screen.getByTestId('plan-task-search-count')).toHaveTextContent('Showing 12 of 12');
  });

  it('T41: search filters by title; clearing restores all; no-match shows empty state', async () => {
    mockPlan({ nodes: bigNodes() });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    await screen.findByTestId('plan-task-list-table');
    const search = screen.getByTestId('plan-task-search');
    expect(search).toHaveAttribute('aria-label', 'Filter tasks');

    // "number 3" matches exactly one node (title).
    fireEvent.change(search, { target: { value: 'number 3' } });
    expect(within(screen.getByTestId('plan-task-list-table')).getAllByTestId('plan-task-row')).toHaveLength(1);
    expect(screen.getByTestId('plan-task-search-count')).toHaveTextContent('Showing 1 of 12');

    // Clearing restores ALL rows.
    fireEvent.change(search, { target: { value: '' } });
    expect(within(screen.getByTestId('plan-task-list-table')).getAllByTestId('plan-task-row')).toHaveLength(12);

    // A non-matching query shows the empty state (and hides the table).
    fireEvent.change(search, { target: { value: 'zzz-no-match' } });
    expect(screen.getByTestId('plan-task-search-empty')).toBeInTheDocument();
    expect(screen.queryByTestId('plan-task-list-table')).not.toBeInTheDocument();
    expect(screen.getByTestId('plan-task-search-count')).toHaveTextContent('Showing 0 of 12');
  });

  it('T41: search filters by Task-id (org_ref) and by assignee handle', async () => {
    // T126: org_ref rides on the plan node (b1 → T900); used directly, no FE
    // task-list re-resolver.
    const nodes = bigNodes().map((n) => (n.task_id === 'b1' ? { ...n, org_ref: 'T900' } : n));
    mockPlan({ nodes });
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    await screen.findByTestId('plan-task-list-table');
    const search = screen.getByTestId('plan-task-search');

    // by Task-id (org_ref) → only b1.
    await waitFor(() => {
      const row = screen.getByTestId('plan-task-list').querySelector('[data-task-id="b1"]') as HTMLElement;
      expect(within(row).getByTestId('plan-row-taskid')).toHaveTextContent('T900');
    });
    fireEvent.change(search, { target: { value: 't900' } });
    const idRows = within(screen.getByTestId('plan-task-list-table')).getAllByTestId('plan-task-row');
    expect(idRows).toHaveLength(1);
    expect(idRows[0]).toHaveAttribute('data-task-id', 'b1');

    // by assignee handle: "dev2" is on the odd-indexed nodes (6 of 12).
    fireEvent.change(search, { target: { value: 'dev2' } });
    expect(within(screen.getByTestId('plan-task-list-table')).getAllByTestId('plan-task-row')).toHaveLength(6);
  });

  it('T41/T147: the single per-row assignee dropdown fires the assign mutation with {assignee}', async () => {
    mockPlan({ nodes: bigNodes() });
    let assignedBody: { assignee?: string } | null = null;
    server.use(
      // members feed the dropdown options (mirror TaskEditModal: prefixed refs).
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'mem-1', organization_id: 'org-test', identity_id: 'agent:dev', kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Dev One' },
          { id: 'mem-2', organization_id: 'org-test', identity_id: 'agent:dev2', kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Dev Two' },
        ]),
      ),
      http.post('/api/projects/proj-a/tasks/b1/assign', async ({ request }) => {
        assignedBody = (await request.json()) as { assignee?: string };
        return HttpResponse.json({ id: 'b1', project_id: 'proj-a', title: 'task number 1', description: '', status: 'open', assignee: assignedBody.assignee, version: 2, created_at: '2026-06-01T01:00:00Z', updated_at: '2026-06-01T01:00:00Z' });
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    await screen.findByTestId('plan-task-list-table');

    // T147: ONE control — the dropdown TRIGGER shows the current assignee (b1 →
    // agent:dev2 → "Dev Two"); there is NO separate read-only assignee element.
    const row = screen.getByTestId('plan-task-list').querySelector('[data-task-id="b1"]') as HTMLElement;
    const trigger = within(row).getByTestId('plan-row-assign-trigger');
    await waitFor(() => expect(trigger).toHaveTextContent('Dev Two'));
    expect(trigger).toHaveAttribute('aria-label', 'Reassign task number 1');

    // open the dropdown + pick "Dev One" (agent:dev).
    await act(async () => fireEvent.click(trigger));
    // T194: the dropdown popover is portaled to <body> (so it isn't clipped by the
    // task-list scroll container), so query the options from the screen, not the row.
    const opt = screen
      .getAllByTestId('plan-row-assign-option')
      .find((o) => o.getAttribute('data-value') === 'agent:dev') as HTMLElement;
    expect(opt).toHaveTextContent('Dev One');
    await act(async () => fireEvent.click(opt));
    await waitFor(() => expect(assignedBody).toEqual({ assignee: 'agent:dev' }));
  });

  it('T41/T147: choosing Unassigned routes to the unassign endpoint', async () => {
    mockPlan({ nodes: bigNodes() });
    let unassigned = false;
    server.use(
      http.post('/api/projects/proj-a/tasks/b2/unassign', () => {
        unassigned = true;
        return HttpResponse.json({ id: 'b2', project_id: 'proj-a', title: 'task number 2', description: '', status: 'open', assignee: '', version: 2, created_at: '2026-06-01T01:00:00Z', updated_at: '2026-06-01T01:00:00Z' });
      }),
    );
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-tasks'));
    await screen.findByTestId('plan-task-list-table');
    const row = screen.getByTestId('plan-task-list').querySelector('[data-task-id="b2"]') as HTMLElement;
    await act(async () => fireEvent.click(within(row).getByTestId('plan-row-assign-trigger')));
    // T194: options are portaled to <body> now — query from the screen.
    const unassignOpt = screen
      .getAllByTestId('plan-row-assign-option')
      .find((o) => o.getAttribute('data-value') === '') as HTMLElement;
    await act(async () => fireEvent.click(unassignOpt));
    await waitFor(() => expect(unassigned).toBe(true));
  });
});

// v2.10.1 [M4] On mobile the SVG DAG reflows to a vertical stepper (the SVG
// graph + its controls are md:-only). jsdom has no CSS media queries, so BOTH
// render in the DOM here; these specs assert the stepper's structure/order.
describe('PlanDetail — v2.10.1 [M4] mobile DAG → vertical stepper', () => {
  afterEach(() => cleanup());

  it('renders a vertical stepper with one node per task (topological order) alongside the desktop graph', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    await waitFor(() => expect(screen.getByTestId('plan-dag')).toBeInTheDocument());
    // Stepper exists with one node per task (same 7 as the graph).
    const stepper = screen.getByTestId('plan-stepper');
    const nodes = within(stepper).getAllByTestId('plan-stepper-node');
    expect(nodes).toHaveLength(7);
    // First stepper node is a DAG root (level 0).
    expect(nodes[0]).toHaveAttribute('data-level', '0');
    // Levels are non-decreasing down the timeline (topological order).
    const levels = nodes.map((n) => Number(n.getAttribute('data-level')));
    expect(levels).toEqual([...levels].sort((a, b) => a - b));
    // The desktop SVG graph still renders (md:-only via CSS, present in jsdom).
    expect(screen.getByTestId('plan-dag-canvas')).toBeInTheDocument();
  });

  it('each stepper node shows a status dot + state chip + a tappable title link to the task', async () => {
    mockPlan();
    wrap();
    fireEvent.click(await screen.findByTestId('plan-tab-dag'));
    const stepper = await screen.findByTestId('plan-stepper');
    const first = within(stepper).getAllByTestId('plan-stepper-node')[0];
    expect(within(first).getByTestId('plan-stepper-dot')).toBeInTheDocument();
    expect(within(first).getByTestId('node-state-chip')).toBeInTheDocument();
    // The title is the task-open link (≥44px touch target).
    const taskId = first.getAttribute('data-task-id')!;
    const link = within(first).getByTestId(`task-open-link-${taskId}`);
    expect(link).toHaveAttribute('href', expect.stringContaining(`/tasks/${taskId}`));
    expect(link.className).toContain('min-h-[44px]');
  });

  // v2.13.0 / I18 F4 — the unmerged-branch ship-gate board.
  it('shows the unmerged-branch board listing un-done Integrate nodes', async () => {
    mockPlan();
    server.use(
      http.get('/api/projects/proj-a/plans/PL-1/unmerged-branches', () =>
        HttpResponse.json({
          plan_id: 'PL-1',
          project_id: 'proj-a',
          plan_name: 'v3.0 release plan',
          plan_status: 'running',
          all_merged: false,
          unmerged_count: 2,
          unmerged: [
            { task_id: 'i1', title: 'F1 integrate', assignee_ref: 'agent:int', node_status: 'running', branch: 'f1-spec', base: 'dev/v2.13.0', skip_merge_check: false, org_ref: 'T261' },
            { task_id: 'i2', title: 'F4 integrate', assignee_ref: 'agent:int', node_status: 'blocked', branch: 'f4-unmerged-board', base: 'dev/v2.13.0', skip_merge_check: true, org_ref: 'T270' },
          ],
        }),
      ),
    );
    wrap();
    const board = await screen.findByTestId('plan-unmerged-board');
    expect(board).toHaveAttribute('data-unmerged-count', '2');
    expect(within(board).getByTestId('plan-unmerged-count')).toHaveTextContent('2');
    const rows = within(board).getAllByTestId('plan-unmerged-row');
    expect(rows).toHaveLength(2);
    // branch → base render as separate spans now (2-line row); assert each.
    expect(rows[0]).toHaveTextContent('f1-spec');
    expect(rows[0]).toHaveTextContent('dev/v2.13.0');
    expect(within(rows[0]).getByTestId('plan-unmerged-ref')).toHaveTextContent('T261');
    // The skip-merge-check feature shows the skip-check marker.
    expect(within(rows[1]).getByTestId('plan-unmerged-skipcheck')).toBeInTheDocument();
  });

  it('collapses and expands the unmerged-branch board via its header (T315)', async () => {
    mockPlan();
    server.use(
      http.get('/api/projects/proj-a/plans/PL-1/unmerged-branches', () =>
        HttpResponse.json({
          project_id: 'proj-a',
          plan_id: 'PL-1',
          unmerged_count: 1,
          unmerged: [
            { task_id: 'i1', title: 'F1 spec', assignee_ref: 'agent:dev', node_status: 'blocked', branch: 'f1-spec', base: 'dev/v2.13.0', skip_merge_check: false, org_ref: 'T261' },
          ],
        }),
      ),
    );
    wrap();
    const toggle = await screen.findByTestId('plan-unmerged-toggle');
    // open by default → list + rows visible.
    expect(screen.getByTestId('plan-unmerged-list')).toBeInTheDocument();
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    // collapse → list removed, count chip still shown in the header.
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByTestId('plan-unmerged-list')).not.toBeInTheDocument();
    expect(within(screen.getByTestId('plan-unmerged-board')).getByTestId('plan-unmerged-count')).toHaveTextContent('1');
    // expand again → list back.
    fireEvent.click(toggle);
    expect(screen.getByTestId('plan-unmerged-list')).toBeInTheDocument();
  });

  it('hides the unmerged-branch board when everything is merged (empty board)', async () => {
    mockPlan();
    server.use(
      http.get('/api/projects/proj-a/plans/PL-1/unmerged-branches', () =>
        HttpResponse.json({
          plan_id: 'PL-1',
          project_id: 'proj-a',
          plan_name: 'v3.0 release plan',
          plan_status: 'running',
          all_merged: true,
          unmerged_count: 0,
          unmerged: [],
        }),
      ),
    );
    wrap();
    // The header renders; the board does not (nothing to reconcile).
    await waitFor(() => expect(screen.getByTestId('plan-detail-header')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-unmerged-board')).not.toBeInTheDocument();
  });
});

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import i18n from '@/i18n';
import InsightCollaboration from './InsightCollaboration';

const relations = ['assign', 'block', 'unblock', 'complete', 'dependency_release', 'review_reject'] as const;
const polarities = ['neutral', 'negative', 'positive', 'positive', 'positive', 'mixed'] as const;
const effects = relations.map((relation, i) => ({
  id: `ce-${i}`, effect_id: `ce-${i}`, source: `agent:a${i}`, target: 'task:T1', relation_type: relation,
  polarity: polarities[i], magnitude: (i % 3 + 1) as 1 | 2 | 3, project_id: 'P1', target_task_id: 'T1',
  source_agent_ref: `agent:a${i}`, target_agent_ref: '', confidence: 'high', occurred_at: `2026-09-03T10:0${i}:00Z`,
  rule_version: 'collaboration-effect.mvp.v1', evidence_event_ids: [`evt-${i}`], before_state: { status: 'running' },
  after_state: { status: 'completed' }, explanation_key: `collaboration.effect.${relation}`,
}));
const graph = {
  graph: { nodes: [...effects.map((e, i) => ({ id: e.source, kind: 'agent', label: `Agent ${i}` })), { id: 'task:T1', kind: 'task', label: 'Task One', task_id: 'T1' }], edges: effects },
  effects, summary: { positive_count: 3, negative_count: 1, neutral_count: 1, mixed_count: 1, affected_task_count: 1 }, next_cursor: 'next',
};

function renderAt(path: string) {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[path]}><Routes><Route path="/organizations/:slug/insights/collaboration" element={<InsightCollaboration />} /></Routes></MemoryRouter></QueryClientProvider>);
}

afterEach(async () => { cleanup(); await i18n.changeLanguage('en'); });

beforeEach(() => {
  server.use(
    http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json({ ...graph, next_cursor: '', graph_version: 'gv-default' })),
    http.get('/api/orgs/:slug/projects', () => HttpResponse.json({ projects: [
      { id: 'P1', organization_id: 'org-1', name: 'Alpha Project', description: '', status: 'active', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z' },
      { id: 'P2', organization_id: 'org-1', name: 'Beta Project', description: '', status: 'active', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z' },
    ] })),
    http.get('/api/orgs/:slug/projects/:projectId/tasks', () => HttpResponse.json({ tasks: [
      { id: 'T1', project_id: 'P1', title: 'Task One', description: '', status: 'completed', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z', org_ref: 'T1001' },
      { id: 'T2', project_id: 'P1', title: 'Deploy analytics', description: '', status: 'open', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z', org_ref: 'T1002' },
    ], total: 2 })),
    http.get('/api/orgs/:slug/projects/:projectId/plans', () => HttpResponse.json({ plans: [{
      id: 'PL1', project_id: 'P1', name: 'Delivery Plan', description: '', status: 'running', creator_ref: 'user:u1', conversation_id: 'c1', has_failed: false,
      progress: { done: 1, total: 2 }, created_at: '2026-09-03T00:00:00Z', nodes_preview: [
        { task_id: 'T1', title: 'Task One', assignee_ref: 'agent:a0', task_status: 'completed', node_status: 'done', depends_on: [] },
        { task_id: 'T2', title: 'Deploy analytics', assignee_ref: 'agent:a0', task_status: 'open', node_status: 'ready', depends_on: ['T1'] },
      ], node_count: 2,
    }] })),
    http.get('/api/orgs/:slug/members', () => HttpResponse.json([
      { id: 'm-agent-1', organization_id: 'org-1', identity_id: 'agent:a0', kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-09-03T00:00:00Z', display_name: 'Runner A' },
      { id: 'm-user-1', organization_id: 'org-1', identity_id: 'user:u1', kind: 'user', role: 'member', status: 'joined', joined_at: '2026-09-03T00:00:00Z', display_name: 'Human One' },
    ])),
    http.get('/api/orgs/:slug/projects/:projectId/members', () => HttpResponse.json({ members: [
      { id: 'pm-agent-1', project_id: 'P1', identity_id: 'agent:a0', role: 'member', added_by: 'user:u1', created_at: '2026-09-03T00:00:00Z' },
      { id: 'pm-user-1', project_id: 'P1', identity_id: 'user:u1', role: 'member', added_by: 'user:u1', created_at: '2026-09-03T00:00:00Z' },
    ] })),
  );
});

describe('Collaboration Insight', () => {
  it('renders contract relations, accessible edges, summary, timeline and lazy evidence', async () => {
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => { const p = new URL(request.url).searchParams; expect(p.get('project_id')).toBe('P1'); expect(p.get('task_id')).toBe('T1'); expect(p.get('limit')).toBe('100'); return HttpResponse.json(graph); }),
      http.get('/api/orgs/:slug/insights/collaboration-effects/:id/evidence', ({ params, request }) => { expect(new URL(request.url).searchParams.get('project_id')).toBe('P1'); return HttpResponse.json({ effect_id: params.id, evidence: [{ event_id: 'evt-0', event_type: 'pm.task.assigned', occurred_at: '2026-09-03T10:00:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { assignee: 'agent:a0' } }] }); }),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    const edgeList = await screen.findByLabelText('Keyboard-accessible graph edges');
    expect(within(edgeList).getAllByRole('button')).toHaveLength(6);
    expect(screen.getByTestId('collaboration-graph')).toHaveTextContent('Review rejected · Mixed');
    expect(screen.getByLabelText('Effect summary')).toHaveTextContent('Affected tasks1');
    expect(screen.getByTestId('collaboration-load-more')).toBeVisible();
    await user.click(within(edgeList).getByRole('button', { name: /Assign/ }));
    const drawer = await screen.findByTestId('collaboration-evidence-drawer');
    expect(await within(drawer).findByText('pm.task.assigned')).toBeVisible();
    expect(drawer).toHaveTextContent('agent:a0');
  });

  it('persists filters in the URL and reloads the query', async () => {
    let requested = '';
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => { requested = request.url; return HttpResponse.json({ ...graph, next_cursor: '' }); }));
    const user = userEvent.setup();
    const view = renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    await screen.findByTestId('collaboration-graph');
    await user.selectOptions(screen.getByLabelText('Polarity'), 'mixed');
    await user.type(screen.getByLabelText('Since'), '2026-09-03T12:30');
    await waitFor(() => expect(new URL(requested).searchParams.get('polarity')).toBe('mixed'));
    expect(new URL(requested).searchParams.get('since')).toMatch(/^2026-09-03T/);
    expect(new URL(requested).searchParams.get('since')).toMatch(/Z$/);
    view.unmount();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1&polarity=mixed');
    expect(screen.getByLabelText('Polarity')).toHaveValue('mixed');
  });

  it('selects project, task and agent from searchable dropdown filters', async () => {
    let requested = '';
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => { requested = request.url; return HttpResponse.json({ ...graph, next_cursor: '' }); }));
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration');
    await user.click(await screen.findByTestId('collaboration-project_id-trigger'));
    await user.type(screen.getByTestId('collaboration-project_id-search'), 'alpha');
    await user.click(screen.getByRole('option', { name: /Alpha Project/ }));
    await user.click(await screen.findByTestId('collaboration-task_id-trigger'));
    await user.type(screen.getByTestId('collaboration-task_id-search'), 'deploy');
    await user.click(screen.getByRole('option', { name: /Deploy analytics/ }));
    await user.click(await screen.findByTestId('collaboration-agent_ref-trigger'));
    await user.type(screen.getByTestId('collaboration-agent_ref-search'), 'runner');
    await user.click(screen.getByRole('option', { name: /Runner A/ }));
    await waitFor(() => expect(new URL(requested).searchParams.get('task_id')).toBe('T2'));
    const search = new URL(requested).searchParams;
    expect(search.get('project_id')).toBe('P1');
    expect(search.get('task_id')).toBe('T2');
    expect(search.get('agent_ref')).toBe('agent:a0');
  });

  it('supports graph viewport controls, truncated labels and selected-neighborhood dimming', async () => {
    const longLabel = 'Agent With A Very Long Display Name';
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json({
        graph: {
          nodes: [
            { id: 'agent:long', kind: 'agent', label: longLabel },
            { id: 'task:T1', kind: 'task', label: 'Task One', task_id: 'T1' },
            { id: 'agent:other', kind: 'agent', label: 'Other Agent' },
            { id: 'task:T2', kind: 'task', label: 'Other Task', task_id: 'T2' },
          ],
          edges: [
            { ...effects[0], id: 'selected-edge', effect_id: 'selected-effect', effect_ids: ['selected-effect'], source: 'agent:long', target: 'task:T1', interaction_count: 1, evidence_count: 1 },
            { ...effects[1], id: 'dimmed-edge', effect_id: 'dimmed-effect', effect_ids: ['dimmed-effect'], source: 'agent:other', target: 'task:T2', interaction_count: 1, evidence_count: 1 },
          ],
        },
        effects: [
          { ...effects[0], effect_id: 'selected-effect', id: 'selected-effect', source: 'agent:long', source_agent_ref: 'agent:long' },
          { ...effects[1], effect_id: 'dimmed-effect', id: 'dimmed-effect', source: 'agent:other', source_agent_ref: 'agent:other', target_task_id: 'T2' },
        ],
        summary: {},
        next_cursor: '',
        graph_version: 'gv-readable',
      })),
      http.get('/api/orgs/:slug/insights/collaboration-effects/:id/evidence', ({ params }) => HttpResponse.json({ effect_id: params.id, evidence: [] })),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration');
    expect(await screen.findByText('Filter / locate')).toBeVisible();
    const svg = await screen.findByTestId('collaboration-graph-svg');
    Object.defineProperty(svg, 'getBoundingClientRect', { configurable: true, value: () => ({ x: 0, y: 0, left: 0, top: 0, right: 720, bottom: 360, width: 720, height: 360, toJSON: () => ({}) }) });
    const originalViewBox = svg.getAttribute('viewBox');
    fireEvent.wheel(svg, { deltaY: -100, clientX: 360, clientY: 180 });
    expect(svg.getAttribute('viewBox')).not.toBe(originalViewBox);
    await user.click(screen.getByRole('button', { name: 'Fit' }));
    expect(svg.getAttribute('viewBox')).toBe(originalViewBox);
    fireEvent.pointerDown(svg, { pointerId: 1, clientX: 360, clientY: 180 });
    fireEvent.pointerMove(svg, { pointerId: 1, clientX: 400, clientY: 200 });
    fireEvent.pointerUp(svg, { pointerId: 1, clientX: 400, clientY: 200 });
    expect(svg.getAttribute('viewBox')).not.toBe(originalViewBox);
    await user.click(screen.getByRole('button', { name: 'Fit' }));
    expect(svg.getAttribute('viewBox')).toBe(originalViewBox);
    const longNode = screen.getByRole('button', { name: longLabel });
    const originalX = longNode.querySelector('circle')?.getAttribute('cx');
    fireEvent.pointerDown(longNode, { pointerId: 2, clientX: 65, clientY: 70 });
    fireEvent.pointerMove(svg, { pointerId: 2, clientX: 125, clientY: 110 });
    fireEvent.pointerUp(svg, { pointerId: 2, clientX: 125, clientY: 110 });
    expect(longNode.querySelector('circle')?.getAttribute('cx')).not.toBe(originalX);
    await user.click(screen.getByRole('button', { name: 'Zoom in' }));
    expect(svg.getAttribute('viewBox')).not.toBe(originalViewBox);
    await user.click(screen.getByRole('button', { name: 'Reset' }));
    expect(svg.getAttribute('viewBox')).toBe(originalViewBox);
    expect(screen.getByText('Agent With A Very...')).toBeVisible();
    expect(screen.getByText(longLabel)).toBeInTheDocument();
    screen.getByRole('button', { name: longLabel }).focus();
    await user.keyboard('{Enter}');
    expect(svg.getAttribute('viewBox')).not.toBe(originalViewBox);
    await user.click(screen.getByRole('button', { name: 'Reset' }));
    expect(svg.getAttribute('viewBox')).toBe(originalViewBox);
    await user.click(within(screen.getByLabelText('Keyboard-accessible graph edges')).getByRole('button', { name: /Assign/ }));
    await waitFor(() => expect(screen.getByTestId('collaboration-evidence-drawer')).toBeVisible());
    expect(document.querySelector('g[opacity="0.16"]')).toBeTruthy();
    expect(document.querySelector('g[opacity="0.18"]')).toBeTruthy();
  });

  it('clears all URL filters and restores the organization graph', async () => {
    let requested = '';
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => { requested = request.url; return HttpResponse.json({ ...graph, next_cursor: '' }); }));
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1&polarity=mixed&relation_type=assign');
    await screen.findByTestId('collaboration-graph');
    await user.click(screen.getByRole('button', { name: 'Clear filters' }));
    await waitFor(() => {
      const search = new URL(requested).searchParams;
      expect([...search.keys()]).toEqual(['limit']);
    });
    expect(screen.getByLabelText('Polarity')).toHaveValue('');
    expect(screen.getByLabelText('Relationship')).toHaveValue('');
  });

  it('loads the organization graph without a project filter and shows empty, permission and server states', async () => {
    let organizationRequest = '';
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => { organizationRequest = request.url; return HttpResponse.json({ ...graph, next_cursor: '', graph_version: 'gv-org' }); }));
    const first = renderAt('/organizations/acme/insights/collaboration');
    expect(await screen.findByTestId('collaboration-graph')).toBeVisible();
    expect(new URL(organizationRequest).searchParams.has('project_id')).toBe(false);
    expect(screen.queryByTestId('collaboration-scope-required')).not.toBeInTheDocument();
    first.unmount();
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json({ graph: { nodes: [], edges: [] }, effects: [], summary: { positive_count: 0, negative_count: 0, neutral_count: 0, mixed_count: 0, affected_task_count: 0 }, next_cursor: '' })));
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    expect(await screen.findByTestId('collaboration-empty')).toBeVisible();
    cleanup();
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json({ error: 'forbidden' }, { status: 403 })));
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    expect(await screen.findByTestId('collaboration-forbidden')).toBeVisible();
    cleanup();
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json({ error: 'boom' }, { status: 500 })));
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    expect(await screen.findByTestId('collaboration-error')).toBeVisible();
  });

  it('normalizes nullable collaboration response collections without crashing', async () => {
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json({
      graph: { nodes: null, edges: null },
      effects: null,
      summary: null,
      next_cursor: null,
    })));
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    expect(await screen.findByTestId('collaboration-empty')).toBeVisible();
    expect(screen.getByLabelText('Effect summary')).toHaveTextContent('Affected tasks0');
  });

  it('accumulates cursor pages and renders real Plan and Stage ownership', async () => {
    const first = { ...effects[0], effect_id: 'page-1', id: 'page-1' };
    const second = { ...effects[1], effect_id: 'page-2', id: 'page-2' };
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => {
        const search = new URL(request.url).searchParams;
        const cursor = search.get('cursor');
        expect(search.get('plan_id')).toBe('PL1');
        const pageEffects = cursor ? [second] : [first];
        const ownershipNodes = [
          { id: 'plan:PL1', kind: 'plan', label: 'Delivery Plan', plan_id: 'PL1' },
          { id: 'stage:S1', kind: 'stage', label: 'Build', plan_id: 'PL1', stage_id: 'S1' },
          { id: 'task:T1', kind: 'task', label: 'Task One', plan_id: 'PL1', stage_id: 'S1', task_id: 'T1' },
          { id: first.source, kind: 'agent', label: 'Agent A' },
          { id: second.source, kind: 'agent', label: 'Agent B' },
          { id: 'agent:peer', kind: 'agent', label: 'Review Peer' },
        ];
        const structural = [
          { id: 'plan-stage', source: 'plan:PL1', target: 'stage:S1', relation_type: 'plan_stage', polarity: 'neutral', magnitude: 1 },
          { id: 'stage-task', source: 'stage:S1', target: 'task:T1', relation_type: 'stage_task', polarity: 'neutral', magnitude: 1 },
        ];
        const graphEffects = cursor
          ? [{ ...second, target: 'agent:peer', interaction_count: 4, evidence_count: 7, first_occurred_at: '2026-09-03T09:00:00Z', last_occurred_at: '2026-09-03T10:05:00Z' }]
          : [{ ...first, interaction_count: 1, evidence_count: 1 }];
        return HttpResponse.json({ graph: { nodes: ownershipNodes, edges: [...structural, ...graphEffects] }, effects: pageEffects, summary: {}, graph_version: cursor ? 'gv-page-2' : 'gv-page-1', next_cursor: cursor ? '' : 'page-2' });
      }),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&plan_id=PL1');
    expect(await screen.findByTestId('collaboration-graph')).toHaveTextContent('Delivery Plan');
    expect(screen.getByTestId('collaboration-graph')).toHaveTextContent('Build');
    expect(within(screen.getByLabelText('Keyboard-accessible graph edges')).getAllByRole('button')).toHaveLength(1);
    await user.click(screen.getByTestId('collaboration-load-more'));
    await waitFor(() => expect(within(screen.getByLabelText('Keyboard-accessible graph edges')).getAllByRole('button')).toHaveLength(2));
    expect(screen.getByTestId('collaboration-graph')).toHaveTextContent('Review Peer');
    expect(screen.getByLabelText('Keyboard-accessible graph edges')).toHaveTextContent('4 effects');
    expect(screen.getByLabelText('Keyboard-accessible graph edges')).toHaveTextContent('evidence 7');
    expect(screen.queryByTestId('collaboration-load-more')).not.toBeInTheDocument();
  });

  it('merges the same semantic edge across cursor pages without merging different polarity', async () => {
    const sameA = { ...effects[0], effect_id: 'same-a', id: 'same-a', source: 'agent:a0', source_agent_ref: 'agent:a0', target: 'task:T1', target_task_id: 'T1', relation_type: 'complete', polarity: 'positive', magnitude: 1, occurred_at: '2026-09-03T10:00:00Z', evidence_event_ids: ['evt-shared'] };
    const sameB = { ...sameA, effect_id: 'same-b', id: 'same-b', magnitude: 3, occurred_at: '2026-09-03T09:00:00Z', evidence_event_ids: ['evt-shared'] };
    const sameC = { ...sameA, effect_id: 'same-c', id: 'same-c', magnitude: 2, occurred_at: '2026-09-03T11:00:00Z', evidence_event_ids: ['evt-new'] };
    const negative = { ...sameA, effect_id: 'negative-a', id: 'negative-a', polarity: 'negative', magnitude: 2, evidence_event_ids: ['evt-neg'] };
    const nodes = [
      { id: 'agent:a0', kind: 'agent', label: 'Agent A' },
      { id: 'task:T1', kind: 'task', label: 'Task One', task_id: 'T1' },
    ];
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        if (!cursor) {
          return HttpResponse.json({
            graph: { nodes, edges: [{ ...sameA, id: 'edge-page-1', interaction_count: 1, evidence_count: 1, first_occurred_at: sameA.occurred_at, last_occurred_at: sameA.occurred_at }] },
            effects: [sameA], summary: {}, graph_version: 'gv-page-1', next_cursor: 'page-2',
          });
        }
        return HttpResponse.json({
          graph: { nodes, edges: [
            { ...sameB, id: 'edge-page-2', interaction_count: 2, evidence_count: 2, first_occurred_at: sameB.occurred_at, last_occurred_at: sameC.occurred_at },
            { ...negative, id: 'edge-negative', interaction_count: 1, evidence_count: 1, first_occurred_at: negative.occurred_at, last_occurred_at: negative.occurred_at },
          ] },
          effects: [sameB, sameC, negative], summary: {}, graph_version: 'gv-page-2', next_cursor: '',
        });
      }),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    expect(await screen.findByLabelText('Keyboard-accessible graph edges')).toHaveTextContent('1 effect');
    await user.click(screen.getByTestId('collaboration-load-more'));
    const edgeList = screen.getByLabelText('Keyboard-accessible graph edges');
    await waitFor(() => expect(within(edgeList).getAllByRole('button')).toHaveLength(2));
    expect(edgeList).toHaveTextContent('3 effects');
    expect(edgeList).toHaveTextContent('evidence 2');
    expect(edgeList).toHaveTextContent('strength 3');
    expect(edgeList).toHaveTextContent('Negative');
  });

  it('loads evidence for every effect contributing to a merged semantic edge across cursor pages', async () => {
    const first = { ...effects[0], effect_id: 'merge-page-1', id: 'merge-page-1', source: 'agent:a0', source_agent_ref: 'agent:a0', target: 'task:T1', target_task_id: 'T1', relation_type: 'complete', polarity: 'positive', magnitude: 1, occurred_at: '2026-09-03T10:00:00Z', evidence_event_ids: ['evt-shared', 'evt-a'] };
    const second = { ...first, effect_id: 'merge-page-2', id: 'merge-page-2', magnitude: 3, occurred_at: '2026-09-03T10:05:00Z', evidence_event_ids: ['evt-shared', 'evt-b'] };
    const nodes = [
      { id: 'agent:a0', kind: 'agent', label: 'Agent A' },
      { id: 'task:T1', kind: 'task', label: 'Task One', task_id: 'T1' },
    ];
    const evidenceRequests: string[] = [];
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        if (!cursor) {
          return HttpResponse.json({
            graph: { nodes, edges: [{ ...first, id: 'edge-page-1', interaction_count: 1, evidence_count: 2, first_occurred_at: first.occurred_at, last_occurred_at: first.occurred_at }] },
            effects: [first], summary: {}, graph_version: 'gv-page-1', next_cursor: 'page-2',
          });
        }
        return HttpResponse.json({
          graph: { nodes, edges: [{ ...second, id: 'edge-page-2', interaction_count: 1, evidence_count: 2, first_occurred_at: second.occurred_at, last_occurred_at: second.occurred_at }] },
          effects: [second], summary: {}, graph_version: 'gv-page-2', next_cursor: '',
        });
      }),
      http.get('/api/orgs/:slug/insights/collaboration-effects/:id/evidence', ({ params, request }) => {
        evidenceRequests.push(String(params.id));
        expect(new URL(request.url).searchParams.get('project_id')).toBe('P1');
        const unique = params.id === 'merge-page-1'
          ? { event_id: 'evt-a', event_type: 'unique.a', occurred_at: '2026-09-03T10:01:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { endpoint: 'a' } }
          : { event_id: 'evt-b', event_type: 'unique.b', occurred_at: '2026-09-03T10:03:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { endpoint: 'b' } };
        return HttpResponse.json({ effect_id: params.id, evidence: [
          { event_id: 'evt-shared', event_type: 'shared.event', occurred_at: '2026-09-03T10:02:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { shared: true } },
          unique,
        ] });
      }),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    await screen.findByLabelText('Keyboard-accessible graph edges');
    await user.click(screen.getByTestId('collaboration-load-more'));
    const edgeList = screen.getByLabelText('Keyboard-accessible graph edges');
    await waitFor(() => expect(edgeList).toHaveTextContent('2 effects'));

    await user.click(within(edgeList).getByRole('button', { name: /Complete/ }));
    await waitFor(() => expect([...evidenceRequests].sort()).toEqual(['merge-page-1', 'merge-page-2']));
    const drawer = await screen.findByTestId('collaboration-evidence-drawer');
    expect(within(drawer).getAllByText('shared.event')).toHaveLength(1);
    expect(within(drawer).getAllByText('unique.a')).toHaveLength(1);
    expect(within(drawer).getAllByText('unique.b')).toHaveLength(1);
    const renderedEvents = within(drawer).getAllByRole('listitem').map((item) => item.textContent ?? '');
    expect(renderedEvents).toHaveLength(3);
    expect(renderedEvents[0]).toContain('unique.a');
    expect(renderedEvents[1]).toContain('shared.event');
    expect(renderedEvents[2]).toContain('unique.b');
  });

  it('loads cross-project agent-agent edge evidence using each contributor project scope', async () => {
    const p1 = {
      ...effects[0],
      effect_id: 'agent-agent-p1',
      id: 'agent-agent-p1',
      source: 'agent:alpha',
      source_agent_ref: 'agent:alpha',
      target: 'agent:beta',
      target_agent_ref: 'agent:beta',
      target_task_id: 'T-P1',
      project_id: 'P1',
      relation_type: 'complete',
      polarity: 'positive',
      magnitude: 1,
      occurred_at: '2026-09-04T10:00:00Z',
      evidence_event_ids: ['evt-shared', 'evt-p1'],
    };
    const p2 = {
      ...p1,
      effect_id: 'agent-agent-p2',
      id: 'agent-agent-p2',
      project_id: 'P2',
      target_task_id: 'T-P2',
      magnitude: 3,
      occurred_at: '2026-09-04T10:05:00Z',
      evidence_event_ids: ['evt-shared', 'evt-p2'],
    };
    const nodes = [
      { id: 'agent:alpha', kind: 'agent', label: 'Agent Alpha' },
      { id: 'agent:beta', kind: 'agent', label: 'Agent Beta' },
    ];
    const requests: Array<{ id: string; project: string | null }> = [];
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => {
        expect(new URL(request.url).searchParams.has('project_id')).toBe(false);
        return HttpResponse.json({
          graph: {
            nodes,
            edges: [{
              id: 'agent-agent-edge',
              source: 'agent:alpha',
              target: 'agent:beta',
              relation_type: 'complete',
              polarity: 'positive',
              magnitude: 3,
              effect_id: 'agent-agent-p1',
              effect_scopes: [
                { effect_id: 'agent-agent-p1', project_id: 'P1' },
                { effect_id: 'agent-agent-p2', project_id: 'P2' },
              ],
              interaction_count: 2,
              evidence_count: 3,
              first_occurred_at: p1.occurred_at,
              last_occurred_at: p2.occurred_at,
            }],
          },
          effects: [p1, p2],
          summary: {},
          graph_version: 'gv-cross-project-agent-agent',
          next_cursor: '',
        });
      }),
      http.get('/api/orgs/:slug/insights/collaboration-effects/:id/evidence', ({ params, request }) => {
        const id = String(params.id);
        const project = new URL(request.url).searchParams.get('project_id');
        requests.push({ id, project });
        if ((id === 'agent-agent-p1' && project !== 'P1') || (id === 'agent-agent-p2' && project !== 'P2')) {
          return HttpResponse.json({ error: 'effect_not_found' }, { status: 404 });
        }
        const unique = id === 'agent-agent-p1'
          ? { event_id: 'evt-p1', event_type: 'p1.unique', occurred_at: '2026-09-04T10:01:00Z', actor_ref: 'agent:alpha', refs: { project_id: 'P1', task_id: 'T-P1' }, payload: { project: 'P1' } }
          : { event_id: 'evt-p2', event_type: 'p2.unique', occurred_at: '2026-09-04T10:03:00Z', actor_ref: 'agent:alpha', refs: { project_id: 'P2', task_id: 'T-P2' }, payload: { project: 'P2' } };
        return HttpResponse.json({ effect_id: id, evidence: [
          { event_id: 'evt-shared', event_type: 'shared.event', occurred_at: '2026-09-04T10:02:00Z', actor_ref: 'agent:alpha', refs: { project_id: project ?? '', task_id: 'shared' }, payload: { shared: true } },
          unique,
        ] });
      }),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration');
    const edgeList = await screen.findByLabelText('Keyboard-accessible graph edges');
    await user.click(within(edgeList).getByRole('button', { name: /Complete/ }));
    await waitFor(() => expect(requests).toEqual([
      { id: 'agent-agent-p1', project: 'P1' },
      { id: 'agent-agent-p2', project: 'P2' },
    ]));
    const drawer = await screen.findByTestId('collaboration-evidence-drawer');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(within(drawer).getAllByText('shared.event')).toHaveLength(1);
    expect(within(drawer).getByText('p1.unique')).toBeVisible();
    expect(within(drawer).getByText('p2.unique')).toBeVisible();
    expect(within(drawer).getAllByRole('listitem')).toHaveLength(3);
  });
});

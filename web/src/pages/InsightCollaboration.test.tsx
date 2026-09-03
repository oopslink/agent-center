import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
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
    http.get('/api/orgs/:slug/projects', () => HttpResponse.json({ projects: [
      { id: 'P1', organization_id: 'org-1', name: 'Alpha Project', description: '', status: 'active', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z' },
      { id: 'P2', organization_id: 'org-1', name: 'Beta Project', description: '', status: 'active', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z' },
    ] })),
    http.get('/api/orgs/:slug/projects/:projectId/tasks', () => HttpResponse.json({ tasks: [
      { id: 'T1', project_id: 'P1', title: 'Task One', description: '', status: 'completed', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z', org_ref: 'T1001' },
      { id: 'T2', project_id: 'P1', title: 'Deploy analytics', description: '', status: 'open', created_by: 'user:u1', version: 1, created_at: '2026-09-03T00:00:00Z', updated_at: '2026-09-03T00:00:00Z', org_ref: 'T1002' },
    ], total: 2 })),
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

  it('shows scope, empty, permission and server states', async () => {
    const first = renderAt('/organizations/acme/insights/collaboration');
    expect(screen.getByTestId('collaboration-scope-required')).toBeVisible();
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

  it('merges the same semantic graph edge across real cursor pages and preserves distinct polarity and relation', async () => {
    const first = {
      ...effects[0],
      id: 'gv-page-1:edge-a',
      effect_id: 'ce-page-1',
      relation_type: 'complete',
      polarity: 'positive',
      magnitude: 1,
      occurred_at: '2026-09-03T10:00:00Z',
      evidence_event_ids: ['evt-a', 'evt-shared'],
    };
    const second = {
      ...first,
      id: 'gv-page-2:edge-a',
      effect_id: 'ce-page-2',
      magnitude: 3,
      occurred_at: '2026-09-03T12:00:00Z',
      evidence_event_ids: ['evt-b', 'evt-shared'],
    };
    const negative = { ...first, id: 'gv-page-2:edge-negative', effect_id: 'ce-negative', polarity: 'negative', evidence_event_ids: ['evt-neg'] };
    const blocked = { ...first, id: 'gv-page-2:edge-block', effect_id: 'ce-block', relation_type: 'block', evidence_event_ids: ['evt-block'] };
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor');
        const nodes = [{ id: first.source, kind: 'agent', label: 'Agent A' }, { id: first.target, kind: 'task', label: 'Task One', task_id: 'T1' }];
        if (!cursor) {
          return HttpResponse.json({ graph: { nodes, edges: [{ ...first, interaction_count: 1, evidence_count: 2, first_occurred_at: first.occurred_at, last_occurred_at: first.occurred_at, evidence_effect_ids: [first.effect_id], evidence_event_ids: first.evidence_event_ids }] }, effects: [first], summary: {}, next_cursor: 'ce-page-1' });
        }
        return HttpResponse.json({ graph: { nodes, edges: [
          { ...second, interaction_count: 1, evidence_count: 2, first_occurred_at: second.occurred_at, last_occurred_at: second.occurred_at, evidence_effect_ids: [second.effect_id], evidence_event_ids: second.evidence_event_ids },
          { ...negative, interaction_count: 1, evidence_count: 1, first_occurred_at: negative.occurred_at, last_occurred_at: negative.occurred_at, evidence_effect_ids: [negative.effect_id], evidence_event_ids: negative.evidence_event_ids },
          { ...blocked, interaction_count: 1, evidence_count: 1, first_occurred_at: blocked.occurred_at, last_occurred_at: blocked.occurred_at, evidence_effect_ids: [blocked.effect_id], evidence_event_ids: blocked.evidence_event_ids },
        ] }, effects: [second, negative, blocked], summary: {}, next_cursor: '' });
      }),
      http.get('/api/orgs/:slug/insights/collaboration-effects/:id/evidence', ({ params, request }) => {
        expect(new URL(request.url).searchParams.get('project_id')).toBe('P1');
        const id = String(params.id);
        const eventByEffect: Record<string, Array<{ event_id: string; event_type: string; occurred_at: string; actor_ref: string; refs: Record<string, string>; payload: Record<string, string> }>> = {
          'ce-page-1': [
            { event_id: 'evt-a', event_type: 'pm.task.completed', occurred_at: '2026-09-03T10:00:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { page: 'one' } },
            { event_id: 'evt-shared', event_type: 'pm.task.audit', occurred_at: '2026-09-03T10:30:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { shared: 'yes' } },
          ],
          'ce-page-2': [
            { event_id: 'evt-b', event_type: 'pm.task.reviewed', occurred_at: '2026-09-03T12:00:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { page: 'two' } },
            { event_id: 'evt-shared', event_type: 'pm.task.audit', occurred_at: '2026-09-03T10:30:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { shared: 'yes' } },
          ],
        };
        return HttpResponse.json({ effect_id: id, evidence: eventByEffect[id] ?? [] });
      }),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    const edgeList = await screen.findByLabelText('Keyboard-accessible graph edges');
    expect(within(edgeList).getAllByRole('button')).toHaveLength(1);
    await user.click(screen.getByTestId('collaboration-load-more'));
    await waitFor(() => expect(within(edgeList).getAllByRole('button')).toHaveLength(3));
    const completePositive = within(edgeList).getByRole('button', { name: /Complete .* Positive/ });
    expect(completePositive).toHaveTextContent('2 effects');
    expect(completePositive).toHaveTextContent('evidence 3');
    expect(completePositive).toHaveTextContent('strength 3');
    expect(completePositive).toHaveTextContent(`${new Date(first.occurred_at).toLocaleString()} - ${new Date(second.occurred_at).toLocaleString()}`);
    expect(within(edgeList).getByRole('button', { name: /Complete .* Negative/ })).toBeVisible();
    expect(within(edgeList).getByRole('button', { name: /Block .* Positive/ })).toBeVisible();
    await user.click(completePositive);
    const drawer = await screen.findByTestId('collaboration-evidence-drawer');
    expect(await within(drawer).findByText('pm.task.completed')).toBeVisible();
    expect(within(drawer).getByText('pm.task.reviewed')).toBeVisible();
    expect(within(drawer).getAllByText('pm.task.audit')).toHaveLength(1);
  });
});

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
      http.get('/api/orgs/:slug/insights/collaboration-effects/:id/evidence', ({ params }) => HttpResponse.json({ effect_id: params.id, evidence: [{ event_id: 'evt-0', event_type: 'pm.task.assigned', occurred_at: '2026-09-03T10:00:00Z', actor_ref: 'agent:a0', refs: { project_id: 'P1', task_id: 'T1' }, payload: { assignee: 'agent:a0' } }] })),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1');
    const edgeList = await screen.findByLabelText('Keyboard-accessible graph edges');
    expect(within(edgeList).getAllByRole('button')).toHaveLength(6);
    expect(screen.getByTestId('collaboration-graph')).toHaveTextContent('Review rejected · Mixed');
    expect(screen.getByLabelText('Effect summary')).toHaveTextContent('Affected tasks1');
    expect(screen.getByTestId('collaboration-truncated')).toBeVisible();
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
});

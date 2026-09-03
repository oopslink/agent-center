import { afterEach, describe, expect, it } from 'vitest';
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
    await waitFor(() => expect(new URL(requested).searchParams.get('polarity')).toBe('mixed'));
    view.unmount();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&task_id=T1&polarity=mixed');
    expect(screen.getByLabelText('Polarity')).toHaveValue('mixed');
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
});

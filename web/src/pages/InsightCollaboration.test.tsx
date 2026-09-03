import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import i18n from '@/i18n';
import InsightCollaboration from './InsightCollaboration';

const relations = ['assign', 'reassign', 'block', 'unblock', 'complete', 'dependency_release', 'review_accept', 'review_reject'] as const;
const polarities = ['neutral', 'mixed', 'negative', 'positive', 'positive', 'positive', 'positive', 'mixed'] as const;

function effect(i: number) {
  const relation = relations[i % relations.length];
  const source = `agent:a${i % 24}`;
  const targetAgent = relation === 'reassign' ? `agent:a${(i + 1) % 24}` : '';
  return {
    id: `ce-${String(i).padStart(3, '0')}`,
    effect_id: `ce-${String(i).padStart(3, '0')}`,
    source,
    target: targetAgent || `task:T${i % 90}`,
    relation_type: relation,
    polarity: polarities[i % polarities.length],
    magnitude: (i % 3 + 1) as 1 | 2 | 3,
    project_id: `P${i % 4}`,
    target_task_id: `T${i % 90}`,
    source_agent_ref: source,
    target_agent_ref: targetAgent,
    confidence: 'high',
    occurred_at: `2026-09-03T10:${String(i % 60).padStart(2, '0')}:00Z`,
    rule_version: 'collaboration-effect.mvp.v1',
    evidence_event_ids: [`evt-${i}`],
    before_state: { status: 'running' },
    after_state: { status: 'completed' },
    explanation_key: `collaboration.effect.${relation}`,
  };
}

function graph(count = 8, next = '') {
  const effects = Array.from({ length: count }, (_, i) => effect(i));
  const nodeMap = new Map<string, { id: string; kind: 'agent' | 'task'; label: string; task_id?: string }>();
  for (const item of effects) {
    nodeMap.set(item.source, { id: item.source, kind: 'agent', label: `Agent ${item.source}` });
    if (item.target_agent_ref) nodeMap.set(item.target_agent_ref, { id: item.target_agent_ref, kind: 'agent', label: `Agent ${item.target_agent_ref}` });
    nodeMap.set(`task:${item.target_task_id}`, { id: `task:${item.target_task_id}`, kind: 'task', label: `Task ${item.target_task_id}`, task_id: item.target_task_id });
  }
  return {
    graph: { nodes: Array.from(nodeMap.values()), edges: effects },
    effects,
    summary: {
      positive_count: effects.filter((item) => item.polarity === 'positive').length,
      negative_count: effects.filter((item) => item.polarity === 'negative').length,
      neutral_count: effects.filter((item) => item.polarity === 'neutral').length,
      mixed_count: effects.filter((item) => item.polarity === 'mixed').length,
      affected_task_count: new Set(effects.map((item) => item.target_task_id)).size,
    },
    as_of: '2026-09-03T11:00:00Z',
    rule_version: 'collaboration-effect.mvp.v1',
    truncated: Boolean(next),
    next_cursor: next,
  };
}

function renderAt(path: string) {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes><Route path="/organizations/:slug/insights/collaboration" element={<InsightCollaboration />} /></Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(async () => { cleanup(); await i18n.changeLanguage('en'); });

describe('Collaboration Insight organization graph', () => {
  it('loads the organization graph without project or task preselection and keeps summary secondary', async () => {
    let requested = '';
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => {
      requested = request.url;
      return HttpResponse.json(graph(8, 'ce-999'));
    }));
    renderAt('/organizations/acme/insights/collaboration');

    const graphRegion = await screen.findByTestId('collaboration-graph');
    expect(new URL(requested).searchParams.get('project_id')).toBeNull();
    expect(new URL(requested).searchParams.get('task_id')).toBeNull();
    expect(graphRegion.compareDocumentPosition(screen.getByLabelText('Effect summary')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByTestId('collaboration-legend')).toHaveTextContent('Agent circle');
    expect(screen.getByTestId('collaboration-legend')).toHaveTextContent('Polarity is encoded');
    expect(screen.getAllByTestId('collaboration-plan-container').length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('collaboration-plan-node').length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('collaboration-agent-agent-edge').length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('collaboration-force-edge').length).toBeGreaterThan(0);
    expect(screen.getByTestId('collaboration-truncated')).toHaveTextContent('Load next slice');
  });

  it('supports keyboard selection, evidence drawer, search focus, clear filters and URL filter reload', async () => {
    let evidenceURL = '';
    let requested = '';
    server.use(
      http.get('/api/orgs/:slug/insights/collaboration-effects', ({ request }) => {
        requested = request.url;
        return HttpResponse.json(graph(8));
      }),
      http.get('/api/orgs/:slug/insights/collaboration-effects/:id/evidence', ({ request, params }) => {
        evidenceURL = request.url;
        return HttpResponse.json({ effect_id: params.id, evidence: [{ event_id: 'evt-1', event_type: 'pm.task.reassigned', occurred_at: '2026-09-03T10:00:00Z', actor_ref: 'agent:a1', refs: { project_id: 'P1', task_id: 'T1' }, payload: { previous_assignee: 'agent:a1', assignee: 'agent:a2' } }] });
      }),
    );
    const user = userEvent.setup();
    renderAt('/organizations/acme/insights/collaboration?project_id=P1&polarity=mixed');
    await screen.findByTestId('collaboration-graph');
    expect(new URL(requested).searchParams.get('project_id')).toBe('P1');
    expect(new URL(requested).searchParams.get('polarity')).toBe('mixed');

    await user.type(screen.getByLabelText('Search graph'), 'agent:a1');
    await user.click(screen.getByRole('button', { name: 'Locate' }));
    const edgeButton = within(screen.getByLabelText('Keyboard-accessible graph edges')).getByRole('button', { name: /Reassign/ });
    edgeButton.focus();
    await user.keyboard('{Enter}');
    const drawer = await screen.findByTestId('collaboration-evidence-drawer');
    expect(await within(drawer).findByText('pm.task.reassigned')).toBeVisible();
    expect(new URL(evidenceURL).searchParams.get('project_id')).toBe('P1');

    await user.click(screen.getAllByRole('button', { name: 'Clear filters' })[0]);
    await waitFor(() => expect(screen.getByLabelText('Project ID')).toHaveValue(''));
    expect(screen.getByLabelText('Polarity')).toHaveValue('');
  });

  it('renders large graph LOD, pointer pan/drag/box selection, empty and error states', async () => {
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json(graph(260, 'ce-next'))));
    renderAt('/organizations/acme/insights/collaboration');
    const canvas = await screen.findByRole('img', { name: 'Organization collaboration graph' });
    expect(screen.getByTestId('collaboration-lod')).toHaveTextContent('LOD active');
    fireEvent.keyDown(canvas, { key: '+' });
    fireEvent.pointerDown(canvas, { clientX: 20, clientY: 20 });
    fireEvent.pointerMove(canvas, { clientX: 80, clientY: 60 });
    fireEvent.pointerUp(canvas);
    fireEvent.pointerDown(canvas, { clientX: 20, clientY: 20, shiftKey: true });
    fireEvent.pointerMove(canvas, { clientX: 220, clientY: 220, shiftKey: true });
    fireEvent.pointerUp(canvas);
    expect(screen.getByTestId('collaboration-graph')).toHaveTextContent('graph is truncated');

    cleanup();
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json(graph(0))));
    renderAt('/organizations/acme/insights/collaboration');
    expect(await screen.findByTestId('collaboration-empty')).toBeVisible();

    cleanup();
    server.use(http.get('/api/orgs/:slug/insights/collaboration-effects', () => HttpResponse.json({ error: 'forbidden' }, { status: 403 })));
    renderAt('/organizations/acme/insights/collaboration');
    expect(await screen.findByTestId('collaboration-forbidden')).toBeVisible();
  });
});

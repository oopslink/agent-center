// v2.10.0 [T7] Members — col④ context panel: the selected agent's current work
// item + the plan it belongs to.
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { AgentContextPanel, pickCurrentWorkItem } from './AgentContextPanel';
import type { AgentWorkItem } from '@/api/types';

function wi(over: Partial<AgentWorkItem>): AgentWorkItem {
  return {
    id: 'wi-1', agent_id: 'A-1', task_ref: 'pm://tasks/task-aaa', status: 'queued',
    interactions: 0, version: 1, created_at: '2026-06-01T00:00:00Z', updated_at: '2026-06-01T00:00:00Z',
    ...over,
  };
}

function renderPanel(agentId = 'A-1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AgentContextPanel agentId={agentId} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('pickCurrentWorkItem', () => {
  it('prefers an active item over everything else', () => {
    const items = [wi({ id: 'a', status: 'queued', updated_at: '2026-06-03T00:00:00Z' }), wi({ id: 'b', status: 'active', updated_at: '2026-06-01T00:00:00Z' })];
    expect(pickCurrentWorkItem(items)?.id).toBe('b');
  });
  it('falls back to the most-recent NON-terminal item', () => {
    const items = [wi({ id: 'old', status: 'queued', updated_at: '2026-06-01T00:00:00Z' }), wi({ id: 'done', status: 'done', updated_at: '2026-06-09T00:00:00Z' }), wi({ id: 'new', status: 'paused', updated_at: '2026-06-05T00:00:00Z' })];
    expect(pickCurrentWorkItem(items)?.id).toBe('new');
  });
  it('falls back to the most-recent item when all are terminal', () => {
    const items = [wi({ id: 'd1', status: 'done', updated_at: '2026-06-01T00:00:00Z' }), wi({ id: 'd2', status: 'canceled', updated_at: '2026-06-08T00:00:00Z' })];
    expect(pickCurrentWorkItem(items)?.id).toBe('d2');
  });
  it('returns undefined for no items', () => {
    expect(pickCurrentWorkItem([])).toBeUndefined();
  });
});

describe('AgentContextPanel', () => {
  beforeEach(() => {
    server.use(
      http.get('/api/agents/:id/work-items', () =>
        HttpResponse.json({
          work_items: [
            wi({ id: 'wi-active', status: 'active', task_id: 'task-xyz', task_ref: 'pm://tasks/task-xyz', task_title: 'Run-real acceptance', project_id: 'proj-1', updated_at: '2026-06-10T00:00:00Z' }),
            wi({ id: 'wi-old', status: 'done', task_id: 'task-old', task_title: 'old', project_id: 'proj-1', updated_at: '2026-06-01T00:00:00Z' }),
          ],
        }),
      ),
      http.get('/api/projects/:pid/plans', () =>
        HttpResponse.json({
          plans: [
            { id: 'plan-A', project_id: 'proj-1', name: 'Other plan', description: '', status: 'running', creator_ref: 'user:x', conversation_id: 'c1', has_failed: false, progress: { done: 1, total: 3 }, created_at: '2026-06-01T00:00:00Z', nodes_preview: [{ task_id: 'task-zzz', title: 'z', assignee_ref: '', task_status: 'open', node_status: 'blocked', depends_on: [] }] },
            { id: 'plan-B', project_id: 'proj-1', name: 'v2.10.0 rebuild', description: '', status: 'running', creator_ref: 'user:x', conversation_id: 'c2', has_failed: false, progress: { done: 2, total: 8 }, created_at: '2026-06-01T00:00:00Z', nodes_preview: [{ task_id: 'task-xyz', title: 'the task', assignee_ref: '', task_status: 'running', node_status: 'dispatched', depends_on: [] }] },
          ],
        }),
      ),
    );
  });
  afterEach(() => cleanup());

  it('shows the active work item (title, status, task handle) linking to its task', async () => {
    renderPanel();
    await waitFor(() => expect(screen.getByTestId('agent-context-workitem')).toBeInTheDocument());
    const card = screen.getByTestId('agent-context-workitem');
    expect(card).toHaveAttribute('data-status', 'active');
    expect(within(card).getByTestId('agent-context-workitem-link')).toHaveTextContent('Run-real acceptance');
    expect(within(card).getByTestId('agent-context-workitem-link')).toHaveAttribute('href', '/projects/proj-1/tasks/task-xyz');
    expect(within(card).getByTestId('agent-context-workitem-status')).toHaveTextContent('Running');
    expect(card.textContent).toContain('task-xyz'); // T126: full task id, never a #id-tail hash
    expect(card.textContent).not.toContain('#sk-xyz');
  });

  it('prefers the task org_ref (T<n>) over the id-tail handle (T100)', async () => {
    server.use(
      http.get('/api/agents/:id/work-items', () =>
        HttpResponse.json({
          work_items: [
            wi({ id: 'wi-active', status: 'active', task_id: 'task-aab6eb82', task_ref: 'pm://tasks/task-aab6eb82', task_title: 'Mobile shell', project_id: 'proj-1', org_ref: 'T84', updated_at: '2026-06-10T00:00:00Z' }),
          ],
        }),
      ),
    );
    renderPanel();
    const card = await screen.findByTestId('agent-context-workitem');
    // org_ref (T84) replaces the #b6eb82 id-tail handle the owner reported.
    expect(card.textContent).toContain('T84');
    expect(card.textContent).not.toContain('#b6eb82');
  });

  it('resolves the owning plan by task membership and links to it', async () => {
    renderPanel();
    await waitFor(() => expect(screen.getByTestId('agent-context-plan-link')).toBeInTheDocument());
    const plan = screen.getByTestId('agent-context-plan-link');
    expect(plan).toHaveAttribute('data-plan-id', 'plan-B');
    expect(plan.textContent).toContain('v2.10.0 rebuild');
    expect(plan).toHaveAttribute('href', '/projects/proj-1/plans/plan-B');
  });

  it('shows an empty state when the agent has no work items', async () => {
    server.use(http.get('/api/agents/:id/work-items', () => HttpResponse.json({ work_items: [] })));
    renderPanel();
    await waitFor(() => expect(screen.getByTestId('agent-context-no-workitem')).toBeInTheDocument());
    expect(screen.getByTestId('agent-context-no-plan')).toHaveTextContent('—');
  });

  it('shows "Not part of a plan" when no plan contains the current task', async () => {
    server.use(
      http.get('/api/agents/:id/work-items', () =>
        HttpResponse.json({ work_items: [wi({ id: 'wi-1', status: 'active', task_id: 'task-orphan', task_title: 'Orphan', project_id: 'proj-1' })] }),
      ),
      http.get('/api/projects/:pid/plans', () => HttpResponse.json({ plans: [] })),
    );
    renderPanel();
    // Wait for the (orphan) work item to load before asserting the plan slot.
    await waitFor(() => expect(screen.getByTestId('agent-context-workitem')).toBeInTheDocument());
    await waitFor(() =>
      expect(screen.getByTestId('agent-context-no-plan')).toHaveTextContent('Not part of a plan.'),
    );
  });
});

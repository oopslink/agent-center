import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { AgentWorkItems } from './AgentWorkItems';
import type { AgentWorkItem, WorkItemStatus } from '@/api/types';

const wi = (id: string, status: WorkItemStatus, extra: Partial<AgentWorkItem> = {}): AgentWorkItem => ({
  id,
  agent_id: 'A1',
  task_ref: `pm://tasks/${id}`,
  status,
  interactions: 0,
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  ...extra,
});

function stub(items: AgentWorkItem[]) {
  server.use(http.get('/api/agents/:id/work-items', () => HttpResponse.json({ work_items: items })));
}

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AgentWorkItems agentId="A1" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AgentWorkItems (#228 PR(d))', () => {
  afterEach(() => cleanup());

  it('shows the Dev empty-state copy when there are no work items (no +New)', async () => {
    stub([]);
    wrap();
    await waitFor(() =>
      expect(screen.getByTestId('agent-workitems-empty')).toHaveTextContent(
        /Work items are created when tasks are assigned/i,
      ),
    );
    // (A) read-only: no create affordance anywhere.
    expect(screen.queryByText('+ New')).not.toBeInTheDocument();
  });

  it('summarises counts by bucket (Total / In Progress / Pending / Done / Blocked)', async () => {
    stub([
      wi('a1', 'active'),
      wi('a2', 'active'),
      wi('q1', 'queued'),
      wi('d1', 'done'),
      wi('f1', 'failed'),
      wi('w1', 'waiting_input'),
    ]);
    wrap();
    const summary = await screen.findByTestId('agent-workitems-summary');
    expect(summary).toHaveTextContent('6 Total');
    expect(summary).toHaveTextContent('2 In Progress');
    expect(summary).toHaveTextContent('1 Pending');
    expect(summary).toHaveTextContent('1 Done');
    // blocked = failed + waiting_input.
    expect(summary).toHaveTextContent('2 Blocked');
  });

  it('renders the columns: short id (full on hover), Task type, "—" priority, mapped status', async () => {
    stub([wi('abcdef123456', 'active')]);
    wrap();
    const row = await screen.findByTestId('agent-workitem-row');
    expect(row).toHaveAttribute('data-status', 'active');
    // ID: short handle + full id on hover (#192 — no full raw id as chrome).
    const id = screen.getByTestId('agent-workitem-id');
    expect(id).toHaveTextContent('#abcdef');
    expect(id).toHaveAttribute('title', 'abcdef123456');
    expect(id).not.toHaveTextContent('abcdef123456');
    // Type fallback chip + Priority fallback.
    expect(screen.getByTestId('agent-workitem-type')).toHaveTextContent('Task');
    expect(screen.getByTestId('agent-workitem-priority')).toHaveTextContent('—');
    // active → "In Progress".
    expect(screen.getByTestId('agent-workitem-status')).toHaveTextContent('In Progress');
  });

  it('links the title to its task when resolved (#206)', async () => {
    stub([wi('w9', 'active', { task_id: 'task-9', task_title: 'Build login flow', project_id: 'proj-x' })]);
    wrap();
    const link = await screen.findByTestId('agent-workitem-task');
    expect(link).toHaveTextContent('Build login flow');
    expect(link.getAttribute('href')).toContain('/projects/proj-x/tasks/task-9');
  });

  it('falls back to "Work item" (no link) when the task is unresolved', async () => {
    stub([wi('w1', 'queued')]);
    wrap();
    const row = await screen.findByTestId('agent-workitem-row');
    expect(row).toHaveTextContent('Work item');
    expect(screen.queryByTestId('agent-workitem-task')).not.toBeInTheDocument();
  });

  it('filters rows by status bucket', async () => {
    stub([wi('a1', 'active'), wi('d1', 'done'), wi('q1', 'queued')]);
    wrap();
    await waitFor(() => expect(screen.getAllByTestId('agent-workitem-row')).toHaveLength(3));
    fireEvent.change(screen.getByTestId('agent-workitems-filter-status'), { target: { value: 'done' } });
    await waitFor(() => expect(screen.getAllByTestId('agent-workitem-row')).toHaveLength(1));
    expect(screen.getByTestId('agent-workitem-row')).toHaveAttribute('data-status', 'done');
  });

  it('shows a no-match note when a filter excludes everything', async () => {
    stub([wi('a1', 'active')]);
    wrap();
    await screen.findByTestId('agent-workitem-row');
    fireEvent.change(screen.getByTestId('agent-workitems-filter-status'), { target: { value: 'blocked' } });
    await waitFor(() => expect(screen.getByTestId('agent-workitems-no-match')).toBeInTheDocument());
    expect(screen.queryByTestId('agent-workitem-row')).not.toBeInTheDocument();
  });
});

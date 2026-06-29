// T231 — Work Board "+ New Task": create a task with a chosen destination
// (Backlog / Assignment Pool / a draft Plan).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { BoardTaskCreateModal } from './BoardTaskCreateModal';
import type { Plan } from '@/api/plans';

function plan(over: Partial<Plan>): Plan {
  return {
    id: 'PL-x', project_id: 'proj-1', name: 'Plan X', description: '', status: 'draft',
    creator_ref: 'user:o', conversation_id: '', has_failed: false,
    progress: { done: 0, total: 0 }, created_at: '2026-06-01T00:00:00Z',
    ...over,
  };
}

const POOL = plan({ id: 'PL-pool', name: '[Built-in]', is_builtin: true, status: 'running' });
const DRAFT = plan({ id: 'PL-draft', name: 'Sprint 1', status: 'draft' });
const RUNNING = plan({ id: 'PL-run', name: 'Running plan', status: 'running' });

function renderModal(plans: Plan[] | undefined, onClose = () => {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BoardTaskCreateModal projectId="proj-1" plans={plans} onClose={onClose} />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('BoardTaskCreateModal (T231)', () => {
  it('offers Backlog + Assignment Pool + draft plans; excludes the builtin/running plans from the plan list', () => {
    renderModal([POOL, DRAFT, RUNNING]);
    const select = screen.getByTestId('board-task-create-destination');
    // Backlog default
    expect((select as HTMLSelectElement).value).toBe('backlog');
    // Pool offered (its own option), draft plan offered by name, running plan NOT.
    expect(screen.getByTestId('board-task-create-dest-pool')).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Sprint 1' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Running plan' })).toBeNull();
  });

  it('Backlog destination: creates the task and does NOT touch any plan', async () => {
    let taskBody: unknown;
    let planSelectCalled = false;
    server.use(
      http.post('/api/projects/proj-1/tasks', async ({ request }) => {
        taskBody = await request.json();
        return HttpResponse.json({ id: 'TS-1', title: 'do it' });
      }),
      http.post('/api/projects/proj-1/plans/:planId/tasks', () => {
        planSelectCalled = true;
        return HttpResponse.json({});
      }),
    );
    const onClose = vi.fn();
    renderModal([POOL, DRAFT], onClose);
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'do it' } });
    fireEvent.click(screen.getByTestId('board-task-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(taskBody).toMatchObject({ title: 'do it' });
    expect(planSelectCalled).toBe(false);
  });

  it('T566: required_capabilities (canonical) flow into the create body', async () => {
    let taskBody: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-1/tasks', async ({ request }) => {
        taskBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ id: 'TS-C', title: 'caps' });
      }),
    );
    const onClose = vi.fn();
    renderModal([POOL, DRAFT], onClose);
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'caps' } });
    const caps = screen.getByTestId('board-task-create-caps-input');
    fireEvent.change(caps, { target: { value: 'GO' } });
    fireEvent.keyDown(caps, { key: 'Enter' });
    fireEvent.click(screen.getByTestId('board-task-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(taskBody).toMatchObject({ title: 'caps', required_capabilities: ['go'] });
  });

  it('Assignment Pool destination: creates the task THEN selects it into the builtin pool', async () => {
    let selectPlanId: string | undefined;
    let selectBody: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-1/tasks', () => HttpResponse.json({ id: 'TS-9', title: 'claim me' })),
      http.post('/api/projects/proj-1/plans/:planId/tasks', async ({ request, params }) => {
        selectPlanId = String(params.planId);
        selectBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({});
      }),
    );
    const onClose = vi.fn();
    renderModal([POOL, DRAFT], onClose);
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'claim me' } });
    fireEvent.change(screen.getByTestId('board-task-create-destination'), { target: { value: POOL.id } });
    fireEvent.click(screen.getByTestId('board-task-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(selectPlanId).toBe('PL-pool');
    expect(selectBody).toEqual({ task_id: 'TS-9' });
  });

  it('Plan destination: selects the new task into the chosen draft plan', async () => {
    let selectPlanId: string | undefined;
    server.use(
      http.post('/api/projects/proj-1/tasks', () => HttpResponse.json({ id: 'TS-7' })),
      http.post('/api/projects/proj-1/plans/:planId/tasks', ({ params }) => {
        selectPlanId = String(params.planId);
        return HttpResponse.json({});
      }),
    );
    renderModal([POOL, DRAFT]);
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'planned work' } });
    fireEvent.change(screen.getByTestId('board-task-create-destination'), { target: { value: DRAFT.id } });
    fireEvent.click(screen.getByTestId('board-task-create-submit'));
    await waitFor(() => expect(selectPlanId).toBe('PL-draft'));
  });

  it('disables submit until a title is entered', () => {
    renderModal([POOL, DRAFT]);
    expect(screen.getByTestId('board-task-create-submit')).toBeDisabled();
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'x' } });
    expect(screen.getByTestId('board-task-create-submit')).not.toBeDisabled();
  });

  it('with no plans loaded yet, still offers Backlog (no pool/plan options)', () => {
    renderModal(undefined);
    expect(screen.getByTestId('board-task-create-destination')).toBeInTheDocument();
    expect(screen.queryByTestId('board-task-create-dest-pool')).toBeNull();
  });
});

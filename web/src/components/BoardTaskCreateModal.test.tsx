// T231 — Work Board "+ New Task": create a task with a chosen destination
// (Backlog / Assignment Pool / a pending Plan).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { BoardTaskCreateModal } from './BoardTaskCreateModal';
import type { AssignmentPool, Plan } from '@/api/plans';

function plan(over: Partial<Plan>): Plan {
  return {
    id: 'PL-x', project_id: 'proj-1', name: 'Plan X', description: '', status: 'pending',
    creator_ref: 'user:o', conversation_id: '', has_failed: false,
    progress: { done: 0, total: 0 }, created_at: '2026-06-01T00:00:00Z',
    ...over,
  };
}

const POOL: AssignmentPool = {
  id: 'POOL-1', project_id: 'proj-1', scheduling_class: 'background',
  auto_assign_enabled: true, holding_cap: 100, tasks: [],
};
const PENDING = plan({ id: 'PL-pending', name: 'Sprint 1', status: 'pending' });
const RUNNING = plan({ id: 'PL-run', name: 'Running plan', status: 'running' });

function renderModal(
  plans: Plan[] | undefined,
  onClose = () => {},
  assignmentPool: AssignmentPool | null = POOL,
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BoardTaskCreateModal
        projectId="proj-1"
        plans={plans}
        assignmentPool={assignmentPool ?? undefined}
        onClose={onClose}
      />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('BoardTaskCreateModal (T231)', () => {
  it('offers Backlog + Assignment Pool + pending plans; excludes running plans', () => {
    renderModal([PENDING, RUNNING]);
    const select = screen.getByTestId('board-task-create-destination');
    // Backlog default
    expect((select as HTMLSelectElement).value).toBe('backlog');
    // Pool offered as a first-class destination, pending plan offered, running plan not.
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
    renderModal([PENDING], onClose);
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
    renderModal([PENDING], onClose);
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'caps' } });
    const caps = screen.getByTestId('board-task-create-caps-input');
    fireEvent.change(caps, { target: { value: 'GO' } });
    fireEvent.keyDown(caps, { key: 'Enter' });
    fireEvent.click(screen.getByTestId('board-task-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(taskBody).toMatchObject({ title: 'caps', required_capabilities: ['go'] });
  });

  it('Assignment Pool destination: creates the task THEN adds it to the first-class pool', async () => {
    let poolCalled = false;
    let selectBody: Record<string, unknown> | undefined;
    server.use(
      http.post('/api/projects/proj-1/tasks', () => HttpResponse.json({ id: 'TS-9', title: 'claim me' })),
      http.post('/api/projects/proj-1/assignment-pool/tasks', async ({ request }) => {
        poolCalled = true;
        selectBody = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({});
      }),
    );
    const onClose = vi.fn();
    renderModal([PENDING], onClose);
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'claim me' } });
    fireEvent.change(screen.getByTestId('board-task-create-destination'), { target: { value: 'assignment-pool' } });
    fireEvent.click(screen.getByTestId('board-task-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(poolCalled).toBe(true);
    expect(selectBody).toEqual({ task_id: 'TS-9', priority: 0 });
  });

  it('Plan destination: selects the new task into the chosen pending plan', async () => {
    let selectPlanId: string | undefined;
    server.use(
      http.post('/api/projects/proj-1/tasks', () => HttpResponse.json({ id: 'TS-7' })),
      http.post('/api/projects/proj-1/plans/:planId/tasks', ({ params }) => {
        selectPlanId = String(params.planId);
        return HttpResponse.json({});
      }),
    );
    renderModal([PENDING]);
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'planned work' } });
    fireEvent.change(screen.getByTestId('board-task-create-destination'), { target: { value: PENDING.id } });
    fireEvent.click(screen.getByTestId('board-task-create-submit'));
    await waitFor(() => expect(selectPlanId).toBe('PL-pending'));
  });

  it('disables submit until a title is entered', () => {
    renderModal([PENDING]);
    expect(screen.getByTestId('board-task-create-submit')).toBeDisabled();
    fireEvent.change(screen.getByTestId('board-task-create-title'), { target: { value: 'x' } });
    expect(screen.getByTestId('board-task-create-submit')).not.toBeDisabled();
  });

  it('with no plans loaded yet, still offers Backlog (no pool/plan options)', () => {
    renderModal(undefined, () => {}, null);
    expect(screen.getByTestId('board-task-create-destination')).toBeInTheDocument();
    expect(screen.queryByTestId('board-task-create-dest-pool')).toBeNull();
  });
});

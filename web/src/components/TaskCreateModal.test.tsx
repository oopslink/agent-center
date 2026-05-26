import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { TaskCreateModal } from './TaskCreateModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const projectsHandler = http.get('/api/projects', () =>
  HttpResponse.json([
    { id: 'proj-1', name: 'Alpha', tags: [] },
    { id: 'proj-2', name: 'Beta', tags: [] },
  ]),
);

describe('TaskCreateModal', () => {
  afterEach(() => cleanup());

  it('renders project picker + title + description + parent + priority + worktree fields', async () => {
    server.use(projectsHandler);
    wrap(<TaskCreateModal onClose={() => undefined} />);
    expect(screen.getByTestId('task-create-modal')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-project')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-title')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-description')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-parent')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-priority')).toBeInTheDocument();
    expect(screen.getByTestId('task-create-worktree')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument());
  });

  it('submit disabled until project + title set', async () => {
    server.use(projectsHandler);
    wrap(<TaskCreateModal onClose={() => undefined} />);
    const submit = screen.getByTestId('task-create-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('task-create-project'), {
      target: { value: 'proj-1' },
    });
    fireEvent.change(screen.getByTestId('task-create-title'), {
      target: { value: 'fix it' },
    });
    expect(submit.disabled).toBe(false);
  });

  it('POSTs /api/tasks with selected fields + calls onClose on success', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(projectsHandler);
    server.use(
      http.post('/api/tasks', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          { task_id: 'T-1', conversation_id: '' },
          { status: 201 },
        );
      }),
    );
    const onClose = vi.fn();
    wrap(<TaskCreateModal defaultProjectId="proj-1" onClose={onClose} />);
    await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('task-create-title'), {
      target: { value: 'fix the bug' },
    });
    fireEvent.change(screen.getByTestId('task-create-priority'), {
      target: { value: 'high' },
    });
    fireEvent.click(screen.getByTestId('task-create-worktree'));
    fireEvent.click(screen.getByTestId('task-create-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(received).toMatchObject({
      project_id: 'proj-1',
      title: 'fix the bug',
      priority: 'high',
      requires_worktree: true,
    });
  });
});

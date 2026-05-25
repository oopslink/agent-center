import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Tasks from './Tasks';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement, initialEntries: string[] = ['/tasks']) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={initialEntries}>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const projectsHandler = http.get('/api/projects', () =>
  HttpResponse.json([
    {
      id: 'proj-a',
      name: 'Project Alpha',
      kind: 'coding',
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T01:00:00Z',
    },
    {
      id: 'proj-b',
      name: 'Project Beta',
      kind: 'coding',
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T01:00:00Z',
    },
  ]),
);

const tasksHandler = http.get('/api/tasks', ({ request }) => {
  const url = new URL(request.url);
  const projectId = url.searchParams.get('project_id');
  const status = url.searchParams.get('status');
  if (!projectId) {
    return HttpResponse.json(
      { error: 'missing_project_id', message: 'project_id required' },
      { status: 400 },
    );
  }
  const all = [
    {
      id: 'TS-1',
      project_id: 'proj-a',
      conversation_id: 'T-1',
      title: 'rebuild docs',
      status: 'open',
      priority: 'high',
      created_at: '2026-05-24T01:00:00Z',
      current_execution_id: 'E-9',
    },
    {
      id: 'TS-2',
      project_id: 'proj-a',
      conversation_id: 'T-2',
      title: 'done task',
      status: 'done',
      priority: 'low',
      created_at: '2026-05-20T01:00:00Z',
    },
    {
      id: 'TS-9',
      project_id: 'proj-b',
      conversation_id: 'T-9',
      title: 'other-project task',
      status: 'open',
      priority: 'medium',
      created_at: '2026-05-24T01:00:00Z',
    },
  ];
  return HttpResponse.json(
    all.filter(
      (tk) => tk.project_id === projectId && (status === null || tk.status === status),
    ),
  );
});

describe('Tasks page', () => {
  afterEach(() => cleanup());

  it('shows the pick-project nudge when no project is selected', async () => {
    server.use(projectsHandler, tasksHandler);
    wrap(<Tasks />);
    expect(await screen.findByTestId('tasks-pick-project')).toBeInTheDocument();
    expect(screen.queryAllByTestId('task-row')).toHaveLength(0);
  });

  it('renders the project tasks when the project chip is selected via URL', async () => {
    server.use(projectsHandler, tasksHandler);
    wrap(<Tasks />, ['/tasks?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('task-row')).toHaveLength(2));
    expect(screen.getByText('rebuild docs')).toBeInTheDocument();
    expect(screen.queryByText('other-project task')).not.toBeInTheDocument();
  });

  it('row only links to trace when current_execution_id is present', async () => {
    server.use(projectsHandler, tasksHandler);
    wrap(<Tasks />, ['/tasks?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('task-row')).toHaveLength(2));
    // TS-1 has a running execution → trace link; TS-2 does not.
    const traceLinks = screen.getAllByTestId('task-row-trace-link');
    expect(traceLinks).toHaveLength(1);
    expect(traceLinks[0]).toHaveAttribute('href', '/tasks/TS-1/trace');
  });

  it('project chip click switches the list', async () => {
    server.use(projectsHandler, tasksHandler);
    wrap(<Tasks />, ['/tasks?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('task-row')).toHaveLength(2));
    fireEvent.click(screen.getByRole('tab', { name: /Project Beta/i }));
    await waitFor(() => expect(screen.getByText('other-project task')).toBeInTheDocument());
  });

  it('status tab narrows the list (server-side filter)', async () => {
    server.use(projectsHandler, tasksHandler);
    wrap(<Tasks />, ['/tasks?project=proj-a']);
    await waitFor(() => expect(screen.getAllByTestId('task-row')).toHaveLength(2));
    fireEvent.click(screen.getByRole('tab', { name: /^done$/i }));
    await waitFor(() => expect(screen.getAllByTestId('task-row')).toHaveLength(1));
    expect(screen.getByText('done task')).toBeInTheDocument();
  });

  it('empty state when no tasks for project', async () => {
    server.use(projectsHandler, http.get('/api/tasks', () => HttpResponse.json([])));
    wrap(<Tasks />, ['/tasks?project=proj-a']);
    await waitFor(() => expect(screen.getByTestId('tasks-empty')).toBeInTheDocument());
  });

  it('surfaces API error', async () => {
    server.use(
      projectsHandler,
      http.get('/api/tasks', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Tasks />, ['/tasks?project=proj-a']);
    await waitFor(() => expect(screen.getByTestId('tasks-error')).toHaveTextContent(/db down/));
  });
});

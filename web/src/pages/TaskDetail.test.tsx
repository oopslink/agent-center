import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import TaskDetail from './TaskDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/projects/:projectId/tasks/:id" element={<TaskDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.7 ProjectManager BC: TaskDetail is nested under a project and is
// driven entirely by the Task projection. The new state-machine actions
// each POST to a sub-route and return the refreshed task.

const taskAt = (status: string, extra: Record<string, unknown> = {}) => ({
  id: 'TS-1',
  project_id: 'proj-a',
  title: 'rebuild docs',
  description: 'regenerate the site',
  status,
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T01:00:00Z',
  ...extra,
});

describe('TaskDetail page', () => {
  afterEach(() => cleanup());

  it('renders header + description from the Task projection', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    // Title appears in the page heading and is echoed by the #137 conversation
    // owner banner — scope to the heading so the assertion stays unambiguous.
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'rebuild docs' })).toBeInTheDocument(),
    );
    expect(screen.getByTestId('task-description')).toHaveTextContent('regenerate the site');
    expect(screen.getByTestId('task-status')).toHaveTextContent('open');
    expect(screen.getByTestId('task-project-link')).toHaveAttribute('href', '/projects/proj-a');
    // open → Assign available.
    expect(screen.getByTestId('task-assign-button')).toBeInTheDocument();
  });

  it('shows a breadcrumb with the project display name, not its ULID (#186-1/2)', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
      http.get('/api/projects/proj-a', () =>
        HttpResponse.json({
          id: 'proj-a',
          organization_id: 'O-1',
          name: 'Alpha Project',
          description: '',
          status: 'active',
          created_by: 'user:x',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        }),
      ),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    const crumb = await screen.findByTestId('task-breadcrumb');
    expect(crumb).toHaveTextContent('Tasks');
    expect(crumb).toHaveTextContent('rebuild docs');
    // project name (not the proj-a ULID) renders + links to the project.
    await waitFor(() =>
      expect(screen.getByTestId('task-breadcrumb-project')).toHaveTextContent('Alpha Project'),
    );
    expect(screen.getByTestId('task-breadcrumb-project')).toHaveAttribute('href', '/projects/proj-a');
  });

  it('assigns an agent via the assign modal', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
      http.post('/api/projects/proj-a/tasks/TS-1/assign', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(taskAt('assigned', { assignee: 'agent:builder' }));
      }),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-assign-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('task-assign-button'));
    fireEvent.change(screen.getByTestId('task-assign-input'), {
      target: { value: 'agent:builder' },
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('task-assign-submit'));
    });
    await waitFor(() => expect(received).toMatchObject({ assignee: 'agent:builder' }));
  });

  it('shows running actions (block + complete) and posts complete', async () => {
    let completed = false;
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running'))),
      http.post('/api/projects/proj-a/tasks/TS-1/complete', () => {
        completed = true;
        return HttpResponse.json(taskAt('completed', { completed_by: 'agent:builder' }));
      }),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-complete-button')).toBeInTheDocument());
    expect(screen.getByTestId('task-block-button')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('task-complete-button'));
    await waitFor(() => expect(completed).toBe(true));
  });

  it('requires a reason when blocking', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running'))),
      http.post('/api/projects/proj-a/tasks/TS-1/block', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(taskAt('blocked', { blocked_reason: 'waiting on infra' }));
      }),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-block-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('task-block-button'));
    // submit disabled until reason filled
    expect((screen.getByTestId('task-block-submit') as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId('task-block-input'), {
      target: { value: 'waiting on infra' },
    });
    await act(async () => {
      fireEvent.click(screen.getByTestId('task-block-submit'));
    });
    await waitFor(() => expect(received).toMatchObject({ reason: 'waiting on infra' }));
  });

  it('completed tasks expose Verify + Reopen, not Cancel', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('completed'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-verify-button')).toBeInTheDocument());
    // completed → {verified, reopened}: no cancel edge.
    expect(screen.getByTestId('task-reopen-button')).toBeInTheDocument();
    expect(screen.queryByTestId('task-cancel-button')).not.toBeInTheDocument();
  });

  it('verified tasks expose only Reopen', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('verified'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-reopen-button')).toBeInTheDocument());
    expect(screen.queryByTestId('task-verify-button')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-cancel-button')).not.toBeInTheDocument();
  });

  it('assigned tasks expose Start + Unassign', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('assigned'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-start-button')).toBeInTheDocument());
    expect(screen.getByTestId('task-unassign-button')).toBeInTheDocument();
  });

  it('canceled tasks hide all lifecycle actions', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('canceled'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-status')).toHaveTextContent('canceled'));
    expect(screen.queryByTestId('task-cancel-button')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-reopen-button')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-verify-button')).not.toBeInTheDocument();
  });

  it('surfaces task lookup error', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such task' }, { status: 404 }),
      ),
    );
    wrap('/projects/proj-a/tasks/missing');
    await waitFor(() =>
      expect(screen.getByTestId('task-not-found')).toHaveTextContent(/no such task/),
    );
  });
});

import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
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
          <Route path="/tasks/:id" element={<TaskDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.3-5b: route param is now the TASK_ID (TaskRuntime BC), not the
// conversation_id. Detail page fetches the Task projection first then
// uses its conversation_id to fetch the message thread.

describe('TaskDetail page', () => {
  afterEach(() => cleanup());

  it('renders header from the Task projection + messages + trace link', async () => {
    server.use(
      http.get('/api/tasks/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          conversation_id: 'T-conv-1',
          title: 'rebuild docs',
          status: 'open',
          priority: 'high',
          created_at: '2026-05-24T01:00:00Z',
        }),
      ),
      http.get('/api/conversations/T-conv-1', () =>
        HttpResponse.json({
          id: 'T-conv-1',
          kind: 'task',
          name: 'rebuild docs',
          status: 'active',
          participants: [
            { identity_id: 'agent:bot-1', role: 'owner', joined_at: 'x', joined_by: 'agent:bot-1' },
          ],
        }),
      ),
      http.get('/api/conversations/T-conv-1/messages', () =>
        HttpResponse.json([
          {
            id: 'M-1',
            conversation_id: 'T-conv-1',
            sender_identity_id: 'agent:bot-1',
            content_kind: 'text',
            content: 'starting work',
            direction: 'inbound',
            posted_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
    );
    wrap('/tasks/TS-1');
    await waitFor(() => expect(screen.getByText('starting work')).toBeInTheDocument());
    expect(screen.getByText('rebuild docs')).toBeInTheDocument();
    expect(screen.getByTestId('task-view-trace')).toHaveAttribute('href', '/tasks/TS-1/trace');
    expect(screen.getByTestId('task-project-link')).toHaveAttribute(
      'href',
      '/projects/proj-a',
    );
    expect(screen.getByTestId('message-composer')).toBeInTheDocument();
  });

  it('surfaces task lookup error', async () => {
    server.use(
      http.get('/api/tasks/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such task' }, { status: 404 }),
      ),
    );
    wrap('/tasks/missing');
    await waitFor(() =>
      expect(screen.getByTestId('task-not-found')).toHaveTextContent(/no such task/),
    );
  });

  it('renders the priority chip and exercises the select-mode toggle', async () => {
    server.use(
      http.get('/api/tasks/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          conversation_id: 'T-conv-2',
          title: 'investigate auth',
          status: 'open',
          priority: 'low',
          created_at: '2026-05-24T01:00:00Z',
          current_execution_id: 'E-7',
        }),
      ),
      http.get('/api/conversations/T-conv-2', () =>
        HttpResponse.json({
          id: 'T-conv-2',
          kind: 'task',
          name: 'investigate auth',
          status: 'active',
          participants: [
            { identity_id: 'agent:bot-1', role: 'owner', joined_at: 'x', joined_by: 'agent:bot-1' },
          ],
        }),
      ),
      http.get('/api/conversations/T-conv-2/messages', () => HttpResponse.json([])),
    );
    wrap('/tasks/TS-2');
    await waitFor(() => expect(screen.getByText('investigate auth')).toBeInTheDocument());
    expect(screen.getByText(/exec · E-7/)).toBeInTheDocument();
    expect(screen.getByText('low')).toBeInTheDocument();
    const toggle = screen.getByTestId('select-mode-toggle');
    toggle.click();
    await waitFor(() => expect(toggle).toHaveAttribute('aria-pressed', 'true'));
  });
});

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

  // v2.5.16 (#69): legacy tasks created without a bound Conversation
  // showed only metadata + action buttons — no message panel, no
  // composer. TaskDetail now surfaces an explicit empty-state with a
  // "Start discussion" CTA wired to POST /api/tasks/{id}/bind-conversation
  // (auto mode). After binding the task projection refresh re-renders
  // the message panel.
  it('offers Start discussion CTA when the task has no conversation', async () => {
    let bound = false;
    server.use(
      http.get('/api/tasks/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id),
          project_id: 'proj-a',
          conversation_id: bound ? 'T-new-conv' : '',
          title: 'feat abc',
          status: 'open',
          priority: 'medium',
          created_at: '2026-05-24T01:00:00Z',
        }),
      ),
      http.post('/api/tasks/:id/bind-conversation', () => {
        bound = true;
        return HttpResponse.json({
          task_id: 'TS-legacy',
          conversation_id: 'T-new-conv',
        });
      }),
      http.get('/api/conversations/T-new-conv', () =>
        HttpResponse.json({
          id: 'T-new-conv',
          kind: 'task',
          name: 'feat abc',
          status: 'active',
          participants: [],
        }),
      ),
      http.get('/api/conversations/T-new-conv/messages', () =>
        HttpResponse.json([]),
      ),
    );
    wrap('/tasks/TS-legacy');
    // Empty-state CTA visible until the task gets a conversation.
    await waitFor(() =>
      expect(screen.getByTestId('task-no-conversation')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('message-composer')).not.toBeInTheDocument();
    const cta = screen.getByTestId('task-start-discussion');
    await act(async () => {
      fireEvent.click(cta);
    });
    // After bind, the projection invalidation refreshes the task with
    // a conversation_id; the message composer + list now render.
    await waitFor(() =>
      expect(screen.getByTestId('message-composer')).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('task-no-conversation')).not.toBeInTheDocument();
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
    fireEvent.click(toggle);
    await waitFor(() => expect(toggle).toHaveAttribute('aria-pressed', 'true'));
  });
});

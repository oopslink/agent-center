import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse, delay } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import DMDetail from './DMDetail';
import TaskDetail from './TaskDetail';
import AgentDetail from './AgentDetail';
import IssueDetail from './IssueDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string, route: string, page: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path={route} element={page} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// F14 coverage closeout: targets the isLoading / isError early-return
// branches on the four detail pages that were missing per the
// `pnpm run test:ci` per-file report. Tests are short — each just
// asserts the right placeholder element appears.

describe('Detail pages — loading + error branches', () => {
  afterEach(() => cleanup());

  it('DMDetail isLoading branch', async () => {
    server.use(
      http.get('/api/conversations/:id', async () => {
        await delay(50);
        return HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          participants: [],
        });
      }),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/dms/C-DM', '/dms/:id', <DMDetail />);
    expect(screen.getByText(/Loading DM/)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText(/Loading DM/)).not.toBeInTheDocument());
  });

  it('DMDetail messages-loading inner branch', async () => {
    server.use(
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'C-DM',
          kind: 'dm',
          name: '',
          status: 'active',
          participants: [],
        }),
      ),
      http.get('/api/conversations/:id/messages', async () => {
        await delay(80);
        return HttpResponse.json([]);
      }),
    );
    wrap('/dms/C-DM', '/dms/:id', <DMDetail />);
    await waitFor(() =>
      expect(screen.getByTestId('dm-messages-loading')).toBeInTheDocument(),
    );
  });

  it('TaskDetail isLoading branch', async () => {
    // v2.3-5b: TaskDetail's outer loading state now keys on the Task
    // projection fetch (BC-native), not the bound conversation.
    server.use(
      http.get('/api/tasks/:id', async () => {
        await delay(50);
        return HttpResponse.json({
          id: 'T-1',
          project_id: 'proj-a',
          conversation_id: 'T-conv-1',
          title: 'x',
          status: 'open',
          priority: 'medium',
          created_at: '2026-05-24T01:00:00Z',
        });
      }),
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'T-conv-1',
          kind: 'task',
          name: 'x',
          status: 'active',
          participants: [],
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
    );
    wrap('/tasks/T-1', '/tasks/:id', <TaskDetail />);
    expect(screen.getByText(/Loading task/)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText(/Loading task/)).not.toBeInTheDocument());
  });

  it('TaskDetail messages-loading inner branch', async () => {
    server.use(
      http.get('/api/tasks/:id', () =>
        HttpResponse.json({
          id: 'T-1',
          project_id: 'proj-a',
          conversation_id: 'T-conv-1',
          title: 'x',
          status: 'open',
          priority: 'medium',
          created_at: '2026-05-24T01:00:00Z',
        }),
      ),
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'T-conv-1',
          kind: 'task',
          name: 'x',
          status: 'active',
          participants: [],
        }),
      ),
      http.get('/api/conversations/:id/messages', async () => {
        await delay(80);
        return HttpResponse.json([]);
      }),
    );
    wrap('/tasks/T-1', '/tasks/:id', <TaskDetail />);
    await waitFor(() =>
      expect(screen.getByTestId('task-messages-loading')).toBeInTheDocument(),
    );
  });

  it('IssueDetail isLoading branch', async () => {
    // v2.3-5b: IssueDetail's outer loading state now keys on the
    // Issue projection fetch (BC-native), not the bound conversation.
    server.use(
      http.get('/api/issues/:id', async () => {
        await delay(50);
        return HttpResponse.json({
          id: 'I-1',
          project_id: 'proj-a',
          conversation_id: 'I-conv-1',
          title: 'x',
          status: 'open',
          opened_at: '2026-05-24T01:00:00Z',
          opener: 'user:hayang',
        });
      }),
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'I-conv-1',
          kind: 'issue',
          name: 'x',
          status: 'active',
          participants: [],
        }),
      ),
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([])),
      http.get('/api/conversations/:id/refs', () => HttpResponse.json([])),
    );
    wrap('/issues/I-1', '/issues/:id', <IssueDetail />);
    expect(screen.getByText(/Loading issue/)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText(/Loading issue/)).not.toBeInTheDocument());
  });

  it('IssueDetail messages-loading inner branch', async () => {
    server.use(
      http.get('/api/issues/:id', () =>
        HttpResponse.json({
          id: 'I-1',
          project_id: 'proj-a',
          conversation_id: 'I-conv-1',
          title: 'x',
          status: 'open',
          opened_at: '2026-05-24T01:00:00Z',
          opener: 'user:hayang',
        }),
      ),
      http.get('/api/conversations/:id', () =>
        HttpResponse.json({
          id: 'I-conv-1',
          kind: 'issue',
          name: 'x',
          status: 'active',
          participants: [],
        }),
      ),
      http.get('/api/conversations/:id/messages', async () => {
        await delay(80);
        return HttpResponse.json([]);
      }),
      http.get('/api/conversations/:id/refs', () => HttpResponse.json([])),
    );
    wrap('/issues/I-1', '/issues/:id', <IssueDetail />);
    await waitFor(() =>
      expect(screen.getByTestId('issue-messages-loading')).toBeInTheDocument(),
    );
  });

  it('AgentDetail isLoading branch', async () => {
    server.use(
      http.get('/api/agents/:name', async () => {
        await delay(50);
        return HttpResponse.json({
          id: 'A1',
          identity_id: 'agent:A1',
          name: 'a',
          agent_cli: 'claudecode',
          state: 'idle',
        });
      }),
      http.get('/api/fleet', () =>
        HttpResponse.json({ executions: [], workers: [], open_input_requests: [], pending_issues: [] }),
      ),
    );
    wrap('/agents/a', '/agents/:name', <AgentDetail />);
    expect(screen.getByText(/Loading agent/)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText(/Loading agent/)).not.toBeInTheDocument());
  });

  it('AgentDetail fleet-loading inner branch', async () => {
    server.use(
      http.get('/api/agents/:name', () =>
        HttpResponse.json({
          id: 'A1',
          identity_id: 'agent:A1',
          name: 'a',
          agent_cli: 'claudecode',
          state: 'idle',
        }),
      ),
      http.get('/api/fleet', async () => {
        await delay(80);
        return HttpResponse.json({ executions: [], workers: [], open_input_requests: [], pending_issues: [] });
      }),
    );
    wrap('/agents/a', '/agents/:name', <AgentDetail />);
    await waitFor(() =>
      expect(screen.getByTestId('agent-exec-loading')).toBeInTheDocument(),
    );
  });
});

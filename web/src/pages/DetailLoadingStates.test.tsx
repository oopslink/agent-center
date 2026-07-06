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
    // #264 P1: inner messages-loading now renders via the shared shell.
    await waitFor(() =>
      expect(screen.getByTestId('conversation-loading')).toBeInTheDocument(),
    );
  });

  it('TaskDetail isLoading branch', async () => {
    // v2.7: TaskDetail's loading state keys on the nested Task
    // projection fetch.
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', async () => {
        await delay(50);
        return HttpResponse.json({
          id: 'T-1',
          project_id: 'proj-a',
          title: 'x',
          description: '',
          status: 'open',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        });
      }),
    );
    wrap('/projects/proj-a/tasks/T-1', '/projects/:projectId/tasks/:id', <TaskDetail />);
    expect(screen.getByText(/Loading task/)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText(/Loading task/)).not.toBeInTheDocument());
  });

  it('IssueDetail isLoading branch', async () => {
    // v2.7: IssueDetail's loading state keys on the nested Issue
    // projection fetch.
    server.use(
      http.get('/api/projects/proj-a/issues/:id', async () => {
        await delay(50);
        return HttpResponse.json({
          id: 'I-1',
          project_id: 'proj-a',
          title: 'x',
          description: '',
          status: 'open',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        });
      }),
    );
    wrap('/projects/proj-a/issues/I-1', '/projects/:projectId/issues/:id', <IssueDetail />);
    expect(screen.getByText(/Loading issue/)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText(/Loading issue/)).not.toBeInTheDocument());
  });

  it('AgentDetail isLoading branch', async () => {
    server.use(
      http.get('/api/agents/:id', async () => {
        await delay(50);
        return HttpResponse.json({
          id: 'A1',
          organization_id: 'O-1',
          name: 'a',
          description: '',
          model: 'claude-opus',
          cli: 'claudecode',
          env_vars: {},

          worker_id: 'w-1',
          lifecycle: 'stopped',
          availability: 'available',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T02:00:00Z',
        });
      }),
    );
    wrap('/agents/A1', '/agents/:id', <AgentDetail />);
    expect(screen.getByText(/Loading agent/)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText(/Loading agent/)).not.toBeInTheDocument());
  });
});

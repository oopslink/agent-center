import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AgentDetail from './AgentDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/agents/:id" element={<AgentDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const agent = (extra: Record<string, unknown> = {}) => ({
  id: 'A1',
  organization_id: 'O-1',
  name: 'bot-1',
  description: 'a helper',
  model: 'claude-opus',
  cli: 'claudecode',
  env_vars: {},
  skills: ['review'],
  worker_id: 'w-1',
  lifecycle: 'stopped',
  availability: 'available',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  ...extra,
});

function stubAgent(extra: Record<string, unknown> = {}) {
  server.use(
    http.get('/api/agents/:id', () => HttpResponse.json(agent(extra))),
    http.get('/api/agents/:id/work-items', () =>
      HttpResponse.json({
        work_items: [
          {
            id: 'WI-1',
            agent_id: 'A1',
            task_ref: 'task:T-1',
            status: 'queued',
            interactions: 0,
            version: 1,
            created_at: '2026-05-24T01:00:00Z',
            updated_at: '2026-05-24T01:00:00Z',
          },
        ],
      }),
    ),
    http.get('/api/agents/:id/activity', () =>
      HttpResponse.json({
        activity: [
          {
            id: 'AC-1',
            agent_id: 'A1',
            event_type: 'agent.started',
            payload: '{}',
            occurred_at: '2026-05-24T01:00:00Z',
          },
        ],
      }),
    ),
  );
}

describe('AgentDetail page', () => {
  afterEach(() => cleanup());

  it('renders header with lifecycle + availability badges, worker, work items + activity', async () => {
    stubAgent();
    wrap('/agents/A1');
    await waitFor(() => expect(screen.getByText('bot-1')).toBeInTheDocument());
    expect(screen.getByTestId('agent-lifecycle-badge')).toHaveAttribute('data-lifecycle', 'stopped');
    expect(screen.getByTestId('agent-availability-badge')).toHaveAttribute('data-availability', 'available');

    // v2.7.1 #228: work items + activity live behind their tabs.
    fireEvent.click(screen.getByTestId('agent-tab-workitems'));
    await waitFor(() => expect(screen.getByTestId('agent-workitem-row')).toBeInTheDocument());
    expect(screen.getByTestId('agent-workitem-row')).toHaveAttribute('data-status', 'queued');
    // No task_title on this fixture → falls back to "Work item" (no link).
    expect(screen.getByTestId('agent-workitem-row')).toHaveTextContent('Work item');
    expect(screen.queryByTestId('agent-workitem-task')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('agent-tab-activity'));
    await waitFor(() => expect(screen.getByTestId('agent-activity-row')).toBeInTheDocument());
    expect(screen.getByTestId('agent-activity-row')).toHaveAttribute('data-event-type', 'agent.started');
  });

  it('Activity tab Refresh button refetches the activity stream (#228)', async () => {
    let hits = 0;
    server.use(
      http.get('/api/agents/:id', () => HttpResponse.json(agent())),
      http.get('/api/agents/:id/work-items', () => HttpResponse.json({ work_items: [] })),
      http.get('/api/agents/:id/activity', () => {
        hits += 1;
        return HttpResponse.json({ activity: [] });
      }),
    );
    wrap('/agents/A1');
    fireEvent.click(await screen.findByTestId('agent-tab-activity'));
    await waitFor(() => expect(hits).toBe(1));
    fireEvent.click(screen.getByTestId('agent-activity-refresh'));
    await waitFor(() => expect(hits).toBe(2));
  });

  it('switches tabs (Profile default) + Workspace shows the v2.8 placeholder (#228)', async () => {
    stubAgent();
    wrap('/agents/A1');
    // Profile is the default tab.
    await waitFor(() => expect(screen.getByTestId('agent-tabpanel-profile')).toBeInTheDocument());
    expect(screen.queryByTestId('agent-tabpanel-workitems')).not.toBeInTheDocument();
    // Workspace = "Coming in v2.8" placeholder.
    fireEvent.click(screen.getByTestId('agent-tab-workspace'));
    await waitFor(() => expect(screen.getByTestId('agent-tabpanel-workspace')).toBeInTheDocument());
    expect(screen.getByTestId('agent-tabpanel-workspace')).toHaveTextContent(/Coming in v2.8/i);
  });

  it('links a work item to its task by title when resolved (#206)', async () => {
    server.use(
      http.get('/api/agents/:id', () => HttpResponse.json(agent())),
      http.get('/api/agents/:id/work-items', () =>
        HttpResponse.json({
          work_items: [
            { id: 'WI-9', agent_id: 'A1', task_ref: 'pm://tasks/task-9', task_id: 'task-9', task_title: 'Build login flow', project_id: 'proj-x', status: 'active', interactions: 0, version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z' },
          ],
        }),
      ),
      http.get('/api/agents/:id/activity', () => HttpResponse.json({ activity: [] })),
    );
    wrap('/agents/A1');
    // v2.7.1 #228: work items behind the Work items tab.
    fireEvent.click(await screen.findByTestId('agent-tab-workitems'));
    const link = await screen.findByTestId('agent-workitem-task');
    expect(link).toHaveTextContent('Build login flow');
    expect(link.getAttribute('href')).toContain('/projects/proj-x/tasks/task-9');
  });

  it('renders without crashing when skills is null (FINDING #183: fresh agent, no skills)', async () => {
    // Pre-#183 the backend sent "skills": null for a no-skills agent and
    // AgentDetail read a.skills.length → TypeError crashed the whole page.
    stubAgent({ skills: null });
    wrap('/agents/A1');
    await waitFor(() => expect(screen.getByText('bot-1')).toBeInTheDocument());
    expect(screen.getByText('Skills')).toBeInTheDocument();
  });

  it('stopped agent shows Start (no Stop/Restart) and can start', async () => {
    stubAgent({ lifecycle: 'stopped' });
    let started = false;
    server.use(
      http.post('/api/agents/:id/start', () => {
        started = true;
        return HttpResponse.json(agent({ lifecycle: 'running' }));
      }),
    );
    wrap('/agents/A1');
    await waitFor(() => expect(screen.getByTestId('agent-start-btn')).toBeInTheDocument());
    expect(screen.queryByTestId('agent-stop-btn')).not.toBeInTheDocument();
    expect(screen.queryByTestId('agent-restart-btn')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('agent-start-btn'));
    await waitFor(() => expect(started).toBe(true));
  });

  it('running agent shows Stop + Restart (no Start)', async () => {
    stubAgent({ lifecycle: 'running' });
    wrap('/agents/A1');
    await waitFor(() => expect(screen.getByTestId('agent-stop-btn')).toBeInTheDocument());
    expect(screen.getByTestId('agent-restart-btn')).toBeInTheDocument();
    expect(screen.queryByTestId('agent-start-btn')).not.toBeInTheDocument();
  });

  it('error agent shows Start', async () => {
    stubAgent({ lifecycle: 'error', lifecycle_error: 'boom' });
    wrap('/agents/A1');
    await waitFor(() => expect(screen.getByTestId('agent-start-btn')).toBeInTheDocument());
    expect(screen.getByTestId('agent-lifecycle-error')).toHaveTextContent('boom');
  });

  it('resetting agent hides Reset + shows transient note', async () => {
    stubAgent({ lifecycle: 'resetting' });
    wrap('/agents/A1');
    await waitFor(() => expect(screen.getByTestId('agent-transient-note')).toBeInTheDocument());
    expect(screen.queryByTestId('agent-reset-btn')).not.toBeInTheDocument();
  });

  it('reset requires scope + second confirmation before firing with confirm:true', async () => {
    stubAgent({ lifecycle: 'running' });
    let body: Record<string, unknown> | null = null;
    server.use(
      http.post('/api/agents/:id/reset', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(agent({ lifecycle: 'stopped' }));
      }),
    );
    wrap('/agents/A1');
    await waitFor(() => expect(screen.getByTestId('agent-reset-btn')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('agent-reset-btn'));
    expect(screen.getByTestId('agent-reset-modal')).toBeInTheDocument();

    // Submit disabled until the confirmation checkbox is ticked.
    expect(screen.getByTestId('agent-reset-submit')).toBeDisabled();
    await userEvent.selectOptions(screen.getByTestId('agent-reset-scope'), 'all');
    fireEvent.click(screen.getByTestId('agent-reset-confirm'));
    expect(screen.getByTestId('agent-reset-submit')).not.toBeDisabled();

    fireEvent.click(screen.getByTestId('agent-reset-submit'));
    await waitFor(() => expect(body).not.toBeNull());
    expect(body).toMatchObject({ scope: 'all', confirm: true });
    await waitFor(() =>
      expect(screen.queryByTestId('agent-reset-modal')).not.toBeInTheDocument(),
    );
  });

  it('surfaces lookup error', async () => {
    server.use(
      http.get('/api/agents/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such agent' }, { status: 404 }),
      ),
    );
    wrap('/agents/ghost');
    await waitFor(() => expect(screen.getByTestId('agent-not-found')).toHaveTextContent(/no such agent/));
  });
});

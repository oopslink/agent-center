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
    await waitFor(() => expect(screen.getByRole('heading', { name: 'bot-1' })).toBeInTheDocument());
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

  it('header Message button: icon + tooltip/aria, opens (deduped) DM with the agent (#240)', async () => {
    stubAgent();
    let posted: Record<string, unknown> | null = null;
    server.use(
      // backend dedups (#215) → returns the existing or a new DM id; the UI
      // just navigates to it, so no duplicate DM is ever created here.
      http.post('/api/conversations', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ conversation_id: 'C-9', event_id: 'E-1', kind: 'dm' }, { status: 201 });
      }),
    );
    wrap('/agents/A1');
    const btn = await screen.findByTestId('agent-message-btn');
    // Lives in the header controls row (with Start/Stop/Reset), icon-only.
    expect(screen.getByTestId('agent-controls')).toContainElement(btn);
    expect(btn).toHaveAttribute('title', 'Send message');
    expect(btn).toHaveAttribute('aria-label', 'Send a direct message');
    expect(btn.querySelector('svg')).not.toBeNull();
    expect(btn).not.toHaveTextContent('Message');

    fireEvent.click(btn);
    await waitFor(() => expect(posted).not.toBeNull());
    // #240 fix: members must be a PREFIXED identity ref (agent:<id>), not a bare
    // business id — the backend ref validator rejects bare ids (400).
    expect(posted).toMatchObject({ kind: 'dm', members: ['agent:A1'] });
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
    await waitFor(() => expect(screen.getByRole('heading', { name: 'bot-1' })).toBeInTheDocument());
    expect(screen.getByText(/Skills/)).toBeInTheDocument();
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

  it('lifecycle controls are icon-ified with tooltip + aria-label (#250)', async () => {
    stubAgent({ lifecycle: 'running' });
    wrap('/agents/A1');
    const stop = await screen.findByTestId('agent-stop-btn');
    const restart = screen.getByTestId('agent-restart-btn');
    const reset = screen.getByTestId('agent-reset-btn');
    // icon-only (SVG, no text label) + tooltip + aria-label.
    for (const [btn, tip, aria] of [
      [stop, 'Stop', 'Stop agent'],
      [restart, 'Restart', 'Restart agent'],
      [reset, 'Reset', 'Reset agent'],
    ] as const) {
      expect(btn.querySelector('svg')).not.toBeNull();
      expect(btn).toHaveAttribute('title', tip);
      expect(btn).toHaveAttribute('aria-label', aria);
      expect(btn).not.toHaveTextContent(tip);
    }
    // Reset keeps the destructive (red) color.
    expect(reset.className).toContain('text-danger');
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

  // v2.8 #271: the Start button is icon-ified (was the only text action button —
  // #250 missed it), consistent with Stop/Restart/Reset/Message. Icon + a11y.
  it('renders the Start control as an icon button with title + aria (#271)', async () => {
    stubAgent({ lifecycle: 'stopped' });
    wrap('/agents/A1');
    const startBtn = await screen.findByTestId('agent-start-btn');
    expect(startBtn.querySelector('svg')).toBeInTheDocument();
    expect(startBtn).toHaveAttribute('title', 'Start');
    expect(startBtn).toHaveAttribute('aria-label', 'Start agent');
    // no longer a plain-text "Start" label
    expect(startBtn).not.toHaveTextContent('Start');
  });

  // v2.8 #270/#272: soft-archive — the user-facing delete path. Icon button on a
  // settled (stopped/error) agent → ConfirmModal二次确认 → POST /archive.
  it('archives a stopped agent via the confirm modal (#270)', async () => {
    let archived = false;
    stubAgent({ lifecycle: 'stopped' });
    server.use(
      http.post('/api/agents/:id/archive', () => {
        archived = true;
        return HttpResponse.json(agent({ lifecycle: 'archived', worker_id: '' }));
      }),
    );
    wrap('/agents/A1');
    const archiveBtn = await screen.findByTestId('agent-archive-btn');
    expect(archiveBtn.querySelector('svg')).toBeInTheDocument();
    expect(archiveBtn).toHaveAttribute('aria-label', 'Archive agent');
    fireEvent.click(archiveBtn);
    // confirm modal appears; confirm button triggers the POST.
    const modal = await screen.findByTestId('confirm-modal');
    expect(modal).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(archived).toBe(true));
  });

  // running agent (b strict-two-step): archive NOT offered — must stop first.
  it('does not offer archive on a running agent (b strict-two-step)', async () => {
    stubAgent({ lifecycle: 'running' });
    wrap('/agents/A1');
    await screen.findByTestId('agent-stop-btn');
    expect(screen.queryByTestId('agent-archive-btn')).toBeNull();
  });

  // v2.8 #270: stop + restart are disruptive (interrupt a running agent) → each
  // is gated behind a ConfirmModal二次确认 (start stays direct — non-destructive).
  it('confirms before stopping a running agent (#270)', async () => {
    let stopped = false;
    stubAgent({ lifecycle: 'running' });
    server.use(
      http.post('/api/agents/:id/stop', () => {
        stopped = true;
        return HttpResponse.json(agent({ lifecycle: 'stopping' }));
      }),
    );
    wrap('/agents/A1');
    fireEvent.click(await screen.findByTestId('agent-stop-btn'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(stopped).toBe(true));
  });

  it('confirms before restarting a running agent (#270)', async () => {
    let restarted = false;
    stubAgent({ lifecycle: 'running' });
    server.use(
      http.post('/api/agents/:id/restart', () => {
        restarted = true;
        return HttpResponse.json(agent({ lifecycle: 'running' }));
      }),
    );
    wrap('/agents/A1');
    fireEvent.click(await screen.findByTestId('agent-restart-btn'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(restarted).toBe(true));
  });

  // start is NOT gated (non-destructive — no confirm friction).
  it('starts a stopped agent directly without a confirm modal (#270)', async () => {
    let started = false;
    stubAgent({ lifecycle: 'stopped' });
    server.use(
      http.post('/api/agents/:id/start', () => {
        started = true;
        return HttpResponse.json(agent({ lifecycle: 'running' }));
      }),
    );
    wrap('/agents/A1');
    fireEvent.click(await screen.findByTestId('agent-start-btn'));
    // no confirm modal — fires immediately.
    await waitFor(() => expect(started).toBe(true));
    expect(screen.queryByTestId('confirm-modal')).toBeNull();
  });

  // archived agent detail = read-only history: no lifecycle action buttons.
  it('renders an archived agent as read-only (no action buttons) (#270)', async () => {
    stubAgent({ lifecycle: 'archived', worker_id: '' });
    wrap('/agents/A1');
    await waitFor(() =>
      expect(screen.getByTestId('agent-lifecycle-badge')).toHaveAttribute('data-lifecycle', 'archived'),
    );
    expect(screen.queryByTestId('agent-start-btn')).toBeNull();
    expect(screen.queryByTestId('agent-stop-btn')).toBeNull();
    expect(screen.queryByTestId('agent-reset-btn')).toBeNull();
    expect(screen.queryByTestId('agent-archive-btn')).toBeNull();
  });

  // v2.8 #274: the Activity feed paginates via "Load older" until next_cursor=null.
  it('paginates activity via "Load older" until the end (#274)', async () => {
    stubAgent();
    server.use(
      http.get('/api/agents/:id/activity', ({ request }) => {
        const before = new URL(request.url).searchParams.get('before');
        const ev = (id: string) => ({
          id,
          agent_id: 'A1',
          event_type: 'result',
          payload: '{}',
          occurred_at: '2026-05-24T01:00:00Z',
        });
        return before
          ? HttpResponse.json({ activity: [ev('AC-old')], next_cursor: null })
          : HttpResponse.json({ activity: [ev('AC-new')], next_cursor: 'AC-new' });
      }),
    );
    wrap('/agents/A1?tab=activity');
    // page 1 → one row + a "Load older" affordance (next_cursor present).
    const loadOlder = await screen.findByTestId('agent-activity-load-older');
    // v2.8.1 UX (@oopslink): icon-only chevron-up button — no visible text, but
    // the semantic label is kept for screen readers (a11y not-text-only).
    expect(loadOlder).toHaveAccessibleName('Load older events');
    expect(loadOlder).not.toHaveTextContent(/Load older/);
    expect(screen.getAllByTestId('agent-activity-row')).toHaveLength(1);
    // load the older page → second row appended + terminal state, no more button.
    fireEvent.click(loadOlder);
    await waitFor(() =>
      expect(screen.getByTestId('agent-activity-end')).toHaveTextContent('No more activity'),
    );
    expect(screen.getAllByTestId('agent-activity-row')).toHaveLength(2);
    expect(screen.queryByTestId('agent-activity-load-older')).toBeNull();
  });
});

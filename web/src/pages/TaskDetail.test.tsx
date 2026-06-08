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

// v2.7 #186-3a: lifecycle transitions live behind the status badge, which is
// a dropdown trigger. Open it before asserting/clicking an action item.
async function openStatusMenu() {
  fireEvent.click(await screen.findByTestId('task-status'));
  await screen.findByTestId('task-status-menu');
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
    // 5th task: status now drives the prominent StatusBlock in the sidebar
    // (the task-status trigger is relabeled "Change status").
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'open');
    expect(screen.getByTestId('task-project-link')).toHaveAttribute('href', '/projects/proj-a');
    // v2.8.1 free-state model: the menu lists ALL task states minus the current
    // one. For an `open` task that is running/blocked/completed/verified/
    // discarded/reopened (NOT open, and NOT a self-set). Assignee stays metadata
    // (meta-row Change link), so no assign item in the status menu.
    await openStatusMenu();
    expect(screen.getByTestId('task-set-status-running')).toBeInTheDocument();
    expect(screen.queryByTestId('task-set-status-open')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-assign-button')).not.toBeInTheDocument();
  });

  it('renders the description as markdown in a height-capped, keyboard-scrollable region', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () =>
        HttpResponse.json(taskAt('open', { description: '# Heading\n\n- one\n- two' })),
      ),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    const desc = await screen.findByTestId('task-description');
    // height cap + internal scroll so a long description never pushes the
    // conversation off-screen; tabIndex keeps the region keyboard-scrollable.
    expect(desc).toHaveClass('max-h-64', 'overflow-y-auto');
    expect(desc).toHaveAttribute('tabindex', '0');
    // markdown is actually rendered (heading + list), not raw text.
    expect(desc.querySelector('h1')).toBeInTheDocument();
    expect(desc.querySelectorAll('li')).toHaveLength(2);
  });

  it('opens a transition menu from the status badge and closes it again (#186-3a)', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    const trigger = await screen.findByTestId('task-status');
    // Closed by default — items hidden.
    expect(screen.queryByTestId('task-status-menu')).not.toBeInTheDocument();
    fireEvent.click(trigger);
    expect(screen.getByTestId('task-status-menu')).toBeInTheDocument();
    // Toggling again closes it.
    fireEvent.click(trigger);
    expect(screen.queryByTestId('task-status-menu')).not.toBeInTheDocument();
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
    // v2.7.1 #238: standardized <Breadcrumb> — Projects / <proj> / Tasks / <task>.
    const crumb = await screen.findByTestId('breadcrumb');
    expect(crumb).toHaveTextContent('Tasks');
    expect(crumb).toHaveTextContent('rebuild docs');
    // project name (not the proj-a ULID) renders + links to the project (seg 1).
    await waitFor(() =>
      expect(screen.getByTestId('breadcrumb-segment-1')).toHaveTextContent('Alpha Project'),
    );
    expect(screen.getByTestId('breadcrumb-segment-1')).toHaveAttribute('href', '/projects/proj-a');
  });

  it('assigns via the searchable picker — agent → agent:<member-id> ref (#186-5b)', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
      // Picker sources agents (→ agent:<identity_member_id>) + human members.
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [{ id: 'agent-bld1', identity_member_id: 'agent-bld1', name: 'builder', worker_id: 'w-1', lifecycle: 'stopped' }],
        }),
      ),
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'mem-h1', organization_id: 'O-1', identity_id: 'user-h1', kind: 'user', role: 'member', status: 'joined', display_name: 'Alice' },
        ]),
      ),
      http.post('/api/projects/proj-a/tasks/TS-1/assign', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        // metadata-only: status unchanged (stays open).
        return HttpResponse.json(taskAt('open', { assignee: 'agent:agent-bld1' }));
      }),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    // v2.8.1 #5th: assign opens from the meta-row "Change" link, not the status menu.
    fireEvent.click(await screen.findByTestId('task-assign-change'));
    // Candidates load (agent + human); filter then pick the agent.
    await waitFor(() => expect(screen.getAllByTestId('task-assign-candidate').length).toBeGreaterThan(0));
    fireEvent.change(screen.getByTestId('task-assign-search'), { target: { value: 'builder' } });
    const agentCandidate = await screen.findByTestId('task-assign-candidate');
    expect(agentCandidate).toHaveAttribute('data-assignee-ref', 'agent:agent-bld1');
    await act(async () => {
      fireEvent.click(agentCandidate);
    });
    await waitFor(() => expect(received).toMatchObject({ assignee: 'agent:agent-bld1' }));
  });

  it('can assign a human (PM tracking) → user:<identity_id> ref (#186-5a)', async () => {
    let received: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'mem-h1', organization_id: 'O-1', identity_id: 'user-h1', kind: 'user', role: 'member', status: 'joined', display_name: 'Alice' },
        ]),
      ),
      http.post('/api/projects/proj-a/tasks/TS-1/assign', async ({ request }) => {
        received = (await request.json()) as Record<string, unknown>;
        // metadata-only: status unchanged (stays open).
        return HttpResponse.json(taskAt('open', { assignee: 'user:user-h1' }));
      }),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    // v2.8.1 #5th: assign opens from the meta-row "Change" link, not the status menu.
    fireEvent.click(await screen.findByTestId('task-assign-change'));
    const human = await screen.findByTestId('task-assign-candidate');
    expect(human).toHaveAttribute('data-assignee-ref', 'user:user-h1');
    expect(human).toHaveAttribute('data-kind', 'human');
    await act(async () => {
      fireEvent.click(human);
    });
    await waitFor(() => expect(received).toMatchObject({ assignee: 'user:user-h1' }));
  });

  it('running task menu shows the full enum minus running, and PATCHes the picked status', async () => {
    // v2.8.1 free-state model: a `running` task can move to ANY other state.
    // The menu lists open/blocked/completed/verified/discarded/reopened (NOT
    // running); clicking one PATCHes .../status with that target.
    let received: Record<string, unknown> | undefined;
    let method: string | undefined;
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running'))),
      http.patch('/api/projects/proj-a/tasks/TS-1/status', async ({ request }) => {
        method = request.method;
        received = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(taskAt('completed', { completed_by: 'agent:builder' }));
      }),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await openStatusMenu();
    // current state is excluded; every other state is offered.
    expect(screen.queryByTestId('task-set-status-running')).not.toBeInTheDocument();
    for (const s of ['open', 'blocked', 'completed', 'verified', 'discarded', 'reopened']) {
      expect(screen.getByTestId(`task-set-status-${s}`)).toBeInTheDocument();
    }
    await act(async () => {
      fireEvent.click(screen.getByTestId('task-set-status-completed'));
    });
    await waitFor(() => expect(received).toMatchObject({ status: 'completed' }));
    expect(method).toBe('PATCH');
  });

  it('completed task menu offers every other state (incl. discarded) — free model', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('completed'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await openStatusMenu();
    expect(screen.queryByTestId('task-set-status-completed')).not.toBeInTheDocument();
    // free model: completed can go to verified/reopened AND discarded/open/etc.
    for (const s of ['open', 'running', 'blocked', 'verified', 'discarded', 'reopened']) {
      expect(screen.getByTestId(`task-set-status-${s}`)).toBeInTheDocument();
    }
  });

  it('verified task menu offers every other state, not just reopen — free model', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('verified'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await openStatusMenu();
    expect(screen.queryByTestId('task-set-status-verified')).not.toBeInTheDocument();
    for (const s of ['open', 'running', 'blocked', 'completed', 'discarded', 'reopened']) {
      expect(screen.getByTestId(`task-set-status-${s}`)).toBeInTheDocument();
    }
  });

  it('assignee is metadata: Unassign appears in the meta row when assigned (running state)', async () => {
    // Assignee is shown + Unassign-able in ANY non-terminal state — proves it is
    // metadata, decoupled from the lifecycle (here the task is `running`).
    let unassigned = false;
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () =>
        HttpResponse.json(taskAt('running', { assignee: 'agent:builder' })),
      ),
      http.post('/api/projects/proj-a/tasks/TS-1/unassign', () => {
        unassigned = true;
        return HttpResponse.json(taskAt('running'));
      }),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    expect(await screen.findByTestId('task-assignee')).toBeInTheDocument();
    const unassignBtn = screen.getByTestId('task-unassign-button');
    expect(unassignBtn).toBeInTheDocument();
    await act(async () => {
      fireEvent.click(unassignBtn);
    });
    await waitFor(() => expect(unassigned).toBe(true));
  });

  it('discarded task: status menu can still move to any other state, but assignee is frozen', async () => {
    // v2.8.1 free-state model: `discarded` is no longer a status dead-end — the
    // menu offers every other state (e.g. reopened/open). It IS still the
    // soft-terminal metadata state, so the assignee "Change" link is hidden.
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('discarded'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() =>
      expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'discarded'),
    );
    await openStatusMenu();
    expect(screen.queryByTestId('task-set-status-discarded')).not.toBeInTheDocument();
    expect(screen.getByTestId('task-set-status-reopened')).toBeInTheDocument();
    expect(screen.getByTestId('task-set-status-open')).toBeInTheDocument();
    // terminal metadata → assignee is no longer editable (no Change link).
    expect(screen.queryByTestId('task-assign-change')).not.toBeInTheDocument();
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

  it('shows org_ref (T<n>) in the header + breadcrumb leaf when present (#245)', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open', { org_ref: 'T7' }))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('task-org-ref')).toHaveTextContent('T7'));
    expect(screen.getByRole('heading', { name: /T7 · rebuild docs/ })).toBeInTheDocument();
    expect(screen.getByTestId('breadcrumb')).toHaveTextContent('T7 - rebuild docs');
  });
});

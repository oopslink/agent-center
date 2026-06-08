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
    // v2.8.1 #5th: open → [Start] behind the status dropdown (NOT Assign —
    // assignee is metadata, set via the meta-row Change link, not a transition).
    await openStatusMenu();
    expect(screen.getByTestId('task-start-button')).toBeInTheDocument();
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
    await openStatusMenu();
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
    await openStatusMenu();
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

  it('completed tasks expose Verify + Reopen, not Discard', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('completed'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await openStatusMenu();
    expect(screen.getByTestId('task-verify-button')).toBeInTheDocument();
    // completed → {verified, reopened}: no discard edge.
    expect(screen.getByTestId('task-reopen-button')).toBeInTheDocument();
    expect(screen.queryByTestId('task-discard-button')).not.toBeInTheDocument();
  });

  it('verified tasks expose only Reopen', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('verified'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await openStatusMenu();
    expect(screen.getByTestId('task-reopen-button')).toBeInTheDocument();
    expect(screen.queryByTestId('task-verify-button')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-discard-button')).not.toBeInTheDocument();
  });

  it('open task: Start is a transition; assignee is metadata (Change link, no assigned state)', async () => {
    // v2.8.1 #5th: there is no `assigned` state. The status menu for an open
    // task offers [Start] (→ running). Assignee is set via the meta-row "Change"
    // link in ANY state and does NOT move the status.
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    // assignee meta row present + editable even when unassigned.
    expect(await screen.findByTestId('task-assignee-empty')).toBeInTheDocument();
    expect(screen.getByTestId('task-assign-change')).toBeInTheDocument();
    await openStatusMenu();
    expect(screen.getByTestId('task-start-button')).toBeInTheDocument();
    expect(screen.queryByTestId('task-assign-button')).not.toBeInTheDocument();
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

  it('discarded tasks hide all lifecycle actions + the assignee Change link', async () => {
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('discarded'))),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() =>
      expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'discarded'),
    );
    // No transitions available → the disabled "Change status" trigger opens nothing.
    fireEvent.click(screen.getByTestId('task-status'));
    expect(screen.queryByTestId('task-status-menu')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-discard-button')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-reopen-button')).not.toBeInTheDocument();
    expect(screen.queryByTestId('task-verify-button')).not.toBeInTheDocument();
    // terminal → assignee is no longer editable (no Change link).
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

  // ───────── v2.8.1 #281: 2-section TaskDetail sidebar redesign (@oopslink mockup)
  describe('redesigned 2-section sidebar (#281)', () => {
    it('renders an editable TOP section and a read-only BOTTOM section', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { tags: ['infra'] })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      // editable top: status + assignee + tags + Edit-Task button
      const editable = await screen.findByTestId('task-sidebar-editable');
      expect(editable).toBeInTheDocument();
      expect(screen.getByTestId('task-sidebar-status')).toBeInTheDocument();
      expect(screen.getByTestId('task-sidebar-assignee')).toBeInTheDocument();
      expect(screen.getByTestId('task-sidebar-tags')).toBeInTheDocument();
      expect(screen.getByTestId('task-edit-button')).toBeInTheDocument();
      // read-only bottom: project + task id + created
      const ro = screen.getByTestId('task-sidebar-readonly');
      expect(ro).toBeInTheDocument();
      expect(screen.getByTestId('task-project-link')).toBeInTheDocument();
      expect(screen.getByTestId('task-id-pill')).toBeInTheDocument();
      expect(screen.getByTestId('task-created')).toBeInTheDocument();
    });

    it('Edit-Task button (SVG icon, no emoji) opens the TaskEditModal', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
        http.get('/api/members', () => HttpResponse.json([])),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      const editBtn = await screen.findByTestId('task-edit-button');
      // accessible name + an SVG icon (not an emoji glyph).
      expect(editBtn).toHaveAttribute('aria-label', 'Edit task');
      expect(editBtn.querySelector('svg')).toBeInTheDocument();
      expect(screen.queryByTestId('task-edit-modal')).not.toBeInTheDocument();
      fireEvent.click(editBtn);
      expect(await screen.findByTestId('task-edit-modal')).toBeInTheDocument();
    });

    it('computes the in-status duration ("Xh Ym") from status_changed_at', async () => {
      // status entered exactly 2h14m before "now" → "2h 14m" (largest two units).
      const since = new Date(Date.now() - (2 * 3600 + 14 * 60) * 1000).toISOString();
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { status_changed_at: since })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      const dur = await screen.findByTestId('task-status-duration');
      expect(dur).toHaveTextContent('2h 14m');
      // accessible text for the duration (not color-only).
      expect(dur).toHaveAttribute('aria-label', expect.stringContaining('2h 14m'));
    });

    it('omits the duration gracefully when status_changed_at is missing', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running'))),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      await screen.findByTestId('task-sidebar-status');
      expect(screen.queryByTestId('task-status-duration')).not.toBeInTheDocument();
    });

    it('renders tags as hashed-color chips — same tag → same stable class', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { tags: ['infra', 'docs', 'infra-extra'] })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      await waitFor(() => expect(screen.getAllByTestId('task-tag-chip').length).toBe(3));
      const chips = screen.getAllByTestId('task-tag-chip');
      const byTag = new Map(chips.map((c) => [c.getAttribute('data-tag'), c.className]));
      // each chip carries a curated bg-X-100 + text-X-800 pair (both-mode AA).
      for (const cls of byTag.values()) {
        expect(cls).toMatch(/bg-\w+-100/);
        expect(cls).toMatch(/text-\w+-800/);
      }
      // deterministic: 'infra' hashes to the same pair as a second 'infra' render.
      const { tagColorFor } = await import('@/components/tagColors');
      const c = tagColorFor('infra');
      expect(byTag.get('infra')).toContain(c.bg);
      expect(byTag.get('infra')).toContain(c.text);
      // tagColorFor is pure/stable across calls.
      expect(tagColorFor('infra')).toEqual(tagColorFor('infra'));
    });

    it('"+ Add" affordance adds a tag via a PATCH with body {tags}', async () => {
      let received: Record<string, unknown> | undefined;
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { tags: ['infra'] })),
        ),
        http.patch('/api/projects/proj-a/tasks/TS-1', async ({ request }) => {
          received = (await request.json()) as Record<string, unknown>;
          return HttpResponse.json(taskAt('running', { tags: ['infra', 'urgent'] }));
        }),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      const addBtn = await screen.findByTestId('task-tag-add');
      expect(addBtn).toHaveAttribute('aria-label', 'Add tag');
      fireEvent.click(addBtn);
      const input = await screen.findByTestId('task-tag-input');
      fireEvent.change(input, { target: { value: 'urgent' } });
      await act(async () => {
        fireEvent.keyDown(input, { key: 'Enter' });
      });
      // PATCH carries the FULL replacement tag set (existing + new).
      await waitFor(() => expect(received).toMatchObject({ tags: ['infra', 'urgent'] }));
    });

    it('rejects an over-long tag inline (rune-16) without PATCHing', async () => {
      let patched = false;
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running', { tags: [] }))),
        http.patch('/api/projects/proj-a/tasks/TS-1', () => {
          patched = true;
          return HttpResponse.json(taskAt('running'));
        }),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      fireEvent.click(await screen.findByTestId('task-tag-add'));
      const input = await screen.findByTestId('task-tag-input');
      fireEvent.change(input, { target: { value: 'x'.repeat(17) } });
      await act(async () => {
        fireEvent.keyDown(input, { key: 'Enter' });
      });
      expect(await screen.findByTestId('task-tag-error')).toBeInTheDocument();
      expect(patched).toBe(false);
    });

    it('read-only TASK ID shows a clean handle/pill (org_ref or tail), not the raw id', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { id: 'task-0001abcdef', org_ref: 'T7' })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      const pill = await screen.findByTestId('task-id-pill');
      // org_ref preferred as the clean handle; full id on hover (title).
      expect(pill).toHaveTextContent('T7');
      expect(pill).toHaveAttribute('title', 'task-0001abcdef');
    });

    it('assignee renders an avatar + name in the editable section', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { assignee: 'agent:builder' })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      const section = await screen.findByTestId('task-sidebar-assignee');
      expect(section.querySelector('[data-testid="avatar"]')).toBeInTheDocument();
      expect(screen.getByTestId('task-assignee')).toBeInTheDocument();
    });
  });
});

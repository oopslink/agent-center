import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
// driven entirely by the Task projection.
//
// v2.8.1 #281 readonly: per @oopslink, a task may ONLY be edited via the Edit
// Task modal (TaskEditModal — one atomic PATCH of title/desc/status/assignee/
// tags; its batch-edit behaviour is covered in TaskEditModal.test.tsx). The
// detail sidebar is now a pure read-only DISPLAY: there are NO inline
// status-change menu, NO assignee Change/Unassign, NO "+ Add" tag input.

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
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  // T309 (mobile): a single compact bar (status + assignee + Show info + Edit)
  // sits under the title; the description/attachments/details collapse behind
  // "Show info" so the chat fills the screen. Title is dropped from the breadcrumb
  // leaf (the <h2> carries it).
  it('mobile (<md): shows the compact bar; Show info reveals details; breadcrumb dedupes title', async () => {
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: true, media: query, onchange: null,
      addEventListener: () => {}, removeEventListener: () => {},
      addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false,
    }));
    server.use(
      http.get('/api/projects/proj-a/tasks/:id', () =>
        HttpResponse.json(taskAt('running', { org_ref: 'T7', assignee: 'agent:builder' })),
      ),
    );
    wrap('/projects/proj-a/tasks/TS-1');
    await waitFor(() => expect(screen.getByTestId('wi-mobile-bar')).toBeInTheDocument());
    // status surfaced in the bar. Scope to the bar — the desktop sidebar (hidden
    // via CSS) is still in the jsdom DOM.
    const bar = screen.getByTestId('wi-mobile-bar');
    expect(within(bar).getByTestId('status-block')).toHaveAttribute('data-status', 'running');
    // the info panel is collapsed by default (chat is the core surface)…
    expect(screen.queryByTestId('wi-mobile-info')).not.toBeInTheDocument();
    // …and "Show info" reveals the details content.
    fireEvent.click(within(bar).getByTestId('wi-mobile-showinfo'));
    expect(screen.getByTestId('wi-mobile-info')).toBeInTheDocument();
    expect(screen.getByTestId('wi-mobile-details-content')).toBeInTheDocument();
    // breadcrumb leaf = just the org_ref (title is NOT repeated there on mobile).
    const crumb = screen.getByTestId('breadcrumb');
    expect(crumb).toHaveTextContent('T7');
    expect(crumb).not.toHaveTextContent('rebuild docs');
    // the title still renders once, in the heading.
    expect(screen.getByRole('heading', { name: /rebuild docs/ })).toBeInTheDocument();
  });

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
    // status drives the read-only StatusBlock in the sidebar (display only).
    expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'open');
    expect(screen.getByTestId('task-project-link')).toHaveAttribute('href', '/projects/proj-a');
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

  // ───────── v2.8.1 #281: 2-section TaskDetail sidebar — read-only display
  describe('redesigned 2-section sidebar (#281) — read-only display', () => {
    it('renders a display TOP section and a read-only BOTTOM section', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { tags: ['infra'] })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      // top: status + assignee + tags + Edit-Task button (the single edit entry)
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

    it('has NO inline edit controls — status menu / assignee Change-Unassign / tag "+ Add" are all GONE', async () => {
      // The directive: a task may ONLY be edited via the Edit Task modal. Prove
      // none of the three former inline affordances render — for an `open` task
      // (the state that formerly exposed Start + the assignee Change link), with
      // an assignee present (formerly the state that exposed Unassign).
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('open', { assignee: 'agent:builder', tags: ['infra'] })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      await screen.findByTestId('task-sidebar-status');
      // (1) status-change menu/dropdown — gone.
      expect(screen.queryByTestId('task-status')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-status-menu')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-start-button')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-block-button')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-complete-button')).not.toBeInTheDocument();
      // (2) assignee Change / Unassign — gone.
      expect(screen.queryByTestId('task-assign-change')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-unassign-button')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-assign-modal')).not.toBeInTheDocument();
      // (3) tag "+ Add" inline input — gone.
      expect(screen.queryByTestId('task-tag-add')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-tag-input')).not.toBeInTheDocument();
    });

    it('Edit-Task button (SVG icon, no emoji) opens the TaskEditModal — the single edit path', async () => {
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
      // the modal exposes the full field set (title/desc/status/assignee/tags) —
      // batch-edit behaviour is asserted in TaskEditModal.test.tsx.
      const modal = await screen.findByTestId('task-edit-modal');
      expect(modal).toBeInTheDocument();
      expect(screen.getByTestId('task-edit-title')).toBeInTheDocument();
      expect(screen.getByTestId('task-edit-status')).toBeInTheDocument();
      expect(screen.getByTestId('task-edit-assignee')).toBeInTheDocument();
      expect(screen.getByTestId('task-edit-tags-input')).toBeInTheDocument();
    });

    it('hides the Edit-Task button on a terminal (discarded) task — nothing to edit', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('discarded'))),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      await waitFor(() =>
        expect(screen.getByTestId('status-block')).toHaveAttribute('data-status', 'discarded'),
      );
      expect(screen.queryByTestId('task-edit-button')).not.toBeInTheDocument();
      // still no inline controls in the terminal state either.
      expect(screen.queryByTestId('task-status')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-assign-change')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-tag-add')).not.toBeInTheDocument();
    });

    it('computes the in-status duration ("Xh Ym") from status_changed_at (display)', async () => {
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

    it('renders tags as hashed-color chips (display only) — same tag → same stable class', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { tags: ['infra', 'docs', 'infra-extra'] })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      await waitFor(() => expect(screen.getAllByTestId('task-tag-chip').length).toBe(3));
      const chips = screen.getAllByTestId('task-tag-chip');
      const byTag = new Map(chips.map((c) => [c.getAttribute('data-tag'), c.className]));
      // each chip carries a curated status-hue bg/fg token pair (both-mode AA):
      // the bg-X-100 + text-X-800 raw classes migrated to bg-status-X-bg /
      // text-status-X-fg (light hex unchanged; dark pair added).
      for (const cls of byTag.values()) {
        expect(cls).toMatch(/bg-status-\w+-bg/);
        expect(cls).toMatch(/text-status-\w+-fg/);
      }
      // deterministic: 'infra' hashes to the same pair as a second 'infra' render.
      const { tagColorFor } = await import('@/components/tagColors');
      const c = tagColorFor('infra');
      expect(byTag.get('infra')).toContain(c.bg);
      expect(byTag.get('infra')).toContain(c.text);
      // tagColorFor is pure/stable across calls.
      expect(tagColorFor('infra')).toEqual(tagColorFor('infra'));
    });

    it('renders an empty-tags placeholder with NO "+ Add" affordance', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running', { tags: [] }))),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      expect(await screen.findByTestId('task-tags-empty')).toHaveTextContent('No tags');
      expect(screen.queryByTestId('task-tag-add')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-tag-input')).not.toBeInTheDocument();
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

    it('assignee renders an avatar + name (display only) in the top section', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { assignee: 'agent:builder' })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      const section = await screen.findByTestId('task-sidebar-assignee');
      expect(section.querySelector('[data-testid="avatar"]')).toBeInTheDocument();
      expect(screen.getByTestId('task-assignee')).toBeInTheDocument();
      // no inline edit affordances on the assignee row.
      expect(screen.queryByTestId('task-assign-change')).not.toBeInTheDocument();
      expect(screen.queryByTestId('task-unassign-button')).not.toBeInTheDocument();
    });

    it('shows "Unassigned" placeholder (display only) when there is no assignee', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('open'))),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      expect(await screen.findByTestId('task-assignee-empty')).toHaveTextContent('Unassigned');
      expect(screen.queryByTestId('task-assign-change')).not.toBeInTheDocument();
    });

    // ───────── ADR-0046 D5: "stuck" annotation (blocked_reason) on a RUNNING task.
    it('shows a "Stuck" badge on a RUNNING task with a non-empty blocked_reason (solid amber, both-mode AA)', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('running', { blocked_reason: 'waiting on review' })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      const badge = await screen.findByTestId('task-blocked-reason');
      expect(badge).toHaveTextContent('Stuck: waiting on review');
      // solid X-100/X-800 token chip (no alpha-tint) → AA ≥4.5 in light + dark.
      expect(badge.className).toContain('bg-status-amber-bg');
      expect(badge.className).toContain('text-status-amber-fg');
    });

    it('does NOT show the Stuck badge when blocked_reason is empty/absent', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () => HttpResponse.json(taskAt('running'))),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      await screen.findByTestId('task-sidebar-status');
      expect(screen.queryByTestId('task-blocked-reason')).not.toBeInTheDocument();
    });

    it('does NOT show the Stuck badge on a non-running task even if blocked_reason is set', async () => {
      server.use(
        http.get('/api/projects/proj-a/tasks/:id', () =>
          HttpResponse.json(taskAt('open', { blocked_reason: 'stale annotation' })),
        ),
      );
      wrap('/projects/proj-a/tasks/TS-1');
      await screen.findByTestId('task-sidebar-status');
      expect(screen.queryByTestId('task-blocked-reason')).not.toBeInTheDocument();
    });
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import OrgWorkItemsPage from './OrgWorkItems';
import type { OrgWorkItemKind } from '@/api/orgWorkItems';

function wrap(_kind: OrgWorkItemKind, path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/organizations/:slug/issues" element={<OrgWorkItemsPage kind="issue" />} />
          <Route path="/organizations/:slug/tasks" element={<OrgWorkItemsPage kind="task" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// #258 DTO lock (thread #ce549176): project = {id, name} (pm.Project has no slug);
// issues are NOT assignable in the pm domain → issue.assignee is always null. Only
// task rows carry an enriched assignee. Fixtures mirror that contract (mock=契约).
const issueRow = (extra: Record<string, unknown> = {}) => ({
  id: 'issue-01KT8DABCDEF',
  org_ref: 'I12',
  project: { id: 'proj-a', name: 'Apollo' },
  title: 'login bug',
  status: 'in_progress',
  assignee: null,
  priority: 'high',
  updated_at: '2026-06-04T01:00:00Z',
  created_at: '2026-06-01T01:00:00Z',
  ...extra,
});

const taskRow = (extra: Record<string, unknown> = {}) => ({
  id: 'task-01KT8DXYZ789',
  org_ref: 'T34',
  project: { id: 'proj-b', name: 'Beacon' },
  title: 'ship docs',
  status: 'running',
  assignee: { ref: 'agent:agent-bot9', display_name: 'Bot Nine', member_id: 'agent-bot9' },
  priority: null,
  updated_at: '2026-06-04T02:00:00Z',
  created_at: '2026-06-01T02:00:00Z',
  ...extra,
});

describe('OrgWorkItems page (#258)', () => {
  afterEach(() => cleanup());

  it('issues: cross-project table (org_ref / project link / title link / status / unassigned)', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/issues', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [issueRow()], total: 1 });
      }),
    );
    wrap('issue', '/organizations/acme/issues');
    await waitFor(() => expect(screen.getByTestId('org-workitem-row')).toBeInTheDocument());
    // default = open only → no status query param (backend default all-open).
    expect(gotQuery).toBe('');
    // ID = org_ref (I12).
    expect(screen.getByTestId('org-workitem-id')).toHaveTextContent('I12');
    expect(screen.getByTestId('org-workitem-id')).toHaveAttribute('title', 'issue-01KT8DABCDEF');
    // Project column links to the project by id, shows name.
    const proj = screen.getByTestId('org-workitem-project').querySelector('a');
    expect(proj).toHaveTextContent('Apollo');
    expect(proj?.getAttribute('href')).toContain('/projects/proj-a');
    // Title links into the issue detail (cross-project path).
    const title = screen.getByTestId('org-workitem-title');
    expect(title.getAttribute('href')).toContain('/projects/proj-a/issues/issue-01KT8DABCDEF');
    // Status chip colored.
    expect(screen.getByTestId('status-chip')).toHaveAttribute('data-status', 'in_progress');
    // Issues are not assignable (pm domain) → "—".
    expect(screen.getByTestId('org-workitem-assignee')).toHaveTextContent('—');
  });

  it('tasks: enriched assignee — name visible + member-id on hover (#192), title → tasks path', async () => {
    server.use(
      http.get('/api/tasks', () => HttpResponse.json({ items: [taskRow()], total: 1 })),
    );
    wrap('task', '/organizations/acme/tasks');
    await waitFor(() => expect(screen.getByTestId('org-workitem-row')).toBeInTheDocument());
    expect(screen.getByTestId('org-workitem-id')).toHaveTextContent('T34');
    const title = screen.getByTestId('org-workitem-title');
    expect(title.getAttribute('href')).toContain('/projects/proj-b/tasks/task-01KT8DXYZ789');
    expect(screen.getByTestId('status-chip')).toHaveAttribute('data-status', 'running');
    const assignee = screen.getByTestId('org-workitem-assignee');
    expect(assignee).toHaveTextContent('Bot Nine');
    expect(assignee.querySelector('[title="agent-bot9"]')).not.toBeNull();
    expect(assignee).not.toHaveTextContent('agent-bot9');
  });

  // v2.8 #270/#272: an archived agent assignee shows a "(archived)" chip (#215
  // deleted-peer pattern). The task's assignee ref/history is preserved; the chip
  // is driven by the backend assignee_lifecycle (#184), no raw-id leak (#192).
  it('tasks: an archived assignee shows a "(archived)" chip (#270)', async () => {
    server.use(
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [
            taskRow({
              assignee: {
                ref: 'agent:agent-bot9',
                display_name: 'Bot Nine',
                member_id: 'agent-bot9',
                assignee_lifecycle: 'archived',
              },
            }),
          ],
          total: 1,
        }),
      ),
    );
    wrap('task', '/organizations/acme/tasks');
    await waitFor(() => expect(screen.getByTestId('org-workitem-row')).toBeInTheDocument());
    const assignee = screen.getByTestId('org-workitem-assignee');
    expect(assignee).toHaveTextContent('Bot Nine');
    expect(screen.getByTestId('org-workitem-assignee-archived')).toHaveTextContent('(archived)');
    // #192: still name + hover id, never a raw id in the text.
    expect(assignee).not.toHaveTextContent('agent-bot9');
    expect(assignee.querySelector('[title="agent-bot9"]')).not.toBeNull();
  });

  it('tasks: a non-archived (running) assignee shows NO archived chip', async () => {
    server.use(
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [
            taskRow({
              assignee: {
                ref: 'agent:agent-bot9',
                display_name: 'Bot Nine',
                member_id: 'agent-bot9',
                assignee_lifecycle: 'running',
              },
            }),
          ],
          total: 1,
        }),
      ),
    );
    wrap('task', '/organizations/acme/tasks');
    await waitFor(() => expect(screen.getByTestId('org-workitem-row')).toBeInTheDocument());
    expect(screen.queryByTestId('org-workitem-assignee-archived')).toBeNull();
  });

  it('falls back to id-tail handle when org_ref absent', async () => {
    server.use(
      http.get('/api/issues', () =>
        HttpResponse.json({ items: [issueRow({ org_ref: undefined })], total: 1 }),
      ),
    );
    wrap('issue', '/organizations/acme/issues');
    const id = await screen.findByTestId('org-workitem-id');
    expect(id).toHaveTextContent('#ABCDEF'); // ULID tail, not head
  });

  it('FilterBar: selecting statuses passes them as the multi status filter', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/issues', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [issueRow()], total: 1 });
      }),
    );
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    // default = empty selection = no status param (backend default all-open).
    // pick terminal statuses via the FilterBar chips → they become the filter.
    fireEvent.click(screen.getByTestId('org-filter-status-resolved'));
    fireEvent.click(screen.getByTestId('org-filter-status-closed'));
    fireEvent.click(screen.getByTestId('org-filter-status-discarded'));
    await waitFor(() => expect(gotQuery).toContain('status=resolved'));
    expect(gotQuery).toContain('status=closed');
    expect(gotQuery).toContain('status=discarded');
  });

  // #258 date-range filter (PR #224): setting a Created-after date must refetch
  // with created_after carrying the viewer's LOCAL offset — NOT a bare date, NOT
  // UTC midnight (the off-by-one 命门). Clear resets it.
  it('FilterBar: a Created-after date sends created_after as an RFC3339 LOCAL-offset instant; Clear resets it', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/issues', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [issueRow()], total: 1 });
      }),
    );
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    expect(gotQuery).toBe(''); // default: no params

    fireEvent.change(screen.getByTestId('org-filter-created-after'), {
      target: { value: '2026-06-08' },
    });

    await waitFor(() => expect(gotQuery).toContain('created_after='));
    const created = new URLSearchParams(gotQuery).get('created_after')!;
    // local start-of-day with a [+-]HH:MM offset — NOT a bare date, NOT Z.
    expect(created).toMatch(/^2026-06-08T00:00:00[+-]\d{2}:\d{2}$/);
    expect(created).not.toMatch(/Z$/);
    expect(created).not.toBe('2026-06-08');
    // matches the runtime local offset.
    const offMin = -new Date(2026, 5, 8, 0, 0, 0).getTimezoneOffset();
    const sign = offMin >= 0 ? '+' : '-';
    const abs = Math.abs(offMin);
    const expectedOffset = `${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`;
    expect(created.endsWith(expectedOffset)).toBe(true);

    // Clear resets the date → param dropped on refetch.
    fireEvent.click(screen.getByTestId('org-filter-clear'));
    await waitFor(() => expect(gotQuery).not.toContain('created_after'));
    expect((screen.getByTestId('org-filter-created-after') as HTMLInputElement).value).toBe('');
  });

  // Project picker (multi) — mirrors the status multi-param test: each selected
  // project id is appended as a repeated `project=<id>` param (OR backend-side).
  it('FilterBar: selecting projects passes them as the multi project filter', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/issues', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [issueRow()], total: 1 });
      }),
      http.get('/api/projects', () =>
        HttpResponse.json({
          projects: [
            { id: 'proj-a', name: 'Apollo' },
            { id: 'proj-b', name: 'Beacon' },
          ],
        }),
      ),
    );
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    expect(gotQuery).toBe(''); // default: no params
    // group label + per-option testids present.
    expect(screen.getByTestId('org-filter-project')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('org-filter-project-proj-a'));
    fireEvent.click(screen.getByTestId('org-filter-project-proj-b'));
    await waitFor(() => expect(gotQuery).toContain('project=proj-a'));
    expect(gotQuery).toContain('project=proj-b');
    // repeated multi-value param shape (same as status).
    expect(new URLSearchParams(gotQuery).getAll('project')).toEqual(['proj-a', 'proj-b']);
  });

  // Assignee picker (single) — sends the prefixed identity ref ("<kind>:<id>").
  // The member fixture carries a BARE identity_id + kind; the picker builds the ref.
  it('FilterBar: selecting an assignee sends assignee=<prefixed-ref>; options show the kind cue', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/issues', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [issueRow()], total: 1 });
      }),
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'mem-1', organization_id: 'org-test', identity_id: 'user-ann',
            kind: 'user', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
            display_name: 'Ann',
          },
          {
            id: 'mem-2', organization_id: 'org-test', identity_id: 'agent-bot9',
            kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
            display_name: 'Bot Nine',
          },
        ]),
      ),
    );
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    const select = screen.getByTestId('org-filter-assignee') as HTMLSelectElement;
    // kind cue text (not color-only): agent option reads "· agent", user "· user".
    const opts = Array.from(select.options).map((o) => o.textContent);
    expect(opts).toContain('Any');
    expect(opts).toContain('Ann · user');
    expect(opts).toContain('Bot Nine · agent');
    // selecting the agent sends the prefixed ref.
    fireEvent.change(select, { target: { value: 'agent:agent-bot9' } });
    await waitFor(() => expect(gotQuery).toContain('assignee=agent%3Aagent-bot9'));
    expect(new URLSearchParams(gotQuery).get('assignee')).toBe('agent:agent-bot9');
  });

  // Clear-all must reset EVERY filter (status + project + assignee + dates), not
  // just one — all params drop and the inputs reset.
  it('FilterBar: Clear-all resets status + project + assignee + date params and inputs', async () => {
    let gotQuery = '';
    server.use(
      http.get('/api/issues', ({ request }) => {
        gotQuery = new URL(request.url).search;
        return HttpResponse.json({ items: [issueRow()], total: 1 });
      }),
      http.get('/api/projects', () =>
        HttpResponse.json({ projects: [{ id: 'proj-a', name: 'Apollo' }] }),
      ),
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'mem-2', organization_id: 'org-test', identity_id: 'agent-bot9',
            kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
            display_name: 'Bot Nine',
          },
        ]),
      ),
    );
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    // set all four filter kinds.
    fireEvent.click(screen.getByTestId('org-filter-status-closed'));
    fireEvent.click(screen.getByTestId('org-filter-project-proj-a'));
    fireEvent.change(screen.getByTestId('org-filter-assignee'), { target: { value: 'agent:agent-bot9' } });
    fireEvent.change(screen.getByTestId('org-filter-created-after'), { target: { value: '2026-06-08' } });
    await waitFor(() => {
      expect(gotQuery).toContain('status=closed');
      expect(gotQuery).toContain('project=proj-a');
      expect(gotQuery).toContain('assignee=');
      expect(gotQuery).toContain('created_after=');
    });
    // Clear-all → every param drops.
    fireEvent.click(screen.getByTestId('org-filter-clear'));
    await waitFor(() => expect(gotQuery).toBe(''));
    // inputs reset.
    expect((screen.getByTestId('org-filter-assignee') as HTMLSelectElement).value).toBe('');
    expect((screen.getByTestId('org-filter-created-after') as HTMLInputElement).value).toBe('');
    expect(screen.getByTestId('org-filter-status-closed')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('org-filter-project-proj-a')).toHaveAttribute('aria-pressed', 'false');
  });

  it('renders a Created column with the created date', async () => {
    server.use(http.get('/api/issues', () => HttpResponse.json({ items: [issueRow()], total: 1 })));
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    expect(screen.getByTestId('org-workitem-created')).toBeInTheDocument();
  });

  it('Create button opens the cross-project create modal with a project picker', async () => {
    server.use(
      http.get('/api/issues', () => HttpResponse.json({ items: [issueRow()], total: 1 })),
      http.get('/api/projects', () => HttpResponse.json({ projects: [{ id: 'proj-a', name: 'Alpha' }] })),
    );
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    expect(screen.queryByTestId('org-create-modal')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('org-workitems-create'));
    expect(screen.getByTestId('org-create-modal')).toBeInTheDocument();
    expect(screen.getByTestId('org-create-project-select')).toBeInTheDocument();
  });

  it('tasks: hits the tasks endpoint + empty state', async () => {
    let hit = false;
    server.use(
      http.get('/api/tasks', () => {
        hit = true;
        return HttpResponse.json({ items: [], total: 0 });
      }),
    );
    wrap('task', '/organizations/acme/tasks');
    await waitFor(() => expect(screen.getByTestId('org-workitems-empty')).toBeInTheDocument());
    expect(hit).toBe(true);
    expect(screen.getByTestId('org-workitems-empty')).toHaveTextContent(/No open tasks/i);
  });
});

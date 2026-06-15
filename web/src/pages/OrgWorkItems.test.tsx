import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import OrgWorkItemsPage from './OrgWorkItems';
import { useContextPanelController } from '@/shell/contextPanel';
import type { OrgWorkItemKind } from '@/api/orgWorkItems';
import type React from 'react';

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
    // v2.10.1 [M3]: both table + mobile card render a StatusChip (dual-render),
    // so scope to the table row to disambiguate the duplicate testid.
    expect(
      within(screen.getByTestId('org-workitem-row')).getByTestId('status-chip'),
    ).toHaveAttribute('data-status', 'in_progress');
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
    expect(
      within(screen.getByTestId('org-workitem-row')).getByTestId('status-chip'),
    ).toHaveAttribute('data-status', 'running');
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

  // REV4 mockup: status chips are toggle buttons. SELECTED = solid REV4 fill +
  // white text + aria-pressed=true + NO dot. UNSELECTED = light bg + a REV4 color
  // dot (●) + aria-pressed=false. Distinguished by FILL + dot + aria, not color
  // alone (a11y). 'open' → sky-600.
  it('FilterBar: a status chip toggles solid-fill (selected) vs light+dot (unselected)', async () => {
    server.use(http.get('/api/issues', () => HttpResponse.json({ items: [issueRow()], total: 1 })));
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    const chip = screen.getByTestId('org-filter-status-open');
    // unselected: aria-pressed=false, light bg (no solid fill), shows a color dot.
    expect(chip).toHaveAttribute('aria-pressed', 'false');
    expect(chip.className).not.toContain('bg-status-sky-solid');
    expect(chip.querySelector('span[aria-hidden="true"]')?.className).toContain('bg-status-sky-solid');
    // toggle on → aria-pressed=true, solid sky-600 fill + white text, dot gone.
    fireEvent.click(chip);
    expect(chip).toHaveAttribute('aria-pressed', 'true');
    expect(chip.className).toContain('bg-status-sky-solid');
    expect(chip.className).toContain('text-white');
    expect(chip.querySelector('span[aria-hidden="true"]')).toBeNull();
  });

  // Spec point 6: each date input carries lang="en" so the native picker shows
  // the `yyyy-mm-dd` placeholder, NOT the viewer's locale (年/月/日).
  it('FilterBar: every date input has lang="en"', async () => {
    server.use(http.get('/api/issues', () => HttpResponse.json({ items: [issueRow()], total: 1 })));
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    for (const id of [
      'org-filter-created-after',
      'org-filter-created-before',
      'org-filter-updated-after',
      'org-filter-updated-before',
    ]) {
      expect(screen.getByTestId(id)).toHaveAttribute('lang', 'en');
    }
  });

  // Spec point 7: Clear is ALWAYS rendered (not conditionally hidden), with an
  // ASCII x glyph (× / U+00D7, not an emoji) + "Clear filters" text. With no
  // active filter it is present (disabled); once a filter is set it enables.
  it('FilterBar: Clear filters is always rendered (ASCII x + text)', async () => {
    server.use(http.get('/api/issues', () => HttpResponse.json({ items: [issueRow()], total: 1 })));
    wrap('issue', '/organizations/acme/issues');
    await screen.findByTestId('org-workitem-row');
    const clear = screen.getByTestId('org-filter-clear');
    expect(clear).toBeInTheDocument();
    expect(clear).toHaveTextContent('×'); // × multiplication sign (ASCII-x glyph, not emoji)
    expect(clear).toHaveTextContent('Clear filters');
    // no filter yet → disabled (but still in the DOM).
    expect(clear).toBeDisabled();
    // set a status → enabled.
    fireEvent.click(screen.getByTestId('org-filter-status-open'));
    await waitFor(() => expect(clear).not.toBeDisabled());
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

  // Project picker is now a SINGLE <select> (mockup): "All projects" + each
  // project. Picking one sends exactly one `project=<id>` param; re-picking
  // another REPLACES it (single-value, not accumulating). "All projects" clears.
  it('FilterBar: project single-select sends one project=<id>; switching replaces it; All clears', async () => {
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
    const select = await screen.findByTestId('org-filter-project');
    // options: "All projects" + each project name.
    const opts = Array.from((select as HTMLSelectElement).options).map((o) => o.textContent);
    expect(opts).toEqual(['All projects', 'Apollo', 'Beacon']);
    // pick Apollo → exactly one param.
    fireEvent.change(select, { target: { value: 'proj-a' } });
    await waitFor(() => expect(gotQuery).toContain('project=proj-a'));
    expect(new URLSearchParams(gotQuery).getAll('project')).toEqual(['proj-a']);
    // switch to Beacon → replaces (still a single value).
    fireEvent.change(select, { target: { value: 'proj-b' } });
    await waitFor(() => expect(new URLSearchParams(gotQuery).getAll('project')).toEqual(['proj-b']));
    // "All projects" → param dropped.
    fireEvent.change(select, { target: { value: '' } });
    await waitFor(() => expect(gotQuery).not.toContain('project='));
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
    fireEvent.change(screen.getByTestId('org-filter-project'), { target: { value: 'proj-a' } });
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
    expect((screen.getByTestId('org-filter-project') as HTMLSelectElement).value).toBe('');
    expect((screen.getByTestId('org-filter-assignee') as HTMLSelectElement).value).toBe('');
    expect((screen.getByTestId('org-filter-created-after') as HTMLInputElement).value).toBe('');
    expect(screen.getByTestId('org-filter-status-closed')).toHaveAttribute('aria-pressed', 'false');
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

// v2.10.0 [T3] — col④ read-only metadata panel. Renders the page inside a
// minimal shell harness that supplies the ContextPanel provider + host (the
// real AppLayout role), so a selected row's <ContextPanel> portals into col④.
function ShellHarness({ children }: { children: React.ReactNode }): React.ReactElement {
  const { Provider, value, setHost, open } = useContextPanelController();
  return (
    <Provider value={value}>
      {children}
      <aside data-testid="ctx-col" data-open={open}>
        <div ref={setHost} />
      </aside>
    </Provider>
  );
}

function wrapShell(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <ShellHarness>
          <Routes>
            <Route path="/organizations/:slug/issues" element={<OrgWorkItemsPage kind="issue" />} />
            <Route path="/organizations/:slug/tasks" element={<OrgWorkItemsPage kind="task" />} />
          </Routes>
        </ShellHarness>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('OrgWorkItems col④ metadata panel (v2.10.0 [T3])', () => {
  afterEach(() => cleanup());

  it('selecting a task row opens the read-only metadata panel in col④', async () => {
    server.use(
      http.get('/api/tasks', () => HttpResponse.json({ items: [taskRow()], total: 1 })),
    );
    wrapShell('/organizations/acme/tasks');
    const row = await screen.findByTestId('org-workitem-row');
    // No panel until a row is selected; col④ is closed.
    expect(screen.queryByTestId('org-workitem-meta-panel')).not.toBeInTheDocument();
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'false');

    fireEvent.click(row);
    const panel = await screen.findByTestId('org-workitem-meta-panel');
    expect(panel).toHaveAttribute('data-id', 'task-01KT8DXYZ789');
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'true');
    expect(screen.getByTestId('org-workitem-row')).toHaveAttribute('aria-selected', 'true');
    // Metadata: status / assignee / project / id + a discussion link.
    expect(panel).toHaveTextContent('running');
    expect(panel).toHaveTextContent('Bot Nine');
    expect(panel).toHaveTextContent('Beacon');
    expect(panel).toHaveTextContent('T34');
    const open = screen.getByTestId('org-workitem-meta-open');
    expect(open.getAttribute('href')).toContain('/projects/proj-b/tasks/task-01KT8DXYZ789');
  });

  it('the close button + re-clicking the row clear the selection (col④ collapses)', async () => {
    server.use(
      http.get('/api/tasks', () => HttpResponse.json({ items: [taskRow()], total: 1 })),
    );
    wrapShell('/organizations/acme/tasks');
    const row = await screen.findByTestId('org-workitem-row');
    fireEvent.click(row);
    await screen.findByTestId('org-workitem-meta-panel');

    // Close button clears it.
    fireEvent.click(screen.getByTestId('org-workitem-meta-close'));
    await waitFor(() =>
      expect(screen.queryByTestId('org-workitem-meta-panel')).not.toBeInTheDocument(),
    );
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'false');

    // Re-clicking the row opens it, clicking again toggles it back off.
    fireEvent.click(screen.getByTestId('org-workitem-row'));
    await screen.findByTestId('org-workitem-meta-panel');
    fireEvent.click(screen.getByTestId('org-workitem-row'));
    await waitFor(() =>
      expect(screen.queryByTestId('org-workitem-meta-panel')).not.toBeInTheDocument(),
    );
  });

  it('issues use the same panel (read-only, unassigned shows —)', async () => {
    server.use(
      http.get('/api/issues', () => HttpResponse.json({ items: [issueRow()], total: 1 })),
    );
    wrapShell('/organizations/acme/issues');
    fireEvent.click(await screen.findByTestId('org-workitem-row'));
    const panel = await screen.findByTestId('org-workitem-meta-panel');
    expect(panel).toHaveTextContent('Issue · metadata');
    expect(panel).toHaveTextContent('in progress');
    expect(panel).toHaveTextContent('Apollo');
    // issue.assignee is always null in the pm domain → em-dash.
    expect(panel).toHaveTextContent('—');
    expect(screen.getByTestId('org-workitem-meta-open').getAttribute('href')).toContain(
      '/projects/proj-a/issues/issue-01KT8DABCDEF',
    );
  });
});

// v2.10.1 [M3] mobile (<md): the wide table reflows to a card flow (md:hidden,
// no horizontal scroll = critical①). jsdom renders both; these specs assert the
// card list mirrors the rows and drives the same selection → col④.
describe('OrgWorkItems mobile card flow (v2.10.1 [M3])', () => {
  afterEach(() => cleanup());

  it('renders a card per work item mirroring the row (id / title link / status)', async () => {
    server.use(
      http.get('/api/tasks', () =>
        HttpResponse.json({ items: [taskRow(), taskRow({ id: 'task-2', org_ref: 'T35', title: 'second' })], total: 2 }),
      ),
    );
    wrap('task', '/organizations/acme/tasks');
    await waitFor(() => expect(screen.getByTestId('org-workitems-cards')).toBeInTheDocument());
    const cards = screen.getAllByTestId('org-workitem-card');
    expect(cards).toHaveLength(2);
    // org_ref (T34) shown, full id on hover.
    expect(within(cards[0]).getByTestId('org-workitem-card-id')).toHaveTextContent('T34');
    expect(within(cards[0]).getByTestId('org-workitem-card-id')).toHaveAttribute('title', 'task-01KT8DXYZ789');
    // title links into the task detail (cross-project path).
    expect(within(cards[0]).getByTestId('org-workitem-card-title').getAttribute('href')).toContain(
      '/projects/proj-b/tasks/task-01KT8DXYZ789',
    );
    // status chip + assignee surface on the card.
    expect(within(cards[0]).getByTestId('status-chip')).toHaveAttribute('data-status', 'running');
    expect(within(cards[0]).getByText('Bot Nine')).toBeInTheDocument();
  });

  it('tapping a card selects it → opens the col④ metadata (M1 reflows to a sheet)', async () => {
    server.use(
      http.get('/api/tasks', () => HttpResponse.json({ items: [taskRow()], total: 1 })),
    );
    wrapShell('/organizations/acme/tasks');
    const card = await screen.findByTestId('org-workitem-card');
    expect(card).toHaveAttribute('aria-selected', 'false');
    fireEvent.click(card);
    const panel = await screen.findByTestId('org-workitem-meta-panel');
    expect(panel).toHaveAttribute('data-id', 'task-01KT8DXYZ789');
    expect(screen.getByTestId('org-workitem-card')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('ctx-col')).toHaveAttribute('data-open', 'true');
  });
});

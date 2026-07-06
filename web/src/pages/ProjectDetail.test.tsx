import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { useAppStore } from '@/store/app';
import ProjectDetail from './ProjectDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/projects/:id" element={<ProjectDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const projectAlpha = {
  id: 'proj-a',
  organization_id: 'org-test',
  name: 'Project Alpha',
  description: 'the alpha project',
  status: 'active',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-20T01:00:00Z',
  updated_at: '2026-05-20T01:00:00Z',
};

describe('ProjectDetail page', () => {
  afterEach(() => cleanup());

  // T566 (issue-577a7b0e): the project-level auto-assign master switch.
  it('T566: auto-assign toggle defaults ON and PATCHes auto_assign_enabled:false when turned off', async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.patch('/api/projects/proj-a', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ...projectAlpha, auto_assign_enabled: false });
      }),
    );
    wrap('/projects/proj-a');
    fireEvent.click(await screen.findByTestId('project-edit-btn'));
    const toggle = await screen.findByTestId('project-edit-auto-assign');
    // absent on the payload ⇒ defaults to ON.
    expect(toggle).toHaveAttribute('aria-checked', 'true');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(screen.getByTestId('project-edit-save'));
    await waitFor(() => expect(body).toBeDefined());
    expect(body).toEqual({ auto_assign_enabled: false });
  });

  it('renders header + per-project Issues / Tasks tabs + Fleet link', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/issues', () =>
        HttpResponse.json({
          issues: [
            {
              id: 'IS-1',
              project_id: 'proj-a',
              title: 'login bug',
              description: '',
              status: 'open',
              created_by: 'user:hayang',
              version: 1,
              created_at: '2026-05-24T01:00:00Z',
              updated_at: '2026-05-24T01:00:00Z',
            },
          ],
        }),
      ),
      http.get('/api/projects/proj-a/tasks', () =>
        HttpResponse.json({
          tasks: [
            {
              id: 'TS-1',
              project_id: 'proj-a',
              title: 'rebuild docs',
              description: '',
              status: 'open',
              version: 1,
              created_at: '2026-05-24T01:00:00Z',
              updated_at: '2026-05-24T01:00:00Z',
            },
          ],
        }),
      ),
    );
    wrap('/projects/proj-a');
    // #238: name appears in both the breadcrumb leaf and the header → scope to heading.
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Project Alpha' })).toBeInTheDocument());
    expect(screen.getByTestId('project-description')).toHaveTextContent('the alpha project');
    expect(screen.getByTestId('project-status-active')).toBeInTheDocument();
    // v2.9 workboard-link-header: the Work Board link lives in the header button
    // cluster alongside Edit/Archive, reads "Work Board", and still navigates to
    // the per-project plans route (§4.2 reachability — testid + href unchanged).
    const plansLink = screen.getByTestId('project-plans-link');
    expect(plansLink).toHaveTextContent('Work Board');
    expect(plansLink).toHaveAttribute('href', '/projects/proj-a/plans');
    const headerCluster = screen.getByTestId('project-edit-btn').parentElement;
    expect(headerCluster).toContainElement(plansLink);
    expect(headerCluster).toContainElement(screen.getByTestId('project-delete-btn'));
    // Issues tab is the default; the issue row shows.
    await waitFor(() => expect(screen.getByText('login bug')).toBeInTheDocument());
    // Switch to the Tasks tab to see the task row.
    fireEvent.click(screen.getByTestId('project-tab-tasks'));
    await waitFor(() => expect(screen.getByText('rebuild docs')).toBeInTheDocument());
    // The Workers/Environment-link panel was removed; the header stats block
    // (counts + recent activity) replaces it.
    expect(screen.queryByTestId('project-fleet-link')).not.toBeInTheDocument();
    expect(screen.getByTestId('project-stats-block')).toBeInTheDocument();
    expect(screen.getByTestId('project-counts-card')).toBeInTheDocument();
    expect(screen.getByTestId('project-activity-card')).toBeInTheDocument();
  });

  it('stats block: shows the project counts + the 3 newest work items (merged, updated_at desc)', async () => {
    server.use(
      http.get('/api/projects/:id', () =>
        HttpResponse.json({ ...projectAlpha, issue_count: 7, task_count: 12, plan_count: 3, repo_count: 2 }),
      ),
      http.get('/api/projects/proj-a/issues', () =>
        HttpResponse.json({
          issues: [
            { id: 'issue-1', project_id: 'proj-a', org_ref: 'I9', title: 'oldest issue', description: '', status: 'open', created_by: 'user:hayang', version: 1, created_at: '2026-06-10T01:00:00Z', updated_at: '2026-06-10T01:00:00Z' },
          ],
          total: 7,
        }),
      ),
      http.get('/api/projects/proj-a/tasks', () =>
        HttpResponse.json({
          tasks: [
            { id: 'task-1', project_id: 'proj-a', org_ref: 'T20', title: 'newest task', description: '', status: 'open', created_by: 'user:hayang', version: 1, created_at: '2026-06-20T01:00:00Z', updated_at: '2026-06-20T01:00:00Z' },
          ],
          total: 12,
        }),
      ),
      http.get('/api/projects/proj-a/plans', () =>
        HttpResponse.json({
          plans: [
            { id: 'plan-1', project_id: 'proj-a', org_ref: 'P5', name: 'middle plan', description: '', status: 'running', creator_ref: 'user:hayang', conversation_id: '', has_failed: false, progress: { done: 1, total: 2 }, created_at: '2026-06-15T01:00:00Z', updated_at: '2026-06-15T01:00:00Z' },
          ],
          total: 3,
        }),
      ),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-counts-card')).toBeInTheDocument());

    // Counts come straight from the project DTO's *_count fields.
    expect(screen.getByTestId('project-stat-issues-value')).toHaveTextContent('7');
    expect(screen.getByTestId('project-stat-tasks-value')).toHaveTextContent('12');
    expect(screen.getByTestId('project-stat-plans-value')).toHaveTextContent('3');
    expect(screen.getByTestId('project-stat-repos-value')).toHaveTextContent('2');
    // The count tiles deep-link to their tab.
    expect(screen.getByTestId('project-stat-tasks')).toHaveAttribute('href', '/projects/proj-a?tab=tasks');

    // Recent activity = the 3 newest-updated items merged across issues/tasks/plans,
    // newest first: task (06-20) → plan (06-15) → issue (06-10).
    await waitFor(() => expect(screen.getAllByTestId('project-activity-row')).toHaveLength(3));
    const rows = screen.getAllByTestId('project-activity-row');
    expect(rows[0]).toHaveTextContent('newest task');
    expect(rows[1]).toHaveTextContent('middle plan');
    expect(rows[2]).toHaveTextContent('oldest issue');
    // The newest row links to its task detail.
    expect(within(rows[0]).getByRole('link')).toHaveAttribute('href', '/projects/proj-a/tasks/task-1');
  });

  it('renders Issues/Tasks as tables: id-tail handle (+hover) / status chip / title link / task assignee+priority (#242)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/issues', () =>
        HttpResponse.json({
          issues: [
            { id: 'issue-01KT8DABCDEF', project_id: 'proj-a', title: 'login bug', description: '', status: 'in_progress', created_by: 'user:hayang', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z' },
          ],
        }),
      ),
      http.get('/api/projects/proj-a/tasks', () =>
        HttpResponse.json({
          tasks: [
            { id: 'task-01KT8DXYZ123', project_id: 'proj-a', title: 'rebuild docs', description: '', status: 'running', blocked_reason: 'waiting on review', assignee: 'agent:bot-9', creator_ref: 'user:owner', plan_id: 'plan-01KT8DPLAN', plan_name: 'Onboarding flow', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z' },
          ],
        }),
      ),
      http.get('/api/members', () =>
        HttpResponse.json([{ identity_id: 'bot-9', display_name: 'Bot Nine', kind: 'agent', status: 'joined' }]),
      ),
    );
    wrap('/projects/proj-a');

    // Issues table (T126): no org_ref → the FULL id (never a #id-tail hash) + full
    // id on hover (#192); colored status chip.
    const issueHandle = await screen.findByTestId('issue-id-handle');
    expect(issueHandle).toHaveTextContent('issue-01KT8DABCDEF');
    expect(issueHandle).toHaveAttribute('title', 'issue-01KT8DABCDEF');
    expect(issueHandle).not.toHaveTextContent('#');
    const issueChip = screen.getByTestId('status-chip');
    expect(issueChip).toHaveAttribute('data-status', 'in_progress');
    // v2.8.1 #5th: StatusChip unified to @oopslink's REVISION 4 white-on-saturated palette (matches StatusBlock).
    expect(issueChip.className).toContain('bg-status-blue-solid');
    expect(issueChip.className).toContain('text-white');
    // Title links into the issue detail.
    const issueLink = screen.getByText('login bug').closest('a');
    expect(issueLink?.getAttribute('href')).toContain('/projects/proj-a/issues/issue-01KT8DABCDEF');

    // Tasks tab: id handle + assignee name (raw ref on hover) + priority fallback.
    fireEvent.click(screen.getByTestId('project-tab-tasks'));
    const taskHandle = await screen.findByTestId('task-id-handle');
    expect(taskHandle).toHaveTextContent('task-01KT8DXYZ123');
    expect(taskHandle).not.toHaveTextContent('#');
    expect(taskHandle).toHaveAttribute('title', 'task-01KT8DXYZ123');
    expect(screen.getByTestId('task-assignee')).toHaveTextContent('Bot Nine');
    expect(screen.getByTestId('task-priority')).toHaveTextContent('—');
    expect(screen.getByTestId('status-chip')).toHaveAttribute('data-status', 'running');
    // Creator column (owner ask): creator_ref resolves to a clean handle when no
    // member row matches — never blank / "—" / the raw "user:" form.
    const creator = screen.getByTestId('task-creator');
    expect(creator).toHaveTextContent('owner');
    expect(creator).not.toHaveTextContent('—');
    expect(creator).toHaveAttribute('title', 'user:owner');
    // PLAN column shows the plan_id (with the plan_name).
    const plan = screen.getByTestId('task-plan');
    expect(plan).toHaveTextContent('Onboarding flow');
    expect(screen.getByTestId('task-plan-id')).toHaveTextContent('plan-01KT8DPLAN');
    // Created/Updated render the FULL date-time WITH timezone (not relative),
    // raw ISO on hover. Compare against the same explicit-component format.
    const fmt = (iso: string) =>
      new Date(iso).toLocaleString(undefined, {
        year: 'numeric', month: 'short', day: 'numeric',
        hour: 'numeric', minute: '2-digit', second: '2-digit', timeZoneName: 'short',
      });
    const created = screen.getByTestId('task-created');
    expect(created).toHaveTextContent(fmt('2026-05-24T01:00:00Z'));
    expect(created.textContent).toMatch(/:\d{2}:\d{2}/);
    expect(created).toHaveAttribute('title', '2026-05-24T01:00:00Z');
    expect(screen.getByTestId('task-updated')).toHaveAttribute('title', '2026-05-24T01:00:00Z');
  });

  it('shows org_ref (T<n>/I<n>) in the ID column when present, hash id on hover (#245)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/issues', () =>
        HttpResponse.json({
          issues: [
            { id: 'issue-01KT8DABCDEF', project_id: 'proj-a', title: 'login bug', description: '', status: 'open', org_ref: 'I42', created_by: 'user:hayang', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z' },
          ],
        }),
      ),
      http.get('/api/projects/proj-a/tasks', () =>
        HttpResponse.json({
          tasks: [
            { id: 'task-01KT8DXYZ123', project_id: 'proj-a', title: 'rebuild docs', description: '', status: 'open', org_ref: 'T7', version: 1, created_at: '2026-05-24T01:00:00Z', updated_at: '2026-05-24T01:00:00Z' },
          ],
        }),
      ),
    );
    wrap('/projects/proj-a');
    const issueHandle = await screen.findByTestId('issue-id-handle');
    expect(issueHandle).toHaveTextContent('I42');
    expect(issueHandle).not.toHaveTextContent('#'); // org_ref replaces the tail handle
    expect(issueHandle).toHaveAttribute('title', 'issue-01KT8DABCDEF'); // hash id on hover (#192)
    fireEvent.click(screen.getByTestId('project-tab-tasks'));
    const taskHandle = await screen.findByTestId('task-id-handle');
    expect(taskHandle).toHaveTextContent('T7');
    expect(taskHandle).toHaveAttribute('title', 'task-01KT8DXYZ123');
  });

  it('shows the per-project empty hint when both panels return []', async () => {
    server.use(
      http.get('/api/projects/:id', () =>
        HttpResponse.json({ ...projectAlpha, id: 'proj-empty', name: 'Empty Project', description: '' }),
      ),
      http.get('/api/projects/proj-empty/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/proj-empty/tasks', () => HttpResponse.json({ tasks: [] })),
    );
    wrap('/projects/proj-empty');
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Empty Project' })).toBeInTheDocument());
    await waitFor(() =>
      expect(screen.getByTestId('project-issues-panel')).toHaveTextContent(/No issues yet/),
    );
    fireEvent.click(screen.getByTestId('project-tab-tasks'));
    await waitFor(() =>
      expect(screen.getByTestId('project-tasks-panel')).toHaveTextContent(/No tasks yet/),
    );
  });

  it('surfaces a 404 with a friendly error + back link', async () => {
    server.use(
      http.get('/api/projects/:id', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such project' }, { status: 404 }),
      ),
    );
    wrap('/projects/ghost');
    await waitFor(() =>
      expect(screen.getByTestId('project-not-found')).toHaveTextContent(/no such project/),
    );
    expect(screen.getByRole('link', { name: /back to projects/i })).toHaveAttribute(
      'href',
      '/projects',
    );
  });

  it('renders a loading skeleton while the project fetch is pending', () => {
    server.use(
      http.get('/api/projects/:id', async () => {
        await new Promise<void>(() => {});
        return HttpResponse.json({});
      }),
    );
    wrap('/projects/proj-a');
    expect(screen.getByTestId('page-ProjectDetail')).toBeInTheDocument();
  });

  it('renders Members + Code repos tabs reading the pm model (#135)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/projects/:pid/members', () =>
        HttpResponse.json({
          members: [
            { id: 'M-1', project_id: 'proj-a', identity_id: 'user:hayang', role: 'owner', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
          ],
        }),
      ),
      http.get('/api/projects/:pid/code-repos', () =>
        HttpResponse.json({
          code_repos: [
            { id: 'R-1', project_id: 'proj-a', url: 'https://example.com/repo.git', label: 'main repo', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
          ],
        }),
      ),
      // T575: the referencer also loads workspace repos to join provider/desc.
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [] })),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());

    fireEvent.click(screen.getByTestId('project-tab-members'));
    await waitFor(() => expect(screen.getByTestId('member-row')).toBeInTheDocument());
    const memberRow = screen.getByTestId('member-row');
    expect(memberRow).toHaveAttribute('data-member-id', 'M-1');
    // #192/#160: an unresolved member (no display_name) shows the CLEAN handle,
    // never the raw "user:hayang" prefixed ref (which stays on title= only).
    const memberRef = within(memberRow).getByTestId('project-member-ref');
    expect(memberRef).toHaveTextContent('hayang');
    expect(memberRef.textContent).not.toContain('user:hayang');
    expect(memberRef).toHaveAttribute('title', 'user:hayang');
    expect(memberRow).toHaveTextContent('owner');

    fireEvent.click(screen.getByTestId('project-tab-repos'));
    await waitFor(() => expect(screen.getByTestId('repo-row')).toBeInTheDocument());
    const repoRow = screen.getByTestId('repo-row');
    expect(repoRow).toHaveAttribute('data-repo-id', 'R-1');
    expect(repoRow).toHaveTextContent('main repo');
  });

  it('T575: Referenced repos panel joins refs to workspace repos (provider/primary) + offers unreferenced repos to add', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/projects/:pid/code-repos', () =>
        HttpResponse.json({
          code_repos: [
            { id: 'ref-1', project_id: 'proj-a', url: '', label: '', added_by: 'user:o', created_at: 'x', repo_id: 'repo-1', is_primary: true },
          ],
        }),
      ),
      http.get('/api/code-repos', () =>
        HttpResponse.json({
          repos: [
            { id: 'repo-1', organization_id: 'org-test', label: 'agent-center', description: 'mono', url: 'git@github.com:o/ac.git', provider: 'github', default_branch: 'main', has_credential: true, created_by: 'user:o', created_at: 'x', updated_at: 'x', version: 1 },
            { id: 'repo-2', organization_id: 'org-test', label: 'infra', description: '', url: 'https://git/infra.git', provider: 'git', default_branch: 'master', has_credential: false, created_by: 'user:o', created_at: 'x', updated_at: 'x', version: 1 },
          ],
        }),
      ),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('project-tab-repos'));
    const row = await screen.findByTestId('repo-row');
    // joined to workspace repo repo-1: provider badge + label + primary star.
    expect(within(row).getByTestId('repo-provider-badge')).toHaveTextContent(/github/i);
    expect(row).toHaveTextContent('agent-center');
    expect(within(row).getByTestId('repo-row-primary')).toHaveAttribute('data-primary', 'true');
    // add-selector offers only the NOT-yet-referenced repo (infra), not repo-1.
    const select = screen.getByTestId('project-repos-add-select') as HTMLSelectElement;
    expect(within(select).getByRole('option', { name: 'infra' })).toBeInTheDocument();
    expect(within(select).queryByRole('option', { name: 'agent-center' })).toBeNull();
  });

  it('renders a Plans tab (after Tasks) listing the project plans (per @oopslink)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      // T302: the Plans panel hits the project plans endpoint with page params →
      // SQL-paginated { plans, total } (builtin excluded server-side).
      http.get('/api/projects/:pid/plans', () =>
        HttpResponse.json({
          plans: [
            {
              id: 'plan-1', project_id: 'proj-a', name: 'Sprint One', description: '',
              status: 'running', creator_ref: 'user:hayang', conversation_id: 'c-1',
              org_ref: 'P7', has_failed: false, progress: { done: 2, total: 5 },
              created_at: '2026-05-20T01:00:00Z',
            },
          ],
          total: 1,
        }),
      ),
      // Owner ask: the Creator cell shows the member's NAME, not the raw id —
      // so the resolver must see the creator as a member with a display name.
      http.get('/api/members', () =>
        HttpResponse.json([{ identity_id: 'hayang', display_name: 'Ha Yang', kind: 'user', status: 'joined' }]),
      ),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());

    // The Plans tab sits after Tasks, before Members.
    expect(screen.getByTestId('project-tab-plans')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('project-tab-plans'));
    await waitFor(() => expect(screen.getByTestId('plan-row')).toBeInTheDocument());
    const row = screen.getByTestId('plan-row');
    expect(row).toHaveAttribute('data-plan-id', 'plan-1');
    expect(within(row).getByTestId('plan-id-handle')).toHaveTextContent('P7');
    expect(row).toHaveTextContent('Sprint One');
    // Creator resolves to the display NAME ("Ha Yang"), never the raw "hayang"
    // handle or the "user:hayang" ref.
    await waitFor(() => expect(row).toHaveTextContent('Ha Yang'));
    expect(row).not.toHaveTextContent('user:hayang');
  });

  // Owner ask (2026-07-03): the Plans list gains a status + name-search filter.
  // These assert the UI wires status[]/q into the (already-supporting) endpoint.
  it('Plans filter: status chip sends ?status=, search sends ?q=, and both reset to page 1', async () => {
    const searches: string[] = [];
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/projects/:pid/plans', ({ request }) => {
        searches.push(new URL(request.url).search);
        return HttpResponse.json({
          plans: [
            {
              id: 'plan-1', project_id: 'proj-a', name: 'Sprint One', description: '',
              status: 'running', creator_ref: 'user:hayang', conversation_id: 'c-1',
              org_ref: 'P7', has_failed: false, progress: { done: 2, total: 5 },
              created_at: '2026-05-20T01:00:00Z',
            },
          ],
          total: 1,
        });
      }),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('project-tab-plans'));
    await waitFor(() => expect(screen.getByTestId('plan-row')).toBeInTheDocument());
    // Initial request carries neither status nor q.
    expect(searches[0]).not.toContain('status=');
    expect(searches[0]).not.toContain('q=');

    // Click the "running" status chip → the next request filters by status=running.
    fireEvent.click(screen.getByTestId('plan-filter-status-running'));
    await waitFor(() => expect(searches.some((s) => s.includes('status=running'))).toBe(true));
    // Page did NOT get pinned to a stale value: a status change resets to page 1
    // (no page=… param, which is only sent for page>1).
    const afterStatus = searches[searches.length - 1];
    expect(afterStatus).not.toContain('page=');

    // Type into the search box → the request carries the q term.
    fireEvent.change(screen.getByTestId('plan-filter-search'), { target: { value: 'sprint' } });
    await waitFor(() => expect(searches.some((s) => s.includes('q=sprint'))).toBe(true));

    // Clear resets both filters → back to an unfiltered request.
    fireEvent.click(screen.getByTestId('plan-filter-clear'));
    await waitFor(() => {
      const last = searches[searches.length - 1];
      return expect(!last.includes('status=') && !last.includes('q=')).toBe(true);
    });
  });

  it('Plans filter: an empty filtered result shows the "no match" copy, not the generic empty', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/projects/:pid/plans', ({ request }) => {
        // Unfiltered → one plan; any status/q filter → empty.
        const s = new URL(request.url).search;
        if (s.includes('status=') || s.includes('q=')) return HttpResponse.json({ plans: [], total: 0 });
        return HttpResponse.json({
          plans: [
            {
              id: 'plan-1', project_id: 'proj-a', name: 'Sprint One', description: '',
              status: 'running', creator_ref: 'user:hayang', conversation_id: 'c-1',
              org_ref: 'P7', has_failed: false, progress: { done: 2, total: 5 },
              created_at: '2026-05-20T01:00:00Z',
            },
          ],
          total: 1,
        });
      }),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('project-tab-plans'));
    await waitFor(() => expect(screen.getByTestId('plan-row')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('plan-filter-status-draft'));
    await waitFor(() => expect(screen.getByTestId('project-plans-empty')).toBeInTheDocument());
    expect(screen.getByTestId('project-plans-empty')).toHaveTextContent('No plans match the filter');
  });

  it('member name links to its detail page (agent → AgentDetail, human → user page)', async () => {
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/projects/:pid/members', () =>
        HttpResponse.json({
          members: [
            { id: 'M-1', project_id: 'proj-a', identity_id: 'user:hayang', role: 'owner', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
            { id: 'M-2', project_id: 'proj-a', identity_id: 'agent:agent-xyz', role: 'member', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
          ],
        }),
      ),
      // The execution Agent carries identity_member_id == the member identity_id;
      // the AgentDetail link uses the Agent's own id (distinct here to prove the map).
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [{ id: 'agent-exec-1', identity_member_id: 'agent:agent-xyz', display_name: 'Builder', status: 'joined' }],
        }),
      ),
      http.get('/api/members', () =>
        HttpResponse.json([
          { identity_id: 'hayang', display_name: 'Ha Yang', kind: 'user', status: 'joined' },
          { identity_id: 'agent-xyz', display_name: 'Builder', kind: 'agent', status: 'joined' },
        ]),
      ),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());

    fireEvent.click(screen.getByTestId('project-tab-members'));
    await waitFor(() => expect(screen.getAllByTestId('member-row').length).toBe(2));
    const rows = screen.getAllByTestId('member-row');
    const human = rows.find((r) => r.getAttribute('data-member-id') === 'M-1');
    const agent = rows.find((r) => r.getAttribute('data-member-id') === 'M-2');

    // Human member → /users/<bare identity>.
    const humanRef = within(human as HTMLElement).getByTestId('project-member-ref');
    expect(humanRef.tagName).toBe('A');
    expect(humanRef).toHaveAttribute('href', '/users/hayang');

    // Agent member → /agents/<execution-agent id> (resolved via identity_member_id).
    const agentRef = within(agent as HTMLElement).getByTestId('project-member-ref');
    expect(agentRef.tagName).toBe('A');
    expect(agentRef).toHaveAttribute('href', '/agents/agent-exec-1');
  });

  // T131: the per-project Task/Issue lists reuse the global FilterBar with the
  // project dimension FIXED — the Project picker is hidden, every other filter is
  // open, and the selections are sent as query params to the project-scoped list.
  it('T131: project Tasks list reuses the FilterBar (no Project picker) + sends status/assignee params', async () => {
    const taskUrls: string[] = [];
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/proj-a/tasks', ({ request }) => {
        taskUrls.push(request.url);
        return HttpResponse.json({ tasks: [] });
      }),
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/projects', () => HttpResponse.json({ projects: [] })),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Project Alpha' })).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('project-tab-tasks'));

    // The shared FilterBar renders with status chips + assignee, but the Project
    // picker is hidden (the project is fixed by the page).
    await waitFor(() => expect(screen.getByTestId('org-workitems-filterbar')).toBeInTheDocument());
    expect(screen.getByTestId('org-filter-status-running')).toBeInTheDocument();
    expect(screen.getByTestId('org-filter-assignee')).toBeInTheDocument();
    expect(screen.queryByTestId('org-filter-project')).not.toBeInTheDocument();

    // Selecting a status sends ?status=running to the PROJECT-scoped endpoint.
    fireEvent.click(screen.getByTestId('org-filter-status-running'));
    await waitFor(() => expect(taskUrls.some((u) => u.includes('status=running'))).toBe(true));
  });

  it('T131: project Issues list reuses the FilterBar (no Project picker) + sends status param', async () => {
    const issueUrls: string[] = [];
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/proj-a/issues', ({ request }) => {
        issueUrls.push(request.url);
        return HttpResponse.json({ issues: [] });
      }),
      http.get('/api/projects/proj-a/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/projects', () => HttpResponse.json({ projects: [] })),
    );
    wrap('/projects/proj-a');
    // Issues is the default tab.
    await waitFor(() => expect(screen.getByTestId('org-workitems-filterbar')).toBeInTheDocument());
    expect(screen.getByTestId('org-filter-status-resolved')).toBeInTheDocument();
    expect(screen.queryByTestId('org-filter-project')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('org-filter-status-resolved'));
    await waitFor(() => expect(issueUrls.some((u) => u.includes('status=resolved'))).toBe(true));
  });

  it('owner sees Add + can Remove a non-owner member (#207)', async () => {
    useAppStore.setState({ currentUserId: 'user:hayang' });
    let removed: string | null = null;
    server.use(
      http.get('/api/projects/:id', () => HttpResponse.json(projectAlpha)),
      http.get('/api/projects/:pid/issues', () => HttpResponse.json({ issues: [] })),
      http.get('/api/projects/:pid/tasks', () => HttpResponse.json({ tasks: [] })),
      http.get('/api/projects/:pid/code-repos', () => HttpResponse.json({ code_repos: [] })),
      http.get('/api/code-repos', () => HttpResponse.json({ repos: [] })),
      http.get('/api/projects/:pid/members', () =>
        HttpResponse.json({
          members: [
            { id: 'M-1', project_id: 'proj-a', identity_id: 'user:hayang', role: 'owner', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
            { id: 'M-2', project_id: 'proj-a', identity_id: 'user:bob', role: 'member', added_by: 'user:hayang', created_at: '2026-05-20T01:00:00Z' },
          ],
        }),
      ),
      http.delete('/api/projects/proj-a/members/:identity', ({ params }) => {
        removed = String(params.identity);
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap('/projects/proj-a');
    await waitFor(() => expect(screen.getByTestId('project-work-tabs')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('project-tab-members'));
    await waitFor(() => expect(screen.getAllByTestId('member-row')).toHaveLength(2));

    // Owner sees the Add button; only the non-owner row exposes Remove.
    expect(screen.getByTestId('project-add-member-button')).toBeInTheDocument();
    const removeButtons = screen.getAllByTestId('project-member-remove');
    expect(removeButtons).toHaveLength(1);
    expect(removeButtons[0]).toHaveAttribute('data-identity', 'user:bob');

    fireEvent.click(removeButtons[0]);
    await screen.findByTestId('confirm-modal');
    await act(async () => {
      fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    });
    await waitFor(() => expect(removed).toMatch(/bob/));
  });
});

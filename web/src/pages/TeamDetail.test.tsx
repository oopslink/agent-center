import { beforeEach, describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import TeamDetail from './TeamDetail';
import { OrgContext } from '@/OrgContext';
import { resetTeamsStore } from '@/api/teamsFixtures';
import { server } from '@/test/mswServer';

function Loc(): React.ReactElement {
  const l = useLocation();
  return <div data-testid="loc">{l.pathname}</div>;
}

function teamDetail(overrides: Record<string, unknown> = {}) {
  return {
    id: 'team-7c19b0',
    org_id: 'org-ooo',
    name: 'agent-center core',
    description: '',
    roles: [],
    version: 3,
    glyph: 'AC',
    status: 'active',
    members_count: 0,
    projects_count: 0,
    created: '2026/6/12',
    ...overrides,
  };
}

function renderAt(id: string, orgRole?: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const routes = (
    <MemoryRouter initialEntries={[`/teams/${id}`]}>
      <Routes>
        <Route path="/teams/:teamId" element={<TeamDetail />} />
        <Route path="/teams" element={<Loc />} />
      </Routes>
    </MemoryRouter>
  );
  return render(
    <QueryClientProvider client={qc}>
      {orgRole ? (
        <OrgContext.Provider value={{ slug: 'test', orgId: 'org-test', orgName: 'Test Org', role: orgRole }}>
          {routes}
        </OrgContext.Provider>
      ) : routes}
    </QueryClientProvider>,
  );
}

describe('TeamDetail', () => {
  beforeEach(() => resetTeamsStore());

  it('shows the overview tab by default with role definitions', async () => {
    renderAt('team-7c19b0');
    expect(await screen.findByRole('heading', { name: 'agent-center core' })).toBeInTheDocument();
    expect(screen.getByText('Role definitions')).toBeInTheDocument();
    expect(screen.queryByText(/planner×/)).not.toBeInTheDocument();
    expect(screen.getByText('Team overview')).toBeInTheDocument();
  });

  it('edits and saves role definitions from the overview', async () => {
    let body: { roles?: Array<{ role: string }> } | undefined;
    server.use(http.patch('/api/teams/:id', async ({ request }) => {
      body = await request.json() as typeof body;
      return HttpResponse.json({
        id: 'team-7c19b0', org_id: 'org-ooo', name: 'agent-center core', description: '',
        roles: [], version: 2, glyph: 'AC', status: 'draft', members_count: 0,
        projects_count: 0, created: '2026-07-12',
      });
    }));
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('team-edit-roles'));
    const modal = await screen.findByTestId('edit-team-roles-modal');
    expect(within(modal).queryByTestId('edit-team-role-0-count')).not.toBeInTheDocument();
    expect(within(modal).getAllByText('Max tasks / agent').length).toBeGreaterThan(0);
    expect(within(modal).getAllByText('Per-agent task concurrency, not role headcount.').length).toBeGreaterThan(0);
    while (within(modal).queryAllByText('Remove').length > 0) {
      const before = within(modal).getAllByText('Remove').length;
      fireEvent.click(within(modal).getAllByText('Remove')[0]);
      await waitFor(() => expect(within(modal).queryAllByText('Remove')).toHaveLength(before - 1));
    }
    fireEvent.click(within(modal).getByTestId('team-save-roles'));
    await waitFor(() => expect(body).toEqual({ roles: [] }));
    await waitFor(() => expect(screen.queryByTestId('edit-team-roles-modal')).not.toBeInTheDocument());
  });

  it('renders an error for an unknown team', async () => {
    renderAt('team-does-not-exist');
    expect(await screen.findByTestId('team-detail-error')).toHaveTextContent('team_not_found');
  });

  it('switches to the Members tab and lists seeded members', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    const table = await screen.findByTestId('members-table');
    expect(table).toBeInTheDocument();
    expect(within(table).getByText('Capabilities')).toBeInTheDocument();
    expect(within(table).queryByText('Tags')).not.toBeInTheDocument();
    expect(screen.getByText('planner-01')).toBeInTheDocument();
    expect(screen.getByTestId('members-exclusivity-note')).toBeInTheDocument();
  });

  it('keeps long role capabilities compact in the members table', async () => {
    server.use(http.get('/api/teams/:id/members', () => HttpResponse.json([{
      team_id: 'team-7c19b0',
      member_ref: 'agent:agent-long',
      name: 'long-cap-agent',
      kind: 'agent',
      role: 'PD, PM',
      roles: ['PD', 'PM'],
      tags: ['产品设计，负责将需求转化为交互方案', 'UI', '用户研究', '需求优先级排序', 'PRD'],
      cli: 'codex',
      model: 'gpt-5',
      concurrency: '3',
      exclusive: true,
    }])));
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    const row = await screen.findByTestId('member-row-agent:agent-long');
    expect(within(row).getByText('产品设计，负责将需求转化为交互方案')).toBeInTheDocument();
    expect(within(row).getByText('UI')).toBeInTheDocument();
    expect(within(row).getByText('用户研究')).toBeInTheDocument();
    expect(within(row).getByText('+2')).toHaveAttribute('title', '需求优先级排序 / PRD');
    expect(within(row).queryByText('需求优先级排序')).not.toBeInTheDocument();
  });

  it('adds a free agent (real directory ref) through the add-member modal', async () => {
    let body: Record<string, unknown> | undefined;
    server.use(http.post('/api/teams/:id/members', async ({ request }) => {
      body = (await request.json()) as Record<string, unknown>;
      return HttpResponse.json({
        team_id: 'team-7c19b0',
        member_ref: 'agent:agent-d5',
        name: 'agent-center-dev5',
        kind: 'agent',
        role: 'planner, coder',
        roles: ['planner', 'coder'],
        tags: [],
        cli: 'claude-code',
        model: 'sonnet-5',
        concurrency: '1 / 2',
        exclusive: false,
      }, { status: 201 });
    }));
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-add'));
    const modal = await screen.findByTestId('add-member-modal');
    // pick a real agent not on any team (free) → direct add, canonical ref
    fireEvent.change(await within(modal).findByTestId('add-member-agent'), { target: { value: 'agent:agent-d5' } });
    fireEvent.click(within(modal).getByTestId('add-member-role-picker-trigger'));
    fireEvent.click(await screen.findByRole('option', { name: 'coder' }));
    fireEvent.click(within(modal).getByTestId('add-member-submit'));
    await waitFor(() => expect(body?.roles).toEqual(['planner', 'coder']));
    expect(body?.role).toBe('planner');
    await waitFor(() => expect(screen.queryByTestId('add-member-modal')).not.toBeInTheDocument());
  });

  it('requires a migration confirm for an agent already on another team', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-add'));
    const modal = await screen.findByTestId('add-member-modal');
    // tester2 is on growth-experiments → migration confirm
    fireEvent.change(await within(modal).findByTestId('add-member-agent'), { target: { value: 'agent:agent-t2' } });
    fireEvent.click(within(modal).getByTestId('add-member-submit'));
    const migrate = await screen.findByTestId('migrate-modal');
    fireEvent.click(within(migrate).getByTestId('migrate-confirm'));
    await waitFor(() => expect(screen.getByText('agent-center-tester2')).toBeInTheDocument());
  });

  it('sends the canonical team id as migrate_from, even when the source team was renamed', async () => {
    // The directory reports each agent's team as an id + a name. migrate_from must
    // come from the ID: deriving it by matching the NAME against the teams list
    // breaks the moment a team is renamed between the two fetches — the old code
    // then resolved to no id at all, dropping migrate_from and turning a confirmed
    // migration into a plain add.
    //
    // Simulate exactly that skew: the teams list carries the POST-rename name while
    // the directory entry still carries the pre-rename one. Only an id-keyed
    // migrate_from survives.
    let body: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/teams', () =>
        HttpResponse.json([
          { id: 'team-4a1f22', org_id: 'org-ooo', name: 'growth-experiments-RENAMED', glyph: 'GX', description: '', roles: [], members_count: 0, projects_count: 0, created_at: '' },
        ]),
      ),
      http.post('/api/teams/:id/members', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ ok: true });
      }),
    );
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-add'));
    const modal = await screen.findByTestId('add-member-modal');
    // tester2 is on growth-experiments (team-4a1f22) → migration confirm
    fireEvent.change(await within(modal).findByTestId('add-member-agent'), { target: { value: 'agent:agent-t2' } });
    fireEvent.click(within(modal).getByTestId('add-member-submit'));
    const migrate = await screen.findByTestId('migrate-modal');
    fireEvent.click(within(migrate).getByTestId('migrate-confirm'));

    await waitFor(() => expect(body).toBeDefined());
    expect(body?.migrate_from).toBe('team-4a1f22');
  });

  it('shows a friendly error (not silent) when migration fails', async () => {
    // backend rejects the move (e.g. identity vanished) → the modal must surface
    // the error and stay open, never silently swallow the failure.
    server.use(
      http.post('/api/teams/:id/members', () =>
        HttpResponse.json({ error: 'identity_not_found', message: 'gone' }, { status: 404 }),
      ),
    );
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-add'));
    const modal = await screen.findByTestId('add-member-modal');
    fireEvent.change(await within(modal).findByTestId('add-member-agent'), { target: { value: 'agent:agent-t2' } });
    fireEvent.click(within(modal).getByTestId('add-member-submit'));
    const migrate = await screen.findByTestId('migrate-modal');
    fireEvent.click(within(migrate).getByTestId('migrate-confirm'));
    // friendly mapped copy, not the raw envelope; modal stays open
    expect(await screen.findByTestId('migrate-error')).toHaveTextContent('no longer exists');
    expect(screen.getByTestId('migrate-modal')).toBeInTheDocument();
  });

  it('removes a member with the confirm modal', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('member-remove-agent:9a70…'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(screen.queryByText('planner-01')).not.toBeInTheDocument());
  });

  it('associates a real picked project and unlinks a project', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-pj'));
    expect(await screen.findByTestId('assoc-project-c7073e48')).toBeInTheDocument();
    // open the real picker, choose an actual org project (not a fabricated ref)
    fireEvent.click(screen.getByTestId('associate-project'));
    const modal = await screen.findByTestId('associate-project-modal');
    fireEvent.change(await within(modal).findByTestId('associate-project-select'), { target: { value: 'proj-a' } });
    fireEvent.click(within(modal).getByTestId('associate-project-submit'));
    await waitFor(() => expect(screen.getByText('Project Alpha')).toBeInTheDocument());
    expect(screen.getByTestId('assoc-proj-a')).toBeInTheDocument();
    // unlink the seeded project
    fireEvent.click(screen.getByTestId('unlink-project-c7073e48'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(screen.queryByTestId('assoc-project-c7073e48')).not.toBeInTheDocument());
  });

  it('groups team-memory entries and rules with read-only management feedback', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    expect(await screen.findByTestId('memory-pane')).toBeInTheDocument();
    expect(screen.getByTestId('memory-permission')).toHaveAttribute('data-can-manage', 'unavailable');
    expect(screen.getByTestId('memory-permission')).toHaveTextContent('Current service does not provide editing capability');
    expect(screen.getByTestId('memory-manage')).toBeDisabled();
    expect(screen.getByTestId('memory-section-entries')).toBeInTheDocument();
    expect(screen.getByTestId('memory-section-rules')).toBeInTheDocument();
    fireEvent.click(await screen.findByTestId('memory-node-ci-runbook'));
    await waitFor(() => expect(screen.getByTestId('memory-view')).toHaveTextContent('CI/CD runbook'));
    fireEvent.click(screen.getByTestId('memory-filter-rules'));
    const ruleNode = await screen.findByTestId('memory-node-review-conventions');
    expect(ruleNode).toHaveTextContent('rules/review-conventions.md');
    expect(screen.getByTestId('memory-rule-badge-review-conventions')).toHaveTextContent('RULE');
    fireEvent.click(ruleNode);
    await waitFor(() => expect(screen.getByTestId('memory-view')).toHaveTextContent('评审约定'));
    expect(screen.getByTestId('memory-doc-rule-badge')).toHaveTextContent('RULE');
    fireEvent.click(screen.getByTestId('memory-raw-toggle'));
    expect(screen.getByTestId('memory-raw-view')).toHaveTextContent('name: review-conventions');
    expect(screen.getByTestId('memory-raw-view')).not.toHaveTextContent(/(?:kind|type): rule/);
  });

  it('reviews team-memory proposals with repo/status CAS metadata', async () => {
    server.use(http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
      memory_permissions: { web_edit: true, can_manage: true },
    }))));
    renderAt('team-7c19b0', 'owner');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    fireEvent.click(await screen.findByTestId('memory-filter-proposals'));
    const proposalNode = await screen.findByTestId('memory-node-proposal-pending-1');
    expect(proposalNode).toHaveTextContent('proposals/proposal-pending-1.md');
    expect(screen.getByTestId('memory-proposal-badge-proposal-pending-1')).toHaveTextContent('PENDING');

    const detail = await screen.findByTestId('memory-proposal-detail');
    expect(detail).toHaveTextContent('warning ack required');
    expect(screen.getByTestId('memory-doc-meta')).toHaveTextContent('commit-proposal');
    expect(screen.getByTestId('memory-proposal-diff')).toHaveTextContent('+++ entries/deploy-rollback');

    fireEvent.click(screen.getByTestId('memory-proposal-promote'));
    await waitFor(() => expect(screen.getByTestId('memory-proposal-badge-proposal-pending-1')).toHaveTextContent('PROMOTED'));
  });

  it('creates update proposals with target UUID and blob CAS', async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
        memory_permissions: { web_edit: true, can_manage: true },
      }))),
      http.post('/api/teams/:id/memory/proposals', async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({
          team_id: 'team-7c19b0',
          proposal_id: 'proposal-cas',
          status: 'pending',
          repo_commit: 'commit-cas',
        }, { status: 201 });
      }),
    );
    renderAt('team-7c19b0', 'admin');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    fireEvent.click(await screen.findByTestId('memory-manage'));
    const modal = await screen.findByTestId('memory-create-proposal-modal');
    fireEvent.change(within(modal).getByTestId('memory-create-operation'), { target: { value: 'update' } });
    fireEvent.change(within(modal).getByTestId('memory-create-kind'), { target: { value: 'rule' } });
    fireEvent.change(await within(modal).findByTestId('memory-create-target'), { target: { value: 'review-conventions' } });
    fireEvent.change(within(modal).getByTestId('memory-create-description'), { target: { value: 'Tighten review rule' } });
    fireEvent.change(within(modal).getByTestId('memory-create-body'), { target: { value: 'Block on missing tests.' } });
    fireEvent.change(within(modal).getByTestId('memory-create-rationale'), { target: { value: 'Reflect current merge gate.' } });
    fireEvent.click(within(modal).getByTestId('memory-create-proposal-submit'));

    await waitFor(() => expect(body).toBeTruthy());
    expect(body).toMatchObject({
      operation: 'update',
      target_kind: 'rule',
      target: {
        source_path: 'rules/review-conventions-uuid-review.md',
        uuid: 'uuid-review',
        expected_blob_hash: 'blob-review',
      },
      candidate: {
        description: 'Tighten review rule',
        body: 'Block on missing tests.',
      },
      rationale: 'Reflect current merge gate.',
    });
    expect(String(body?.idempotency_key)).toMatch(/^web:/);
  });

  it('does not infer rules from entry names that merely contain rule text', async () => {
    server.use(
      http.get('/api/teams/:id/memory', () => HttpResponse.json([
        { slug: 'MEMORY.md', pinned: true },
        { group: 'entries/' },
        { slug: 'rules-of-thumb' },
        { slug: 'release-rulebook' },
        { slug: 'policy', path: 'team-memory/rules/policy.md' },
      ])),
      http.get('/api/teams/:id/memory/:entry', ({ params }) => HttpResponse.json({
        slug: String(params.entry),
        path: `team-memory/entries/${String(params.entry)}.md`,
        title: String(params.entry),
        frontmatter: null,
        body: String(params.entry),
      })),
    );
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-tm'));

    fireEvent.click(await screen.findByTestId('memory-filter-entries'));
    expect(await screen.findByTestId('memory-node-rules-of-thumb')).toHaveTextContent('entries/rules-of-thumb.md');
    expect(screen.getByTestId('memory-node-release-rulebook')).toHaveTextContent('entries/release-rulebook.md');
    expect(screen.queryByTestId('memory-rule-badge-rules-of-thumb')).not.toBeInTheDocument();
    expect(screen.queryByTestId('memory-rule-badge-release-rulebook')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('memory-filter-rules'));
    expect(await screen.findByTestId('memory-node-policy')).toHaveTextContent('rules/policy.md');
    expect(screen.queryByTestId('memory-node-rules-of-thumb')).not.toBeInTheDocument();
    expect(screen.queryByTestId('memory-node-release-rulebook')).not.toBeInTheDocument();
  });

  it.each(['owner', 'admin'])('derives team-memory manage access for %s when the team capability is exposed', async (role) => {
    server.use(http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
      memory_permissions: { web_edit: true },
    }))));
    renderAt('team-7c19b0', role);
    fireEvent.click(await screen.findByTestId('tab-tm'));
    const permission = await screen.findByTestId('memory-permission');
    expect(permission).toHaveAttribute('data-can-manage', 'true');
    expect(permission).toHaveTextContent(`Your ${role} role can manage team memory.`);
    expect(screen.getByTestId('memory-manage')).not.toBeDisabled();
  });

  it('keeps regular members read-only when team-memory editing exists but their role cannot manage it', async () => {
    server.use(http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
      memory_permissions: { web_edit: true },
    }))));
    renderAt('team-7c19b0', 'member');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    const permission = await screen.findByTestId('memory-permission');
    expect(permission).toHaveAttribute('data-can-manage', 'false');
    expect(permission).toHaveTextContent('Your member role is read-only for team memory');
    expect(screen.getByTestId('memory-manage')).toBeDisabled();
  });

  it('shows an empty state when the rules group has no entries', async () => {
    server.use(http.get('/api/teams/:id/memory', () => HttpResponse.json([
      { slug: 'MEMORY.md', pinned: true },
      { group: 'entries/' },
      { slug: 'ci-runbook' },
      { group: 'rules/' },
    ])));
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    fireEvent.click(await screen.findByTestId('memory-filter-rules'));
    expect(await screen.findByTestId('memory-rules-empty')).toHaveTextContent('No rules yet');
  });

  it('shows team-memory load errors and document permission errors', async () => {
    server.use(http.get('/api/teams/:id/memory', () =>
      HttpResponse.json({ error: 'memory_failed', message: 'git unavailable' }, { status: 500 }),
    ));
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    expect(await screen.findByTestId('memory-index-error')).toHaveTextContent("Couldn't load team memory.");
  });

  it('shows permission feedback when a rules document is forbidden', async () => {
    server.use(
      http.get('/api/teams/:id/memory', () => HttpResponse.json([
        { group: 'rules/' },
        { slug: 'restricted-rule' },
      ])),
      http.get('/api/teams/:id/memory/:entry', () =>
        HttpResponse.json({ error: 'forbidden', message: 'read denied' }, { status: 403 }),
      ),
    );
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    fireEvent.click(await screen.findByTestId('memory-filter-rules'));
    expect(await screen.findByTestId('memory-doc-error')).toHaveTextContent("You don't have permission");
  });
});

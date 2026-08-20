import { beforeEach, describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
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

  it('selects RAM roles with CAS, saves declaration keys, and reloads server-read sources', async () => {
    let body: { roles?: Array<{ role: string; ram_role_keys?: string[]; access_requirements?: string[] }> } | undefined;
    let previewBody: { ram_role_ids?: string[] } | undefined;
    let putBody: { ram_role_ids?: string[]; expected_version?: number } | undefined;
    let getCount = 0;
    let serverTeam = teamDetail({
        roles: [{
          role: 'planner',
          cli: 'claude-code',
          model: 'claude-opus-4-8',
          capability_tags: [],
          ram_role_keys: ['Team basic'],
          access_requirements: ['team.read', 'team.memory.read'],
          max_concurrency: 1,
          count: 0,
        }],
      });
    let mapping = {
      team_id: 'team-7c19b0',
      team_role: 'planner',
      ram_role_ids: ['team-basic'],
      version: 1,
    };
    server.use(
      http.get('/api/teams/:id', () => {
        getCount += 1;
        return HttpResponse.json(serverTeam);
      }),
      http.patch('/api/teams/:id', async ({ request }) => {
        body = await request.json() as typeof body;
        serverTeam = teamDetail({
          ...serverTeam,
          roles: (body?.roles ?? []).map((role) => ({
            ...role,
            capability_tags: [],
            max_concurrency: 1,
            count: 0,
          })),
        });
        return HttpResponse.json(serverTeam);
      }),
      http.get('/api/teams/:id/roles/:role/ram-roles', () => HttpResponse.json(mapping)),
      http.post('/api/teams/:id/roles/:role/ram-roles/preview', async ({ request }) => {
        previewBody = await request.json() as typeof previewBody;
        return HttpResponse.json({
          team_id: 'team-7c19b0',
          team_role: 'planner',
          current_ram_role_ids: ['team-basic'],
          next_ram_role_ids: previewBody?.ram_role_ids ?? [],
          added_ram_role_ids: ['team-curator'],
          removed_ram_role_ids: [],
          affected_members: 0,
          affected_project_ids: [],
          version: 1,
        });
      }),
      http.put('/api/teams/:id/roles/:role/ram-roles', async ({ request }) => {
        putBody = await request.json() as typeof putBody;
        mapping = {
          team_id: 'team-7c19b0',
          team_role: 'planner',
          ram_role_ids: putBody?.ram_role_ids ?? [],
          version: 2,
        };
        return HttpResponse.json(mapping);
      }),
    );
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('team-edit-roles'));
    const modal = await screen.findByTestId('edit-team-roles-modal');
    expect(within(modal).getByTestId('edit-team-role-0-runtime-config')).toBeInTheDocument();
    expect(within(modal).queryByTestId('edit-team-role-0-access-editor')).not.toBeInTheDocument();
    fireEvent.click(within(modal).getByTestId('team-role-access-panel'));
    const role = await within(modal).findByTestId('edit-team-access-role-0-access-role');
    await waitFor(() => expect(within(modal).getAllByRole('option', { name: 'Team basic v1' }).length).toBeGreaterThan(0));
    fireEvent.change(role, { target: { value: 'team-basic@1' } });
    expect(within(modal).getByTestId('edit-team-access-role-0-access-permissions')).toHaveTextContent('team.memory.read');
    expect(within(modal).getByTestId('edit-team-access-role-0-ram-role-summary')).toHaveTextContent('1 roles · 2 permissions');
    fireEvent.click(within(modal).getByTestId('edit-team-access-role-0-ram-role-trigger'));
    const options = await screen.findAllByTestId('edit-team-access-role-0-ram-role-option');
    fireEvent.click(options.find((option) => option.getAttribute('data-value') === 'team-curator') as HTMLElement);
    expect(within(modal).getByTestId('edit-team-access-role-0-ram-role-summary')).toHaveTextContent('2 roles · 5 permissions');
    expect(within(modal).getByTestId('team-role-save-preview')).toHaveTextContent('1 changed roles');
    expect(within(modal).getByTestId('team-role-effective-hint')).toHaveTextContent('take effect immediately');
    fireEvent.click(within(modal).getByTestId('team-preview-access'));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(within(confirm).getByTestId('team-access-confirm-diff')).toHaveTextContent('planner');
    expect(putBody).toBeUndefined();
    fireEvent.click(within(confirm).getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(body?.roles?.[0]?.ram_role_keys).toEqual(['Team basic', 'Team curator']));
    expect(body?.roles?.[0]?.access_requirements).toEqual(['team.memory.propose', 'team.memory.read', 'team.memory.review', 'team.read', 'team.write']);
    await waitFor(() => expect(previewBody?.ram_role_ids).toEqual(['team-basic', 'team-curator']));
    expect(putBody).toEqual({ ram_role_ids: ['team-basic', 'team-curator'], expected_version: 1 });
    await waitFor(() => expect(screen.queryByTestId('edit-team-roles-modal')).not.toBeInTheDocument());
    await waitFor(() => expect(getCount).toBeGreaterThanOrEqual(2));
    expect(await screen.findByTestId('team-role-used-by-planner')).toHaveTextContent('Team curator');

    cleanup();
    renderAt('team-7c19b0');
    expect(await screen.findByTestId('team-role-used-by-planner')).toHaveTextContent('Team basic');
    expect(screen.getByTestId('team-role-used-by-planner')).toHaveTextContent('Team curator');
  });

  it('shows RAM role usage and member permission source scope', async () => {
    renderAt('team-7c19b0');
    const planner = await screen.findByTestId('team-role-used-by-planner');
    expect(planner).toHaveTextContent('Used by 1 members');
    expect(planner).toHaveTextContent('Team contributor');

    fireEvent.click(screen.getByTestId('tab-mm'));
    const source = await screen.findByTestId('member-permission-source-agent:9a70…');
    expect(source).toHaveTextContent('team_member → Team Role');
    expect(source).toHaveTextContent('scope team:team-7c19b0');
    expect(source).toHaveTextContent('Team contributor');
  });

  it('preserves and refreshes server access_lint in the edit role model', async () => {
    let patched = false;
    const invalidTeam = teamDetail({
      roles: [{
        role: 'planner',
        cli: 'claude-code',
        model: 'claude-opus-4-8',
        capability_tags: [],
        access_requirements: ['team.read', 'team.unknown'],
        access_lint: [{ severity: 'error', permission: 'team.unknown', message: 'server says unknown permission' }],
        max_concurrency: 1,
        count: 0,
      }],
    });
    const fixedTeam = teamDetail({
      roles: [{
        role: 'planner',
        cli: 'claude-code',
        model: 'claude-opus-4-8',
        capability_tags: [],
        access_requirements: ['team.read'],
        access_lint: [{ severity: 'warning', permission: 'team.read', message: 'server refreshed lint' }],
        max_concurrency: 1,
        count: 0,
      }],
    });
    server.use(
      http.get('/api/teams/:id', () => HttpResponse.json(patched ? fixedTeam : invalidTeam)),
      http.patch('/api/teams/:id', async () => {
        patched = true;
        return HttpResponse.json(fixedTeam);
      }),
    );
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('team-edit-roles'));
    let modal = await screen.findByTestId('edit-team-roles-modal');
    fireEvent.click(within(modal).getByTestId('team-role-access-panel'));
    expect(within(modal).getByTestId('edit-team-access-role-0-access-lint')).toHaveTextContent('team.unknown: server says unknown permission');
    expect(within(modal).queryByText('Unknown permission: team.unknown')).not.toBeInTheDocument();

    patched = true;
    cleanup();
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('team-edit-roles'));
    modal = await screen.findByTestId('edit-team-roles-modal');
    fireEvent.click(within(modal).getByTestId('team-role-access-panel'));
    expect(within(modal).getByTestId('edit-team-access-role-0-access-lint')).toHaveTextContent('team.read: server refreshed lint');
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

  it('opens the independent permission configuration entry from Members', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-configure-access'));
    const modal = await screen.findByTestId('edit-team-roles-modal');
    expect(within(modal).getByTestId('team-role-access-panel')).toHaveAttribute('aria-selected', 'true');
    expect(within(modal).getByTestId('edit-team-access-role-0-access-editor')).toBeInTheDocument();
    expect(within(modal).queryByTestId('edit-team-access-role-0-runtime-config')).not.toBeInTheDocument();
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

  it('cancels member removal without calling delete', async () => {
    let called = false;
    server.use(http.delete('/api/teams/:id/members/:ref', () => {
      called = true;
      return HttpResponse.json({});
    }));
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('member-remove-agent:9a70…'));
    fireEvent.click(await screen.findByTestId('confirm-modal-cancel'));
    await waitFor(() => expect(screen.queryByTestId('confirm-modal')).not.toBeInTheDocument());
    expect(called).toBe(false);
    expect(screen.getByText('planner-01')).toBeInTheDocument();
  });

  it('keeps the remove confirm open and shows an error when removal fails', async () => {
    server.use(http.delete('/api/teams/:id/members/:ref', () =>
      HttpResponse.json({ error: 'conflict', message: 'member has running work' }, { status: 409 }),
    ));
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('member-remove-agent:9a70…'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    expect(await screen.findByTestId('member-remove-error')).toHaveTextContent('member has running work');
    expect(screen.getByTestId('confirm-modal')).toBeInTheDocument();
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

  it('reviews and promotes team-memory proposals with metadata and diff', async () => {
    server.use(http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
      memory_permissions: { web_edit: true, can_manage: true },
    }))));
    renderAt('team-7c19b0', 'owner');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    fireEvent.click(await screen.findByTestId('memory-filter-proposals'));
    const proposalNode = await screen.findByTestId('memory-node-proposal-pending-1');
    expect(proposalNode).toHaveTextContent('proposals/proposal-pending-1.md');
    expect(screen.getByTestId('memory-proposal-badge-proposal-pending-1')).toHaveTextContent('PENDING');
    fireEvent.click(proposalNode);
    const detail = await screen.findByTestId('memory-proposal-detail');
    expect(detail).toHaveTextContent('warning ack');
    expect(screen.getByTestId('memory-doc-meta')).toHaveTextContent('uuid-proposal-1');
    expect(screen.getByTestId('memory-proposal-diff')).toHaveTextContent('+++ entries/deploy-rollback');
    fireEvent.click(screen.getByTestId('memory-proposal-promote'));
    await waitFor(() => expect(screen.getByTestId('memory-proposal-detail')).toHaveTextContent('entries/deploy-rollback-targetuuid.md'));
  });

  it('creates a team-memory proposal only after warning acknowledgement', async () => {
    server.use(http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
      memory_permissions: { web_edit: true, can_manage: true },
    }))));
    renderAt('team-7c19b0', 'admin');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    fireEvent.click(await screen.findByTestId('memory-manage'));
    const modal = await screen.findByTestId('memory-create-proposal-modal');
    fireEvent.change(within(modal).getByTestId('memory-create-slug'), { target: { value: 'new-note' } });
    fireEvent.change(within(modal).getByTestId('memory-create-description'), { target: { value: 'New note' } });
    fireEvent.change(within(modal).getByTestId('memory-create-body'), { target: { value: 'Remember this.' } });
    expect(within(modal).getByTestId('memory-create-proposal-submit')).toBeDisabled();
    fireEvent.click(within(modal).getByTestId('memory-create-ack'));
    fireEvent.click(within(modal).getByTestId('memory-create-proposal-submit'));
    await waitFor(() => expect(screen.queryByTestId('memory-create-proposal-modal')).not.toBeInTheDocument());
  });

  it('filters unsafe markdown URLs in memory documents', async () => {
    server.use(
      http.get('/api/teams/:id/memory/:entry', ({ params }) => HttpResponse.json({
        slug: String(params.entry),
        path: 'team-memory/entries/url-test.md',
        source_path: 'entries/url-test.md',
        title: 'URL test',
        frontmatter: null,
        body: '[bad](javascript:alert(1)) [ok](https://example.com)',
        uuid: 'uuid-url',
        commit: 'abc123def456',
        kind: 'entry',
      })),
    );
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    fireEvent.click(await screen.findByTestId('memory-node-ci-runbook'));
    await waitFor(() => expect(screen.getByTestId('memory-view')).toHaveTextContent('URL test'));
    const links = screen.getByTestId('memory-view').querySelectorAll('a');
    expect(links[0].getAttribute('href') ?? '').not.toContain('javascript:');
    expect(links[1]).toHaveAttribute('href', 'https://example.com');
  });

  it('manages curator agents and policy in team settings for owner/admin only', async () => {
    server.use(http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
      memory_permissions: { web_edit: true, can_manage: true },
    }))));
    renderAt('team-7c19b0', 'owner');
    fireEvent.click(await screen.findByTestId('tab-st'));
    const panel = await screen.findByTestId('team-memory-settings');
    const policySelect = await within(panel).findByTestId('team-memory-policy');
    expect(policySelect).toHaveValue('owner_admin_review');
    expect(policySelect).toHaveTextContent('Proposal only - owners/admins review');
    expect(policySelect).toHaveTextContent('Curator review - listed agents can review');
    expect(policySelect).not.toHaveTextContent('Read-only');
    expect(within(panel).getByTestId('team-memory-policy-description')).toHaveTextContent('Only human owners/admins can promote or reject');
    expect(within(panel).getByTestId('team-memory-curators-help')).toHaveTextContent('Select Curator review to activate this grant list');
    await waitFor(() => {
      expect(panel.querySelector('[data-testid="team-memory-curator-picker-chip"][data-value="agent:agent-t1"]')).not.toBeNull();
    });
    expect(within(panel).getByTestId('team-memory-curator-picker-trigger')).toBeDisabled();
    fireEvent.change(within(panel).getByTestId('team-memory-policy'), { target: { value: 'curator_review' } });
    expect(within(panel).getByTestId('team-memory-policy-description')).toHaveTextContent('curator agents listed below can also promote or reject');
    expect(within(panel).getByTestId('team-memory-curators-help')).toHaveTextContent('These agents can promote or reject pending proposals');
    expect(within(panel).getByTestId('team-memory-curator-picker-trigger')).not.toBeDisabled();
    fireEvent.click(within(panel).getByTestId('team-memory-curator-picker-trigger'));
    const options = await screen.findAllByTestId('team-memory-curator-picker-option');
    const dataMiner = options.find((option) => option.getAttribute('data-value') === 'agent:agent-d5');
    expect(dataMiner).toBeTruthy();
    fireEvent.click(dataMiner!);
    const saveButton = within(panel).getByTestId('team-memory-settings-save');
    expect(saveButton).toHaveTextContent('Save');
    fireEvent.click(saveButton);
    await waitFor(() => expect(within(panel).getByTestId('team-memory-settings-success')).toHaveTextContent('Settings saved.'));
    await waitFor(() => expect(within(panel).getByTestId('team-memory-settings-meta')).toHaveTextContent('settings commit'));
  });

  it('does not preserve curator grants when saving proposal-only policy', async () => {
    let body: { policy?: string; curator_agents?: string[] } | undefined;
    server.use(
      http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
        memory_permissions: { web_edit: true, can_manage: true },
      }))),
      http.put('/api/teams/:id/memory/settings', async ({ request }) => {
        body = await request.json() as typeof body;
        return HttpResponse.json({
          curator_agents: body?.curator_agents ?? [],
          policy: body?.policy ?? 'owner_admin_review',
          updated_at: '2026-08-08T12:30:00Z',
          updated_by: 'user:user-oops',
          commit: 'settingscommit',
          effect_hint: 'New sessions and fresh forks load promoted team memory from the current commit; in-flight sessions keep their snapshotted rules until restarted or forked again.',
        });
      }),
    );
    renderAt('team-7c19b0', 'owner');
    fireEvent.click(await screen.findByTestId('tab-st'));
    const panel = await screen.findByTestId('team-memory-settings');
    await waitFor(() => expect(within(panel).getByTestId('team-memory-curator-picker-trigger')).toBeDisabled());
    fireEvent.click(within(panel).getByTestId('team-memory-settings-save'));
    await waitFor(() => expect(body).toEqual({ policy: 'owner_admin_review', curator_agents: [] }));
  });

  it('shows failed team settings save feedback without discarding edits', async () => {
    server.use(
      http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
        memory_permissions: { web_edit: true, can_manage: true },
      }))),
      http.put('/api/teams/:id/memory/settings', () => HttpResponse.json(
        { error: 'invalid_input', message: 'policy rejected' },
        { status: 400 },
      )),
    );
    renderAt('team-7c19b0', 'owner');
    fireEvent.click(await screen.findByTestId('tab-st'));
    const panel = await screen.findByTestId('team-memory-settings');
    fireEvent.change(await within(panel).findByTestId('team-memory-policy'), { target: { value: 'curator_review' } });
    fireEvent.click(within(panel).getByTestId('team-memory-settings-save'));
    await waitFor(() => expect(within(panel).getByTestId('team-memory-settings-error')).toHaveTextContent('Save failed: [400 invalid_input] policy rejected'));
    expect(within(panel).getByTestId('team-memory-policy')).toHaveValue('curator_review');
  });

  it('keeps team settings read-only for regular members', async () => {
    server.use(http.get('/api/teams/:id', () => HttpResponse.json(teamDetail({
      memory_permissions: { web_edit: true, can_manage: false },
    }))));
    renderAt('team-7c19b0', 'member');
    fireEvent.click(await screen.findByTestId('tab-st'));
    const panel = await screen.findByTestId('team-memory-settings');
    expect(await within(panel).findByTestId('team-memory-policy')).toBeDisabled();
    expect(within(panel).getByTestId('team-memory-settings-save')).toBeDisabled();
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

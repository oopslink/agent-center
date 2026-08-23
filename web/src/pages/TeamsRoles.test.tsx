import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { BrowserRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { resetTeamsStore } from '@/api/teamsFixtures';
import TeamsRoles from './TeamsRoles';

type RAMRoleCreateBody = {
  name?: string;
  stable_key?: string;
  permissions?: string[];
  expected_latest_version?: number;
};

function renderPage(path = '/organizations/test/teams/roles') {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <TeamsRoles />
      </BrowserRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('TeamsRoles page', () => {
  beforeEach(() => resetTeamsStore());

  it('renders Team Role list/detail, work config, RAM mappings, and 1280-safe table layout', async () => {
    renderPage();

    expect(await screen.findByTestId('page-TeamsRoles')).toBeInTheDocument();
    const list = await screen.findByTestId('team-role-list');
    expect(list).toHaveTextContent('agent-center core');
    expect(list).toHaveTextContent('planner');
    expect(list).toHaveTextContent('claude-code · opus-4.8 · max 1');

    const mappings = await screen.findByTestId('team-role-ram-mappings');
    expect(mappings).toHaveTextContent('Team contributor');
    expect(mappings).toHaveTextContent('v1');
    expect(mappings.querySelector('.min-w-\\[48rem\\]')).toBeNull();
    expect(mappings.querySelector('table')).toHaveClass('table-fixed');

    fireEvent.click(await screen.findByTestId('team-role-detail-team-7c19b0-planner'));
    const drawer = await screen.findByTestId('team-role-mapping-drawer');
    expect(within(drawer).getByTestId('team-role-work-config')).toHaveTextContent('Max concurrency');
    expect(within(drawer).getByTestId('team-role-immediate-impact')).toHaveTextContent('CAS v1');
  });

  it('keeps refreshed create/edit RAM key readback visible as 2/2 when mapping reads are stale-empty', async () => {
    const acceptanceTeam = {
      id: 'team-acceptance',
      org_id: 'org-ooo',
      name: 'acceptance-squad',
      description: '',
      roles: [
        {
          role: 'planner',
          cli: 'claude-code',
          model: 'sonnet-5',
          capability_tags: [],
          ram_role_keys: ['Team basic'],
          access_requirements: ['team.read', 'team.memory.read'],
          max_concurrency: 1,
          count: 0,
        },
        {
          role: 'coder',
          cli: 'claude-code',
          model: 'sonnet-5',
          capability_tags: [],
          ram_role_keys: ['Team contributor'],
          access_requirements: ['team.read', 'team.write', 'team.memory.read', 'team.memory.propose'],
          max_concurrency: 1,
          count: 0,
        },
      ],
      version: 3,
      glyph: 'AS',
      status: 'draft',
      members_count: 0,
      projects_count: 0,
      created: '2026/8/23',
    };
    const staleEmptyMapping = ({ params }: { params: { role?: string | readonly string[] } }) => HttpResponse.json({
      team_id: 'team-acceptance',
      team_role: String(params.role),
      ram_role_ids: [],
      version: 1,
    });
    server.use(
      http.get('/api/teams', () => HttpResponse.json([acceptanceTeam])),
      http.get('*/api/orgs/:slug/teams', () => HttpResponse.json([acceptanceTeam])),
      http.get('/api/teams/team-acceptance/members', () => HttpResponse.json([])),
      http.get('*/api/orgs/:slug/teams/team-acceptance/members', () => HttpResponse.json([])),
      http.get('/api/teams/team-acceptance/roles/:role/ram-roles', staleEmptyMapping),
      http.get('*/api/orgs/:slug/teams/team-acceptance/roles/:role/ram-roles', staleEmptyMapping),
    );

    renderPage();
    await waitFor(() => expect(screen.getByTestId('page-TeamsRoles')).toHaveTextContent('2/2'));
    const mappings = await screen.findByTestId('team-role-ram-mappings');
    expect(mappings).toHaveTextContent('Team basic');
    expect(mappings).toHaveTextContent('Team contributor');
    expect(mappings).not.toHaveTextContent('No RAM Roles');
  });

  it('previews mapping impact, surfaces CAS errors, refreshes server version, and applies the fresh version', async () => {
    let version = 7;
    let ids = ['team-basic'];
    const putBodies: Array<{ ram_role_ids?: string[]; expected_version?: number }> = [];
    server.use(
      http.get('*/api/orgs/:slug/teams/team-7c19b0/roles/ops/ram-roles', () => HttpResponse.json({
        team_id: 'team-7c19b0',
        team_role: 'ops',
        ram_role_ids: ids,
        version,
      })),
      http.post('*/api/orgs/:slug/teams/team-7c19b0/roles/ops/ram-roles/preview', async ({ request }) => {
        const body = await request.json() as { ram_role_ids?: string[] };
        return HttpResponse.json({
          team_id: 'team-7c19b0',
          team_role: 'ops',
          current_ram_role_ids: ids,
          next_ram_role_ids: body.ram_role_ids ?? [],
          added_ram_role_ids: (body.ram_role_ids ?? []).filter((id) => !ids.includes(id)),
          removed_ram_role_ids: ids.filter((id) => !(body.ram_role_ids ?? []).includes(id)),
          affected_members: 1,
          affected_project_ids: ['project-c7073e48', 'project-11f0aa'],
          version,
        });
      }),
      http.put('*/api/orgs/:slug/teams/team-7c19b0/roles/ops/ram-roles', async ({ request }) => {
        const body = await request.json() as { ram_role_ids?: string[]; expected_version?: number };
        putBodies.push(body);
        if (body.expected_version === 7) {
          version = 8;
          return HttpResponse.json({ error: 'version_conflict', message: 'version_conflict' }, { status: 409 });
        }
        ids = body.ram_role_ids ?? [];
        version = 9;
        return HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'ops', ram_role_ids: ids, version });
      }),
    );

    renderPage();
    fireEvent.click(await screen.findByTestId('team-role-edit-mapping-team-7c19b0-ops'));
    let drawer = await screen.findByTestId('team-role-mapping-drawer');
    expect(within(drawer).getByTestId('team-role-immediate-impact')).toHaveTextContent('CAS v7');

    fireEvent.click(within(drawer).getByTestId('team-role-drawer-ram-roles-trigger'));
    let options = await screen.findByTestId('team-role-drawer-ram-roles-options');
    fireEvent.click(within(options).getByRole('option', { name: /Team curator/ }));
    fireEvent.mouseDown(document.body);
    fireEvent.click(within(drawer).getByRole('button', { name: 'Preview impact' }));
    expect(await within(drawer).findByText('2')).toBeInTheDocument();
    let applyButton = within(drawer).getByRole('button', { name: 'Apply mapping' });
    await waitFor(() => expect(applyButton).not.toBeDisabled());
    fireEvent.click(applyButton);
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));

    expect(await within(drawer).findByTestId('team-role-mapping-error')).toHaveTextContent('version_conflict');
    fireEvent.click(within(drawer).getByRole('button', { name: 'Refresh server version' }));
    expect(await screen.findByTestId('team-role-notice')).toHaveTextContent('CAS conflict detected');
    await waitFor(() => expect(within(drawer).getByTestId('team-role-immediate-impact')).toHaveTextContent('CAS v8'));

    fireEvent.click(within(drawer).getByTestId('team-role-drawer-ram-roles-trigger'));
    options = await screen.findByTestId('team-role-drawer-ram-roles-options');
    fireEvent.click(within(options).getByRole('option', { name: /Team curator/ }));
    fireEvent.mouseDown(document.body);
    fireEvent.click(within(drawer).getByRole('button', { name: 'Preview impact' }));
    applyButton = within(drawer).getByRole('button', { name: 'Apply mapping' });
    await waitFor(() => expect(applyButton).not.toBeDisabled());
    fireEvent.click(applyButton);
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));

    await waitFor(() => expect(putBodies.at(-1)).toEqual({ ram_role_ids: ['team-basic', 'team-curator'], expected_version: 8 }));
    await waitFor(() => expect(screen.queryByTestId('team-role-mapping-drawer')).not.toBeInTheDocument());
    expect(await screen.findByTestId('team-role-notice')).toHaveTextContent('Applied agent-center core / ops');
  });

  it('opens edit/duplicate RAM Role drawers and creates the duplicate through the RAM Role API', async () => {
    let createBody: RAMRoleCreateBody | null = null;
    const requireCreateBody = (): RAMRoleCreateBody => {
      if (!createBody) throw new Error('RAM Role create body was not captured');
      return createBody;
    };
    server.use(
      http.post('*/api/orgs/:slug/access/ram-roles', async ({ request }) => {
        createBody = await request.json() as typeof createBody;
        const latest = {
          id: 'role-copy',
          stable_key: createBody?.stable_key ?? 'team.curator.copy',
          name: createBody?.name ?? 'Team curator copy',
          kind: 'custom',
          version: 1,
          description: '',
          permissions: createBody?.permissions ?? [],
          risk: 'medium',
          scope: 'team',
        };
        return HttpResponse.json({ id: latest.id, stable_key: latest.stable_key, name: latest.name, kind: latest.kind, description: latest.description, latest, versions: [latest], references: [] }, { status: 201 });
      }),
    );

    renderPage();
    const card = await screen.findByTestId('team-ram-role-team-curator');
    fireEvent.click(within(card).getByRole('button', { name: 'Edit' }));
    let drawer = await screen.findByTestId('team-ram-role-drawer');
    expect(drawer).toHaveTextContent('Edit RAM Role');
    expect(within(drawer).getByTestId('team-ram-role-audit')).toHaveTextContent('Referenced by agent-center core/reviewer');
    fireEvent.click(within(drawer).getByLabelText('Close drawer'));

    fireEvent.click(within(card).getByRole('button', { name: 'Duplicate' }));
    drawer = await screen.findByTestId('team-ram-role-drawer');
    expect(within(drawer).getByTestId('team-ram-role-name')).toHaveValue('Team curator copy');
    expect(within(drawer).getByTestId('team-ram-role-stable-key')).toHaveValue('team-curator.copy');
    fireEvent.click(within(drawer).getByRole('button', { name: 'Create' }));

    await waitFor(() => expect(createBody?.name).toBe('Team curator copy'));
    const capturedCreateBody = requireCreateBody();
    expect(capturedCreateBody.permissions).toContain('team.memory.review');
    expect(capturedCreateBody.expected_latest_version).toBe(2);
    expect(await screen.findByTestId('team-role-notice')).toHaveTextContent('Created RAM Role Team curator copy');
  });

  it('blocks RAM Role delete while Team Role references exist', async () => {
    renderPage();
    const card = await screen.findByTestId('team-ram-role-team-contributor');
    expect(card).toHaveTextContent('Safeguard: referenced by');

    fireEvent.click(within(card).getByRole('button', { name: 'Delete' }));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(confirm).toHaveTextContent('Delete is blocked until mappings are removed or migrated');
    fireEvent.click(within(confirm).getByTestId('confirm-modal-confirm'));
    expect(await screen.findByTestId('team-role-notice')).toHaveTextContent('is still referenced');
  });
});

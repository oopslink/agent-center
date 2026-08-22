import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { BrowserRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import RAMRoles from './RAMRoles';

function renderPage(path = '/organizations/test/access/ram-roles') {
  window.history.pushState({}, '', path);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <RAMRoles />
      </BrowserRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

const baseCatalog = {
  generated_at: '2026-08-22T08:00:00Z',
  subjects: [],
  roles: [],
  decisions: [],
  grants: [],
  summary: { allowed: 0, high_risk: 0, expiring: 0, denied: 0, not_applicable: 0 },
  catalog: [
    { key: 'team.read', label: 'Read team', description: 'Read team', resource_kinds: ['team'], actions: ['read'], risk: 'low', category: 'access', legacy_sources: ['team_role_ram'] },
    { key: 'team.write', label: 'Write team', description: 'Write team', resource_kinds: ['team'], actions: ['write'], risk: 'medium', category: 'access', legacy_sources: ['team_role_ram'] },
    { key: 'project.write', label: 'Write project', description: 'Write project', resource_kinds: ['project'], actions: ['write'], risk: 'high', category: 'access', legacy_sources: ['team_role_ram'] },
  ],
};

describe('RAM Roles page', () => {
  it('renders a standalone searchable, filterable, paginated RAM Role catalog with detail and permission summary', async () => {
    server.use(
      http.get('/api/orgs/:slug/access/overview', () => HttpResponse.json(baseCatalog)),
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles: [
        { id: 'team-basic', stable_key: 'Team basic', name: 'Team basic', kind: 'system', version: 1, description: 'Read team metadata.', permissions: ['team.read'], risk: 'low', scope: 'team' },
        { id: 'team-curator', stable_key: 'Team curator', name: 'Team curator', kind: 'system', version: 2, description: 'Review team work.', permissions: ['team.read', 'team.write', 'project.write'], risk: 'high', scope: 'team' },
        { id: 'role-extra-1', stable_key: 'extra.1', name: 'Extra one', kind: 'custom', version: 1, description: 'extra', permissions: ['team.read'], risk: 'low', scope: 'project' },
        { id: 'role-extra-2', stable_key: 'extra.2', name: 'Extra two', kind: 'custom', version: 1, description: 'extra', permissions: ['team.write'], risk: 'medium', scope: 'project' },
        { id: 'role-extra-3', stable_key: 'extra.3', name: 'Extra three', kind: 'custom', version: 1, description: 'extra', permissions: ['project.write'], risk: 'high', scope: 'project' },
      ] })),
      http.get('/api/orgs/:slug/access/ram-roles/team-curator', () => HttpResponse.json({
        id: 'team-curator',
        stable_key: 'Team curator',
        name: 'Team curator',
        kind: 'system',
        description: 'Review team work.',
        scope: 'team',
        latest: { id: 'team-curator', stable_key: 'Team curator', name: 'Team curator', kind: 'system', version: 2, description: 'Review team work.', permissions: ['team.read', 'team.write', 'project.write'], risk: 'high', scope: 'team' },
        versions: [
          { id: 'team-curator', stable_key: 'Team curator', name: 'Team curator', kind: 'system', version: 2, description: 'Review team work.', permissions: ['team.read', 'team.write', 'project.write'], risk: 'high', scope: 'team' },
          { id: 'team-curator', stable_key: 'Team curator', name: 'Team curator', kind: 'system', version: 1, description: 'Review team work.', permissions: ['team.read'], risk: 'low', scope: 'team' },
        ],
        references: [],
      })),
    );

    renderPage();
    expect(await screen.findByTestId('page-RAMRoles')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'RAM Roles' })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Roles & mappings' })).not.toBeInTheDocument();
    fireEvent.change(screen.getByTestId('ram-role-filter-rows'), { target: { value: '4' } });
    expect(await screen.findByTestId('ram-role-pagination')).toHaveTextContent('Page 1 / 2');
    fireEvent.click(screen.getByTestId('ram-role-page-next'));
    expect(screen.getByTestId('ram-role-pagination')).toHaveTextContent('Page 2 / 2');
    fireEvent.change(screen.getByTestId('ram-role-search'), { target: { value: 'curator' } });
    const table = await screen.findByTestId('ram-role-table');
    expect(within(table).getByTestId('ram-role-row-team-curator')).toHaveTextContent('Team curator');
    expect(within(table).queryByTestId('ram-role-row-team-basic')).not.toBeInTheDocument();
    fireEvent.click(within(table).getByTestId('ram-role-row-team-curator'));
    const detail = await screen.findByTestId('ram-role-detail');
    expect(detail).toHaveTextContent('latest v2');
    expect(within(detail).getByTestId('ram-role-permission-summary')).toHaveTextContent('project.write');
    expect(within(detail).getByTestId('ram-role-version-1')).toHaveTextContent('v1');
  });

  it('creates and edits RAM Roles with full fields, versioning, and toast feedback', async () => {
    let createBody: Record<string, unknown> | null = null;
    let versionBody: Record<string, unknown> | null = null;
    server.use(
      http.get('/api/orgs/:slug/access/overview', () => HttpResponse.json(baseCatalog)),
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles: [
        { id: 'role-created', stable_key: 'release.operator', name: 'Release operator', kind: 'custom', version: 1, description: 'release work', permissions: ['team.read'], risk: 'low', scope: 'team' },
      ] })),
      http.get('/api/orgs/:slug/access/ram-roles/role-created', () => HttpResponse.json({
        id: 'role-created',
        stable_key: 'release.operator',
        name: 'Release operator',
        kind: 'custom',
        description: 'release work',
        scope: 'team',
        latest: { id: 'role-created', stable_key: 'release.operator', name: 'Release operator', kind: 'custom', version: 1, description: 'release work', permissions: ['team.read'], risk: 'low', scope: 'team' },
        versions: [{ id: 'role-created', stable_key: 'release.operator', name: 'Release operator', kind: 'custom', version: 1, description: 'release work', permissions: ['team.read'], risk: 'low', scope: 'team' }],
        references: [],
      })),
      http.post('/api/orgs/:slug/access/ram-roles', async ({ request }) => {
        createBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({
          id: 'role-created',
          stable_key: createBody.stable_key,
          name: createBody.name,
          kind: 'custom',
          description: createBody.description,
          scope: createBody.scope,
          latest: { id: 'role-created', stable_key: createBody.stable_key, name: createBody.name, kind: 'custom', version: 1, description: createBody.description, permissions: createBody.permissions, risk: 'high', scope: createBody.scope },
          versions: [],
          references: [],
        }, { status: 201 });
      }),
      http.post('/api/orgs/:slug/access/ram-roles/role-created/versions', async ({ request }) => {
        versionBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({
          id: 'role-created',
          stable_key: versionBody.stable_key,
          name: versionBody.name,
          kind: 'custom',
          description: versionBody.description,
          scope: versionBody.scope,
          latest: { id: 'role-created', stable_key: versionBody.stable_key, name: versionBody.name, kind: 'custom', version: 2, description: versionBody.description, permissions: versionBody.permissions, risk: 'high', scope: versionBody.scope },
          versions: [],
          references: [],
        }, { status: 201 });
      }),
    );

    renderPage();
    await screen.findByTestId('page-RAMRoles');
    fireEvent.click(screen.getByTestId('ram-role-create-open'));
    const createDrawer = await screen.findByTestId('ram-role-drawer');
    fireEvent.change(within(createDrawer).getByTestId('ram-role-form-name'), { target: { value: 'Release operator' } });
    fireEvent.change(within(createDrawer).getByTestId('ram-role-form-stable-key'), { target: { value: 'release.operator' } });
    fireEvent.change(within(createDrawer).getByTestId('ram-role-form-description'), { target: { value: 'release work' } });
    fireEvent.change(within(createDrawer).getByTestId('ram-role-form-scope'), { target: { value: 'project' } });
    fireEvent.click(within(createDrawer).getByText('project.write'));
    expect(within(createDrawer).getByTestId('ram-role-form-permission-summary')).toHaveTextContent('project.write');
    fireEvent.click(within(createDrawer).getByTestId('ram-role-form-save'));
    await waitFor(() => expect(createBody).toMatchObject({ name: 'Release operator', stable_key: 'release.operator', scope: 'project', permissions: ['project.write'] }));
    expect(await screen.findByTestId('ram-role-toast')).toHaveTextContent('Created RAM Role Release operator.');

    fireEvent.click(await screen.findByTestId('ram-role-edit-open'));
    const editDrawer = await screen.findByTestId('ram-role-drawer');
    fireEvent.click(within(editDrawer).getByText('team.write'));
    fireEvent.click(within(editDrawer).getByTestId('ram-role-form-create-version'));
    await waitFor(() => expect(versionBody).toMatchObject({ expected_latest_version: 1, permissions: ['team.read', 'team.write'] }));
    expect(await screen.findByTestId('ram-role-toast')).toHaveTextContent('Created v2 for Release operator.');
  });

  it('blocks referenced delete, migrates Team Role references, and confirms unreferenced delete', async () => {
    let putBody: Record<string, unknown> | null = null;
    let deleteBody: Record<string, unknown> | null = null;
    const oldRole = { id: 'role-old', stable_key: 'old.deployer', name: 'Old deployer', kind: 'custom', version: 3, description: 'legacy', permissions: ['project.write'], risk: 'high', scope: 'project' };
    const targetRole = { id: 'role-target', stable_key: 'new.deployer', name: 'New deployer', kind: 'custom', version: 1, description: 'replacement', permissions: ['project.write'], risk: 'high', scope: 'project' };
    const unusedRole = { id: 'role-unused', stable_key: 'unused.reviewer', name: 'Unused reviewer', kind: 'custom', version: 2, description: 'cleanup', permissions: ['team.read'], risk: 'low', scope: 'team' };
    server.use(
      http.get('/api/orgs/:slug/access/overview', () => HttpResponse.json(baseCatalog)),
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles: [oldRole, targetRole, unusedRole] })),
      http.get('/api/orgs/:slug/access/ram-roles/role-old', () => HttpResponse.json({ ...oldRole, latest: oldRole, versions: [oldRole], references: [] })),
      http.get('/api/orgs/:slug/access/ram-roles/role-target', () => HttpResponse.json({ ...targetRole, latest: targetRole, versions: [targetRole], references: [] })),
      http.get('/api/orgs/:slug/access/ram-roles/role-unused', () => HttpResponse.json({ ...unusedRole, latest: unusedRole, versions: [unusedRole], references: [] })),
      http.get('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles', () => HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: ['role-old', 'team-basic'], version: 9 })),
      http.put('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles', async ({ request }) => {
        putBody = await request.json() as Record<string, unknown>;
        return HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: putBody.ram_role_ids, version: 10 });
      }),
      http.delete('/api/orgs/:slug/access/ram-roles/role-unused', async ({ request }) => {
        deleteBody = await request.json() as Record<string, unknown>;
        return new HttpResponse(null, { status: 204 });
      }),
    );

    renderPage();
    fireEvent.click(await screen.findByTestId('ram-role-row-role-old'));
    const oldDetail = await screen.findByTestId('ram-role-detail');
    expect(await within(oldDetail).findByTestId('ram-role-team-references')).toHaveTextContent('agent-center core / planner');
    fireEvent.click(within(oldDetail).getByTestId('ram-role-delete-open'));
    const blockedConfirm = await screen.findByTestId('confirm-modal');
    expect(within(blockedConfirm).getByTestId('confirm-modal-confirm')).toBeDisabled();
    fireEvent.click(within(blockedConfirm).getByTestId('confirm-modal-cancel'));
    fireEvent.change(within(oldDetail).getByTestId('ram-role-migrate-target'), { target: { value: 'role-target' } });
    fireEvent.click(within(oldDetail).getByTestId('ram-role-migrate-submit'));
    await waitFor(() => expect(putBody).toEqual({ ram_role_ids: ['role-target', 'team-basic'], expected_version: 9 }));
    expect(await screen.findByTestId('ram-role-toast')).toHaveTextContent('Migrated 1 Team Role references');

    fireEvent.click(screen.getByTestId('ram-role-row-role-unused'));
    const unusedDetail = await screen.findByTestId('ram-role-detail');
    await waitFor(() => expect(unusedDetail).toHaveTextContent('No Team Role references.'));
    fireEvent.change(within(unusedDetail).getByTestId('ram-role-delete-name'), { target: { value: 'Unused reviewer' } });
    fireEvent.click(within(unusedDetail).getByTestId('ram-role-delete-open'));
    fireEvent.click(within(await screen.findByTestId('confirm-modal')).getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(deleteBody).toMatchObject({ expected_latest_version: 2, confirm_unreferenced: true }));
    expect(await screen.findByTestId('ram-role-toast')).toHaveTextContent('Deleted RAM Role Unused reviewer.');
  });
});

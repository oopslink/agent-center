import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { BrowserRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { useAppStore } from '@/store/app';
import Access from './Access';

function renderPage(path = '/organizations/test/access', currentUserId = 'user:hayang') {
  window.history.pushState({}, '', path);
  useAppStore.setState({ currentUserId });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <Access />
      </BrowserRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  useAppStore.setState({ currentUserId: '' });
});

describe('Access page', () => {
  it('defaults to RAM Roles and exposes only the three Access views', async () => {
    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    expect(await screen.findByText('Permission catalog')).toBeInTheDocument();
    expect(screen.getByTestId('access-roles-view')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'RAM Roles' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'RAM Roles' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('tab', { name: 'Team Role mappings' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Subject access' })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: /Profiles/i })).not.toBeInTheDocument();
    expect(screen.queryByText('Access roles')).not.toBeInTheDocument();
  });

  it('opens Team Role mappings from the dedicated Access view, then exposes expandable subject access', async () => {
    renderPage('/organizations/test/access?view=team-role-mappings');
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    expect(await screen.findByRole('tab', { name: 'Team Role mappings' })).toHaveAttribute('aria-selected', 'true');
    expect(await screen.findByTestId('access-team-role-mappings-view')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Team Role mappings' })).toBeInTheDocument();
    expect(await screen.findByTestId('access-mapping-team-7c19b0-planner')).toHaveTextContent('agent-center core');

    fireEvent.click(screen.getByTestId('access-view-subject-access'));
    expect(await screen.findByTestId('access-subject-view')).toBeInTheDocument();
    expect(window.location.search).toBe('?view=subject-access');
    expect(screen.getByTestId('access-subject-view')).toBeInTheDocument();
    expect(screen.getAllByText('High risk').length).toBeGreaterThan(0);
    expect(screen.getAllByText('No access').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Not applicable').length).toBeGreaterThan(0);
    expect(screen.getByText('subject is not a joined organization member')).toBeInTheDocument();
    expect(screen.getByText('file.download does not apply to team resources')).toBeInTheDocument();

    fireEvent.change(screen.getByTestId('access-filter-status'), { target: { value: 'unauthorized' } });
    await waitFor(() => {
      expect(screen.getByText('subject is not a joined organization member')).toBeInTheDocument();
      expect(screen.queryByText('file.download does not apply to team resources')).not.toBeInTheDocument();
    });

    const effective = screen.getByTestId('access-subject-effective-agent:external');
    fireEvent.click(within(effective).getByText(/effective permissions/));
    expect(effective).toHaveTextContent('Direct/other bindings');
  });

  it('previews and saves a Team Role mapping with the fetched CAS version and refreshes immediately', async () => {
    let previewBody: { ram_role_ids?: string[] } | null = null;
    let putBody: { ram_role_ids?: string[]; expected_version?: number } | null = null;
    server.use(
      http.get('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles', () => HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: ['team-basic'], version: 7 })),
      http.post('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles/preview', async ({ request }) => {
        previewBody = await request.json() as typeof previewBody;
        return HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', current_ram_role_ids: ['team-basic'], next_ram_role_ids: previewBody?.ram_role_ids ?? [], added_ram_role_ids: ['team-curator'], removed_ram_role_ids: [], affected_members: 1, affected_project_ids: ['project-c7073e48'], version: 7 });
      }),
      http.put('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles', async ({ request }) => {
        putBody = await request.json() as typeof putBody;
        return HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: putBody?.ram_role_ids ?? [], version: 8 });
      }),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('access-view-team-role-mappings'));
    const row = await screen.findByTestId('access-mapping-team-7c19b0-planner');
    expect(within(row).queryByRole('checkbox')).toBeNull();
    fireEvent.click(within(row).getByTestId('access-mapping-roles-team-7c19b0-planner-trigger'));
    const options = await screen.findByTestId('access-mapping-roles-team-7c19b0-planner-options');
    fireEvent.click(within(options).getByRole('option', { name: /Team curator/ }));
    expect(within(row).getAllByTestId('access-mapping-roles-team-7c19b0-planner-chip').map((chip) => chip.textContent)).toContain('Team curator×');
    fireEvent.click(within(row).getByRole('button', { name: 'Preview impact' }));
    expect(await within(row).findByTestId('access-mapping-preview')).toHaveTextContent('1 members');
    fireEvent.click(within(row).getByRole('button', { name: 'Save mapping' }));
    await waitFor(() => expect(putBody).toEqual({ ram_role_ids: ['team-basic', 'team-curator'], expected_version: 7 }));
    await waitFor(() => expect(row).toHaveTextContent('v8'));
    expect(previewBody).toEqual({ ram_role_ids: ['team-basic', 'team-curator'] });
  });

  it('renders Team Role mapping preview when nullable impact arrays are returned', async () => {
    server.use(
      http.get('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles', () => HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: ['team-basic'], version: 7 })),
      http.post('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles/preview', () => HttpResponse.json({
        team_id: 'team-7c19b0',
        team_role: 'planner',
        current_ram_role_ids: ['team-basic'],
        next_ram_role_ids: ['team-basic', 'team-curator'],
        added_ram_role_ids: ['team-curator'],
        removed_ram_role_ids: null,
        affected_members: 1,
        affected_project_ids: null,
        version: 7,
      })),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('access-view-team-role-mappings'));
    const row = await screen.findByTestId('access-mapping-team-7c19b0-planner');
    fireEvent.click(within(row).getByTestId('access-mapping-roles-team-7c19b0-planner-trigger'));
    const options = await screen.findByTestId('access-mapping-roles-team-7c19b0-planner-options');
    fireEvent.click(within(options).getByRole('option', { name: /Team curator/ }));
    fireEvent.click(within(row).getByRole('button', { name: 'Preview impact' }));

    expect(await within(row).findByTestId('access-mapping-preview')).toHaveTextContent('1 members · +1 / −0 roles · 0 projects');
  });

  it('previews and applies a four-step batch grant without deriving final permissions in the UI', async () => {
    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-roles-view');
    fireEvent.click(screen.getByTestId('access-open-batch'));

    const drawer = await screen.findByTestId('access-batch-drawer');
    fireEvent.click(within(drawer).getByRole('button', { name: /Builder/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /External Bot/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /project\.write/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /team\.memory\.review/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /Project Alpha/ }));
    fireEvent.change(within(drawer).getByTestId('access-batch-expires'), {
      target: { value: '2026-08-20T12:30' },
    });
    fireEvent.change(within(drawer).getByTestId('access-batch-reason'), {
      target: { value: 'temporary release support' },
    });
    fireEvent.click(within(drawer).getByTestId('access-run-preview'));

    const preview = await within(drawer).findByTestId('access-preview-summary');
    expect(preview).toHaveTextContent('High risk');
    expect(preview).toHaveTextContent('No access');
    expect(preview).toHaveTextContent('N/A');
    expect(preview).toHaveTextContent('Expires');
    expect(preview).not.toHaveTextContent('Expires -');
    const previewRows = within(drawer).getByTestId('access-batch-items');
    expect(previewRows).toHaveTextContent('No access');
    expect(previewRows).toHaveTextContent('Not applicable');

    const continueButton = within(drawer).getByTestId('access-preview-continue');
    expect(continueButton).toBeDisabled();
    fireEvent.click(within(drawer).getByTestId('access-high-risk-ack'));
    expect(continueButton).not.toBeDisabled();
    fireEvent.click(continueButton);

    await within(drawer).findByText(/grantable items/);
    fireEvent.click(within(drawer).getByTestId('access-apply-batch'));
    const result = await within(drawer).findByTestId('access-result');
    expect(result).toHaveTextContent('Partial failure');
    expect(result).toHaveTextContent('no access');
    expect(result).toHaveTextContent('not applicable');
  });

  it('previews, confirms, and reports selected grant revokes', async () => {
    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    const grants = await screen.findByTestId('access-grants');
    fireEvent.click(within(grants).getByRole('checkbox', { name: /Select project\.write for revoke/ }));
    fireEvent.click(within(grants).getByRole('checkbox', { name: /Select org\.member\.role\.manage for revoke/ }));
    fireEvent.click(within(grants).getByTestId('access-revoke-preview'));

    const preview = await within(grants).findByTestId('access-revoke-preview-panel');
    expect(preview).toHaveTextContent('derived permission and must be revoked at its source');
    fireEvent.click(within(preview).getByTestId('access-revoke-confirm'));

    const result = await within(grants).findByTestId('access-result');
    expect(result).toHaveTextContent('Partial failure');
    expect(result).toHaveTextContent('derived permission and must be revoked at its source');
    expect(result).toHaveTextContent('Not applicable');
  });

  it('confirms revoke with the original preview token, stable idempotency key, and reason/message', async () => {
    let previewBody: { grant_ids?: string[]; reason?: string; message?: string } | null = null;
    let confirmBody: { grant_ids?: string[]; reason?: string; message?: string; preview_id?: string; token?: string; idempotency_key?: string } | null = null;
    server.use(
      http.post('*/api/orgs/:slug/access/grants/revoke/preview', async ({ request }) => {
        previewBody = (await request.json()) as typeof previewBody;
        return HttpResponse.json({
          preview_id: 'rvp-original',
          token: 'token-original',
          expires_at: '2026-08-14T08:07:00Z',
          items: (previewBody?.grant_ids ?? []).map((id, idx) => ({
            id: `revoke-${idx + 1}`,
            subject_ref: 'agent:builder',
            subject_name: 'Builder',
            permission: 'project.write',
            resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' },
            status: 'allowed',
            risk: 'medium',
            high_risk: false,
            reason: 'grant can be revoked by unified authorization API',
            grant_id: id,
          })),
          summary: { total: 1, grantable: 1, high_risk: 0, unauthorized: 0, not_applicable: 0 },
        });
      }),
      http.post('*/api/orgs/:slug/access/grants/revoke/confirm', async ({ request }) => {
        confirmBody = (await request.json()) as typeof confirmBody;
        return HttpResponse.json({
          operation_id: 'access-revoke-1',
          applied_at: '2026-08-14T08:02:00Z',
          items: [],
          summary: { total: 0, succeeded: 0, failed: 0, unauthorized: 0, not_applicable: 0, partial_failure: false },
        });
      }),
    );

    renderPage();
    const grants = await screen.findByTestId('access-grants');
    fireEvent.change(within(grants).getByLabelText('Reason'), { target: { value: 'original audit reason' } });
    fireEvent.click(within(grants).getByRole('checkbox', { name: /Select project\.write for revoke/ }));
    fireEvent.click(within(grants).getByTestId('access-revoke-preview'));

    const preview = await within(grants).findByTestId('access-revoke-preview-panel');
    fireEvent.change(within(grants).getByLabelText('Reason'), { target: { value: 'mutated after preview' } });
    fireEvent.click(within(preview).getByTestId('access-revoke-confirm'));

    await waitFor(() => {
      expect(confirmBody).toEqual({
        grant_ids: ['grant-custom-1'],
        reason: 'original audit reason',
        message: 'original audit reason',
        preview_id: 'rvp-original',
        token: 'token-original',
        idempotency_key: 'access-revoke-rvp-original',
      });
    });
    expect(previewBody).toEqual({
      grant_ids: ['grant-custom-1'],
      reason: 'original audit reason',
      message: 'original audit reason',
    });
  });

  it('browses RAM role versions and publishes a real v2 RAM role version with CAS payload', async () => {
    const roleV1 = {
      id: 'role-created',
      name: 'Release operator',
      kind: 'custom',
      description: 'release work',
      version: 1,
      permissions: ['org.read', 'project.write'],
      risk: 'medium',
    };
    let roleDetail = {
      id: roleV1.id,
      name: roleV1.name,
      description: roleV1.description,
      kind: roleV1.kind,
      latest: roleV1,
      versions: [roleV1],
    };
    let publishBody: { permissions?: string[]; expected_latest_version?: number } | null = null;
    server.use(
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({
        roles: [
          { id: 'team-basic', name: 'Team basic', kind: 'system', version: 1, description: 'Read team metadata and memory.', permissions: ['team.read', 'team.memory.read'], risk: 'low' },
          { id: 'team-contributor', name: 'Team contributor', kind: 'system', version: 1, description: 'Read/write team work and propose memory.', permissions: ['team.read', 'team.write', 'team.memory.read', 'team.memory.propose'], risk: 'medium' },
          { id: 'team-curator', name: 'Team curator', kind: 'system', version: 2, description: 'Review team memory.', permissions: ['team.read', 'team.write', 'team.memory.read', 'team.memory.propose', 'team.memory.review'], risk: 'high' },
          roleDetail.latest,
        ],
      })),
      http.get('/api/orgs/:slug/access/ram-roles/role-created', () => HttpResponse.json(roleDetail)),
      http.post('/api/orgs/:slug/access/ram-roles', async ({ request }) => {
        const body = (await request.json()) as { name: string; description?: string; permissions: string[] };
        roleDetail = {
          id: roleV1.id,
          name: body.name,
          kind: roleV1.kind,
          description: body.description ?? '',
          latest: { ...roleV1, name: body.name, description: body.description ?? '', permissions: body.permissions },
          versions: [{ ...roleV1, name: body.name, description: body.description ?? '', permissions: body.permissions }],
        };
        return HttpResponse.json(roleDetail, { status: 201 });
      }),
      http.post('/api/orgs/:slug/access/ram-roles/role-created/versions', async ({ request }) => {
        publishBody = (await request.json()) as { permissions: string[]; expected_latest_version?: number };
        const latest = { ...roleDetail.latest, version: 2, permissions: publishBody.permissions ?? [], risk: 'high' };
        roleDetail = { ...roleDetail, latest, versions: [latest, roleDetail.latest] };
        return HttpResponse.json(roleDetail, { status: 201 });
      }),
    );

    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-roles-view');
    fireEvent.click(screen.getByTestId('access-view-ram-roles'));

    const view = await screen.findByTestId('access-roles-view');
    expect(await within(view).findByTestId('access-role-row-team-curator')).toHaveTextContent('v2');
    fireEvent.click(within(view).getByTestId('access-role-row-team-curator'));
    expect(await within(view).findByTestId('access-role-versions')).toHaveTextContent('v1');
    expect(within(view).getByTestId('access-role-versions')).toHaveTextContent('v2');

    const create = within(view).getByTestId('access-role-create');
    fireEvent.change(within(create).getByTestId('access-role-name'), { target: { value: 'Release operator' } });
    fireEvent.change(within(create).getByTestId('access-role-description'), { target: { value: 'release work' } });
    fireEvent.click(within(create).getByText('org.read'));
    fireEvent.click(within(create).getByText('project.write'));
    fireEvent.click(within(create).getByTestId('access-role-create-submit'));

    await waitFor(() => expect(screen.getByTestId('access-role-detail')).toHaveTextContent('Release operator'));
    const detail = screen.getByTestId('access-role-detail');
    await waitFor(() => expect(within(detail).getByTestId('access-role-new-version-submit')).toHaveTextContent('Publish v2'));
    expect(within(detail).getByTestId('access-role-new-version-submit')).not.toBeDisabled();
    fireEvent.click(within(detail).getByText('team.memory.review'));
    fireEvent.click(within(detail).getByTestId('access-role-new-version-submit'));

    await waitFor(() => {
      expect(publishBody).toEqual({
        permissions: ['org.read', 'project.write', 'team.memory.review'],
        expected_latest_version: 1,
      });
    });
    await waitFor(() => expect(detail).toHaveTextContent('Latest v2'));
    const versions = within(detail).getByTestId('access-role-versions');
    expect(versions).toHaveTextContent('v2');
    expect(versions).toHaveTextContent('team.memory.review');
    expect(versions).toHaveTextContent('v1');
  });

  it('keeps RAM role versions pinned and shows an error when publish hits a CAS conflict', async () => {
    const roleV1 = {
      id: 'role-cas',
      name: 'Deploy operator',
      kind: 'custom',
      description: 'deploy work',
      version: 1,
      permissions: ['org.read', 'project.write'],
      risk: 'medium',
    };
    let publishBody: { permissions?: string[]; expected_latest_version?: number } | null = null;
    server.use(
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({
        roles: [roleV1],
      })),
      http.get('/api/orgs/:slug/access/ram-roles/role-cas', () => HttpResponse.json({
        id: roleV1.id,
        name: roleV1.name,
        kind: roleV1.kind,
        description: roleV1.description,
        latest: roleV1,
        versions: [roleV1],
      })),
      http.post('/api/orgs/:slug/access/ram-roles/role-cas/versions', async ({ request }) => {
        publishBody = (await request.json()) as { permissions: string[]; expected_latest_version?: number };
        return HttpResponse.json(
          { error: 'version_conflict', message: 'access RAM role latest version changed' },
          { status: 409 },
        );
      }),
    );

    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-roles-view');
    fireEvent.click(screen.getByTestId('access-view-ram-roles'));

    const view = await screen.findByTestId('access-roles-view');
    fireEvent.click(await within(view).findByTestId('access-role-row-role-cas'));
    const detail = await screen.findByTestId('access-role-detail');
    await waitFor(() => expect(detail).toHaveTextContent('Latest v1'));

    fireEvent.click(within(detail).getByText('team.memory.review'));
    fireEvent.click(within(detail).getByTestId('access-role-new-version-submit'));

    await waitFor(() => {
      expect(publishBody).toEqual({
        permissions: ['org.read', 'project.write', 'team.memory.review'],
        expected_latest_version: 1,
      });
    });
    expect(await within(detail).findByRole('alert')).toHaveTextContent('access RAM role latest version changed');
    expect(detail).toHaveTextContent('Latest v1');
    const versions = within(detail).getByTestId('access-role-versions');
    expect(versions).toHaveTextContent('v1');
    expect(versions).not.toHaveTextContent('v2');
  });

  it('gates the Access route with current subject effective permissions and shows the 403 reason', async () => {
    server.use(
      http.get('/api/orgs/:slug/permissions/effective', () =>
        HttpResponse.json({
          subject_ref: 'user:ops',
          resource: { kind: 'org', id: 'org-test', org_id: 'org-test' },
          permissions: [{ key: 'org.read', source: 'org_role', evidence_ref: 'members:mem-ops' }],
        }),
      ),
      http.post('/api/orgs/:slug/permissions/explain', () =>
        HttpResponse.json({
          decision: {
            allowed: false,
            subject_ref: 'user:ops',
            permission: 'org.member.role.manage',
            resource: { kind: 'org', id: 'org-test', org_id: 'org-test' },
            source: 'org_role',
            reason: 'admin role cannot manage owner-only permission mapping',
            evidence_ref: 'members:mem-ops',
          },
          effective: [],
          denied_by: ['admin role cannot manage owner-only permission mapping'],
        }),
      ),
    );

    renderPage('/organizations/test/access', 'user:ops');

    const forbidden = await screen.findByTestId('access-forbidden');
    expect(forbidden).toHaveTextContent('Access unavailable (403)');
    await waitFor(() => expect(forbidden).toHaveTextContent('admin role cannot manage owner-only permission mapping'));
    expect(screen.getByTestId('access-open-batch')).toBeDisabled();
    expect(screen.queryByTestId('access-subject-view')).not.toBeInTheDocument();
  });
});

import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { BrowserRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { useAppStore } from '@/store/app';
import Access from './Access';

function renderPage(path = '/organizations/test/access/ram-roles', currentUserId = 'user:hayang') {
  window.history.pushState({}, '', path);
  useAppStore.setState({ currentUserId });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <Access page={path.includes('/subject-access') ? 'subject-access' : 'ram-roles'} />
      </BrowserRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  useAppStore.setState({ currentUserId: '' });
});

describe('Access page', () => {
  it('renders the independent RAM Roles page without subject controls or duplicate tabs', async () => {
    renderPage();
    const page = await screen.findByTestId('page-Access');
    expect(page).toHaveAttribute('aria-labelledby', 'access-ram-roles-title');
    expect(page).toHaveClass('min-w-0');
    const roles = await screen.findByTestId('access-roles-view');
    expect(roles).toHaveClass('min-w-0');
    expect((await within(roles).findByRole('table')).parentElement).toHaveClass('overflow-x-auto');
    expect(roles.className).not.toContain('minmax(30rem');
    expect(screen.getByTestId('access-runtime-sha')).toHaveTextContent('Runtime SHA:');
    expect(screen.getByRole('heading', { name: 'RAM Roles' })).toBeInTheDocument();
    expect(screen.queryByText('Permission catalog')).not.toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(screen.queryByTestId('access-open-direct-binding')).not.toBeInTheDocument();
    expect(screen.queryByTestId('access-open-batch')).not.toBeInTheDocument();
    expect(screen.queryByTestId('subject-access-filters')).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: /Profiles/i })).not.toBeInTheDocument();
    expect(screen.queryByText('Access roles')).not.toBeInTheDocument();
  });

  it('filters by search, risk, and scope, switches density, paginates, and explains every permission from the registry', async () => {
    const roles = Array.from({ length: 12 }, (_, index) => ({
      id: `role-${index + 1}`,
      stable_key: `role-${index + 1}`,
      name: index === 10 ? 'Critical reviewer' : `Registry role ${index + 1}`,
      kind: 'custom' as const,
      description: 'Registry-backed role',
      scope: index === 10 ? 'project' : 'team',
      version: 1,
      permissions: index === 10 ? ['team.memory.review'] : ['team.read'],
      risk: index === 10 ? 'high' as const : 'low' as const,
      references: 0,
    }));
    server.use(
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles })),
      http.get('/api/orgs/:slug/access/ram-roles/:id', ({ params }) => {
        const role = roles.find((item) => item.id === params.id) ?? roles[0];
        return HttpResponse.json({ ...role, latest: role, versions: [role], references: [] });
      }),
    );

    renderPage();
    const view = await screen.findByTestId('access-roles-view');
    expect(await within(view).findByTestId('access-role-pagination')).toHaveTextContent('Showing 1 to 10 of 12');
    fireEvent.click(within(view).getByRole('button', { name: 'Next' }));
    expect(within(view).getByTestId('access-role-pagination')).toHaveTextContent('Showing 11 to 12 of 12');

    fireEvent.change(within(view).getByTestId('access-role-density'), { target: { value: 'compact' } });
    expect(within(view).getByTestId('access-role-row-role-11').querySelector('td')).toHaveClass('py-1.5');
    fireEvent.change(within(view).getByTestId('access-filter-ram role risk'), { target: { value: 'high' } });
    fireEvent.change(within(view).getByTestId('access-filter-ram role scope'), { target: { value: 'project' } });
    expect(await within(view).findByTestId('access-role-row-role-11')).toHaveTextContent('Critical reviewer');
    expect(within(view).queryByTestId('access-role-row-role-1')).not.toBeInTheDocument();
    fireEvent.change(within(view).getByTestId('access-role-search'), { target: { value: 'critical' } });
    fireEvent.click(within(view).getByTestId('access-role-row-role-11'));

    const summary = await screen.findByTestId('access-role-permission-summary');
    const permission = within(summary).getByTestId('access-role-permission-team.memory.review');
    expect(permission).toHaveTextContent('team.memory.review');
    expect(permission).toHaveTextContent('High risk');
    expect(permission).not.toHaveTextContent('Unregistered permission');

    fireEvent.click(within(view).getByTestId('access-role-new'));
    const drawer = await screen.findByTestId('access-role-drawer');
    expect(within(drawer).getByText('team.memory.review')).toBeInTheDocument();
    expect(within(drawer).queryByText('made.up.permission')).not.toBeInTheDocument();
  });

  it('renders explicit empty, list error, and detail error states with recovery actions', async () => {
    server.use(http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles: [] })));
    const first = renderPage();
    expect(await screen.findByTestId('access-role-empty')).toHaveTextContent('No RAM Roles yet');
    expect(screen.getByTestId('access-role-detail-empty')).toHaveTextContent('Select a RAM Role');
    first.unmount();

    server.use(http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ message: 'registry unavailable' }, { status: 503 })));
    const second = renderPage();
    expect(await screen.findByTestId('access-ram-roles-error')).toHaveTextContent('registry unavailable');
    second.unmount();

    const role = { id: 'role-detail-error', stable_key: 'role-detail-error', name: 'Detail error role', kind: 'custom', description: '', scope: 'team', version: 1, permissions: ['team.read'], risk: 'low' };
    server.use(
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles: [role] })),
      http.get('/api/orgs/:slug/access/ram-roles/role-detail-error', () => HttpResponse.json({ message: 'detail unavailable' }, { status: 500 })),
    );
    renderPage();
    expect(await screen.findByTestId('access-role-detail-error')).toHaveTextContent('detail unavailable');
  });

  it('renders Subject access as a distinct titled page with its own actions and filters', async () => {
    renderPage('/organizations/test/access/subject-access');
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Subject access' })).toBeInTheDocument();
    expect(screen.queryByTestId('access-roles-view')).not.toBeInTheDocument();
    expect(await screen.findByTestId('access-subject-view')).toBeInTheDocument();
    expect(screen.queryByRole('tab')).not.toBeInTheDocument();
    expect(screen.getByTestId('access-open-direct-binding')).toBeInTheDocument();
    expect(screen.getByTestId('access-open-batch')).toBeInTheDocument();
    expect(screen.getByTestId('subject-access-filters')).toBeInTheDocument();
    expect(screen.getByTestId('access-subject-view')).toBeInTheDocument();
    expect(screen.getAllByText('High risk').length).toBeGreaterThan(0);
    expect(screen.getAllByText('No access').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Not applicable').length).toBeGreaterThan(0);
    expect(screen.getAllByText('subject is not a joined organization member').length).toBeGreaterThan(0);
    expect(screen.getAllByText('file.download does not apply to team resources').length).toBeGreaterThan(0);
    // Builder has one real denied decision plus one N/A decision; N/A must not inflate explicit deny to two.
    expect(screen.getByTestId('access-subject-metrics')).toHaveTextContent('Explicit deny1');

    fireEvent.change(screen.getByTestId('access-filter-status'), { target: { value: 'unauthorized' } });
    await waitFor(() => {
      expect(screen.getAllByText('subject is not a joined organization member').length).toBeGreaterThan(0);
      expect(screen.queryByText('file.download does not apply to team resources')).not.toBeInTheDocument();
    });

    const effective = screen.getByTestId('access-subject-effective-agent:external');
    fireEvent.click(within(effective).getByText(/effective permissions/));
    expect(effective).toHaveTextContent('Other bindings');

    fireEvent.change(screen.getByTestId('access-filter-status'), { target: { value: 'all' } });
    expect(screen.getByPlaceholderText('Subject, name, email, or ID')).toBeInTheDocument();
    expect(screen.getByTestId('access-filter-project')).toBeInTheDocument();
    expect(screen.getByTestId('access-filter-permission')).toBeInTheDocument();
    fireEvent.change(screen.getByTestId('access-filter-type'), { target: { value: 'agent' } });
    await waitFor(() => {
      expect(screen.getAllByText('Builder').length).toBeGreaterThan(0);
      expect(screen.queryByText('Hayang')).not.toBeInTheDocument();
    });
  });

  it.each([
    ['ram-roles', '/organizations/test/access/ram-roles', 'RAM Roles'],
    ['subject-access', '/organizations/test/access/subject-access', 'Subject access'],
  ])('renders a page-specific loading skeleton for %s', async (page, path, title) => {
    server.use(
      http.get('/api/orgs/:slug/permissions/effective', () => new Promise(() => undefined)),
    );
    renderPage(path);
    expect(screen.getByRole('heading', { name: title })).toBeInTheDocument();
    expect(screen.getByTestId(`access-${page}-loading`)).toHaveAttribute('aria-busy', 'true');
  });

  it.each([
    ['ram-roles', '/organizations/test/access/ram-roles'],
    ['subject-access', '/organizations/test/access/subject-access'],
  ])('renders a page-specific data error for %s', async (page, path) => {
    server.use(
      http.get('*/api/orgs/:slug/access/overview', () => HttpResponse.json({ message: 'Access data unavailable' }, { status: 500 })),
    );
    renderPage(path);
    expect(await screen.findByTestId(`access-${page}-error`)).toHaveTextContent('Access data unavailable');
  });

  it('renders the canonical empty subject state after filters return no decisions', async () => {
    server.use(
      http.get('*/api/orgs/:slug/access/overview', () => HttpResponse.json({
        generated_at: '2026-08-24T00:00:00Z',
        subjects: [], roles: [], catalog: [], decisions: [], grants: [],
        summary: { allowed: 0, high_risk: 0, expiring: 0, denied: 0, not_applicable: 0 },
      })),
    );
    renderPage('/organizations/test/access/subject-access');
    expect(await screen.findByTestId('access-empty')).toHaveTextContent('No matching access decisions');
    expect(screen.getByTestId('access-open-direct-binding')).toBeDisabled();
  });

  it('shows Team RAM and direct binding source chains, then opens the direct binding flow', async () => {
    renderPage('/organizations/test/access/subject-access');
    const builder = await screen.findByTestId('access-subject-effective-agent:builder');
    fireEvent.click(within(builder).getByText(/^3 effective permissions/));

    expect(builder).toHaveTextContent('membership:agent-center core');
    expect(builder).toHaveTextContent('Team Role reviewer');
    expect(builder).toHaveTextContent('RAM Role team-curator');
    expect(builder).toHaveTextContent('direct binding');
    expect(builder).toHaveTextContent('grant grant-custom-1');
    const sidebar = await screen.findByTestId('access-subject-sidebar');
    const trace = within(sidebar).getByTestId('access-permission-trace');
    expect(trace).toHaveTextContent('Membership → Team Role → RAM Role');
    expect(trace).toHaveTextContent('agent-center core → reviewer → team-curator');
    expect(trace).toHaveTextContent('Explicit deny');
    expect(trace).toHaveTextContent('Final → denied');
    expect(within(sidebar).getByTestId('access-direct-binding-union')).toHaveTextContent('grant-custom-1');
    await waitFor(() => expect(within(sidebar).getByTestId('access-audit-history')).toHaveTextContent('authorization.assignment.created'));

    fireEvent.click(screen.getByTestId('access-open-direct-binding'));
    const drawer = await screen.findByRole('dialog', { name: 'Add direct binding' });
    expect(within(drawer).getByTestId('access-direct-subject-context')).toHaveTextContent('agent:builder');
    expect(within(drawer).queryByRole('button', { name: /Hayang/ })).not.toBeInTheDocument();
  });

  it('previews and applies a four-step batch grant without deriving final permissions in the UI', async () => {
    renderPage('/organizations/test/access/subject-access');
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-subject-view');
    fireEvent.click(screen.getByTestId('access-open-batch'));

    const drawer = await screen.findByTestId('access-batch-drawer');
    fireEvent.click(within(drawer).getByRole('button', { name: /Builder/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /External Bot/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /team\.memory\.review/ }));
    fireEvent.click(within(drawer).getAllByRole('button', { name: /agent-center core/ })[0]);
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
    expect(preview).toHaveTextContent('N/A0');
    expect(preview).toHaveTextContent('Expires');
    expect(preview).not.toHaveTextContent('Expires -');
    const previewRows = within(drawer).getByTestId('access-batch-items');
    expect(previewRows).toHaveTextContent('No access');
    expect(previewRows).not.toHaveTextContent('Not applicable');

    const continueButton = within(drawer).getByTestId('access-preview-continue');
    expect(continueButton).toBeDisabled();
    fireEvent.click(within(drawer).getByTestId('access-high-risk-ack'));
    expect(continueButton).not.toBeDisabled();
    fireEvent.click(continueButton);

    await within(drawer).findByText(/grantable items/);
    fireEvent.click(within(drawer).getByTestId('access-apply-batch'));
    const result = await within(drawer).findByTestId('access-result');
    expect(result).toHaveTextContent('Partial failure');
    expect(result).toHaveTextContent('409 conflict');
    expect(result).toHaveTextContent('no access');
    expect(result).toHaveTextContent('0 not applicable');
    expect(result).not.toHaveTextContent('Unknown resource');
  });

  it('previews, confirms, and reports a direct revoke within the selected subject', async () => {
    server.use(
      http.post('*/api/orgs/:slug/access/grants/revoke/preview', () => HttpResponse.json({
        preview_id: 'revoke-direct-preview', token: 'revoke-direct-token', expires_at: '2026-08-24T01:00:00Z',
        items: [{ id: 'revoke-direct', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: { kind: 'project', id: 'proj-a', label: 'Project Alpha' }, status: 'allowed', risk: 'medium', high_risk: false, reason: 'direct binding and can be revoked', grant_id: 'grant-custom-1' }],
        summary: { total: 1, grantable: 1, high_risk: 0, unauthorized: 0, not_applicable: 0 },
      })),
      http.post('*/api/orgs/:slug/access/grants/revoke/confirm', () => HttpResponse.json({
        operation_id: 'revoke-direct-op', applied_at: '2026-08-24T00:00:00Z',
        items: [{ id: 'revoke-direct', subject_ref: 'agent:builder', subject_name: 'Builder', permission: 'project.write', resource: { kind: 'project', id: 'proj-a', label: 'Project Alpha' }, status: 'allowed', risk: 'medium', high_risk: false, reason: 'direct binding revoked', grant_id: 'grant-custom-1' }],
        summary: { total: 1, succeeded: 1, failed: 0, unauthorized: 0, not_applicable: 0, partial_failure: false },
      })),
    );
    renderPage('/organizations/test/access/subject-access');
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-grants');
    await waitFor(() => expect(screen.getByTestId('access-grants')).toHaveTextContent('agent:builder'));
    const grants = screen.getByTestId('access-grants');
    fireEvent.click(within(grants).getByRole('checkbox', { name: /Select project\.write for revoke/ }));
    expect(within(grants).queryByRole('checkbox', { name: /org\.member\.role\.manage/ })).not.toBeInTheDocument();
    fireEvent.click(within(grants).getByTestId('access-revoke-preview'));

    const preview = await within(grants).findByTestId('access-revoke-preview-panel');
    expect(preview).toHaveTextContent('direct binding and can be revoked');
    fireEvent.click(within(preview).getByTestId('access-revoke-confirm'));

    await within(grants).findByText('direct binding revoked');
    const result = within(grants).getAllByTestId('access-result').at(-1)!;
    expect(result).toHaveTextContent('Revoke result');
    expect(result).toHaveTextContent('direct binding revoked');
    expect(await screen.findByTestId('access-toast')).toHaveTextContent('Revoke completed');
  });

  it('shows a success toast after the Add direct binding flow applies', async () => {
    server.use(
      http.post('*/api/orgs/:slug/access/batch/apply', async ({ request }) => {
        const body = (await request.json()) as { subject_refs?: string[]; permission_keys?: string[]; resources?: Array<{ kind: string; id: string; org_id?: string; label?: string }> };
        return HttpResponse.json({
          operation_id: 'access-op-success',
          applied_at: '2026-08-14T08:01:00Z',
          items: [{
            id: 'item-1',
            subject_ref: body.subject_refs?.[0] ?? 'agent:builder',
            subject_name: 'Builder',
            permission: body.permission_keys?.[0] ?? 'project.write',
            resource: body.resources?.[0] ?? { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' },
            status: 'allowed',
            risk: 'medium',
            high_risk: false,
            reason: 'grant applied by unified authorization API',
            grant_id: 'grant-new-direct',
          }],
          summary: { total: 1, succeeded: 1, failed: 0, unauthorized: 0, not_applicable: 0, partial_failure: false },
        });
      }),
    );
    renderPage('/organizations/test/access/subject-access');
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-subject-view');
    fireEvent.click(screen.getByTestId('access-open-direct-binding'));

    const drawer = await screen.findByTestId('access-batch-drawer');
    fireEvent.click(within(drawer).getByRole('button', { name: /Builder/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /project\.write/ }));
    fireEvent.click(within(drawer).getAllByRole('button', { name: /agent-center core/ })[0]);
    fireEvent.click(within(drawer).getByRole('button', { name: /Project Alpha/ }));
    fireEvent.change(within(drawer).getByTestId('access-batch-reason'), {
      target: { value: 'temporary direct binding' },
    });
    fireEvent.click(within(drawer).getByTestId('access-run-preview'));

    await within(drawer).findByTestId('access-preview-summary');
    fireEvent.click(within(drawer).getByTestId('access-preview-continue'));
    await within(drawer).findByText(/Ready to apply/);
    fireEvent.click(within(drawer).getByTestId('access-apply-batch'));

    expect(await within(drawer).findByTestId('access-result')).toHaveTextContent('Authorization result');
    expect(await screen.findByTestId('access-toast')).toHaveTextContent('Direct binding granted');
  });

  it('keeps the selected subject context and surfaces a 409 apply conflict as a toast', async () => {
    server.use(
      http.post('*/api/orgs/:slug/access/batch/apply', () => HttpResponse.json(
        { error: 'version_conflict', message: 'subject access changed after preview' },
        { status: 409 },
      )),
    );
    renderPage('/organizations/test/access/subject-access');
    await screen.findByTestId('access-subject-view');
    fireEvent.click(screen.getByTestId('access-open-direct-binding'));
    const drawer = await screen.findByTestId('access-batch-drawer');
    expect(within(drawer).getByTestId('access-direct-subject-context')).toHaveTextContent('agent:builder');
    fireEvent.click(within(drawer).getByRole('button', { name: /project\.write/ }));
    fireEvent.click(within(drawer).getAllByRole('button', { name: /agent-center core/ })[0]);
    fireEvent.click(within(drawer).getByRole('button', { name: /Project Alpha/ }));
    fireEvent.change(within(drawer).getByTestId('access-batch-reason'), { target: { value: 'conflict probe' } });
    fireEvent.click(within(drawer).getByTestId('access-run-preview'));
    await within(drawer).findByTestId('access-preview-summary');
    fireEvent.click(within(drawer).getByTestId('access-preview-continue'));
    fireEvent.click(within(drawer).getByTestId('access-apply-batch'));
    expect(await screen.findByTestId('access-toast')).toHaveTextContent('409:');
    expect(screen.getByTestId('access-toast')).toHaveTextContent('subject access changed after preview');
  });

  it('does not preselect resources and clears incompatible resources when permission changes', async () => {
    type PreviewRequestBody = { permission_keys?: string[]; resources?: Array<{ kind: string; id: string; label?: string }> };
    let previewBody: PreviewRequestBody | null = null;
    server.use(
      http.post('*/api/orgs/:slug/access/batch/preview', async ({ request }) => {
        previewBody = (await request.json()) as PreviewRequestBody;
        return HttpResponse.json({
          request_id: 'preview-org-only',
          expires_at: null,
          items: [{
            id: 'item-1',
            subject_ref: 'agent:builder',
            subject_name: 'Builder',
            permission: 'org.read',
            resource: { kind: 'org', id: 'org-test', org_id: 'org-test', label: 'Test Org' },
            status: 'allowed',
            risk: 'low',
            high_risk: false,
            reason: 'grant can be applied by unified authorization API',
          }],
          summary: { total: 1, grantable: 1, high_risk: 0, unauthorized: 0, not_applicable: 0 },
        });
      }),
    );
    renderPage('/organizations/test/access/subject-access');
    await screen.findByTestId('access-subject-view');
    fireEvent.click(screen.getByTestId('access-open-direct-binding'));
    const drawer = await screen.findByTestId('access-batch-drawer');
    const orgResource = within(drawer).getByRole('button', { name: /Test Org/ });
    const projectResource = within(drawer).getByRole('button', { name: /Project Alpha/ });
    expect(orgResource).toHaveAttribute('aria-pressed', 'false');
    expect(projectResource).toHaveAttribute('aria-pressed', 'false');

    fireEvent.click(projectResource);
    expect(projectResource).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(orgResource);
    fireEvent.click(within(drawer).getByRole('button', { name: /org\.read/ }));
    expect(projectResource).toHaveAttribute('aria-pressed', 'false');
    expect(projectResource).toBeDisabled();
    expect(orgResource).toHaveAttribute('aria-pressed', 'true');

    fireEvent.change(within(drawer).getByTestId('access-batch-reason'), { target: { value: 'org-only direct grant' } });
    fireEvent.click(within(drawer).getByTestId('access-run-preview'));
    await within(drawer).findByTestId('access-preview-summary');
    const capturedPreviewBody = previewBody as PreviewRequestBody | null;
    expect(capturedPreviewBody?.permission_keys).toEqual(['org.read']);
    expect(capturedPreviewBody?.resources).toEqual([{ kind: 'org', id: 'org-test', label: 'Test Org' }]);
  });

  it('renders partial failure result items without unknown phantom rows', async () => {
    server.use(
      http.post('*/api/orgs/:slug/access/batch/preview', () => HttpResponse.json({
        request_id: 'preview-partial',
        expires_at: null,
        items: [{
          id: 'item-1',
          subject_ref: 'agent:builder',
          subject_name: 'Builder',
          permission: 'org.read',
          resource: { kind: 'org', id: 'org-test', org_id: 'org-test', label: 'Test Org' },
          status: 'allowed',
          risk: 'low',
          high_risk: false,
          reason: 'grant can be applied by unified authorization API',
        }],
        summary: { total: 1, grantable: 1, high_risk: 0, unauthorized: 0, not_applicable: 0 },
      })),
      http.post('*/api/orgs/:slug/access/batch/apply', () => HttpResponse.json({
        operation_id: 'access-op-partial',
        applied_at: '2026-08-14T08:01:00Z',
        items: [
          {
            id: 'item-1',
            subject_ref: 'agent:builder',
            subject_name: 'Builder',
            permission: 'org.read',
            resource: { kind: 'org', id: 'org-test', org_id: 'org-test', label: 'Test Org' },
            status: 'allowed',
            risk: 'low',
            high_risk: false,
            reason: 'grant applied by unified authorization API',
          },
          {
            id: 'item-2',
            subject_ref: 'agent:builder',
            subject_name: 'Builder',
            permission: 'org.read',
            resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' },
            status: 'not_applicable',
            risk: 'low',
            high_risk: false,
            reason: 'org.read does not apply to project',
          },
        ],
        summary: { total: 2, succeeded: 1, failed: 1, unauthorized: 0, not_applicable: 1, partial_failure: true },
      })),
    );
    renderPage('/organizations/test/access/subject-access');
    await screen.findByTestId('access-subject-view');
    fireEvent.click(screen.getByTestId('access-open-direct-binding'));
    const drawer = await screen.findByTestId('access-batch-drawer');
    fireEvent.click(within(drawer).getByRole('button', { name: /org\.read/ }));
    fireEvent.click(within(drawer).getByRole('button', { name: /Test Org/ }));
    fireEvent.change(within(drawer).getByTestId('access-batch-reason'), { target: { value: 'partial mapping' } });
    fireEvent.click(within(drawer).getByTestId('access-run-preview'));
    await within(drawer).findByTestId('access-preview-summary');
    fireEvent.click(within(drawer).getByTestId('access-preview-continue'));
    fireEvent.click(within(drawer).getByTestId('access-apply-batch'));

    const result = await within(drawer).findByTestId('access-result');
    expect(result).toHaveTextContent('1 succeeded, 1 failed, 0 no access, 1 not applicable');
    const rows = within(result).getAllByRole('row');
    expect(rows).toHaveLength(3);
    expect(result).not.toHaveTextContent('Unknown resource');
    expect(result).not.toHaveTextContent('unknown');
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

    renderPage('/organizations/test/access/subject-access');
    await screen.findByTestId('access-grants');
    await waitFor(() => expect(screen.getByTestId('access-grants')).toHaveTextContent('agent:builder'));
    const grants = screen.getByTestId('access-grants');
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
      stable_key: 'release-operator',
      name: 'Release operator',
      kind: 'custom',
      description: 'release work',
      scope: 'team',
      version: 1,
      permissions: ['org.read', 'project.write'],
      risk: 'medium',
    };
    let roleDetail = {
      id: roleV1.id,
      stable_key: 'release-operator',
      name: roleV1.name,
      description: roleV1.description,
      kind: roleV1.kind,
      scope: 'team',
      latest: roleV1,
      versions: [roleV1],
      references: [],
    };
    let publishBody: { name?: string; stable_key?: string; description?: string; scope?: string; permissions?: string[]; expected_latest_version?: number } | null = null;
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
        const body = (await request.json()) as { name: string; stable_key?: string; description?: string; scope?: string; permissions: string[] };
        roleDetail = {
          id: roleV1.id,
          stable_key: body.stable_key ?? 'release-operator',
          name: body.name,
          kind: roleV1.kind,
          description: body.description ?? '',
          scope: body.scope ?? 'team',
          latest: { ...roleV1, stable_key: body.stable_key ?? 'release-operator', scope: body.scope ?? 'team', name: body.name, description: body.description ?? '', permissions: body.permissions },
          versions: [{ ...roleV1, stable_key: body.stable_key ?? 'release-operator', scope: body.scope ?? 'team', name: body.name, description: body.description ?? '', permissions: body.permissions }],
          references: [],
        };
        return HttpResponse.json(roleDetail, { status: 201 });
      }),
      http.post('/api/orgs/:slug/access/ram-roles/role-created/versions', async ({ request }) => {
        publishBody = (await request.json()) as { name?: string; stable_key?: string; description?: string; scope?: string; permissions: string[]; expected_latest_version?: number };
        const latest = { ...roleDetail.latest, version: 2, permissions: publishBody.permissions ?? [], risk: 'high' };
        roleDetail = { ...roleDetail, latest, versions: [latest, roleDetail.latest] };
        return HttpResponse.json(roleDetail, { status: 201 });
      }),
    );

    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-roles-view');
    const view = await screen.findByTestId('access-roles-view');
    expect(await within(view).findByTestId('access-role-row-team-curator')).toHaveTextContent('v2');
    fireEvent.click(within(view).getByTestId('access-role-row-team-curator'));
    expect(await within(view).findByTestId('access-role-versions')).toHaveTextContent('v1');
    expect(within(view).getByTestId('access-role-versions')).toHaveTextContent('v2');

    fireEvent.click(within(view).getByTestId('access-role-new'));
    const create = await screen.findByTestId('access-role-drawer');
    fireEvent.change(within(create).getByTestId('access-role-name'), { target: { value: 'Release operator' } });
    fireEvent.change(within(create).getByTestId('access-role-stable-key'), { target: { value: 'release-operator' } });
    fireEvent.change(within(create).getByTestId('access-role-description'), { target: { value: 'release work' } });
    fireEvent.click(within(create).getByText('org.read'));
    fireEvent.click(within(create).getByText('project.write'));
    fireEvent.click(within(create).getByTestId('access-role-create-submit'));

    await waitFor(() => expect(screen.getByTestId('access-role-detail')).toHaveTextContent('Release operator'));
    const detail = screen.getByTestId('access-role-detail');
    await waitFor(() => expect(within(detail).getByTestId('access-role-new-version-submit')).toHaveTextContent('Create version'));
    expect(within(detail).getByTestId('access-role-new-version-submit')).not.toBeDisabled();
    fireEvent.click(within(detail).getByText('team.memory.review'));
    fireEvent.click(within(detail).getByTestId('access-role-new-version-submit'));

    await waitFor(() => {
      expect(publishBody).toEqual({
        name: 'Release operator',
        stable_key: 'release-operator',
        description: 'release work',
        scope: 'team',
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

  it('keeps Used by Team Roles read-only and sends changes to the Team Role editor', async () => {
    const oldRole = {
      id: 'role-old',
      name: 'Old deployer',
      kind: 'custom',
      description: 'legacy deploy access',
      version: 3,
      permissions: ['project.write'],
      risk: 'medium',
      created_at: '2026-08-14T08:02:00Z',
    };
    const targetRole = {
      id: 'role-target',
      name: 'New deployer',
      kind: 'custom',
      description: 'replacement deploy access',
      version: 1,
      permissions: ['project.write', 'team.read'],
      risk: 'medium',
      created_at: '2026-08-14T08:03:00Z',
    };
    server.use(
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles: [oldRole, targetRole] })),
      http.get('/api/orgs/:slug/access/ram-roles/role-old', () => HttpResponse.json({
        id: oldRole.id,
        stable_key: 'old-deployer',
        name: oldRole.name,
        kind: oldRole.kind,
        description: oldRole.description,
        scope: 'team',
        latest: oldRole,
        versions: [oldRole],
        references: [],
      })),
      http.get('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles', () => HttpResponse.json({
        team_id: 'team-7c19b0',
        team_role: 'planner',
        ram_role_ids: ['role-old', 'team-basic'],
        version: 9,
      })),
    );

    renderPage();
    const view = await screen.findByTestId('access-roles-view');
    fireEvent.click(await within(view).findByTestId('access-role-row-role-old'));

    const detail = await screen.findByTestId('access-role-detail');
    await waitFor(() => expect(detail).toHaveTextContent('Used by Team Roles (read-only)'));
    expect(detail).toHaveTextContent('agent-center core / planner');
    expect(detail).toHaveTextContent('Open the Team Role to change its RAM Roles.');
    expect(within(detail).getByTestId('access-role-disable-submit')).toBeDisabled();
    expect(within(detail).getByTestId('access-role-delete-blocked')).toHaveTextContent('cannot be deleted');
    fireEvent.click(within(detail).getByTestId('access-role-view-references'));
    const references = within(detail).getByTestId('access-role-references');
    expect(references).toHaveTextContent('agent-center core / planner');
    expect(within(references).getByRole('link', { name: 'Open Team Role' })).toHaveAttribute('href', '/teams/team-7c19b0/roles/planner');
    expect(within(references).queryByRole('button', { name: /migrate|remove|save/i })).not.toBeInTheDocument();
    expect(within(references).queryByRole('combobox')).not.toBeInTheDocument();
  });

  it('requires the full RAM role name before deleting an unreferenced custom role', async () => {
    const role = {
      id: 'role-unused',
      name: 'Unused reviewer',
      kind: 'custom',
      description: 'cleanup candidate',
      version: 2,
      permissions: ['team.read'],
      risk: 'low',
      created_at: '2026-08-14T08:02:00Z',
    };
    let deleteBody: { expected_latest_version?: number; confirm_unreferenced?: boolean; reason?: string } | null = null;
    server.use(
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({ roles: [role] })),
      http.get('/api/orgs/:slug/access/ram-roles/role-unused', () => HttpResponse.json({
        id: role.id,
        stable_key: 'unused-reviewer',
        name: role.name,
        kind: role.kind,
        description: role.description,
        scope: 'team',
        latest: role,
        versions: [role],
        references: [],
      })),
      http.get('*/api/orgs/:slug/teams/team-7c19b0/roles/planner/ram-roles', () => HttpResponse.json({
        team_id: 'team-7c19b0',
        team_role: 'planner',
        ram_role_ids: ['team-basic'],
        version: 7,
      })),
      http.delete('/api/orgs/:slug/access/ram-roles/role-unused', async ({ request }) => {
        deleteBody = await request.json() as typeof deleteBody;
        return new HttpResponse(null, { status: 204 });
      }),
    );

    renderPage();
    const view = await screen.findByTestId('access-roles-view');
    fireEvent.click(await within(view).findByTestId('access-role-row-role-unused'));
    const detail = await screen.findByTestId('access-role-detail');
    await waitFor(() => expect(detail).toHaveTextContent('Unused reviewer'));
    expect(within(detail).getByTestId('access-role-disable-submit')).toBeDisabled();
    fireEvent.change(within(detail).getByTestId('access-role-delete-name'), { target: { value: 'Unused' } });
    expect(within(detail).getByTestId('access-role-disable-submit')).toBeDisabled();
    fireEvent.change(within(detail).getByTestId('access-role-delete-name'), { target: { value: 'Unused reviewer' } });
    fireEvent.click(within(detail).getByTestId('access-role-disable-submit'));

    await waitFor(() => expect(deleteBody).toEqual({
      expected_latest_version: 2,
      confirm_unreferenced: true,
      reason: 'RAM role deleted after typed-name confirmation',
    }));
    expect(await screen.findByTestId('access-role-success')).toHaveTextContent('Deleted RAM Role Unused reviewer.');
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
    let publishBody: { name?: string; stable_key?: string; description?: string; scope?: string; permissions?: string[]; expected_latest_version?: number } | null = null;
    server.use(
      http.get('/api/orgs/:slug/access/ram-roles', () => HttpResponse.json({
        roles: [roleV1],
      })),
      http.get('/api/orgs/:slug/access/ram-roles/role-cas', () => HttpResponse.json({
        id: roleV1.id,
        stable_key: 'deploy-operator',
        name: roleV1.name,
        kind: roleV1.kind,
        description: roleV1.description,
        scope: 'team',
        latest: roleV1,
        versions: [roleV1],
        references: [],
      })),
      http.post('/api/orgs/:slug/access/ram-roles/role-cas/versions', async ({ request }) => {
        publishBody = (await request.json()) as { name?: string; stable_key?: string; description?: string; scope?: string; permissions: string[]; expected_latest_version?: number };
        return HttpResponse.json(
          { error: 'version_conflict', message: 'access RAM role latest version changed' },
          { status: 409 },
        );
      }),
    );

    renderPage();
    expect(await screen.findByTestId('page-Access')).toBeInTheDocument();
    await screen.findByTestId('access-roles-view');
    const view = await screen.findByTestId('access-roles-view');
    fireEvent.click(await within(view).findByTestId('access-role-row-role-cas'));
    const detail = await screen.findByTestId('access-role-detail');
    await waitFor(() => expect(detail).toHaveTextContent('Latest v1'));

    fireEvent.click(within(detail).getByText('team.memory.review'));
    fireEvent.click(within(detail).getByTestId('access-role-new-version-submit'));

    await waitFor(() => {
      expect(publishBody).toEqual({
        name: 'Deploy operator',
        stable_key: 'deploy-operator',
        description: 'deploy work',
        scope: 'team',
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

  it.each([
    ['ram-roles', '/organizations/test/access/ram-roles', 'RAM Roles'],
    ['subject-access', '/organizations/test/access/subject-access', 'Subject access'],
  ])('gates the %s route with a page-specific forbidden state', async (page, path, title) => {
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
            reason: 'admin role cannot manage this owner-only permission',
            evidence_ref: 'members:mem-ops',
          },
          effective: [],
          denied_by: ['admin role cannot manage this owner-only permission'],
        }),
      ),
    );

    renderPage(path, 'user:ops');

    const forbidden = await screen.findByTestId(`access-${page}-forbidden`);
    expect(forbidden).toHaveTextContent(`${title} unavailable (403)`);
    await waitFor(() => expect(forbidden).toHaveTextContent('admin role cannot manage this owner-only permission'));
    expect(screen.queryByTestId('access-subject-view')).not.toBeInTheDocument();
  });
});

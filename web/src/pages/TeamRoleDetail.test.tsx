import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import { OrgContext } from '@/OrgContext';
import { useAppStore } from '@/store/app';
import { resetTeamsStore } from '@/api/teamsFixtures';
import { server } from '@/test/mswServer';
import TeamRoleDetail from './TeamRoleDetail';

function renderPage(role = 'planner', subject = 'user:hayang') {
  window.history.pushState({}, '', `/organizations/test/teams/team-7c19b0/roles/${role}`);
  useAppStore.setState({ currentUserId: subject });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug: 'test', orgId: 'org-test', orgName: 'Test', role: 'owner' }}>
        <BrowserRouter><Routes><Route path="/organizations/:slug/teams/:teamId/roles/:role" element={<TeamRoleDetail />} /></Routes></BrowserRouter>
      </OrgContext.Provider>
    </QueryClientProvider>,
  );
}

afterEach(() => { cleanup(); useAppStore.setState({ currentUserId: '' }); });
beforeEach(() => resetTeamsStore());

describe('TeamRoleDetail canonical Team Role surface', () => {
  it('renders list, detail, work/access configuration, audit, and no relationship-management copy', async () => {
    renderPage();
    const page = await screen.findByTestId('page-TeamRoleDetail');
    expect(within(page).getByRole('heading', { name: 'planner' })).toBeInTheDocument();
    expect(within(page).getByTestId('team-role-work-configuration')).toHaveTextContent('claude-code');
    expect(within(page).getByTestId('team-role-access-configuration')).toHaveTextContent('Team contributor');
    expect(within(page).getByTestId('team-role-audit')).toHaveTextContent('server');
    expect(within(page).getByTestId('team-role-list')).toHaveTextContent('reviewer');
    expect(page.textContent?.toLowerCase()).not.toMatch(/mapping|replace mapping/);
  });

  it('adds/removes RAM Roles, previews impact, saves with CAS, then shows server readback', async () => {
    let mapping = { team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: ['team-basic'], version: 7, updated_by: 'alice', updated_at: '2026-08-24T01:00:00Z' };
    let putBody: { ram_role_ids?: string[]; expected_version?: number } | undefined;
    server.use(
      http.get('*/api/orgs/:slug/teams/:id/roles/:role/ram-roles', () => HttpResponse.json(mapping)),
      http.post('*/api/orgs/:slug/teams/:id/roles/:role/ram-roles/preview', async ({ request }) => {
        const body = await request.json() as { ram_role_ids: string[] };
        return HttpResponse.json({ team_id: mapping.team_id, team_role: mapping.team_role, current_ram_role_ids: mapping.ram_role_ids, next_ram_role_ids: body.ram_role_ids, added_ram_role_ids: body.ram_role_ids.filter((id) => !mapping.ram_role_ids.includes(id)), removed_ram_role_ids: mapping.ram_role_ids.filter((id) => !body.ram_role_ids.includes(id)), affected_members: 1, affected_project_ids: ['project-a', 'project-b'], version: mapping.version });
      }),
      http.put('*/api/orgs/:slug/teams/:id/roles/:role/ram-roles', async ({ request }) => {
        putBody = await request.json() as typeof putBody;
        mapping = { ...mapping, ram_role_ids: putBody?.ram_role_ids ?? [], version: 8, updated_by: 'user:hayang', updated_at: '2026-08-24T02:00:00Z' };
        return HttpResponse.json(mapping);
      }),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('team-role-edit'));
    const editor = await screen.findByTestId('team-role-editor');
    fireEvent.click(within(editor).getByTestId('team-role-ram-roles-trigger'));
    const options = await screen.findByTestId('team-role-ram-roles-options');
    fireEvent.click(within(options).getAllByTestId('team-role-ram-roles-option').find((option) => option.getAttribute('data-value') === 'team-contributor') as HTMLElement);
    await waitFor(() => expect(within(editor).getByTestId('team-role-impact')).toHaveTextContent('Affected members1'));
    fireEvent.click(within(editor).getByTestId('team-role-save'));
    await waitFor(() => expect(putBody?.expected_version).toBe(7));
    expect(await screen.findByTestId('team-role-success')).toHaveTextContent('Server readback confirmed version 8');
    expect(screen.getByTestId('team-role-audit')).toHaveTextContent('user:hayang');
  });

  it('preserves the editor on 409 and refreshes the authoritative version before retry', async () => {
    let version = 4;
    server.use(
      http.get('*/api/orgs/:slug/teams/:id/roles/:role/ram-roles', () => HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: ['team-contributor'], version })),
      http.post('*/api/orgs/:slug/teams/:id/roles/:role/ram-roles/preview', async ({ request }) => {
        const body = await request.json() as { ram_role_ids: string[] };
        return HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', current_ram_role_ids: ['team-contributor'], next_ram_role_ids: body.ram_role_ids, added_ram_role_ids: [], removed_ram_role_ids: ['team-contributor'], affected_members: 1, affected_project_ids: [], version });
      }),
      http.put('*/api/orgs/:slug/teams/:id/roles/:role/ram-roles', () => { version = 5; return HttpResponse.json({ error: 'version_conflict', message: 'version_conflict' }, { status: 409 }); }),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('team-role-edit'));
    const editor = await screen.findByTestId('team-role-editor');
    fireEvent.click(within(editor).getByRole('button', { name: 'Remove' }));
    await waitFor(() => expect(within(editor).getByTestId('team-role-save')).toBeEnabled());
    fireEvent.click(within(editor).getByTestId('team-role-save'));
    const conflict = await within(editor).findByTestId('team-role-conflict');
    expect(conflict).toHaveTextContent('409');
    expect(screen.queryByTestId('team-role-success')).not.toBeInTheDocument();
    fireEvent.click(within(conflict).getByRole('button', { name: 'Refresh latest' }));
    await waitFor(() => expect(editor).toHaveTextContent('server version 5'));
  });

  it('renders a read-only permission gate without exposing mutation controls', async () => {
    renderPage('planner', 'user:ops');
    expect(await screen.findByTestId('team-role-permission-gate')).toHaveTextContent('org.member.role.manage');
    expect(screen.getByTestId('team-role-edit')).toBeDisabled();
  });

  it('renders explicit loading, error, and empty RAM Roles states', async () => {
    server.use(http.get('*/api/orgs/:slug/teams/:id', () => new Promise(() => undefined)));
    const loading = renderPage();
    expect(screen.getByTestId('team-role-loading')).toHaveAttribute('aria-busy', 'true');
    loading.unmount();

    server.use(http.get('*/api/orgs/:slug/teams/:id', () => HttpResponse.json({ message: 'team unavailable' }, { status: 503 })));
    const error = renderPage();
    expect(await screen.findByTestId('team-role-error')).toHaveTextContent('team unavailable');
    error.unmount();

    server.resetHandlers();
    resetTeamsStore();
    server.use(http.get('*/api/orgs/:slug/teams/:id/roles/:role/ram-roles', () => HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'planner', ram_role_ids: [], version: 1 })));
    renderPage();
    expect(await screen.findByTestId('team-role-ram-roles-empty')).toHaveTextContent('No RAM Roles');
  });
});

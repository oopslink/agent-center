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

describe('RAM Roles page', () => {
  it('renders an independent RAM Role catalog with stats, filters, detail, references, and history', async () => {
    renderPage();

    expect(await screen.findByTestId('page-RAMRoles')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'RAM Roles' })).toBeInTheDocument();
    expect(await screen.findByTestId('ram-roles-list')).toBeInTheDocument();
    expect(await screen.findByTestId('ram-role-row-team-curator')).toHaveTextContent('Team curator');

    fireEvent.change(screen.getByTestId('ram-roles-search'), { target: { value: 'reviewer' } });
    await waitFor(() => expect(screen.getByTestId('ram-role-row-team-curator')).toBeInTheDocument());
    expect(screen.queryByTestId('ram-role-row-team-basic')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('ram-role-row-team-curator'));
    const detail = await screen.findByTestId('ram-role-detail');
    expect(detail).toHaveTextContent('Team curator');
    expect(await screen.findByTestId('ram-role-permission-summary')).toHaveTextContent('team.memory.review');
    expect(await screen.findByTestId('ram-role-team-references')).toHaveTextContent('reviewer');
    expect(await screen.findByTestId('ram-role-version-history')).toHaveTextContent('v2');
  });

  it('blocks deletion while referenced, then migrates Team Role references before revoke', async () => {
    let putBody: { ram_role_ids?: string[]; expected_version?: number } | null = null;
    let revokeBody: { expected_latest_version?: number; reason?: string } | null = null;
    server.use(
      http.put('*/api/orgs/:slug/teams/team-7c19b0/roles/reviewer/ram-roles', async ({ request }) => {
        putBody = await request.json() as typeof putBody;
        return HttpResponse.json({ team_id: 'team-7c19b0', team_role: 'reviewer', ram_role_ids: putBody?.ram_role_ids ?? [], version: 2 });
      }),
      http.post('*/api/orgs/:slug/access/ram-roles/team-curator/revoke', async ({ request }) => {
        revokeBody = await request.json() as typeof revokeBody;
        return new HttpResponse(null, { status: 204 });
      }),
    );

    renderPage();
    fireEvent.click(await screen.findByTestId('ram-role-row-team-curator'));
    expect(await screen.findByTestId('ram-role-team-references')).toHaveTextContent('reviewer');

    fireEvent.click(screen.getByTestId('ram-role-delete'));
    const confirm = await screen.findByTestId('confirm-modal');
    expect(within(confirm).getByTestId('ram-role-delete-confirm')).toHaveTextContent('Delete is blocked');
    expect(within(confirm).getByTestId('confirm-modal-confirm')).toBeDisabled();

    fireEvent.change(within(confirm).getByTestId('ram-role-delete-migration'), { target: { value: 'team-basic' } });
    expect(within(confirm).getByTestId('confirm-modal-confirm')).not.toBeDisabled();
    fireEvent.click(within(confirm).getByTestId('confirm-modal-confirm'));

    await waitFor(() => expect(putBody).toEqual({ ram_role_ids: ['team-basic'], expected_version: 1 }));
    await waitFor(() => expect(revokeBody).toEqual({ expected_latest_version: 2, reason: 'RAM Roles page revoke safeguard' }));
    expect(await screen.findByTestId('ram-role-notice')).toHaveTextContent('Revoked RAM Role Team curator');
  });
});

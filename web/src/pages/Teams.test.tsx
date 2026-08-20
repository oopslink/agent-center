import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import { http, HttpResponse } from 'msw';
import Teams from './Teams';
import { resetTeamsStore } from '@/api/teamsFixtures';
import { server } from '@/test/mswServer';

function Loc(): React.ReactElement {
  const l = useLocation();
  return <div data-testid="loc">{l.pathname}</div>;
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/teams']}>
        <Routes>
          <Route path="/teams" element={<Teams />} />
          <Route path="/teams/:teamId" element={<Loc />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Teams list', () => {
  afterEach(() => cleanup());
  beforeEach(() => resetTeamsStore());

  it('renders the seeded teams in a table', async () => {
    renderPage();
    expect(await screen.findByTestId('teams-table')).toBeInTheDocument();
    expect(screen.getByTestId('team-row-team-7c19b0')).toBeInTheDocument();
    expect(screen.getByText('agent-center core')).toBeInTheDocument();
    expect(screen.getByText('growth-experiments')).toBeInTheDocument();
    // draft status chip present for docs-and-dx
    expect(screen.getAllByTestId('team-status-active').length).toBeGreaterThan(0);
    expect(screen.getByTestId('team-status-draft')).toBeInTheDocument();
  });

  it('navigates to a team on row click', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('team-row-team-4a1f22'));
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/teams/team-4a1f22'));
  });

  it('creates a team through the role-builder modal', async () => {
    let body: { candidate_assignments?: Array<{ subject_ref: string; role: string }>; roles?: Array<{ access_requirements?: string[] }> } | undefined;
    server.use(http.post('/api/teams', async ({ request }) => {
      body = await request.json() as typeof body;
      return HttpResponse.json({
        id: 'team-created',
        org_id: 'org-test',
        name: 'payments-squad',
        description: '',
        roles: [],
        version: 1,
        glyph: 'PS',
        status: 'draft',
        members_count: 0,
        projects_count: 0,
        created: '2026-08-14T08:00:00Z',
      }, { status: 201 });
    }));
    renderPage();
    fireEvent.click(await screen.findByTestId('teams-new'));
    const modal = await screen.findByTestId('new-team-modal');

    fireEvent.change(within(modal).getByTestId('new-team-name'), { target: { value: 'payments-squad' } });

    // stepper: bump the coder count (role index 1)
    const before = within(modal).getByTestId('new-team-role-1-count').textContent;
    fireEvent.click(within(modal).getByTestId('new-team-role-1-inc'));
    expect(within(modal).getByTestId('new-team-role-1-count').textContent).not.toBe(before);
    fireEvent.click(within(modal).getByTestId('new-team-role-1-dec'));

    // add + remove a role
    fireEvent.click(within(modal).getByTestId('new-team-add-role'));
    expect(within(modal).getByTestId('new-team-role-2')).toBeInTheDocument();
    fireEvent.click(within(modal).getByTestId('new-team-role-2-remove'));

    const role = await within(modal).findByTestId('new-team-role-0-access-role');
    fireEvent.change(role, { target: { value: 'team-contributor@1' } });
    expect(within(modal).getByTestId('new-team-role-0-access-permissions')).toHaveTextContent('team.memory.propose');

    fireEvent.change(await within(modal).findByTestId('new-team-assignment-subject'), { target: { value: 'agent:agent-d5' } });
    fireEvent.change(within(modal).getByTestId('new-team-assignment-role'), { target: { value: 'planner' } });
    fireEvent.click(within(modal).getByTestId('new-team-assignment-add'));
    expect(within(modal).getByTestId('new-team-assignment-preview')).toHaveTextContent('agent:agent-d5 -> planner');
    expect(within(modal).getByTestId('new-team-assignment-preview')).toHaveTextContent('4 permissions');

    await waitFor(() => expect(within(modal).getByTestId('new-team-submit')).not.toBeDisabled());
    fireEvent.click(within(modal).getByTestId('new-team-submit'));
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toMatch(/^\/teams\/team-/));
    expect(body?.candidate_assignments).toEqual([{ subject_ref: 'agent:agent-d5', role: 'planner' }]);
    expect(body?.roles?.[0]?.access_requirements).toEqual(['team.read', 'team.write', 'team.memory.read', 'team.memory.propose']);
  });

  it('blocks team creation when role runtime selection is not in the AI Runtime catalog', async () => {
    server.use(
      http.get('/api/ai-runtime', () =>
        HttpResponse.json({
          revision: 1,
          clis: [{ id: 'cli-codex', key: 'codex', display_name: 'Codex', executable: 'codex', enabled: true }],
          models: [{ id: 'model-gpt', key: 'gpt', model_key: 'gpt-5', display_name: 'GPT-5', compatible_cli_keys: ['codex'], enabled: true }],
          roles: [],
        }),
      ),
    );
    renderPage();
    fireEvent.click(await screen.findByTestId('teams-new'));
    const modal = await screen.findByTestId('new-team-modal');

    fireEvent.change(within(modal).getByTestId('new-team-name'), { target: { value: 'runtime-blocked' } });

    await waitFor(() =>
      expect(within(modal).getByTestId('new-team-runtime-validation-error')).toHaveTextContent(/enabled AI Runtime CLI\/model/i),
    );
    expect(within(modal).getByTestId('new-team-role-0-runtime-error')).toBeInTheDocument();
    expect(within(modal).getByTestId('new-team-submit')).toBeDisabled();
  });

  it('prevents creating a team when any role has no name', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('teams-new'));
    const modal = await screen.findByTestId('new-team-modal');

    fireEvent.change(within(modal).getByTestId('new-team-name'), { target: { value: 'test' } });
    fireEvent.click(within(modal).getByTestId('new-team-role-1-remove'));
    fireEvent.change(within(modal).getByTestId('new-team-role-0-name'), { target: { value: '' } });
    fireEvent.change(within(modal).getByTestId('new-team-role-0-tags'), { target: { value: 'go' } });

    expect(within(modal).getByTestId('new-team-validation-error')).toHaveTextContent('Each role needs a role name.');
    expect(within(modal).getByTestId('new-team-submit')).toBeDisabled();
  });

  it('creates an empty team after removing every role', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('teams-new'));
    const modal = await screen.findByTestId('new-team-modal');

    fireEvent.change(within(modal).getByTestId('new-team-name'), { target: { value: 'empty-squad' } });
    fireEvent.click(within(modal).getByTestId('new-team-role-1-remove'));
    fireEvent.click(within(modal).getByTestId('new-team-role-0-remove'));

    fireEvent.click(within(modal).getByTestId('new-team-submit'));
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toMatch(/^\/teams\/team-/));
  });

  it('closes the modal via the close button', async () => {
    renderPage();
    fireEvent.click(await screen.findByTestId('teams-new'));
    const modal = await screen.findByTestId('new-team-modal');
    fireEvent.click(within(modal).getByTestId('new-team-modal-close'));
    await waitFor(() => expect(screen.queryByTestId('new-team-modal')).not.toBeInTheDocument());
  });
});

import { beforeEach, describe, expect, it } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import Teams from './Teams';
import { resetTeamsStore } from '@/api/teamsFixtures';

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

    fireEvent.click(within(modal).getByTestId('new-team-submit'));
    await waitFor(() => expect(screen.getByTestId('loc').textContent).toMatch(/^\/teams\/team-/));
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

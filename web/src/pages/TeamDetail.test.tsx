import { beforeEach, describe, expect, it } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import TeamDetail from './TeamDetail';
import { resetTeamsStore } from '@/api/teamsFixtures';

function Loc(): React.ReactElement {
  const l = useLocation();
  return <div data-testid="loc">{l.pathname}</div>;
}

function renderAt(id: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/teams/${id}`]}>
        <Routes>
          <Route path="/teams/:teamId" element={<TeamDetail />} />
          <Route path="/teams" element={<Loc />} />
          <Route path="/teams/templates" element={<Loc />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('TeamDetail', () => {
  beforeEach(() => resetTeamsStore());

  it('shows the overview tab by default with the role配比', async () => {
    renderAt('team-7c19b0');
    expect(await screen.findByRole('heading', { name: 'agent-center core' })).toBeInTheDocument();
    expect(screen.getByText('角色配比')).toBeInTheDocument();
    expect(screen.getByText('健康度')).toBeInTheDocument();
  });

  it('renders an error for an unknown team', async () => {
    renderAt('team-does-not-exist');
    expect(await screen.findByTestId('team-detail-error')).toHaveTextContent('team_not_found');
  });

  it('switches to the Members tab and lists seeded members', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    expect(await screen.findByTestId('members-table')).toBeInTheDocument();
    expect(screen.getByText('planner-01')).toBeInTheDocument();
    expect(screen.getByTestId('members-exclusivity-note')).toBeInTheDocument();
  });

  it('adds a free agent through the add-member modal', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-add'));
    const modal = await screen.findByTestId('add-member-modal');
    fireEvent.click(within(modal).getByTestId('add-member-submit'));
    await waitFor(() => expect(screen.getByText('coder-04')).toBeInTheDocument());
  });

  it('requires a migration confirm for a busy agent', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-add'));
    const modal = await screen.findByTestId('add-member-modal');
    fireEvent.change(within(modal).getByTestId('add-member-agent'), { target: { value: 'busy' } });
    fireEvent.click(within(modal).getByTestId('add-member-submit'));
    const migrate = await screen.findByTestId('migrate-modal');
    fireEvent.click(within(migrate).getByTestId('migrate-confirm'));
    await waitFor(() => expect(screen.getByText('coder-09')).toBeInTheDocument());
  });

  it('removes a member with the confirm modal', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('member-remove-agent:9a70…'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(screen.queryByText('planner-01')).not.toBeInTheDocument());
  });

  it('associates and unlinks a project', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-pj'));
    expect(await screen.findByTestId('assoc-project-c7073e48')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('associate-project'));
    await waitFor(() => expect(screen.getByText('new-project')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('unlink-project-c7073e48'));
    fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(screen.queryByTestId('assoc-project-c7073e48')).not.toBeInTheDocument());
  });

  it('renders the read-only team-memory two-pane', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-tm'));
    expect(await screen.findByTestId('memory-pane')).toBeInTheDocument();
    fireEvent.click(await screen.findByTestId('memory-node-ci-runbook'));
    await waitFor(() => expect(screen.getByTestId('memory-view')).toHaveTextContent('CI/CD runbook'));
  });

  it('runs the Extract → Template curation gate', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('team-extract'));
    const modal = await screen.findByTestId('extract-modal');
    // default seeds one high-risk kept? No — defaults scrub all hi. Gate passes.
    await waitFor(() => expect(within(modal).getByTestId('extract-gate')).toHaveTextContent('已过门'));
    // keep a high-risk token → gate blocks
    fireEvent.click(within(modal).getByTestId('scrub-0-keep'));
    await waitFor(() => expect(within(modal).getByTestId('extract-gate')).toHaveTextContent('门未通过'));
    expect(within(modal).getByTestId('extract-save')).toBeDisabled();
    // scrub it back → save enabled → save navigates to templates
    fireEvent.click(within(modal).getByTestId('scrub-0-scrub'));
    await waitFor(() => expect(within(modal).getByTestId('extract-save')).not.toBeDisabled());
    fireEvent.click(within(modal).getByTestId('extract-save'));
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/teams/templates'));
  });
});

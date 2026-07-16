import { beforeEach, describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import TeamDetail from './TeamDetail';
import { resetTeamsStore } from '@/api/teamsFixtures';
import { server } from '@/test/mswServer';

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
    expect(screen.getByText('Role mix')).toBeInTheDocument();
    expect(screen.getByText('Team overview')).toBeInTheDocument();
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

  it('adds a free agent (real directory ref) through the add-member modal', async () => {
    renderAt('team-7c19b0');
    fireEvent.click(await screen.findByTestId('tab-mm'));
    fireEvent.click(await screen.findByTestId('members-add'));
    const modal = await screen.findByTestId('add-member-modal');
    // pick a real agent not on any team (free) → direct add, canonical ref
    fireEvent.change(await within(modal).findByTestId('add-member-agent'), { target: { value: 'agent:agent-d5' } });
    fireEvent.click(within(modal).getByTestId('add-member-submit'));
    await waitFor(() => expect(screen.getByText('agent-center-dev5')).toBeInTheDocument());
    // the stored member_ref is the full canonical ref (no truncation)
    expect(await screen.findByTestId('member-row-agent:agent-d5')).toBeInTheDocument();
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
    await waitFor(() => expect(within(modal).getByTestId('extract-gate')).toHaveTextContent('Curation cleared'));
    // keep a high-risk token → gate blocks
    fireEvent.click(within(modal).getByTestId('scrub-0-keep'));
    await waitFor(() => expect(within(modal).getByTestId('extract-gate')).toHaveTextContent('Gate not passed'));
    expect(within(modal).getByTestId('extract-save')).toBeDisabled();
    // scrub it back → save enabled → save navigates to templates
    fireEvent.click(within(modal).getByTestId('scrub-0-scrub'));
    await waitFor(() => expect(within(modal).getByTestId('extract-save')).not.toBeDisabled());
    fireEvent.click(within(modal).getByTestId('extract-save'));
    await waitFor(() => expect(screen.getByTestId('loc')).toHaveTextContent('/teams/templates'));
  });
});

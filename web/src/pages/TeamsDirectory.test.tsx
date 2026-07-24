import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { cleanup, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import TeamsDirectoryAgents from './TeamsDirectoryAgents';
import TeamsDirectoryHumans from './TeamsDirectoryHumans';
import { resetTeamsStore } from '@/api/teamsFixtures';

function renderPage(el: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{el}</MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

// ---------------------------------------------------------------------------
// Merged Agents directory: directory (team membership + runtime) OUTER-JOINed
// with the org member list (org role + membership status), keyed by
// normalizeIdentityRef on both sides.
// ---------------------------------------------------------------------------
describe('TeamsDirectoryAgents (merged directory + members)', () => {
  beforeEach(() => {
    resetTeamsStore();
    server.use(
      // no live agents → the directory + members are the join spine.
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/members', () =>
        HttpResponse.json([
          // JOINS the directory row agent:agent-pd (agent-center-pd) — supplies the
          // org role + membership dimensions the directory lacks.
          { id: 'M-pd', organization_id: 'org', identity_id: 'agent:agent-pd', kind: 'agent', role: 'owner', status: 'joined', display_name: 'agent-center-pd' },
          // members-only agent (no directory row) — proves the OUTER join keeps it.
          { id: 'M-solo', organization_id: 'org', identity_id: 'agent:agent-solo', kind: 'agent', role: 'member', status: 'joined', display_name: 'solo-agent' },
        ]),
      ),
    );
  });

  it('renders the agents table with a TEAMS column and an Unassigned cell', async () => {
    renderPage(<TeamsDirectoryAgents />);
    expect(await screen.findByTestId('agents-table')).toBeInTheDocument();
    expect(screen.getByTestId('agent-row-agent-center-pd')).toBeInTheDocument();
    expect(screen.queryByText('/organizations/:slug/teams/agents')).not.toBeInTheDocument();
    // an agent with no team shows the Unassigned placeholder.
    expect(screen.getAllByText('Unassigned').length).toBeGreaterThan(0);
  });

  it('a joined row surfaces BOTH dimensions (team role/runtime + org role/membership)', async () => {
    renderPage(<TeamsDirectoryAgents />);
    const row = await screen.findByTestId('agent-row-agent-center-pd');
    // directory dimension: team role + runtime working + team chip.
    expect(row).toHaveTextContent('planner');
    expect(row).toHaveTextContent('Working');
    expect(row).toHaveTextContent('agent-center core');
    // member dimension: org role + membership status.
    expect(within(row).getByTestId('agent-role')).toHaveTextContent('owner');
    expect(within(row).getByTestId('agent-status')).toHaveAttribute('data-status', 'joined');
  });

  it('keeps a members-only agent (outer join, graceful degradation)', async () => {
    renderPage(<TeamsDirectoryAgents />);
    const row = await screen.findByTestId('agent-row-solo-agent');
    // org role present, directory columns em-dashed (unknown).
    expect(within(row).getByTestId('agent-role')).toHaveTextContent('member');
    expect(within(row).getByTestId('agent-status')).toHaveAttribute('data-status', 'joined');
  });

  // T232 batch multi-select — ported from the retired Agents page. Checkboxes
  // are scoped to rows backed by a LIVE execution agent.
  const liveAgent = {
    id: 'A-pd', organization_id: 'org', name: 'agent-center-pd', description: '', model: 'opus-4.8',
    cli: 'claude-code', env_vars: {}, worker_id: 'w-1', lifecycle: 'stopped', availability: 'available',
    created_by: 'user:hayang', identity_member_id: 'agent:agent-pd', version: 1,
    created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
  };

  it('renders a select checkbox only on a live-agent row (not a directory-only row)', async () => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: [liveAgent] })));
    renderPage(<TeamsDirectoryAgents />);
    const liveRow = await screen.findByTestId('agent-row-agent-center-pd');
    expect(within(liveRow).getByTestId('agent-select-checkbox')).toBeInTheDocument();
    // agent-center-dev1 is directory-only (no live agent) → no checkbox.
    const dirOnly = screen.getByTestId('agent-row-agent-center-dev1');
    expect(within(dirOnly).queryByTestId('agent-select-checkbox')).toBeNull();
  });

  it('renders directory rows even when the live agents overlay is still unavailable', async () => {
    let agentsURL = '';
    server.use(
      http.get('/api/agents', ({ request }) => {
        agentsURL = request.url;
        return HttpResponse.json({ error: 'slow_agents', message: 'still loading' }, { status: 503 });
      }),
    );
    renderPage(<TeamsDirectoryAgents />);
    expect(await screen.findByTestId('agents-table')).toBeInTheDocument();
    expect(screen.getByTestId('agent-row-agent-center-pd')).toBeInTheDocument();
    await waitFor(() => expect(agentsURL).toContain('include_availability=false'));
    expect(agentsURL).toContain('include_enrichment=false');
  });

  it('does not show a stopped live agent as idle in the runtime column', async () => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: [liveAgent] })));
    renderPage(<TeamsDirectoryAgents />);
    const row = await screen.findByTestId('agent-row-agent-center-pd');
    expect(within(row).queryByTestId('agent-runtime')).toBeNull();

    fireEvent.click(screen.getByTestId('agents-filter-idle'));
    await waitFor(() => expect(screen.queryByTestId('agent-row-agent-center-pd')).not.toBeInTheDocument());
  });

  it('mobile agent cards link to detail and surface runtime load and model', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [{ ...liveAgent, lifecycle: 'running', availability: 'busy', running_tasks: 1, pending_tasks: 3, task_load: 0.25 }],
        }),
      ),
    );
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    const card = screen.getAllByTestId('agent-member-card').find((node) => node.textContent?.includes('agent-center-pd'));
    expect(card).toBeDefined();
    if (!card) throw new Error('agent-center-pd mobile card not found');
    expect(within(card).getByTestId('agent-card-link').getAttribute('href')).toContain('/agents/A-pd');
    expect(within(card).queryByTestId('agent-card-dm')).toBeNull();
    expect(within(card).getByTestId('agent-card-runtime')).toHaveTextContent('Working');
    expect(within(card).getByTestId('agent-load-badge')).toHaveTextContent('load: 0.3');
    expect(within(card).getByTestId('agent-card-model')).toHaveTextContent('opus-4.8');
  });

  it('select-all + a batch action fires the lifecycle mutation over the selected live agents', async () => {
    const started: string[] = [];
    server.use(
      http.get('/api/agents', () => HttpResponse.json({ agents: [liveAgent] })),
      http.post('/api/agents/:id/start', ({ params }) => {
        started.push(String(params.id));
        return HttpResponse.json({});
      }),
    );
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    // select-all selects every VISIBLE live-agent row (here just A-pd).
    fireEvent.click(screen.getByTestId('agents-select-all'));
    // start is non-destructive → runs immediately (no confirm gate).
    fireEvent.click(await screen.findByTestId('agents-batch-start'));
    await waitFor(() => expect(started).toContain('A-pd'));
  });

  it('filters by status', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.click(screen.getByTestId('agents-filter-working'));
    await waitFor(() => expect(screen.queryByTestId('agent-row-agent-center-dev1')).not.toBeInTheDocument());
    expect(screen.getByTestId('agent-row-agent-center-pd')).toBeInTheDocument();
  });

  it('filters by team', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.change(screen.getByTestId('agents-team-filter'), { target: { value: 'growth-experiments' } });
    await waitFor(() => expect(screen.queryByTestId('agent-row-agent-center-pd')).not.toBeInTheDocument());
    expect(screen.getByTestId('agent-row-agent-center-tester3')).toBeInTheDocument();
  });

  it('searches by name', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.change(screen.getByTestId('agents-search'), { target: { value: 'UDE' } });
    await waitFor(() => expect(screen.queryByTestId('agent-row-agent-center-pd')).not.toBeInTheDocument());
    expect(screen.getByTestId('agent-row-UDE')).toBeInTheDocument();
  });

  it('shows an empty state when nothing matches', async () => {
    renderPage(<TeamsDirectoryAgents />);
    await screen.findByTestId('agents-table');
    fireEvent.change(screen.getByTestId('agents-search'), { target: { value: 'zzzz-none' } });
    expect(await screen.findByTestId('agents-empty')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Merged Humans directory: directory OUTER-JOINed with the org member list,
// keyed by normalizeIdentityRef on both sides. Dual role/status dimensions +
// the ported change-role/disable kebab.
// ---------------------------------------------------------------------------
describe('TeamsDirectoryHumans (merged directory + members)', () => {
  beforeEach(() => {
    resetTeamsStore();
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          // JOINS the directory human user:user-oops (oopslink).
          { id: 'M-oops', organization_id: 'org', identity_id: 'user:user-oops', kind: 'user', role: 'owner', status: 'joined', display_name: 'oopslink', email: 'oops@x.com', created_at: '2026-01-01T00:00:00Z', last_session_at: '2026-06-01T00:00:00Z' },
          // members-only human (no directory row) — proves the OUTER join keeps it.
          { id: 'M-solo', organization_id: 'org', identity_id: 'user:user-solo', kind: 'user', role: 'member', status: 'joined', display_name: 'solo-user' },
        ]),
      ),
    );
  });

  it('renders humans with a multi-team TEAMS column and an Unassigned cell', async () => {
    renderPage(<TeamsDirectoryHumans />);
    expect(await screen.findByTestId('humans-table')).toBeInTheDocument();
    expect(screen.getByTestId('human-row-oopslink')).toBeInTheDocument();
    expect(screen.queryByText('/organizations/:slug/teams/humans')).not.toBeInTheDocument();
    // carol is invited with no teams → the TEAMS cell shows Unassigned.
    expect(screen.getAllByText('Unassigned').length).toBeGreaterThan(0);
  });

  it('a joined row surfaces BOTH role + status dimensions', async () => {
    renderPage(<TeamsDirectoryHumans />);
    const row = await screen.findByTestId('human-row-oopslink');
    // org role badge (member side) + team role (directory side, free string).
    expect(row).toHaveTextContent('owner');
    expect(row).toHaveTextContent('ops');
    // membership (member side: joined) + invite (directory side: Joined).
    expect(within(row).getByTestId('human-membership')).toHaveTextContent('Joined');
    expect(within(row).getByTestId('human-invite')).toHaveTextContent('Joined');
    // member-side email folded in.
    expect(within(row).getByTestId('human-email')).toHaveTextContent('oops@x.com');
  });

  it('keeps a members-only human and a directory-only human (outer join)', async () => {
    renderPage(<TeamsDirectoryHumans />);
    await screen.findByTestId('humans-table');
    // members-only (no directory row).
    expect(screen.getByTestId('human-row-solo-user')).toBeInTheDocument();
    // directory-only (no member row) still renders.
    expect(screen.getByTestId('human-row-carol')).toBeInTheDocument();
  });

  it('shows the kebab only for rows with a member; hides it for directory-only rows', async () => {
    renderPage(<TeamsDirectoryHumans />);
    const joined = await screen.findByTestId('human-row-oopslink');
    const dirOnly = screen.getByTestId('human-row-carol');
    expect(within(joined).getByLabelText('Member actions')).toBeInTheDocument();
    expect(within(dirOnly).queryByLabelText('Member actions')).toBeNull();
  });

  it('the change-role kebab fires the member mutation', async () => {
    let patchedRole: string | undefined;
    server.use(
      http.patch('/api/members/:id/role', async ({ request }) => {
        patchedRole = ((await request.json()) as { role: string }).role;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage(<TeamsDirectoryHumans />);
    const row = await screen.findByTestId('human-row-oopslink');
    fireEvent.click(within(row).getByLabelText('Member actions'));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Change role' }));
    fireEvent.click(await screen.findByRole('button', { name: 'admin' }));
    await waitFor(() => expect(patchedRole).toBe('admin'));
  });

  // Bug ② regression: disable used to POST an empty reason (backend rejects it
  // with HTTP 500) and unconditionally closed the confirm popover, hiding the
  // failure. The fix passes a non-empty reason and closes only on success.
  it('disable sends a NON-EMPTY reason and closes the popover on success', async () => {
    let sentReason: unknown = 'UNSENT';
    server.use(
      http.post('/api/members/:id/disable', async ({ request }) => {
        sentReason = ((await request.json()) as { reason?: string }).reason;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    renderPage(<TeamsDirectoryHumans />);
    const row = await screen.findByTestId('human-row-oopslink');
    fireEvent.click(within(row).getByLabelText('Member actions'));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Disable' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(sentReason).toBe('Disabled by admin'));
    // non-empty reason → the backend would 204, and the popover closes on success.
    expect(sentReason).not.toBe('');
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: 'Confirm' })).not.toBeInTheDocument(),
    );
  });

  it('disable failure renders an error and keeps the confirm popover open (never fails silently)', async () => {
    server.use(
      http.post('/api/members/:id/disable', () =>
        HttpResponse.json(
          { error: 'disable_failed', message: 'payload.reason must be a non-empty string' },
          { status: 500 },
        ),
      ),
    );
    renderPage(<TeamsDirectoryHumans />);
    const row = await screen.findByTestId('human-row-oopslink');
    fireEvent.click(within(row).getByLabelText('Member actions'));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Disable' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm' }));
    // error surfaces and the popover stays open (Confirm still present).
    expect(await screen.findByTestId('member-disable-error')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument();
  });

  it('filters to joined only', async () => {
    renderPage(<TeamsDirectoryHumans />);
    await screen.findByTestId('humans-table');
    fireEvent.click(screen.getByTestId('humans-filter-joined'));
    await waitFor(() => expect(screen.queryByTestId('human-row-carol')).not.toBeInTheDocument());
  });

  it('filters by team', async () => {
    renderPage(<TeamsDirectoryHumans />);
    await screen.findByTestId('humans-table');
    fireEvent.change(screen.getByTestId('humans-team-filter'), { target: { value: 'docs-and-dx' } });
    await waitFor(() => expect(screen.queryByTestId('human-row-alice')).not.toBeInTheDocument());
    expect(screen.getByTestId('human-row-bob')).toBeInTheDocument();
  });
});

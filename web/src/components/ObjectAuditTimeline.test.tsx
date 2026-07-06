import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import i18n from '@/i18n';
import { ObjectAuditTimeline } from './ObjectAuditTimeline';
import type { AuditEntry } from '@/api/audit';

function renderWithProvider(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

// renderInOrg wraps the timeline in an OrgContext so the entity-ref resolvers
// (task/plan/issue are slug-gated) are enabled and can linkify.
function renderInOrg(ui: React.ReactElement, slug = 'test-org') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug, orgId: 'O', orgName: 'Test Org' }}>{ui}</OrgContext.Provider>
    </QueryClientProvider>,
  );
}

// Default-empty org lists so a render doesn't trip an unhandled request — every
// audit row loads members + tasks + plans + issues via the ref resolvers.
function mockEmptyOrgLists() {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/tasks', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
  );
}

function entry(over: Partial<AuditEntry>): AuditEntry {
  return {
    id: 'a1',
    object_type: 'task',
    object_id: 'task-1',
    change_type: 'status_changed',
    field: 'status',
    from: 'open',
    to: 'running',
    actor: 'user:alice',
    detail: {},
    occurred_at: '2026-07-03T10:00:00Z',
    ...over,
  };
}

afterEach(() => cleanup());

describe('ObjectAuditTimeline', () => {
  it('renders the ledger newest-first with human-readable sentences + actor', async () => {
    server.use(
      http.get('/api/projects/:pid/tasks/:tid/audit', () =>
        HttpResponse.json({
          entries: [
            entry({ id: 'a2', change_type: 'status_changed', from: 'open', to: 'running', actor: 'user:alice' }),
            entry({ id: 'a1', change_type: 'assigned', field: 'assignee', from: '', to: 'agent:bot', actor: 'user:pd' }),
            entry({ id: 'a0', change_type: 'created', from: '', to: 'open', actor: 'user:pd', detail: {} }),
          ],
          next_cursor: '',
        }),
      ),
    );
    renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);

    await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
    const rows = screen.getAllByTestId('audit-row');
    expect(rows).toHaveLength(3);
    // Newest-first: first row is the status change (open → running).
    expect(rows[0]).toHaveAttribute('data-change-type', 'status_changed');
    expect(rows[0]).toHaveTextContent(/open → running/);
    // Actor rendered without the ADR-0033 scheme prefix, as @handle.
    expect(rows[0]).toHaveTextContent('@alice');
    // The assigned row composes "assigned to <agent>".
    expect(rows[1]).toHaveTextContent(/assigned to bot/);
  });

  it('shows the empty state when the ledger has no entries', async () => {
    server.use(
      http.get('/api/projects/:pid/tasks/:tid/audit', () =>
        HttpResponse.json({ entries: [], next_cursor: '' }),
      ),
    );
    renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);
    await waitFor(() => expect(screen.getByTestId('audit-empty')).toBeInTheDocument());
  });

  it('renders plan dependency + node changes from structured detail', async () => {
    server.use(
      http.get('/api/projects/:pid/plans/:planId/audit', () =>
        HttpResponse.json({
          entries: [
            entry({
              id: 'p2',
              object_type: 'plan',
              change_type: 'dependency_added',
              from: '',
              to: '',
              actor: 'user:pd',
              detail: { from: 'T1', to: 'T2', kind: 'seq' },
            }),
            entry({
              id: 'p1',
              object_type: 'plan',
              change_type: 'started',
              actor: 'user:pd',
              detail: { status: 'running' },
            }),
          ],
          next_cursor: '',
        }),
      ),
    );
    renderWithProvider(<ObjectAuditTimeline objectType="plan" projectId="proj-a" objectId="plan-1" />);
    await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
    expect(screen.getByText(/added dependency T1 → T2/)).toBeInTheDocument();
    expect(screen.getByText(/started the plan/)).toBeInTheDocument();
  });

  it('renders plan gate decision_outcome + loopback from structured detail', async () => {
    server.use(
      http.get('/api/projects/:pid/plans/:planId/audit', () =>
        HttpResponse.json({
          entries: [
            entry({
              id: 'g2',
              object_type: 'plan',
              change_type: 'loopback',
              actor: 'system:plan-engine',
              detail: { round: 1, from: 'T1', to: 'T3' },
            }),
            entry({
              id: 'g1',
              object_type: 'plan',
              change_type: 'decision_outcome',
              from: '',
              to: '',
              actor: 'user:pd',
              detail: { outcome: 'reject', decision: 'Ship?' },
            }),
          ],
          next_cursor: '',
        }),
      ),
    );
    renderWithProvider(<ObjectAuditTimeline objectType="plan" projectId="proj-a" objectId="plan-1" />);
    await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
    // gate ruling reads its outcome from detail; loopback reads its round.
    expect(screen.getByText(/decision outcome reject/)).toBeInTheDocument();
    expect(screen.getByText(/loopback re-run \(round 1\)/)).toBeInTheDocument();
    // system actor renders via localized copy, not a raw "system:plan-engine".
    expect(screen.getByText(/@system \(plan-engine\)/)).toBeInTheDocument();
  });

  // reminder-event (issue-c7629d30 #2): the cognition reminder projector records
  // reminder_armed / reminder_fired on the triggering entity's ledger. Each needs a
  // human-readable sentence (not the raw change_type) + a category badge.
  describe('reminder-event rows', () => {
    it('composes reminder_armed with remindee, event + a compact +delay suffix', async () => {
      server.use(
        http.get('/api/projects/:pid/tasks/:tid/audit', () =>
          HttpResponse.json({
            entries: [
              entry({
                id: 'r1',
                object_type: 'task',
                change_type: 'reminder_armed',
                from: '',
                to: '',
                actor: 'system:reminder-event',
                detail: { reminder_id: 'rem-1', remindee_agent_id: 'agent-r1', event: 'completed', delay_seconds: 300 },
              }),
            ],
            next_cursor: '',
          }),
        ),
      );
      renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);
      await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
      const row = screen.getByTestId('audit-row');
      expect(row).toHaveAttribute('data-change-type', 'reminder_armed');
      // Badge is the localized "Reminder" label, not the raw change_type.
      expect(screen.getByTestId('audit-badge')).toHaveTextContent('Reminder');
      // Sentence: verb phrase + remindee id + on <event> + (+5m); actor prefixed.
      expect(row).toHaveTextContent(/armed a reminder for agent-r1 on completed \(\+5m\)/);
      expect(row).toHaveTextContent('@system (reminder-event)');
    });

    it('composes reminder_fired with the delivery target', async () => {
      server.use(
        http.get('/api/projects/:pid/tasks/:tid/audit', () =>
          HttpResponse.json({
            entries: [
              entry({
                id: 'r2',
                object_type: 'task',
                change_type: 'reminder_fired',
                from: '',
                to: '',
                actor: 'system:reminder-event',
                detail: { reminder_id: 'rem-1', remindee_agent_id: 'agent-r1', event: 'completed', fired_count: 1 },
              }),
            ],
            next_cursor: '',
          }),
        ),
      );
      renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);
      await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
      const row = screen.getByTestId('audit-row');
      expect(row).toHaveAttribute('data-change-type', 'reminder_fired');
      expect(screen.getByTestId('audit-badge')).toHaveTextContent('Reminder');
      expect(row).toHaveTextContent(/reminder fired → agent-r1/);
    });

    it('degrades gracefully when reminder detail fields are missing (no crash, em-dash)', async () => {
      server.use(
        http.get('/api/projects/:pid/tasks/:tid/audit', () =>
          HttpResponse.json({
            entries: [
              // reminder_armed with an empty detail — no remindee, no event, no delay.
              entry({ id: 'r3', object_type: 'task', change_type: 'reminder_armed', from: '', to: '', actor: 'system:reminder-event', detail: {} }),
              // reminder_fired with an empty detail.
              entry({ id: 'r4', object_type: 'task', change_type: 'reminder_fired', from: '', to: '', actor: 'system:reminder-event', detail: {} }),
            ],
            next_cursor: '',
          }),
        ),
      );
      renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);
      await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
      const rows = screen.getAllByTestId('audit-row');
      expect(rows).toHaveLength(2);
      // Missing fields fall back to the em-dash placeholder; no +delay suffix, no throw.
      expect(rows[0]).toHaveTextContent('armed a reminder for — on —');
      expect(rows[0]).not.toHaveTextContent('(+');
      expect(rows[1]).toHaveTextContent('reminder fired → —');
    });
  });

  // Plan Change History (oopslink DM 2026-07-06): entity ids in a history row —
  // the structured actor + the raw task/plan/issue/agent ids interpolated into the
  // composed sentence (incl. a dependency's from→to, TWO ids on one line) — render
  // as clickable short-ref links, reusing the site ref resolvers.
  describe('entity ref links in history rows', () => {
    it('linkifies BOTH task ids on a dependency row to their T-refs (two links, one line)', async () => {
      mockEmptyOrgLists();
      server.use(
        http.get('/api/tasks', () =>
          HttpResponse.json({
            items: [
              { id: 'task-6b5a3d51', org_ref: 'T90', project: { id: 'proj-x', name: 'X' }, title: 'a', status: 'running', assignee: null, updated_at: 'x', created_at: 'x' },
              { id: 'task-6f17013d', org_ref: 'T91', project: { id: 'proj-x', name: 'X' }, title: 'b', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' },
            ],
            total: 2,
          }),
        ),
        http.get('/api/projects/:pid/plans/:planId/audit', () =>
          HttpResponse.json({
            entries: [
              entry({
                id: 'd1',
                object_type: 'plan',
                change_type: 'dependency_added',
                from: '',
                to: '',
                actor: 'user:pd',
                // raw entity ids (as the backend ships them), one line, from → to.
                detail: { from: 'task-6b5a3d51', to: 'task-6f17013d', kind: 'seq' },
              }),
            ],
            next_cursor: '',
          }),
        ),
      );
      renderInOrg(<ObjectAuditTimeline objectType="plan" projectId="proj-a" objectId="plan-1" />);

      const links = await screen.findAllByTestId('activity-task-ref-link');
      expect(links).toHaveLength(2);
      // Each raw id becomes its own link, labelled with the T-ref (not the raw id).
      expect(links[0]).toHaveTextContent('T90');
      expect(links[0]).toHaveAttribute('data-task-id', 'task-6b5a3d51');
      expect(links[0]).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-6b5a3d51');
      expect(links[1]).toHaveTextContent('T91');
      expect(links[1]).toHaveAttribute('data-task-id', 'task-6f17013d');
      // The raw ids are no longer shown as plain text.
      expect(screen.queryByText(/task-6b5a3d51/)).toBeNull();
    });

    it('renders an AGENT actor as its display_name link (not the raw agent-<id>)', async () => {
      mockEmptyOrgLists();
      server.use(
        http.get('/api/members', () =>
          HttpResponse.json([
            { id: 'mem-1', organization_id: 'O', identity_id: 'agent-b5036ea8', display_name: 'agent-center-dev2', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x' },
          ]),
        ),
        http.get('/api/projects/:pid/plans/:planId/audit', () =>
          HttpResponse.json({
            entries: [
              entry({ id: 's1', object_type: 'plan', change_type: 'started', actor: 'agent:agent-b5036ea8', detail: { status: 'running' } }),
            ],
            next_cursor: '',
          }),
        ),
      );
      renderInOrg(<ObjectAuditTimeline objectType="plan" projectId="proj-a" objectId="plan-1" />);

      const actor = await screen.findByTestId('audit-actor-agent-link');
      expect(actor).toHaveTextContent('agent-center-dev2');
      expect(actor).not.toHaveTextContent('agent-b5036ea8'); // raw id not shown as text
      expect(actor).toHaveAttribute('data-agent-ref', 'agent:agent-b5036ea8');
      expect(actor).toHaveAttribute('href', '/organizations/test-org/agents/agent-b5036ea8');
    });

    it('leaves a user actor + an unknown agent as plain text (verify-not-trust)', async () => {
      mockEmptyOrgLists();
      server.use(
        http.get('/api/projects/:pid/plans/:planId/audit', () =>
          HttpResponse.json({
            entries: [
              entry({ id: 'u1', object_type: 'plan', change_type: 'started', actor: 'agent:agent-unknown', detail: {} }),
              entry({ id: 'u0', object_type: 'plan', change_type: 'started', actor: 'user:alice', detail: {} }),
            ],
            next_cursor: '',
          }),
        ),
      );
      renderInOrg(<ObjectAuditTimeline objectType="plan" projectId="proj-a" objectId="plan-1" />);
      await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
      // Unknown agent → no link, plain "@agent-unknown"; user → plain "@alice".
      expect(screen.queryByTestId('audit-actor-agent-link')).toBeNull();
      expect(screen.getByText(/@agent-unknown/)).toBeInTheDocument();
      expect(screen.getByText(/@alice/)).toBeInTheDocument();
    });
  });

  describe('i18n (H-1: no hardcoded English)', () => {
    afterEach(async () => {
      await i18n.changeLanguage('en');
    });

    it('renders section copy + sentences in Chinese under the zh locale', async () => {
      await i18n.changeLanguage('zh');
      server.use(
        http.get('/api/projects/:pid/tasks/:tid/audit', () =>
          HttpResponse.json({
            entries: [entry({ id: 'z1', change_type: 'status_changed', from: 'open', to: 'running', actor: 'user:alice' })],
            next_cursor: '',
          }),
        ),
      );
      renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);
      await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
      // Section heading + status category label + sentence are all localized.
      expect(screen.getByText('变更记录')).toBeInTheDocument();
      expect(screen.getByTestId('audit-badge')).toHaveTextContent('状态');
      expect(screen.getByTestId('audit-sentence')).toHaveTextContent('状态变更 open → running');
    });

    it('renders the empty state in Chinese', async () => {
      await i18n.changeLanguage('zh');
      server.use(
        http.get('/api/projects/:pid/tasks/:tid/audit', () => HttpResponse.json({ entries: [], next_cursor: '' })),
      );
      renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);
      await waitFor(() => expect(screen.getByTestId('audit-empty')).toHaveTextContent('暂无变更记录。'));
    });
  });
});

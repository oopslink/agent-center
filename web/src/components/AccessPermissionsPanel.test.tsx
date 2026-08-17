import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { delay, http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { AccessPermissionsPanel } from './AccessPermissionsPanel';

function wrap(overrides: Partial<React.ComponentProps<typeof AccessPermissionsPanel>> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AccessPermissionsPanel
          subjectRef="agent:agent-member-1"
          subjectLabel="Planner Bot"
          resource={{ kind: 'team', id: 'team-1', org_id: 'org-1' }}
          resourceLabel="Platform Team"
          {...overrides}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function defs() {
  return {
    definitions: [
      { key: 'team.read', category: 'access', resource_kinds: ['team'], actions: ['read'], legacy_sources: ['team_members'] },
      { key: 'team.memory.review', category: 'access', resource_kinds: ['team'], actions: ['review'], legacy_sources: ['team_memory_policies'] },
      { key: 'team.member.manage', category: 'access', resource_kinds: ['team'], actions: ['manage'], legacy_sources: ['authorization_role_assignments'] },
    ],
  };
}

function effective() {
  return {
    subject_ref: 'agent:agent-member-1',
    resource: { kind: 'team', id: 'team-1', org_id: 'org-1' },
    permissions: [
      {
        key: 'team.read',
        source: 'team_member',
        evidence_ref: 'team_members:team-1/agent:agent-member-1/planner',
      },
      {
        key: 'team.memory.review',
        source: 'team_memory_policy',
        evidence_ref: 'team_memory_policies:team-1',
      },
      {
        key: 'team.member.manage',
        source: 'custom_role',
        evidence_ref: 'authorization_role_assignments:asgn-access-auditor',
        role_id: 'role-access-auditor',
        assignment_id: 'asgn-access-auditor',
        delegatable: true,
      },
    ],
  };
}

function explain() {
  return {
    decision: {
      allowed: true,
      subject_ref: 'agent:agent-member-1',
      permission: 'team.read',
      resource: { kind: 'team', id: 'team-1', org_id: 'org-1' },
      source: 'team_member',
      reason: 'matched team_member',
      evidence_ref: 'team_members:team-1/agent:agent-member-1/planner',
    },
    effective: effective().permissions,
    denied_by: [],
    resolved_org: 'org-1',
  };
}

describe('AccessPermissionsPanel', () => {
  afterEach(() => cleanup());

  it('renders read-only overview, graph, explain, source locations, and the Team Role / Access Role split', async () => {
    server.use(
      http.get('/api/permissions/definitions', () => HttpResponse.json(defs())),
      http.get('/api/permissions/effective', () => HttpResponse.json(effective())),
      http.post('/api/permissions/explain', () => HttpResponse.json(explain())),
      http.get('/api/permissions/audit', () =>
        HttpResponse.json({
          events: [
            {
              id: 'audit-1',
              event_type: 'authorization.assignment.created',
              actor_ref: 'user:owner',
              subject_ref: 'agent:agent-member-1',
              permission_key: 'team.member.manage',
              resource_kind: 'team',
              resource_id: 'team-1',
              role_id: 'role-access-auditor',
              assignment_id: 'asgn-access-auditor',
              payload: {},
              created_at: '2026-08-14T04:00:00Z',
            },
          ],
        }),
      ),
    );

    wrap();

    await waitFor(() => expect(screen.getByTestId('access-overview-total')).toHaveTextContent('3'));
    expect(screen.getByTestId('access-overview-access-roles')).toHaveTextContent('1');
    expect(screen.getByTestId('access-overview-legacy')).toHaveTextContent('2');
    expect(screen.getByTestId('access-readonly-banner')).toHaveTextContent('read-only');
    expect(screen.queryByTestId('access-grant-submit')).not.toBeInTheDocument();
    expect(screen.queryByTestId('access-direct-revoke')).not.toBeInTheDocument();

    expect(screen.getByRole('region', { name: 'Access subject and resource drill-down' })).toHaveTextContent('agent:agent-member-1');
    expect(screen.getByRole('region', { name: 'Access graph' })).toHaveTextContent('team.read');
    expect(screen.getAllByTestId('access-graph-edge')[0]).toHaveClass('grid-cols-1');
    expect(screen.getAllByTestId('access-graph-edge')[0].className).toContain('lg:grid-cols');
    expect(screen.getByTestId('access-explain-tree')).toHaveTextContent('Source explanation tree');
    expect(await screen.findByTestId('access-explain-decision')).toHaveTextContent('Allowed');

    expect(screen.getByTestId('access-role-model')).toHaveTextContent('Team Role vs Access Role');
    expect(screen.getByTestId('access-team-roles')).toHaveTextContent('planner');
    expect(screen.getByTestId('access-access-roles')).toHaveTextContent('role-access-auditor');
    expect(screen.getByTestId('access-combination-tags')).toHaveTextContent('Team planner + Access Team member');
    expect(screen.getByTestId('access-role-model')).toHaveTextContent('display-only');

    expect(screen.getByTestId('access-source-locations')).toHaveTextContent('Team roster');
    expect(screen.getByTestId('access-source-locations')).toHaveTextContent('P0 does not expose editing');
    expect(screen.getByTestId('access-source-locations')).toHaveTextContent('Team Role is runtime configuration');
    expect(screen.getAllByTestId('access-source-link')[0]).toHaveAttribute('href', '/teams/team-1?tab=mm');

    expect(screen.getByTestId('access-effective-partial')).toHaveTextContent('completeness is not asserted');
    expect(screen.getByTestId('access-audit-partial')).toHaveTextContent('capped diagnostic window');
    expect(screen.getByTestId('access-risk-list')).toHaveTextContent('Team Role is runtime configuration');
    expect(screen.getByTestId('access-audit-list')).toHaveTextContent('created assignment team.member.manage on team:team-1');
  });

  it('does not issue batch apply or revoke calls from the P0 read-only UI', async () => {
    let batchCalls = 0;
    server.use(
      http.get('/api/permissions/definitions', () => HttpResponse.json(defs())),
      http.get('/api/permissions/effective', () => HttpResponse.json(effective())),
      http.post('/api/permissions/explain', () => HttpResponse.json(explain())),
      http.get('/api/permissions/audit', () => HttpResponse.json({ events: [] })),
      http.post('/api/permissions/batch/apply', () => {
        batchCalls += 1;
        return HttpResponse.json({ preview: false, operations: [] });
      }),
      http.post('/api/permissions/batch/revoke', () => {
        batchCalls += 1;
        return HttpResponse.json({ preview: false, operations: [] });
      }),
    );

    wrap();

    await waitFor(() => expect(screen.getByTestId('access-overview-total')).toHaveTextContent('3'));
    expect(screen.queryByRole('button', { name: /grant/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /revoke/i })).not.toBeInTheDocument();
    expect(batchCalls).toBe(0);
  });

  it('covers loading and empty states with explicit complete markers', async () => {
    server.use(
      http.get('/api/permissions/definitions', async () => {
        await delay(20);
        return HttpResponse.json({ definitions: [] });
      }),
      http.get('/api/permissions/effective', async () => {
        await delay(20);
        return HttpResponse.json({
          subject_ref: 'agent:agent-member-1',
          resource: { kind: 'team', id: 'team-1', org_id: 'org-1' },
          permissions: [],
          complete: true,
        });
      }),
      http.get('/api/permissions/audit', () => HttpResponse.json({ events: [], complete: true })),
    );

    wrap();

    expect(screen.getByTestId('access-loading')).toHaveTextContent('Loading permissions');
    await waitFor(() => expect(screen.getByTestId('access-effective-empty')).toHaveTextContent('No effective permissions'));
    expect(screen.getByTestId('access-graph-empty')).toHaveTextContent('No access edges');
    expect(screen.getByTestId('access-risk-empty')).toHaveTextContent('No additional warnings');
    expect(screen.getByTestId('access-effective-complete')).toHaveTextContent('marked complete');
    expect(screen.getByTestId('access-audit-complete')).toHaveTextContent('marked complete');
    expect(screen.getByLabelText('Permission to explain')).toBeDisabled();
  });

  it('renders effective and explain errors as accessible alerts', async () => {
    server.use(
      http.get('/api/permissions/definitions', () => HttpResponse.json(defs())),
      http.get('/api/permissions/effective', () => HttpResponse.json({ message: 'effective failed' }, { status: 500 })),
      http.post('/api/permissions/explain', () => HttpResponse.json({ message: 'explain failed' }, { status: 500 })),
      http.get('/api/permissions/audit', () => HttpResponse.json({ events: [] })),
    );

    wrap();

    await waitFor(() => expect(screen.getByTestId('access-error')).toBeInTheDocument());
    expect(screen.getByTestId('access-error')).toHaveAttribute('role', 'alert');
    await waitFor(() => expect(screen.getByTestId('access-explain-error')).toBeInTheDocument());
    expect(screen.getByTestId('access-explain-error')).toHaveAttribute('role', 'alert');
  });
});

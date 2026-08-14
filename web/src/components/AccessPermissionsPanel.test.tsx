import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { AccessPermissionsPanel } from './AccessPermissionsPanel';

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AccessPermissionsPanel
          subjectRef="user:user-abc12345"
          subjectLabel="Alice"
          resource={{ kind: 'org', id: 'org-1' }}
          resourceLabel="Acme"
        />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function defs() {
  return {
    definitions: [
      { key: 'org.read', category: 'access', resource_kinds: ['org'], actions: ['read'], legacy_sources: ['members'] },
      { key: 'org.member.list', category: 'access', resource_kinds: ['org'], actions: ['list'], legacy_sources: ['members'] },
    ],
  };
}

function effective(revoked = false) {
  return {
    subject_ref: 'user:user-abc12345',
    resource: { kind: 'org', id: 'org-1', org_id: 'org-1' },
    permissions: [
      { key: 'org.read', source: 'org_role', evidence_ref: 'members:member-1', delegatable: true },
      ...(revoked
        ? []
        : [
            {
              key: 'org.read',
              source: 'custom_role',
              evidence_ref: 'authorization_role_assignments:asgn-direct-read',
              role_id: 'role-direct-org-read',
              assignment_id: 'asgn-direct-read',
            },
          ]),
      {
        key: 'org.member.list',
        source: 'custom_role',
        evidence_ref: 'authorization_role_assignments:asgn-direct-list',
        role_id: 'role-direct-org-member-list',
        assignment_id: 'asgn-direct-list',
      },
    ],
  };
}

describe('AccessPermissionsPanel', () => {
  afterEach(() => cleanup());

  it('renders overview, effective permissions, source tree, direct grants, inherited sources and subject audit', async () => {
    server.use(
      http.get('/api/permissions/definitions', () => HttpResponse.json(defs())),
      http.get('/api/permissions/effective', () => HttpResponse.json(effective())),
      http.post('/api/permissions/explain', () =>
        HttpResponse.json({
          decision: {
            allowed: true,
            subject_ref: 'user:user-abc12345',
            permission: 'org.read',
            resource: { kind: 'org', id: 'org-1', org_id: 'org-1' },
            source: 'org_role',
            reason: 'matched org_role',
            evidence_ref: 'members:member-1',
          },
          effective: effective().permissions,
          denied_by: [],
          resolved_org: 'org-1',
        }),
      ),
      http.get('/api/permissions/audit', () =>
        HttpResponse.json({
          events: [
            {
              id: 'audit-1',
              event_type: 'authorization.assignment.created',
              actor_ref: 'user:owner',
              subject_ref: 'user:user-abc12345',
              permission_key: 'org.read',
              resource_kind: 'org',
              resource_id: 'org-1',
              role_id: 'role-direct-org-read',
              assignment_id: 'asgn-direct-read',
              payload: {},
              created_at: '2026-08-14T04:00:00Z',
            },
          ],
        }),
      ),
    );

    wrap();

    await waitFor(() => expect(screen.getByTestId('access-overview-total')).toHaveTextContent('3'));
    expect(screen.getByTestId('access-overview-direct')).toHaveTextContent('2');
    expect(screen.getByTestId('access-overview-inherited')).toHaveTextContent('1');
    expect(screen.getByTestId('access-effective-list')).toHaveTextContent('org.member.list');
    expect(screen.getByTestId('access-explain-tree')).toHaveTextContent('Source explanation tree');
    expect(await screen.findByTestId('access-explain-decision')).toHaveTextContent('Allowed');
    expect(screen.getByTestId('access-direct-list')).toHaveTextContent('asgn-direct-read');
    expect(screen.getByTestId('access-inherited-list')).toHaveTextContent('Org role');
    expect(screen.getByTestId('access-audit-list')).toHaveTextContent('created assignment org.read on org:org-1');
    expect(screen.getAllByTestId('access-source-link')[0]).toHaveAttribute('href', '/users/user-abc12345');
  });

  it('warns when revoking a direct grant leaves the permission effective through inheritance', async () => {
    let revoked = false;
    server.use(
      http.get('/api/permissions/definitions', () => HttpResponse.json(defs())),
      http.get('/api/permissions/effective', () => HttpResponse.json(effective(revoked))),
      http.post('/api/permissions/explain', () =>
        HttpResponse.json({
          decision: {
            allowed: true,
            subject_ref: 'user:user-abc12345',
            permission: 'org.read',
            resource: { kind: 'org', id: 'org-1', org_id: 'org-1' },
            source: 'org_role',
            reason: 'matched org_role',
            evidence_ref: 'members:member-1',
          },
          effective: effective(revoked).permissions,
          denied_by: [],
        }),
      ),
      http.get('/api/permissions/audit', () => HttpResponse.json({ events: [] })),
      http.post('/api/permissions/batch/revoke', async ({ request }) => {
        const body = (await request.json()) as { operations?: Array<{ revoke?: { assignment_id?: string } }> };
        expect(body.operations?.[0]?.revoke?.assignment_id).toBe('asgn-direct-read');
        revoked = true;
        return HttpResponse.json({
          preview: false,
          operations: [{ id: 'revoke', type: 'revoke_assignment', status: 'revoked', assignment_id: 'asgn-direct-read' }],
        });
      }),
    );

    wrap();
    await waitFor(() => expect(screen.getByTestId('access-direct-list')).toHaveTextContent('asgn-direct-read'));
    fireEvent.click(screen.getAllByTestId('access-direct-revoke')[0]);

    await waitFor(() =>
      expect(screen.getByTestId('access-revoke-notice')).toHaveTextContent(
        'Direct grant revoked for org.read, but it is still effective through Org role',
      ),
    );
    expect(screen.queryByText('asgn-direct-read')).not.toBeInTheDocument();
    expect(screen.getByTestId('access-inherited-list')).toHaveTextContent('members:member-1');
  });
});

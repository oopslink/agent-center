import type React from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useUser } from '@/api/users';
import { useOrgs } from '@/api/auth';

function fmtDate(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

// UserDetail (/users/:userId, userId = member-id `user-<8hex>`). v2.7.1 #193 —
// a trimmed AgentDetail-shaped page: a profile section + the user's org
// memberships (with role). No activity stream (v2.8). The route uses the
// member-id (not the handle) so it survives display-name renames.
export default function UserDetail(): React.ReactElement {
  const { userId = '' } = useParams<{ userId: string }>();
  const user = useUser(userId);
  const orgs = useOrgs();
  const orgName = (id: string): string | undefined =>
    (orgs.data ?? []).find((o) => o.id === id)?.name || undefined;

  if (user.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-UserDetail">
        Loading user…
      </section>
    );
  }
  if (user.isError || !user.data) {
    return (
      <section className="space-y-3" data-testid="page-UserDetail">
        <p className="text-sm text-danger" data-testid="user-detail-not-found">
          {user.error instanceof Error ? user.error.message : 'User not found.'}
        </p>
        <OrgLink to="/members/humans" className="text-accent hover:underline">
          Back to Humans
        </OrgLink>
      </section>
    );
  }

  const u = user.data;

  return (
    <section className="flex h-full flex-col gap-6" data-testid="page-UserDetail" data-user-id={u.user_id}>
      <header className="border-b border-border-base pb-3">
        {/* member-id on hover (#192). */}
        <h2 className="text-xl font-semibold" data-testid="user-detail-name" title={u.user_id}>
          {u.display_name || u.user_id}
        </h2>
        <p className="text-xs text-text-muted">User</p>
      </header>

      <section className="rounded-lg border border-border-base bg-bg-elevated p-4">
        <h3 className="mb-3 text-sm font-semibold text-text-primary">Profile</h3>
        <dl className="grid grid-cols-[8rem_1fr] gap-y-2 text-sm">
          <dt className="text-text-muted">Email</dt>
          <dd className="text-text-primary" data-testid="user-detail-email">{u.email || '—'}</dd>
          <dt className="text-text-muted">Created</dt>
          <dd className="text-text-secondary" data-testid="user-detail-created">{fmtDate(u.created_at)}</dd>
          <dt className="text-text-muted">Last active</dt>
          <dd className="text-text-secondary" data-testid="user-detail-last-session">{fmtDate(u.last_session_at)}</dd>
        </dl>
      </section>

      <section className="rounded-lg border border-border-base bg-bg-elevated p-4">
        <h3 className="mb-3 text-sm font-semibold text-text-primary">Organizations</h3>
        {u.orgs.length === 0 ? (
          <p className="text-xs text-text-muted" data-testid="user-detail-orgs-empty">
            Not a member of any organization.
          </p>
        ) : (
          <ul className="divide-y divide-border-base" data-testid="user-detail-orgs">
            {u.orgs.map((o) => (
              <li
                key={o.org_id}
                data-testid="user-detail-org-row"
                data-org-id={o.org_id}
                className="flex items-center justify-between py-1.5 text-sm"
              >
                {/* org name (raw id on hover, #192). */}
                <span className="text-text-primary" title={o.org_id}>{orgName(o.org_id) || o.org_id}</span>
                <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                  {o.role}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </section>
  );
}

import type React from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useUser, type UserDetailResult, type UserOrgMembership } from '@/api/users';
import { useOrgs, useMe } from '@/api/auth';
import { Breadcrumb } from '@/components/Breadcrumb';
import AccountPanel from '@/components/AccountPanel';
import { useTablistKeyboard } from '@/components/useTablistKeyboard';

function fmtDate(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

type UserTab = 'profile' | 'organizations' | 'account';

// UserDetail (/users/:userId, userId = member-id `user-<8hex>`). v2.7.1 #193 →
// v2.8.1 #8: a tabbed profile page. The "User" kind shows as a tag next to the
// name; Profile / Organizations / Account are tabs (Account is self-only and
// holds change-password + sign-out). The route uses the member-id (not the
// handle) so it survives display-name renames.
export default function UserDetail(): React.ReactElement {
  const { userId = '' } = useParams<{ userId: string }>();
  const user = useUser(userId);
  const me = useMe();

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

  // self-view: the Account tab (change password + sign out) only when you are
  // looking at your own profile. me.identity_id == the member-id used in the
  // route (verified: GET /api/auth/me identity_id == GET /api/users/{id}.user_id).
  const isSelf = !!me.data && me.data.identity_id === user.data.user_id;
  return <UserDetailView user={user.data} isSelf={isSelf} />;
}

// UserDetailView renders the loaded page. Split out so the tablist hooks run
// against the resolved data (tabs depend on isSelf) without sitting behind the
// loading/error early returns in the parent.
function UserDetailView({
  user: u,
  isSelf,
}: {
  user: UserDetailResult;
  isSelf: boolean;
}): React.ReactElement {
  const [searchParams, setSearchParams] = useSearchParams();
  const orgs = useOrgs();
  // T478 #1: resolve the org's display name. The server now sends org_name on the
  // membership (authoritative, works for any org); fall back to the viewer's own
  // org list and finally to the raw id only if neither is available.
  const orgName = (o: UserOrgMembership): string =>
    o.org_name?.trim() ||
    (orgs.data ?? []).find((x) => x.id === o.org_id)?.name ||
    o.org_id;

  const tabs: { key: UserTab; label: string }[] = [
    { key: 'profile', label: 'Profile' },
    { key: 'organizations', label: 'Organizations' },
    ...(isSelf ? [{ key: 'account' as UserTab, label: 'Account' }] : []),
  ];
  // Active tab synced to ?tab= (shareable + the /me redirect lands on ?tab=account).
  // An unknown tab — or ?tab=account when viewing someone else — falls back to Profile.
  const tabParam = searchParams.get('tab');
  const tab: UserTab = tabs.some((t) => t.key === tabParam) ? (tabParam as UserTab) : 'profile';
  const setTab = (t: UserTab) =>
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.set('tab', t);
        return p;
      },
      { replace: true },
    );
  // Shared WAI-ARIA tablist keyboard nav (arrow keys + roving tabindex + Home/End).
  const tablist = useTablistKeyboard({ keys: tabs.map((t) => t.key), active: tab });

  return (
    <section className="flex h-full flex-col gap-6" data-testid="page-UserDetail" data-user-id={u.user_id}>
      <Breadcrumb
        items={[
          { label: 'Members' },
          { label: 'Humans', to: '/members/humans' },
          { label: u.display_name || u.user_id },
        ]}
      />
      <header className="border-b border-border-base pb-3">
        {/* name + kind tag inline; member-id on name hover (#192). */}
        <div className="flex items-center gap-2">
          <h2 className="text-xl font-semibold" data-testid="user-detail-name" title={u.user_id}>
            {u.display_name || u.user_id}
          </h2>
          <span
            className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted"
            data-testid="user-detail-kind-tag"
          >
            User
          </span>
        </div>
      </header>

      {/* tab bar */}
      <nav
        className="flex gap-1 border-b border-border-base"
        role="tablist"
        aria-orientation="horizontal"
        ref={tablist.tablistRef}
        onKeyDown={tablist.onKeyDown}
        onBlur={tablist.onBlur}
        data-testid="user-tabs"
      >
        {tabs.map((t) => (
          <button
            key={t.key}
            type="button"
            role="tab"
            aria-selected={tab === t.key}
            aria-controls={`user-panel-${t.key}`}
            id={`user-tab-${t.key}`}
            tabIndex={tablist.tabIndexFor(t.key)}
            onClick={() => setTab(t.key)}
            data-testid={`user-tab-${t.key}`}
            className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium ${
              tab === t.key
                ? 'border-brand text-text-primary'
                : 'border-transparent text-text-muted hover:text-text-primary'
            }`}
          >
            {t.label}
          </button>
        ))}
      </nav>

      {tab === 'profile' && (
        <section
          role="tabpanel"
          id="user-panel-profile"
          aria-labelledby="user-tab-profile"
          tabIndex={0}
          className="rounded-lg border border-border-base bg-bg-elevated p-4"
          data-testid="user-detail-profile"
        >
          <dl className="grid grid-cols-[8rem_1fr] gap-y-2 text-sm">
            <dt className="text-text-muted">Email</dt>
            <dd className="text-text-primary" data-testid="user-detail-email">{u.email || '—'}</dd>
            <dt className="text-text-muted">Created</dt>
            <dd className="text-text-secondary" data-testid="user-detail-created">{fmtDate(u.created_at)}</dd>
            <dt className="text-text-muted">Last active</dt>
            <dd className="text-text-secondary" data-testid="user-detail-last-session">{fmtDate(u.last_session_at)}</dd>
          </dl>
        </section>
      )}

      {tab === 'organizations' && (
        <section
          role="tabpanel"
          id="user-panel-organizations"
          aria-labelledby="user-tab-organizations"
          tabIndex={0}
          className="rounded-lg border border-border-base bg-bg-elevated p-4"
        >
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
                  {/* T478 #1: show org name AND id (id no longer hidden behind a
                      hover, so members can read the org-<hex>/organization-<hex>
                      identifiers they navigate by). */}
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate text-text-primary" title={o.org_id} data-testid="user-detail-org-name">
                      {orgName(o)}
                    </span>
                    <span className="truncate text-[0.6875rem] text-text-muted" data-testid="user-detail-org-id">
                      {o.org_id}
                    </span>
                  </span>
                  <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                    {o.role}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {isSelf && tab === 'account' && (
        <section
          role="tabpanel"
          id="user-panel-account"
          aria-labelledby="user-tab-account"
          tabIndex={0}
          className="rounded-lg border border-border-base bg-bg-elevated p-4"
          data-testid="user-detail-account"
        >
          <AccountPanel />
        </section>
      )}
    </section>
  );
}

import type React from 'react';
import { Navigate } from 'react-router-dom';
import { useMe } from '@/api/auth';
import { useOrgContext } from '@/OrgContext';

// Me (/me) — v2.8.1 #8: now a lightweight alias for the signed-in user's own
// UserDetail page. The account controls (change password + sign out) live on
// UserDetail as a self-only section, so /me redirects there instead of being a
// second, divergent profile page. The old /me entry points (sidebar user,
// org-error screen) keep working through this redirect.
export default function Me(): React.ReactElement {
  const me = useMe();
  const { slug } = useOrgContext();

  if (me.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-Me">
        Loading…
      </section>
    );
  }
  if (me.isError || !me.data) {
    return <Navigate to="/signin" replace />;
  }
  return (
    <Navigate
      to={`/organizations/${slug}/users/${me.data.identity_id}?tab=account`}
      replace
    />
  );
}

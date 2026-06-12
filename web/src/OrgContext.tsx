import React, { createContext, useContext } from 'react';
import { Navigate, useParams, Link } from 'react-router-dom';
import { useOrgs } from '@/api/auth';

interface OrgContextValue {
  slug: string;
  orgId: string;
  orgName: string;
}

export const OrgContext = createContext<OrgContextValue | null>(null);

// useOrgContext returns the current organization context.
// Must be used within an OrgGuard route.
export function useOrgContext(): OrgContextValue {
  const ctx = useContext(OrgContext);
  if (!ctx) throw new Error('useOrgContext must be used inside OrgGuard');
  return ctx;
}

// useOptionalOrgContext returns the org context or null if not inside OrgGuard.
export function useOptionalOrgContext(): OrgContextValue | null {
  return useContext(OrgContext);
}

// orgPath prefixes an app-absolute path (e.g. "/tasks/x") with the current
// org base so navigation stays under /organizations/{slug}. Paths that are
// already org-scoped, external, or non-absolute are returned unchanged.
export function orgPath(to: string, slug: string | undefined): string {
  if (!slug) return to;
  if (!to.startsWith('/')) return to; // relative / hash — leave as-is
  if (to.startsWith('/organizations/')) return to;
  if (to === '/') return to;
  return `/organizations/${slug}${to}`;
}

// OrgLink is a drop-in for react-router's Link that rewrites app-absolute
// resource paths to the current organization's scope (v2.6 X1 §5). Use it for
// in-app navigation between org-scoped resources so links never escape
// /organizations/{slug} and trigger a legacy redirect.
export function OrgLink(
  props: React.ComponentProps<typeof Link>,
): React.ReactElement {
  const ctx = useContext(OrgContext);
  const to = typeof props.to === 'string' ? orgPath(props.to, ctx?.slug) : props.to;
  return <Link {...props} to={to} />;
}

// OrgErrorScreen renders an explicit not-found / forbidden message instead of
// silently redirecting. v2.6 X1 §2.7/§2.10/§7.2: a deleted slug must read as
// 404 and a not-member slug as 403 so users can tell "no access" from "gone".
function OrgErrorScreen({ code, slug }: { code: 403 | 404; slug?: string }): React.ReactElement {
  const orgs = useOrgs();
  const firstOrg = orgs.data?.[0];
  const title = code === 404 ? 'Organization not found or no access' : 'No access to this organization (403)';
  const body =
    code === 404
      ? `Organization "${slug ?? ''}" does not exist, has been deleted, or you are not a member.`
      : `You are not a member of organization "${slug ?? ''}" and cannot access it.`;
  return (
    <div className="flex h-screen flex-col items-center justify-center gap-3 bg-bg-base px-4 text-center" data-testid="org-error">
      <h1 className="text-xl font-semibold text-text-primary">{title}</h1>
      <p className="text-sm text-text-muted">{body}</p>
      <div className="flex gap-3 pt-2">
        {firstOrg && (
          <Link to={`/organizations/${firstOrg.slug}`} className="text-accent hover:underline" data-testid="org-error-home">
            Go to my organization
          </Link>
        )}
        <Link to="/me" className="text-accent hover:underline">
          Account settings
        </Link>
      </div>
    </div>
  );
}

// OrgGuard validates the :slug URL parameter against the user's org list.
// - Loading OR not-yet-successfully-settled (incl. a transient error that React
//   Query is still retrying): shows spinner. We must NOT treat an errored/
//   undefined query the same as "genuinely no orgs" — under startup CPU
//   starvation GET /api/orgs can transiently 401, and prematurely redirecting to
//   /signup before the query settles is a robustness bug (v2.9 OrgGuard 401
//   retry). A genuine unauthenticated user is handled at the fetch layer
//   (client.ts request() → redirectUnauthenticated() on a real 401), so this
//   spinner never spins forever for unauth.
// - Settled successfully with no orgs at all: redirect to /signup.
// - Slug present but not in the user's active orgs: 404 (deleted/unknown) vs
//   403 (exists but not a member) — but the /api/orgs list only returns orgs
//   the caller belongs to, so from the SPA's view an unmatched slug is "not a
//   member or does not exist". We surface 404 by default; the backend is the
//   authoritative 403/404 boundary on every /api call.
// - Slug matches: provides OrgContext to children
export function OrgGuard({ children }: { children: React.ReactNode }): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const orgs = useOrgs();

  // Loading or not-yet-settled-successfully (e.g. a transient error still being
  // retried) → spinner. Only a settled-success result drives a redirect.
  if (!orgs.isSuccess) {
    return (
      <div className="flex h-screen items-center justify-center bg-bg-base">
        <span className="text-sm text-text-muted">Loading…</span>
      </div>
    );
  }

  // Settled successfully with no organizations at all → must sign up / be added.
  if ((orgs.data ?? []).length === 0) {
    return <Navigate to="/signup" replace />;
  }

  const activeOrg = (orgs.data ?? []).find((o) => o.slug === slug);

  if (!activeOrg) {
    // Slug not among the caller's orgs. /api/orgs only lists orgs the caller
    // is a member of, so this is "unknown to you" — show 404 (deleted/unknown)
    // rather than redirecting and hiding the problem.
    return <OrgErrorScreen code={404} slug={slug} />;
  }

  return (
    <OrgContext.Provider
      value={{ slug: activeOrg.slug, orgId: activeOrg.id, orgName: activeOrg.name }}
    >
      {children}
    </OrgContext.Provider>
  );
}

// OrgRedirect — redirect from / or unknown paths to the first org's home.
// Mirrors OrgGuard's settle gating: only a settled-success result drives a
// redirect. While loading or while a transient error is still being retried we
// show the spinner (genuine unauth is handled at the fetch layer), so a starved
// GET /api/orgs never bounces the user to /signup before the query settles.
export function OrgRedirect(): React.ReactElement {
  const orgs = useOrgs();
  if (!orgs.isSuccess) {
    return (
      <div className="flex h-screen items-center justify-center bg-bg-base">
        <span className="text-sm text-text-muted">Loading…</span>
      </div>
    );
  }
  const firstOrg = orgs.data?.[0];
  if (firstOrg) {
    return <Navigate to={`/organizations/${firstOrg.slug}`} replace />;
  }
  return <Navigate to="/signup" replace />;
}

import React, { createContext, useContext } from 'react';
import { Navigate, useParams, Link } from 'react-router-dom';
import { useOrgs } from '@/api/auth';

interface OrgContextValue {
  slug: string;
  orgId: string;
  orgName: string;
}

const OrgContext = createContext<OrgContextValue | null>(null);

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

// OrgErrorScreen renders an explicit not-found / forbidden message instead of
// silently redirecting. v2.6 X1 §2.7/§2.10/§7.2: a deleted slug must read as
// 404 and a not-member slug as 403 so users can tell "no access" from "gone".
function OrgErrorScreen({ code, slug }: { code: 403 | 404; slug?: string }): React.ReactElement {
  const orgs = useOrgs();
  const firstOrg = orgs.data?.[0];
  const title = code === 404 ? '组织不存在（404）' : '无权访问该组织（403）';
  const body =
    code === 404
      ? `组织 "${slug ?? ''}" 不存在或已被删除。`
      : `你不是组织 "${slug ?? ''}" 的成员，无权访问。`;
  return (
    <div className="flex h-screen flex-col items-center justify-center gap-3 bg-bg-base px-4 text-center" data-testid="org-error">
      <h1 className="text-xl font-semibold text-text-primary">{title}</h1>
      <p className="text-sm text-text-muted">{body}</p>
      <div className="flex gap-3 pt-2">
        {firstOrg && (
          <Link to={`/organizations/${firstOrg.slug}`} className="text-accent hover:underline" data-testid="org-error-home">
            前往我的组织
          </Link>
        )}
        <Link to="/me" className="text-accent hover:underline">
          账户设置
        </Link>
      </div>
    </div>
  );
}

// OrgGuard validates the :slug URL parameter against the user's org list.
// - Loading: shows spinner
// - No orgs at all: redirect to /signup
// - Slug present but not in the user's active orgs: 404 (deleted/unknown) vs
//   403 (exists but not a member) — but the /api/orgs list only returns orgs
//   the caller belongs to, so from the SPA's view an unmatched slug is "not a
//   member or does not exist". We surface 404 by default; the backend is the
//   authoritative 403/404 boundary on every /api call.
// - Slug matches: provides OrgContext to children
export function OrgGuard({ children }: { children: React.ReactNode }): React.ReactElement {
  const { slug } = useParams<{ slug: string }>();
  const orgs = useOrgs();

  if (orgs.isLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-bg-base">
        <span className="text-sm text-text-muted">加载中…</span>
      </div>
    );
  }

  // No organizations at all → must sign up / be added to one.
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
export function OrgRedirect(): React.ReactElement {
  const orgs = useOrgs();
  if (orgs.isLoading) {
    return (
      <div className="flex h-screen items-center justify-center bg-bg-base">
        <span className="text-sm text-text-muted">加载中…</span>
      </div>
    );
  }
  const firstOrg = orgs.data?.[0];
  if (firstOrg) {
    return <Navigate to={`/organizations/${firstOrg.slug}`} replace />;
  }
  return <Navigate to="/signup" replace />;
}

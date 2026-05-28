import React, { createContext, useContext } from 'react';
import { Navigate, useParams } from 'react-router-dom';
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

// OrgGuard validates the :slug URL parameter against the user's org list.
// - Loading: shows spinner
// - Slug not found: redirects to first org or /signup
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

  const activeOrg = (orgs.data ?? []).find((o) => o.slug === slug);

  if (!activeOrg) {
    const firstOrg = orgs.data?.[0];
    if (firstOrg) {
      return <Navigate to={`/organizations/${firstOrg.slug}`} replace />;
    }
    return <Navigate to="/signup" replace />;
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

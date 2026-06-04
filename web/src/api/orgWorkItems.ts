import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { OrgWorkItem } from './types';

// v2.8 #258: org-scoped cross-project Issues/Tasks aggregation.
// GET /api/issues?org_slug=<slug> | GET /api/tasks?org_slug=<slug>
//   → { items: OrgWorkItem[], total }.
// org scope follows the /api/projects convention: org_slug is auto-injected by
// the api client from the current /organizations/:slug/* URL (these paths are
// NOT in ORG_INJECT_EXEMPT), so the hook just calls /issues|/tasks.

export type OrgWorkItemKind = 'issue' | 'task';

export interface OrgWorkItemFilters {
  /** project ids (multi) — narrow the aggregation to specific projects. */
  project?: string[];
  /** status values (multi). Omitted = backend default "all open" (excludes terminal states). */
  status?: string[];
  /** assignee member-id or prefixed ref. */
  assignee?: string;
}

function buildQuery(f?: OrgWorkItemFilters): string {
  if (!f) return '';
  const p = new URLSearchParams();
  for (const id of f.project ?? []) p.append('project', id);
  for (const s of f.status ?? []) p.append('status', s);
  if (f.assignee) p.set('assignee', f.assignee);
  const s = p.toString();
  return s ? `?${s}` : '';
}

interface OrgWorkItemList {
  items: OrgWorkItem[];
  total: number;
}

export function useOrgWorkItems(
  kind: OrgWorkItemKind,
  slug: string | undefined,
  filters?: OrgWorkItemFilters,
) {
  const path = kind === 'issue' ? '/issues' : '/tasks';
  const key = kind === 'issue' ? qk.orgIssues({ slug, filters }) : qk.orgTasks({ slug, filters });
  return useQuery({
    queryKey: key,
    // org_slug is auto-injected by the client (/api/projects convention); slug is
    // kept only to scope the query-cache key + gate the fetch to an org route.
    queryFn: () => api.get<OrgWorkItemList>(`${path}${buildQuery(filters)}`),
    enabled: !!slug,
  });
}

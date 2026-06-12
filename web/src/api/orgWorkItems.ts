import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { OrgWorkItem } from './types';

// v2.8 #258: org-scoped cross-project Issues/Tasks aggregation.
// GET /api/orgs/{slug}/issues | GET /api/orgs/{slug}/tasks
//   → { items: OrgWorkItem[], total }.
// org scope follows the /api/projects convention: the /orgs/{slug} path segment
// is auto-injected by the api client (withOrgSlug) from the current
// /organizations/:slug/* URL (these paths are NOT in ORG_INJECT_EXEMPT), so the
// hook just calls /issues|/tasks.

export type OrgWorkItemKind = 'issue' | 'task';

export interface OrgWorkItemFilters {
  /** project ids (multi) — narrow the aggregation to specific projects. */
  project?: string[];
  /** status values (multi). Omitted = backend default "all open" (excludes terminal states). */
  status?: string[];
  /** assignee member-id or prefixed ref. */
  assignee?: string;
  // #258 date-range filters (PR #224). Each an ABSOLUTE RFC3339 instant carrying
  // the viewer's LOCAL offset (built via localDateToRFC3339 — NOT a naive date /
  // UTC midnight). The backend compares absolute instants. Each is optional and
  // independent; omitted when unset.
  created_after?: string;
  created_before?: string;
  updated_after?: string;
  updated_before?: string;
}

function buildQuery(f?: OrgWorkItemFilters): string {
  if (!f) return '';
  const p = new URLSearchParams();
  for (const id of f.project ?? []) p.append('project', id);
  for (const s of f.status ?? []) p.append('status', s);
  if (f.assignee) p.set('assignee', f.assignee);
  // #258 date-range params — only appended when set (already RFC3339-local-offset).
  if (f.created_after) p.set('created_after', f.created_after);
  if (f.created_before) p.set('created_before', f.created_before);
  if (f.updated_after) p.set('updated_after', f.updated_after);
  if (f.updated_before) p.set('updated_before', f.updated_before);
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

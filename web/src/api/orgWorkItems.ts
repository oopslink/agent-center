import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { OrgWorkItem } from './types';

// v2.8 #258: org-scoped cross-project Issues/Tasks aggregation.
// GET /api/orgs/:slug/issues|tasks → { items: OrgWorkItem[], total }.
// `/orgs` is exempt from the client's org_slug auto-injection (the slug is the
// explicit path segment here), so no double-scoping.

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
  const path = kind === 'issue' ? 'issues' : 'tasks';
  const key = kind === 'issue' ? qk.orgIssues({ slug, filters }) : qk.orgTasks({ slug, filters });
  return useQuery({
    queryKey: key,
    queryFn: () => api.get<OrgWorkItemList>(`/orgs/${slug}/${path}${buildQuery(filters)}`),
    enabled: !!slug,
  });
}

import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import { localDateToRFC3339 } from '@/utils/time';
import type { DateRange } from '@/components/WorkItemFilterBar';
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
  // server-side sort + pagination (backend handlers_pm_org applyPageItems).
  /** sort column key: created_at | updated_at | status | title | org_ref. */
  sort?: string;
  /** sort direction. */
  dir?: 'asc' | 'desc';
  /** 1-based page number (paired with page_size). */
  page?: number;
  /** page size; omitted = no pagination (all rows). */
  page_size?: number;
}

// buildWorkItemQuery — the SINGLE place the work-item filter params are turned
// into a query string. Reused by the org-scoped aggregation hook AND the
// per-project Task/Issue list hooks (T131), so both surfaces send the identical
// param contract (status / project / assignee / created_*/updated_*).
export function buildWorkItemQuery(f?: OrgWorkItemFilters): string {
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
  if (f.sort) p.set('sort', f.sort);
  if (f.dir) p.set('dir', f.dir);
  if (f.page && f.page > 1) p.set('page', String(f.page));
  if (f.page_size) p.set('page_size', String(f.page_size));
  const s = p.toString();
  return s ? `?${s}` : '';
}

// buildWorkItemFilters — convert the FilterBar's raw UI state into the wire
// filter object: empty status omits the param (backend default excludes terminal),
// project ids pass through, and each date picker is converted local→RFC3339-offset
// (start = 00:00:00, end/"before" = 23:59:59) and omitted when empty. Shared by the
// global page and the per-project lists so the local-date→instant 命门 lives in ONE
// place. Returns undefined when nothing is set (so the hook skips the query string).
export function buildWorkItemFilters(opts: {
  selectedStatuses: string[];
  selectedProjects: string[];
  assignee: string;
  dateRange: DateRange;
}): OrgWorkItemFilters | undefined {
  const { selectedStatuses, selectedProjects, assignee, dateRange } = opts;
  const f: OrgWorkItemFilters = {};
  if (selectedStatuses.length > 0) f.status = selectedStatuses;
  if (selectedProjects.length > 0) f.project = selectedProjects;
  if (assignee) f.assignee = assignee;
  const createdAfter = localDateToRFC3339(dateRange.created_after, 'start');
  const createdBefore = localDateToRFC3339(dateRange.created_before, 'end');
  const updatedAfter = localDateToRFC3339(dateRange.updated_after, 'start');
  const updatedBefore = localDateToRFC3339(dateRange.updated_before, 'end');
  if (createdAfter) f.created_after = createdAfter;
  if (createdBefore) f.created_before = createdBefore;
  if (updatedAfter) f.updated_after = updatedAfter;
  if (updatedBefore) f.updated_before = updatedBefore;
  return Object.keys(f).length > 0 ? f : undefined;
}

interface OrgWorkItemList {
  items: OrgWorkItem[];
  total: number;
}

interface OrgWorkItemQueryOptions {
  refetchOnMount?: boolean | 'always';
}

export function useOrgWorkItems(
  kind: OrgWorkItemKind,
  slug: string | undefined,
  filters?: OrgWorkItemFilters,
  options?: OrgWorkItemQueryOptions,
) {
  const path = kind === 'issue' ? '/issues' : '/tasks';
  const key = kind === 'issue' ? qk.orgIssues({ slug, filters }) : qk.orgTasks({ slug, filters });
  return useQuery({
    queryKey: key,
    // org_slug is auto-injected by the client (/api/projects convention); slug is
    // kept only to scope the query-cache key + gate the fetch to an org route.
    queryFn: () => api.get<OrgWorkItemList>(`${path}${buildWorkItemQuery(filters)}`),
    enabled: !!slug,
    refetchOnMount: options?.refetchOnMount,
  });
}

import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Issue, IssueStatus } from './types';

// useIssues / useIssue — v2.3-5b Discussion BC list/show reads.
//
// Layer note (per § 0.6): these hooks reach the BC that OWNS the
// Issue projection (Discussion BC), replacing the previous
// `useConversations({kind:'issue'})` cross-BC reach. The Conversation
// BC reads stay in place for message thread rendering only — that is
// genuinely Conversation BC's responsibility (message ownership).
//
// `project_id` is REQUIRED by the backend (returns 400 otherwise; see
// internal/webconsole/api/handlers.go listIssuesHandler). The hook
// therefore short-circuits via `enabled: !!projectId` — callers must
// pick a project before the list is fetched. The Issues page renders
// an "all projects" pseudo-filter by leaving the chip on "All" and
// surfacing an empty-state nudge ("pick a project").

export function useIssues(filter?: { projectId?: string; status?: IssueStatus }) {
  const projectId = filter?.projectId;
  const status = filter?.status;
  const search = new URLSearchParams();
  if (projectId) search.set('project_id', projectId);
  if (status) search.set('status', status);
  const qs = search.toString();
  return useQuery({
    queryKey: qk.issues({ projectId, status }),
    queryFn: () => api.get<Issue[]>(`/issues${qs ? `?${qs}` : ''}`),
    enabled: !!projectId,
  });
}

export function useIssue(id: string | undefined) {
  return useQuery({
    queryKey: qk.issue(id ?? ''),
    queryFn: () => api.get<Issue>(`/issues/${id}`),
    enabled: !!id,
  });
}

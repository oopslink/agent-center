import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import { buildWorkItemQuery, type OrgWorkItemFilters } from './orgWorkItems';
import type { Issue, IssueStatus } from './types';

// Issues (v2.7 ProjectManager BC). Project-scoped: every read/write is
// nested under /projects/{project_id}/issues. Responses are wrapped
// ({ issues: [...] }) for the list; single endpoints return IssueMap.

// T131: the project Issue list accepts the SAME filters as the org list (status /
// created_*/updated_*; issues are not assignable). Filters become a query-key
// SUFFIX (per-filter cache) while `qk.issuesByProject(projectId)` stays a PREFIX so
// the existing create/update invalidations still refresh every filtered variant.
export function useIssues(projectId: string | undefined, filters?: OrgWorkItemFilters) {
  return useQuery({
    queryKey: [...qk.issuesByProject(projectId ?? ''), filters ?? null],
    queryFn: async () => {
      const resp = await api.get<{ issues: Issue[] }>(
        `/projects/${projectId}/issues${buildWorkItemQuery(filters)}`,
      );
      return resp.issues;
    },
    enabled: !!projectId,
  });
}

export function useIssue(projectId: string | undefined, issueId: string | undefined) {
  return useQuery({
    queryKey: qk.issue(issueId ?? ''),
    queryFn: () =>
      api.get<Issue>(`/projects/${projectId}/issues/${issueId}`),
    enabled: !!projectId && !!issueId,
  });
}

export interface CreateIssueInput {
  title: string;
  description?: string;
}

export function useCreateIssue(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateIssueInput) =>
      api.post<Issue>(`/projects/${projectId}/issues`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.issuesByProject(projectId) });
    },
  });
}

// Mirror of Dev's #251 PATCH contract: atomic, partial (only changed fields),
// version-CAS. Issue editable fields = title / description / status / tags.
// NO assignee — Issues are not assignable (unlike Tasks, #278).
export interface UpdateIssueInput {
  title?: string;
  description?: string;
  status?: IssueStatus;
  tags?: string[];
}

export function useUpdateIssue(projectId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateIssueInput) =>
      api.patch<Issue>(`/projects/${projectId}/issues/${issueId}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.issue(issueId) });
      void qc.invalidateQueries({ queryKey: qk.issuesByProject(projectId) });
    },
  });
}

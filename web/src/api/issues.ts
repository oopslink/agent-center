import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Issue, IssueStatus } from './types';

// Issues (v2.7 ProjectManager BC). Project-scoped: every read/write is
// nested under /projects/{project_id}/issues. Responses are wrapped
// ({ issues: [...] }) for the list; single endpoints return IssueMap.

export function useIssues(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.issuesByProject(projectId ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ issues: Issue[] }>(
        `/projects/${projectId}/issues`,
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

export interface UpdateIssueInput {
  title?: string;
  description?: string;
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

// useTransitionIssue — single transition endpoint driving the issue
// state machine. The caller passes the target status.
export function useTransitionIssue(projectId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (status: IssueStatus) =>
      api.post<Issue>(`/projects/${projectId}/issues/${issueId}/transition`, {
        status,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.issue(issueId) });
      void qc.invalidateQueries({ queryKey: qk.issuesByProject(projectId) });
    },
  });
}

// ISSUE_TRANSITIONS — valid target states keyed by current status.
export const ISSUE_TRANSITIONS: Record<IssueStatus, IssueStatus[]> = {
  open: ['in_progress', 'discarded'],
  in_progress: ['resolved', 'discarded'],
  resolved: ['closed', 'reopened'],
  closed: ['reopened'],
  reopened: ['open'],
  discarded: [],
};

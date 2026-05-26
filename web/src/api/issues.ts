import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
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

// v2.5.x #61 — Open Issue from scratch (no message source). The same
// POST /api/issues endpoint also serves the CV4 derive flow when the
// payload includes source_conversation_id; this mutation only fires
// the open-from-scratch branch.

export interface OpenIssueInput {
  project_id: string;
  title: string;
  description?: string;
}

export interface OpenIssueResult {
  issue_id: string;
  conversation_id: string;
  event_id: string;
}

export function useOpenIssue() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: OpenIssueInput) =>
      api.post<OpenIssueResult>('/issues', input),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({
        queryKey: qk.issues({ projectId: vars.project_id }),
      });
    },
  });
}

// v2.5.x #61 — Conclude an Issue with one of 3 kinds.
// closed_with_tasks requires at least one task spec (title required;
// description + priority optional). closed_no_action / withdrawn only
// need a summary.

export type ConcludeKind = 'closed_no_action' | 'closed_with_tasks' | 'withdrawn';

export interface ConcludeTaskSpec {
  title: string;
  description?: string;
  priority?: string;
  local_id?: string;
}

export interface ConcludeIssueInput {
  kind: ConcludeKind;
  summary: string;
  tasks?: ConcludeTaskSpec[];
}

export interface ConcludeIssueResult {
  issue_id: string;
  task_ids: string[];
  event_ids: string[];
}

// v2.5.x #64 — Edit issue metadata.

export interface UpdateIssueInput {
  title: string;
  description?: string;
}

interface LifecycleResult {
  issue_id: string;
  event_id: string;
}

export function useUpdateIssue(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateIssueInput) =>
      api.patch<LifecycleResult>(`/issues/${issueId}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.issue(issueId) });
      void qc.invalidateQueries({ queryKey: qk.issues() });
    },
  });
}

// v2.5.x #64 — Reopen issue (any concluded/withdrawn → open).

export function useReopenIssue(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<LifecycleResult>(`/issues/${issueId}/reopen`, {}),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.issue(issueId) });
      void qc.invalidateQueries({ queryKey: qk.issues() });
    },
  });
}

export function useConcludeIssue(issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ConcludeIssueInput) =>
      api.post<ConcludeIssueResult>(`/issues/${issueId}/conclude`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.issue(issueId) });
      void qc.invalidateQueries({ queryKey: qk.issues() });
    },
  });
}

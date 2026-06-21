import { useMemo } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import { buildWorkItemQuery, type OrgWorkItemFilters } from './orgWorkItems';
import type { Task, TaskStatus } from './types';

// Tasks (v2.7 ProjectManager BC). Project-scoped: every read/write is
// nested under /projects/{project_id}/tasks. The list response is
// wrapped ({ tasks: [...] }); single + action endpoints return TaskMap.

// T131: the project Task list accepts the SAME filters as the org list (status /
// assignee / created_*/updated_*) — the project dimension is fixed by the path,
// so `filters.project` is unused here. The filters become a query-key SUFFIX so
// each filtered view caches separately, while `qk.tasksByProject(projectId)` stays
// a PREFIX — the existing create/update invalidations still refresh every variant.
// Returns { items, total } so the project Tasks panel can render server-side
// pagination (T302). filters carries status/assignee/q/time PLUS sort/dir/page/
// page_size; the backend paginates in SQL. `total` is the full pre-page count.
export function useTasksList(projectId: string | undefined, filters?: OrgWorkItemFilters) {
  return useQuery({
    queryKey: [...qk.tasksByProject(projectId ?? ''), filters ?? null],
    queryFn: async () => {
      const resp = await api.get<{ tasks: Task[]; total?: number }>(
        `/projects/${projectId}/tasks${buildWorkItemQuery(filters)}`,
      );
      return { items: resp.tasks ?? [], total: resp.total ?? (resp.tasks ?? []).length };
    },
    enabled: !!projectId,
  });
}

// useTasksOfIssue — the tasks DERIVED from an issue (reverse-lookup by
// derived_from_issue), for the issue detail sidebar's "Derived Tasks" block.
// Reuses the project task list (status=['all'] so terminal derived tasks still
// surface) and filters client-side by derived_from_issue — mirrors the agent
// `list_tasks_of_issue` reverse-lookup without a new endpoint, so it shares the
// project-tasks cache and refreshes on the same task mutations. Read-only.
export function useTasksOfIssue(projectId: string | undefined, issueId: string | undefined) {
  const q = useTasksList(projectId, { status: ['all'] });
  const data = useMemo(
    () => (issueId ? (q.data?.items ?? []).filter((t) => t.derived_from_issue === issueId) : []),
    [q.data, issueId],
  );
  return { ...q, data };
}

export function useTask(projectId: string | undefined, taskId: string | undefined) {
  return useQuery({
    queryKey: qk.task(taskId ?? ''),
    queryFn: () => api.get<Task>(`/projects/${projectId}/tasks/${taskId}`),
    enabled: !!projectId && !!taskId,
  });
}

export interface CreateTaskInput {
  title: string;
  description?: string;
  derived_from_issue?: string;
}

export function useCreateTask(projectId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateTaskInput) =>
      api.post<Task>(`/projects/${projectId}/tasks`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.tasksByProject(projectId) });
      // T233: also refresh the cross-project org Tasks aggregation list — the
      // OrgWorkItems page (useOrgWorkItems → qk.orgTasks) reads from a SEPARATE
      // cache key, so without this the table didn't show the just-added task
      // until the 30s staleTime lapsed.
      void qc.invalidateQueries({ queryKey: qk.orgTasksAll() });
    },
  });
}

// UpdateTaskInput is the body of the bare batch PATCH
// (PATCH /projects/{pid}/tasks/{id} → pmBatchUpdateTaskHandler, v2.8.1 #278).
// Atomic all-or-none; send ONLY the changed fields (omitted = unchanged). Wire
// keys match the Go handler exactly: status / assignee / tags / title /
// description (NOT "desc"). assignee:"" unassigns; status is any valid TaskStatus
// (free SetStatus, no adjacency); tags is the full replacement label set.
export interface UpdateTaskInput {
  title?: string;
  description?: string;
  status?: TaskStatus;
  assignee?: string;
  tags?: string[];
}

export function useUpdateTask(projectId: string, taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateTaskInput) =>
      api.patch<Task>(`/projects/${projectId}/tasks/${taskId}`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.task(taskId) });
      void qc.invalidateQueries({ queryKey: qk.tasksByProject(projectId) });
    },
  });
}

// Task lifecycle actions. Each POSTs to a sub-route and returns the
// refreshed task. They share an invalidation onSuccess.
function useTaskAction<TVars>(
  projectId: string,
  taskId: string,
  fn: (vars: TVars) => Promise<Task>,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.task(taskId) });
      void qc.invalidateQueries({ queryKey: qk.tasksByProject(projectId) });
    },
  });
}

const taskPath = (projectId: string, taskId: string) =>
  `/projects/${projectId}/tasks/${taskId}`;

export function useAssignTask(projectId: string, taskId: string) {
  return useTaskAction<{ assignee: string }>(projectId, taskId, (vars) =>
    api.post<Task>(`${taskPath(projectId, taskId)}/assign`, vars),
  );
}

export function useStartTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/start`),
  );
}

export function useBlockTask(projectId: string, taskId: string) {
  return useTaskAction<{ reason: string }>(projectId, taskId, (vars) =>
    api.post<Task>(`${taskPath(projectId, taskId)}/block`, vars),
  );
}

export function useUnblockTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/unblock`),
  );
}

export function useCompleteTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/complete`),
  );
}

export function useDiscardTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/discard`),
  );
}

export function useUnassignTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/unassign`),
  );
}

export function useReopenTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/reopen`),
  );
}

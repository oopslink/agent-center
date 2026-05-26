import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Task, TaskStatus } from './types';

// useTasksList / useTask — v2.3-5b TaskRuntime BC list/show reads.
//
// Layer note (per § 0.6): these reach the BC that OWNS the Task
// projection (TaskRuntime BC), replacing the previous
// `useConversations({kind:'task'})` cross-BC reach. The trace hook
// already in `api/fleet.ts` (`useTaskTrace`) is intentionally
// separate — both surfaces live on TaskRuntime BC but they answer
// different questions (list/show vs execution trace).
//
// Backend requires `project_id` (returns 400 otherwise; see
// internal/webconsole/api/handlers.go listTasksHandler). Hook
// short-circuits via `enabled: !!projectId`.

export function useTasksList(filter?: { projectId?: string; status?: TaskStatus }) {
  const projectId = filter?.projectId;
  const status = filter?.status;
  const search = new URLSearchParams();
  if (projectId) search.set('project_id', projectId);
  if (status) search.set('status', status);
  const qs = search.toString();
  return useQuery({
    queryKey: qk.tasksList({ projectId, status }),
    queryFn: () => api.get<Task[]>(`/tasks${qs ? `?${qs}` : ''}`),
    enabled: !!projectId,
  });
}

export function useTask(id: string | undefined) {
  return useQuery({
    queryKey: qk.task(id ?? ''),
    queryFn: () => api.get<Task>(`/tasks/${id}`),
    enabled: !!id,
  });
}

// v2.5.x #62 — Create Task from scratch (no message source). Same
// POST /api/tasks endpoint also routes the CV4 derive flow when the
// payload includes source_conversation_id.

export interface CreateTaskInput {
  project_id: string;
  title: string;
  description?: string;
  parent_task_id?: string;
  priority?: string;
  requires_worktree?: boolean;
  with_conversation?: boolean;
}

export interface CreateTaskResult {
  task_id: string;
  conversation_id?: string;
}

export function useCreateTask() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateTaskInput) =>
      api.post<CreateTaskResult>('/tasks', input),
    onSuccess: (_data, vars) => {
      void qc.invalidateQueries({
        queryKey: qk.tasksList({ projectId: vars.project_id }),
      });
    },
  });
}

// v2.5.x #62 — Task lifecycle transitions.

interface LifecycleResult {
  task_id: string;
  event_id: string;
}

export function useSuspendTask(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<LifecycleResult>(`/tasks/${taskId}/suspend`, {}),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.task(taskId) });
      void qc.invalidateQueries({ queryKey: qk.tasksList() });
    },
  });
}

export function useResumeTask(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<LifecycleResult>(`/tasks/${taskId}/resume`, {}),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.task(taskId) });
      void qc.invalidateQueries({ queryKey: qk.tasksList() });
    },
  });
}

export interface AbandonTaskInput {
  reason: string;
  message: string;
}

export function useAbandonTask(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: AbandonTaskInput) =>
      api.post<LifecycleResult>(`/tasks/${taskId}/abandon`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.task(taskId) });
      void qc.invalidateQueries({ queryKey: qk.tasksList() });
    },
  });
}

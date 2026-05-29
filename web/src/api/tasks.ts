import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Task } from './types';

// Tasks (v2.7 ProjectManager BC). Project-scoped: every read/write is
// nested under /projects/{project_id}/tasks. The list response is
// wrapped ({ tasks: [...] }); single + action endpoints return TaskMap.

export function useTasksList(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.tasksByProject(projectId ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ tasks: Task[] }>(
        `/projects/${projectId}/tasks`,
      );
      return resp.tasks;
    },
    enabled: !!projectId,
  });
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
    },
  });
}

export interface UpdateTaskInput {
  title?: string;
  description?: string;
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

export function useVerifyTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/verify`),
  );
}

export function useCancelTask(projectId: string, taskId: string) {
  return useTaskAction<void>(projectId, taskId, () =>
    api.post<Task>(`${taskPath(projectId, taskId)}/cancel`),
  );
}

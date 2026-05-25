import { useQuery } from '@tanstack/react-query';
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

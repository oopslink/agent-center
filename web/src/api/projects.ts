import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Project } from './types';

// Projects (v2.1-A picker + v2.3-4 list/detail surface). Read-only;
// CRUD verbs go through the `agent-center project` CLI (ADR-0029).

// Re-export the canonical Project type so existing call-sites that
// previously imported it from `@/api/projects` keep working.
export type { Project };

export function useProjects() {
  return useQuery({
    queryKey: qk.projects(),
    queryFn: () => api.get<Project[]>('/projects'),
    staleTime: 5_000,
  });
}

export function useProject(id: string | undefined) {
  return useQuery({
    queryKey: qk.project(id ?? ''),
    queryFn: () => api.get<Project>(`/projects/${id}`),
    enabled: !!id,
  });
}

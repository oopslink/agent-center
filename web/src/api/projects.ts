import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// Projects (v2.1-A — powers the DeriveModal project picker). Read-only;
// CRUD verbs go through the `agent-center project` CLI subtree.

export interface Project {
  id: string;
  name: string;
  kind?: string;
  created_at: string;
}

export function useProjects() {
  return useQuery({
    queryKey: qk.projects(),
    queryFn: () => api.get<Project[]>('/projects'),
    staleTime: 5_000,
  });
}

import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { AgentInstance } from './types';

export function useAgents() {
  return useQuery({
    queryKey: qk.agents(),
    queryFn: () => api.get<AgentInstance[]>('/agents'),
  });
}

export function useAgent(name: string | undefined) {
  return useQuery({
    queryKey: qk.agent(name ?? ''),
    queryFn: () => api.get<AgentInstance>(`/agents/${name}`),
    enabled: !!name,
  });
}

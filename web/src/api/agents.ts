import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Agent, AgentActivityEvent, AgentWorkItem } from './types';

// Agent BC (v2.7 #101). Org-scoped agents backed by /api/agents. Replaces
// the retired workforce.AgentInstance surface. List/work-items/activity
// responses are WRAPPED objects; detail + lifecycle mutations return a
// bare AgentMap.

export interface CreateAgentInput {
  name: string;
  description?: string;
  model?: string;
  cli?: string;
  env_vars?: Record<string, string>;
  skills?: string[];
  worker_id: string;
}

export type ResetScope = 'memory' | 'workspace' | 'all';

export interface ResetAgentInput {
  scope: ResetScope;
  confirm: boolean;
}

export function useAgents() {
  return useQuery({
    queryKey: qk.agents(),
    queryFn: async () => {
      const resp = await api.get<{ agents: Agent[] }>('/agents');
      return resp.agents;
    },
  });
}

export function useAgent(id: string | undefined) {
  return useQuery({
    queryKey: qk.agent(id ?? ''),
    queryFn: () => api.get<Agent>(`/agents/${id}`),
    enabled: !!id,
  });
}

export function useCreateAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateAgentInput) => api.post<Agent>('/agents', input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.agents() });
    },
  });
}

// Lifecycle mutations. Each POSTs to a sub-route and returns the refreshed
// AgentMap; on success we invalidate both the detail + list query keys so
// the badge/controls re-render against the new lifecycle.
function useLifecycleMutation(id: string, action: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<Agent>(`/agents/${id}/${action}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.agent(id) });
      void qc.invalidateQueries({ queryKey: qk.agents() });
    },
  });
}

export function useStartAgent(id: string) {
  return useLifecycleMutation(id, 'start');
}

export function useStopAgent(id: string) {
  return useLifecycleMutation(id, 'stop');
}

export function useRestartAgent(id: string) {
  return useLifecycleMutation(id, 'restart');
}

export function useResetAgent(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: ResetAgentInput) =>
      api.post<Agent>(`/agents/${id}/reset`, input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.agent(id) });
      void qc.invalidateQueries({ queryKey: qk.agents() });
    },
  });
}

export function useAgentWorkItems(id: string | undefined) {
  return useQuery({
    queryKey: qk.agentWorkItems(id ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ work_items: AgentWorkItem[] }>(
        `/agents/${id}/work-items`,
      );
      return resp.work_items;
    },
    enabled: !!id,
  });
}

export function useAgentActivity(id: string | undefined) {
  return useQuery({
    queryKey: qk.agentActivity(id ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ activity: AgentActivityEvent[] }>(
        `/agents/${id}/activity`,
      );
      return resp.activity;
    },
    enabled: !!id,
  });
}

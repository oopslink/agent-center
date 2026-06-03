import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Agent, AgentActivityEvent, AgentWorkItem } from './types';

// Agent BC (v2.7 #101). Org-scoped agents backed by /api/agents. Replaces
// the retired workforce.AgentInstance surface. List/work-items/activity
// responses are WRAPPED objects; detail + lifecycle mutations return a
// bare AgentMap.

// v2.7 #186/#77: CreateAgentInput + useCreateAgent removed — POST /api/agents
// was deleted ("no middle state": agent always has a member id). Agent creation
// now goes through the unified POST /api/members/agent (useAddAgentMember in
// api/members.ts), which atomically creates the identity-member + execution Agent.

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

// useDeleteAgent hard-deletes an agent and its identity-member in one tx
// (v2.7 #197, symmetric to #157's atomic create). The backend guards reject
// a non-stopped agent (409 `agent_running`) or one with active work items
// (409 `agent_has_active_work`); the worker binding is released (worker row
// untouched). Errors surface as ApiError for the caller to map + display.
export function useDeleteAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del<void>(`/agents/${id}`),
    onSuccess: (_, id) => {
      void qc.invalidateQueries({ queryKey: qk.agents() });
      void qc.removeQueries({ queryKey: qk.agent(id) });
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

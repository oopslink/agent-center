import { useCallback, useState } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { Agent, AgentActivityEvent, AgentTask, ExecutorProfile } from './types';

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

// v2.8 #270/#272: soft-archive — the ONLY user-facing delete path. POSTs the
// #272 endpoint (not DELETE; soft, REST-strict). Backend guards: only a settled
// agent (stopped/error) archives — running → 409 invalid_state "must be stopped
// first" (b strict-two-step); idempotent (already-archived → 200 no-op); worker
// binding cleared. Returns the refreshed (lifecycle=archived) agent. Hard
// DeleteAgent (above) is admin-only and intentionally has NO UI surface.
export function useArchiveAgent(id: string) {
  return useLifecycleMutation(id, 'archive');
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

// T236: edit the agent's LLM config (model/cli/reasoning/mode/provider). Persists
// immediately; the change applies on the next (re)start, so the UI pairs it with
// a restart (useRestartAgent) behind a confirm dialog.
export interface UpdateAgentConfigInput {
  model: string;
  cli: string;
  reasoning: string;
  mode: string;
  provider: string;
  env_vars?: Record<string, string>;
  // v2.18.1 (issue-8746a5b9) executor concurrency. allowed_executors is the
  // authoritative {cli,model} candidate list (server hard-validates cli ∈
  // {claude-code,codex}); max_concurrent_tasks gates parallelism (0/1 = single
  // active). Both optional so non-concurrency edits keep the legacy body shape.
  max_concurrent_tasks?: number;
  allowed_executors?: ExecutorProfile[];
  // T566 (issue-577a7b0e): per-agent opt-out of auto-assignment (default true).
  // Optional so a config edit that doesn't touch it preserves the server value.
  auto_assignable?: boolean;
  // Agent description. Optional so a config edit that doesn't touch it preserves
  // the server value.
  description?: string;
  // T728 (issue-0619f315): inject the description into the system prompt (default
  // true). Optional so a config edit that omits it preserves the server value.
  include_description_in_system_prompt?: boolean;
}

export function useUpdateAgentConfig(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UpdateAgentConfigInput) =>
      api.patch<Agent>(`/agents/${id}/config`, input),
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

// v2.8.1 force-delete (admin): DELETE /api/agents/{id}?force=true cleans the
// center's metadata only — the backend skips the stop/active guards that the
// soft path enforces (no-force + active → 409 `agent_active`) and does NOT kill
// the process; the worker binding is released. Returns the bare `{ok:true}` body.
// Org-admin gated server-side. ApiError surfaces 404/403/409 for the caller.
export function useForceDeleteAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      api.del<{ ok: boolean }>(`/agents/${id}?force=true`),
    onSuccess: (_, id) => {
      void qc.invalidateQueries({ queryKey: qk.agents() });
      void qc.removeQueries({ queryKey: qk.agent(id) });
    },
  });
}

// T232: batch lifecycle operations for the Agents list. The center exposes only
// PER-AGENT lifecycle endpoints (/agents/:id/{start,stop,restart,reset}), so a
// "batch" is a client-side SEQUENTIAL fan-out — POST each id in turn, tracking
// done/total + per-agent ok/error so the list page can render a progress bar and
// a partial-failure summary (one agent failing a transition never aborts the
// rest; the backend's per-agent guards surface as that row's error). 'reset'
// reuses the single-reset body ({scope:'all', confirm:true}). On completion the
// agents list is invalidated ONCE so every affected row re-renders.
export const AGENT_BATCH_ACTIONS = ['start', 'stop', 'restart', 'reset'] as const;
export type AgentBatchAction = (typeof AGENT_BATCH_ACTIONS)[number];

export interface BatchItemResult {
  id: string;
  ok: boolean;
  error?: string;
}

export interface BatchLifecycleProgress {
  action: AgentBatchAction | null;
  total: number;
  done: number;
  running: boolean;
  results: BatchItemResult[];
}

const IDLE_BATCH_PROGRESS: BatchLifecycleProgress = {
  action: null,
  total: 0,
  done: 0,
  running: false,
  results: [],
};

export function useBatchAgentLifecycle() {
  const qc = useQueryClient();
  const [progress, setProgress] = useState<BatchLifecycleProgress>(IDLE_BATCH_PROGRESS);

  const run = useCallback(
    async (ids: string[], action: AgentBatchAction): Promise<void> => {
      if (ids.length === 0) return;
      setProgress({ action, total: ids.length, done: 0, running: true, results: [] });
      const results: BatchItemResult[] = [];
      for (const id of ids) {
        try {
          if (action === 'reset') {
            await api.post<Agent>(`/agents/${id}/reset`, { scope: 'all', confirm: true });
          } else {
            await api.post<Agent>(`/agents/${id}/${action}`);
          }
          results.push({ id, ok: true });
        } catch (e) {
          results.push({ id, ok: false, error: e instanceof Error ? e.message : 'failed' });
        }
        // snapshot a copy each tick so React sees a new array reference
        setProgress((p) => ({ ...p, done: results.length, results: [...results] }));
      }
      await qc.invalidateQueries({ queryKey: qk.agents() });
      setProgress((p) => ({ ...p, running: false }));
    },
    [qc],
  );

  const reset = useCallback(() => setProgress(IDLE_BATCH_PROGRESS), []);

  return { run, progress, reset };
}

// v2.14.0 / issue I14: an agent's unit of work is the Task now (AgentWorkItem
// retired). The endpoint + envelope are task-named to match the backend rename.
export function useAgentTasks(id: string | undefined) {
  return useQuery({
    queryKey: qk.agentTasks(id ?? ''),
    queryFn: async () => {
      const resp = await api.get<{ tasks: AgentTask[] }>(
        `/agents/${id}/tasks`,
      );
      return resp.tasks;
    },
    enabled: !!id,
  });
}

// v2.8 #274: the Activity feed is cursor-paginated. Each page is
// GET /agents/:id/activity?limit=50[&before=<event_id>] → { activity, next_cursor }.
// We always send an explicit limit=50 (self-documenting — never the absent→default
// path) and follow next_cursor (null = 末页). The "Load older" control calls
// fetchNextPage; the consumer flattens data.pages → events and re-groups over the
// FULL accumulated set (so a Checking run spanning a page boundary merges, #274).
export interface AgentActivityPage {
  activity: AgentActivityEvent[];
  next_cursor: string | null;
}

export function useAgentActivity(id: string | undefined) {
  return useInfiniteQuery({
    queryKey: qk.agentActivity(id ?? ''),
    queryFn: ({ pageParam }) => {
      const before = pageParam ? `&before=${encodeURIComponent(pageParam)}` : '';
      return api.get<AgentActivityPage>(`/agents/${id}/activity?limit=50${before}`);
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!id,
  });
}

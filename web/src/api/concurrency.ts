import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// Per-agent live concurrency slots (T593, #并发讨论2). The Center serves a snapshot
// of the agent's worker run-slots (refreshed on the worker's adaptive heartbeat);
// the Tasks tab polls it every 3s and overlays it onto the task rows by task_id.
// When the worker is offline / its snapshot exceeds TTL the snapshot comes back
// stale=true — the overlay is marked "stale (last known)" but the task list (which
// is Center-sourced) stays fully visible.

export interface ConcurrencyExecutor {
  executor_id: string;
  task_id: string;
  cli: string;
  model: string;
  // running | starting | orphan(-monitored) | … (free text; the UI special-cases
  // "starting" and any state containing "orphan").
  state: string;
  started_at: string;
  pid?: number;
}

export interface AgentConcurrency {
  agent_id: string;
  cap: number;
  active: number;
  queued: number;
  // stale — coarse "live view not usable" flag (no fresh snapshot). Kept for
  // back-compat; the overlay now branches on reachable + has_snapshot below.
  stale: boolean;
  // T606 three-state freshness (issue-af03da2f):
  //   reachable    — is the bound worker ONLINE? false = worker truly offline.
  //   has_snapshot — has this agent EVER reported a live snapshot? false = concurrency
  //                  not active on the worker (the common non-concurrent-agent case).
  // Optional so an older Center (pre-T606) degrades gracefully (treated as online +
  // snapshot-present, i.e. the legacy stale-only behavior).
  reachable?: boolean;
  has_snapshot?: boolean;
  snapshot_age_ms: number;
  executors: ConcurrencyExecutor[];
}

export const CONCURRENCY_POLL_MS = 3000;

export function useAgentConcurrency(agentId: string | undefined) {
  return useQuery({
    queryKey: qk.agentConcurrency(agentId ?? ''),
    queryFn: () => api.get<AgentConcurrency>(`/agents/${agentId}/concurrency`),
    enabled: !!agentId,
    // 3s live poll (overlay only — the task list has its own cadence).
    refetchInterval: CONCURRENCY_POLL_MS,
    retry: false,
  });
}

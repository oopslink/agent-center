import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { AgentAnalytics, AgentAnalyticsTaskDrilldown } from './types';

// I28/F6: per-agent analytics dashboard reads, over the F4 endpoints
// (GET /api/orgs/{slug}/agents/{id}/analytics + .../analytics/tasks/{taskId}).
// The composed analytics read is one fetch the dashboard page makes; the
// drill-down is lazy (enabled only when a Top-Cost-Task row is expanded).

export interface AnalyticsRange {
  from?: string; // "YYYY-MM-DD"; omitted → server default (53-week window)
  to?: string;
  top?: number; // Top-Cost-Tasks limit; omitted → server default
}

function analyticsPath(id: string, range?: AnalyticsRange): string {
  const q = new URLSearchParams();
  if (range?.from) q.set('from', range.from);
  if (range?.to) q.set('to', range.to);
  if (range?.top != null) q.set('top', String(range.top));
  const qs = q.toString();
  return `/agents/${id}/analytics${qs ? `?${qs}` : ''}`;
}

// useAgentAnalytics fetches the composed dashboard payload for an agent. Disabled
// until id is known. Analytics changes slowly (daily rollup), so a small
// staleTime avoids refetch churn on tab switches.
export function useAgentAnalytics(id: string | undefined, range?: AnalyticsRange) {
  return useQuery({
    queryKey: qk.agentAnalytics(id ?? '', range?.from, range?.to),
    queryFn: () => api.get<AgentAnalytics>(analyticsPath(id as string, range)),
    enabled: !!id,
    staleTime: 30_000,
  });
}

// useAgentAnalyticsTask lazily fetches a Top-Cost-Task's raw usage events
// (the drill-down). Pass enabled=false until the row is expanded so the request
// is not made on initial dashboard load.
export function useAgentAnalyticsTask(
  id: string | undefined,
  taskId: string | undefined,
  enabled: boolean,
) {
  return useQuery({
    queryKey: qk.agentAnalyticsTask(id ?? '', taskId ?? ''),
    queryFn: () =>
      api.get<AgentAnalyticsTaskDrilldown>(`/agents/${id}/analytics/tasks/${taskId}`),
    enabled: enabled && !!id && !!taskId,
    staleTime: 30_000,
  });
}

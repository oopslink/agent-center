import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { InsightOverview, InsightTaskExecutions } from './types';

export type InsightDrillFilter = 'metric' | 'agent' | 'project';

export interface InsightDrilldownParams {
  filter: InsightDrillFilter;
  value: string;
}

export function useInsightOverview() {
  return useQuery({
    queryKey: qk.insightsOverview(),
    queryFn: () => api.get<InsightOverview>('/insights/overview'),
    refetchInterval: 30_000,
  });
}

export function useInsightTaskExecutions(params: InsightDrilldownParams | null) {
  return useQuery({
    queryKey: qk.insightsTaskExecutions(params?.filter, params?.value),
    queryFn: () => {
      const q = new URLSearchParams();
      if (params) {
        q.set('filter', params.filter);
        q.set('value', params.value);
      }
      return api.get<InsightTaskExecutions>(`/insights/task-executions?${q.toString()}`);
    },
    enabled: params != null,
  });
}

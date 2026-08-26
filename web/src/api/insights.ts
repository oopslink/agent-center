import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';
import type { InsightsExecution, InsightsOverview } from './types';

export function useInsightsOverview() {
  return useQuery({
    queryKey: qk.insightsOverview(),
    queryFn: () => api.get<InsightsOverview>('/insights/overview'),
    staleTime: 30_000,
  });
}

export function useInsightsExecution(executionId: string | undefined) {
  return useQuery({
    queryKey: qk.insightsExecution(executionId ?? ''),
    queryFn: () => api.get<InsightsExecution>(`/insights/executions/${encodeURIComponent(executionId as string)}`),
    enabled: !!executionId,
    staleTime: 30_000,
  });
}

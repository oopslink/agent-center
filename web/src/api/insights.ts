import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

export type InsightFreshnessState = 'fresh' | 'stale' | 'rebuilding' | 'unavailable';

export interface InsightWindow {
  kind: 'rolling';
  duration: '24h';
  start: string;
  end: string;
}

export interface InsightFreshness {
  state: InsightFreshnessState;
  age_ms: number;
  threshold_ms: number;
}

export interface InsightPercentiles {
  p50: number | null;
  p95: number | null;
  samples: number;
}

export interface InsightSummary {
  completed_executions: number;
  failed_executions: number;
  failure_rate: number | null;
  slot_utilization: number | null;
  slot_coverage_ratio: number | null;
  queue_wait_ms: InsightPercentiles;
  execution_duration_ms: InsightPercentiles;
}

export interface InsightLeaderboardAgent {
  agent_ref: string;
  display_name: string | null;
  summary: InsightSummary;
}

export interface InsightLeaderboardProject {
  project_id: string;
  name: string | null;
  summary: InsightSummary;
}

export interface InsightDiagnostics {
  invalid_facts: number;
  late_events: number;
}

export interface InsightOverview {
  window: InsightWindow;
  as_of: string;
  refreshed_at: string;
  freshness: InsightFreshness;
  summary: InsightSummary;
  agents: InsightLeaderboardAgent[];
  projects: InsightLeaderboardProject[];
  diagnostics: InsightDiagnostics;
}

export interface InsightExecutionRow {
  execution_id: string;
  command_id: string | null;
  task_id: string | null;
  task_ref: string | null;
  task_title: string | null;
  agent_ref: string;
  agent_name: string | null;
  project_id: string | null;
  project_name: string | null;
  worker_id: string | null;
  outcome: string | null;
  failure_reason: string | null;
  queued_at: string | null;
  started_at: string | null;
  finished_at: string | null;
  queue_wait_ms: number | null;
  duration_ms: number | null;
  recovered: boolean;
  quality: string;
}

export interface InsightExecutions {
  window: InsightWindow;
  as_of: string;
  refreshed_at: string;
  freshness: InsightFreshness;
  executions: InsightExecutionRow[];
  next_cursor: string | null;
}

export interface InsightExecutionDetail {
  window: InsightWindow;
  as_of: string;
  refreshed_at: string;
  freshness: InsightFreshness;
  execution: InsightExecutionRow;
}

export interface InsightExecutionFilters {
  agent_ref?: string;
  project_id?: string;
  cursor?: string;
  limit?: number;
}

function executionParams(filters: InsightExecutionFilters = {}): string {
  const params = new URLSearchParams({ window: '24h' });
  if (filters.agent_ref) params.set('agent_ref', filters.agent_ref);
  if (filters.project_id) params.set('project_id', filters.project_id);
  if (filters.cursor) params.set('cursor', filters.cursor);
  if (filters.limit) params.set('limit', String(filters.limit));
  return params.toString();
}

export function useInsightOverview() {
  return useQuery({
    queryKey: qk.insightOverview(),
    queryFn: () => api.get<InsightOverview>('/insights/overview?window=24h'),
  });
}

export function useInsightExecutions(filters: InsightExecutionFilters, enabled: boolean) {
  return useQuery({
    queryKey: qk.insightExecutions(filters),
    queryFn: () => api.get<InsightExecutions>(`/insights/executions?${executionParams(filters)}`),
    enabled,
  });
}

export function useInsightExecution(executionId: string | undefined) {
  return useQuery({
    queryKey: qk.insightExecution(executionId ?? ''),
    queryFn: () => api.get<InsightExecutionDetail>(`/insights/executions/${encodeURIComponent(executionId ?? '')}?window=24h`),
    enabled: Boolean(executionId),
  });
}

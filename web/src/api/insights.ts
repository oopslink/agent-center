import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

export type InsightFreshnessState = 'fresh' | 'stale' | 'rebuilding' | 'unavailable' | 'unknown';

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
  failure_message: string | null;
  command_status: string | null;
  status_reason: string | null;
  status_message: string | null;
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

export interface InsightV2Meta {
  metric_version: 'insight.metrics.v2';
  sample_count: number;
  coverage: number | null;
  freshness: InsightFreshness;
  unknown_count: number;
  known: boolean;
}

export interface InsightV2Health {
  status: 'healthy' | 'elevated' | 'degraded' | 'unknown' | string;
  reason_codes: string[];
  evidence: Array<Record<string, unknown>>;
}

export interface InsightV2CountMetric {
  value: number | null;
  meta: InsightV2Meta;
}

export interface InsightV2AgentSummary {
  id: string;
  name: string | null;
  health: InsightV2Health;
  execution_count: InsightV2CountMetric;
  failure_rate: number | null;
  open_issues: InsightV2CountMetric;
  blocked_tasks: InsightV2CountMetric;
  active_plans: InsightV2CountMetric;
  reason_codes: string[];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function arrayOrEmpty<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : [];
}

function stringOrEmpty(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function stringOrNull(value: unknown): string | null {
  return typeof value === 'string' ? value : null;
}

function numberOrZero(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function numberOrNull(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

function booleanOrFalse(value: unknown): boolean {
  return typeof value === 'boolean' ? value : false;
}

function normalizeWindow(value: unknown): InsightWindow {
  const source = isRecord(value) ? value : {};
  return {
    kind: 'rolling',
    duration: '24h',
    start: stringOrEmpty(source.start),
    end: stringOrEmpty(source.end),
  };
}

function normalizeFreshness(value: unknown): InsightFreshness {
  const source = isRecord(value) ? value : {};
  const state = source.state;
  return {
    state: state === 'fresh' || state === 'stale' || state === 'rebuilding' || state === 'unavailable'
      ? state
      : 'unknown',
    age_ms: numberOrZero(source.age_ms),
    threshold_ms: numberOrZero(source.threshold_ms),
  };
}

function normalizePercentiles(value: unknown): InsightPercentiles {
  const source = isRecord(value) ? value : {};
  return {
    p50: numberOrNull(source.p50),
    p95: numberOrNull(source.p95),
    samples: numberOrZero(source.samples),
  };
}

function normalizeSummary(value: unknown): InsightSummary {
  const source = isRecord(value) ? value : {};
  return {
    completed_executions: numberOrZero(source.completed_executions),
    failed_executions: numberOrZero(source.failed_executions),
    failure_rate: numberOrNull(source.failure_rate),
    slot_utilization: numberOrNull(source.slot_utilization),
    slot_coverage_ratio: numberOrNull(source.slot_coverage_ratio),
    queue_wait_ms: normalizePercentiles(source.queue_wait_ms),
    execution_duration_ms: normalizePercentiles(source.execution_duration_ms),
  };
}

function normalizeAgent(value: unknown): InsightLeaderboardAgent {
  const source = isRecord(value) ? value : {};
  const agentRef = stringOrEmpty(source.agent_ref) || 'unknown';
  return {
    agent_ref: agentRef,
    display_name: stringOrNull(source.display_name),
    summary: normalizeSummary(source.summary),
  };
}

function normalizeProject(value: unknown): InsightLeaderboardProject {
  const source = isRecord(value) ? value : {};
  const projectId = stringOrEmpty(source.project_id) || 'unknown';
  return {
    project_id: projectId,
    name: stringOrNull(source.name),
    summary: normalizeSummary(source.summary),
  };
}

function normalizeDiagnostics(value: unknown): InsightDiagnostics {
  const source = isRecord(value) ? value : {};
  return {
    invalid_facts: numberOrZero(source.invalid_facts),
    late_events: numberOrZero(source.late_events),
  };
}

function normalizeExecutionRow(value: unknown): InsightExecutionRow {
  const source = isRecord(value) ? value : {};
  return {
    execution_id: stringOrEmpty(source.execution_id) || 'unknown',
    command_id: stringOrNull(source.command_id),
    task_id: stringOrNull(source.task_id),
    task_ref: stringOrNull(source.task_ref),
    task_title: stringOrNull(source.task_title),
    agent_ref: stringOrEmpty(source.agent_ref) || 'unknown',
    agent_name: stringOrNull(source.agent_name),
    project_id: stringOrNull(source.project_id),
    project_name: stringOrNull(source.project_name),
    worker_id: stringOrNull(source.worker_id),
    outcome: stringOrNull(source.outcome),
    failure_reason: stringOrNull(source.failure_reason),
    failure_message: stringOrNull(source.failure_message),
    command_status: stringOrNull(source.command_status),
    status_reason: stringOrNull(source.status_reason),
    status_message: stringOrNull(source.status_message),
    queued_at: stringOrNull(source.queued_at),
    started_at: stringOrNull(source.started_at),
    finished_at: stringOrNull(source.finished_at),
    queue_wait_ms: numberOrNull(source.queue_wait_ms),
    duration_ms: numberOrNull(source.duration_ms),
    recovered: booleanOrFalse(source.recovered),
    quality: stringOrEmpty(source.quality) || 'unknown',
  };
}

function normalizeOverview(value: unknown): InsightOverview {
  const source = isRecord(value) ? value : {};
  return {
    window: normalizeWindow(source.window),
    as_of: stringOrEmpty(source.as_of),
    refreshed_at: stringOrEmpty(source.refreshed_at),
    freshness: normalizeFreshness(source.freshness),
    summary: normalizeSummary(source.summary),
    agents: arrayOrEmpty(source.agents as InsightLeaderboardAgent[] | null | undefined).map(normalizeAgent),
    projects: arrayOrEmpty(source.projects as InsightLeaderboardProject[] | null | undefined).map(normalizeProject),
    diagnostics: normalizeDiagnostics(source.diagnostics),
  };
}

function normalizeExecutions(value: unknown): InsightExecutions {
  const source = isRecord(value) ? value : {};
  return {
    window: normalizeWindow(source.window),
    as_of: stringOrEmpty(source.as_of),
    refreshed_at: stringOrEmpty(source.refreshed_at),
    freshness: normalizeFreshness(source.freshness),
    executions: arrayOrEmpty(source.executions as InsightExecutionRow[] | null | undefined).map(normalizeExecutionRow),
    next_cursor: stringOrNull(source.next_cursor),
  };
}

function normalizeExecutionDetail(value: unknown): InsightExecutionDetail {
  const source = isRecord(value) ? value : {};
  return {
    window: normalizeWindow(source.window),
    as_of: stringOrEmpty(source.as_of),
    refreshed_at: stringOrEmpty(source.refreshed_at),
    freshness: normalizeFreshness(source.freshness),
    execution: normalizeExecutionRow(source.execution),
  };
}

function normalizeV2Meta(value: unknown): InsightV2Meta {
  const source = isRecord(value) ? value : {};
  return {
    metric_version: 'insight.metrics.v2',
    sample_count: numberOrZero(source.sample_count),
    coverage: numberOrNull(source.coverage),
    freshness: normalizeFreshness(source.freshness),
    unknown_count: numberOrZero(source.unknown_count),
    known: typeof source.known === 'boolean' ? source.known : false,
  };
}

function normalizeV2Health(value: unknown): InsightV2Health {
  const source = isRecord(value) ? value : {};
  const status = stringOrEmpty(source.status) || 'unknown';
  const reasonCodes = Array.isArray(source.reason_codes) ? source.reason_codes.filter((v): v is string => typeof v === 'string') : [];
  return {
    status,
    reason_codes: reasonCodes,
    evidence: Array.isArray(source.evidence) ? source.evidence.filter(isRecord) : [],
  };
}

function normalizeV2CountMetric(value: unknown): InsightV2CountMetric {
  const source = isRecord(value) ? value : {};
  return {
    value: numberOrNull(source.value),
    meta: normalizeV2Meta(source.meta),
  };
}

function normalizeV2Agent(value: unknown): InsightV2AgentSummary {
  const source = isRecord(value) ? value : {};
  const health = normalizeV2Health(source.health);
  return {
    id: stringOrEmpty(source.id) || 'unknown',
    name: stringOrNull(source.name),
    health,
    execution_count: normalizeV2CountMetric(source.execution_count),
    failure_rate: numberOrNull(source.failure_rate),
    open_issues: normalizeV2CountMetric(source.open_issues),
    blocked_tasks: normalizeV2CountMetric(source.blocked_tasks),
    active_plans: normalizeV2CountMetric(source.active_plans),
    reason_codes: Array.isArray(source.reason_codes) ? source.reason_codes.filter((v): v is string => typeof v === 'string') : health.reason_codes,
  };
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
    queryFn: async () => normalizeOverview(await api.get<unknown>('/insights/overview?window=24h')),
  });
}

export function useInsightExecutions(filters: InsightExecutionFilters, enabled: boolean) {
  return useQuery({
    queryKey: qk.insightExecutions(filters),
    queryFn: async () => normalizeExecutions(await api.get<unknown>(`/insights/executions?${executionParams(filters)}`)),
    enabled,
  });
}

export function useInsightExecution(executionId: string | undefined) {
  return useQuery({
    queryKey: qk.insightExecution(executionId ?? ''),
    queryFn: async () => normalizeExecutionDetail(await api.get<unknown>(`/insights/executions/${encodeURIComponent(executionId ?? '')}?window=24h`)),
    enabled: Boolean(executionId),
  });
}

export function useInsightAgents() {
  return useQuery({
    queryKey: qk.insightAgents(),
    queryFn: async () => arrayOrEmpty(await api.get<unknown[]>('/insights/v2/agents?window=24h')).map(normalizeV2Agent),
  });
}

export function useInsightAgent(agentRef: string | undefined) {
  return useQuery({
    queryKey: qk.insightAgent(agentRef ?? ''),
    queryFn: async () => normalizeV2Agent(await api.get<unknown>(`/insights/v2/agents/${encodeURIComponent(agentRef ?? '')}?window=24h`)),
    enabled: Boolean(agentRef),
  });
}

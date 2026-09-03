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
  recovery_finalized_executions?: number;
  failure_rate: number | null;
  slot_utilization: number | null;
  slot_coverage_ratio: number | null;
  queue_wait_ms: InsightPercentiles;
  execution_duration_ms: InsightPercentiles;
}

export interface InsightTrendPoint {
  bucket_start: string;
  completed_executions: number;
  failed_executions: number;
  recovery_finalized_executions: number;
  avg_duration_ms: number | null;
}

export interface InsightUsageTrendPoint {
  bucket_start: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  total_tokens: number;
  cost_micros: number;
}

export interface InsightUsageModelSummary {
  model: string;
  events: number;
  total_tokens: number;
  cost_micros: number;
}

export interface InsightUsageSummary {
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  total_tokens: number;
  cost_micros: number;
  events: number;
  trend: InsightUsageTrendPoint[];
  by_model: InsightUsageModelSummary[];
}

export interface InsightPlanScaleSummary {
  plan_id: string;
  plan_name: string;
  project_id: string;
  project_name: string | null;
  status: string;
  task_count: number;
  edge_count: number;
  generation_count: number;
  evolution_count: number;
  active_task_count: number;
  blocked_task_count: number;
  failed_task_count: number;
  done_task_count: number;
}

export interface InsightProjectLifecycleTrendPoint {
  bucket_start: string;
  issue_created: number;
  issue_done: number;
  issue_canceled: number;
  plan_created: number;
  plan_done: number;
  plan_failed: number;
  plan_canceled: number;
  task_created: number;
  task_done: number;
  task_failed: number;
  task_canceled: number;
}

export interface InsightDurationHistogramBucket {
  label: string;
  min_ms: number;
  max_ms: number | null;
  count: number;
}

export interface InsightProjectLifecycleSummary {
  project_id: string;
  project_name: string | null;
  trend: InsightProjectLifecycleTrendPoint[];
  task_duration_histogram: InsightDurationHistogramBucket[];
  plan_duration_histogram: InsightDurationHistogramBucket[];
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
  trend: InsightTrendPoint[];
  usage: InsightUsageSummary;
  plan_scale: InsightPlanScaleSummary[];
  project_lifecycle: InsightProjectLifecycleSummary[];
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

export type CollaborationRelation = 'assign' | 'reassign' | 'complete' | 'block' | 'unblock' | 'dependency_release' | 'review_accept' | 'review_reject';
export type CollaborationPolarity = 'positive' | 'negative' | 'neutral' | 'mixed';

export interface CollaborationNode { id: string; kind: 'agent' | 'task'; label: string; task_id?: string }
export interface CollaborationEdge { id: string; source: string; target: string; relation_type: CollaborationRelation; polarity: CollaborationPolarity; magnitude: 1 | 2 | 3; effect_id: string }
export interface CollaborationEffect extends CollaborationEdge {
  project_id: string; target_task_id: string; source_agent_ref: string; target_agent_ref: string;
  confidence: string; occurred_at: string; rule_version: string; evidence_event_ids: string[];
  before_state: Record<string, unknown>; after_state: Record<string, unknown>; explanation_key: string;
}
export interface CollaborationGraphResponse {
  graph: { nodes: CollaborationNode[]; edges: CollaborationEdge[] };
  effects: CollaborationEffect[];
  summary: { positive_count: number; negative_count: number; neutral_count: number; mixed_count: number; affected_task_count: number };
  next_cursor: string;
}
export interface CollaborationEvidenceEvent { event_id: string; event_type: string; occurred_at: string; actor_ref: string; refs: Record<string, string>; payload: Record<string, unknown> }
export interface CollaborationEvidenceResponse { effect_id: string; evidence: CollaborationEvidenceEvent[] }
export interface CollaborationFilters {
  project_id: string; task_id?: string; agent_ref?: string; relation_type?: CollaborationRelation; polarity?: CollaborationPolarity;
  since?: string; until?: string; cursor?: string; limit?: number;
}

export type InsightV2HealthStatus = 'healthy' | 'elevated' | 'degraded' | 'unknown';

export interface InsightV2Health {
  status: InsightV2HealthStatus;
  reason_codes: string[];
  evidence: Record<string, unknown>[];
}

export interface InsightV2Meta {
  metric_version: 'insight.metrics.v2';
  sample_count: number;
  coverage: number | null;
  freshness: InsightFreshness;
  unknown_count: number;
  known: boolean;
}

export interface InsightV2Envelope {
  metric_version: 'insight.metrics.v2';
  time_window: InsightWindow;
  as_of: string;
  health: InsightV2Health;
  meta: InsightV2Meta;
}

export interface InsightV2CountMetric {
  value: number | null;
  meta: InsightV2Meta;
}

export interface InsightV2ProjectSummary {
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

export type InsightV2Projects = InsightV2ProjectSummary[];

export interface InsightV2FunnelBreak {
  kind: string;
  count: InsightV2CountMetric;
  drilldown: Record<string, unknown>;
}

export interface InsightV2Funnel {
  issues: InsightV2CountMetric;
  tasks: InsightV2CountMetric;
  plans: InsightV2CountMetric;
  done: InsightV2CountMetric;
  breaks: InsightV2FunnelBreak[];
}

export interface InsightV2ProjectDelivery extends InsightV2Envelope {
  project_id: string;
  funnel: InsightV2Funnel;
}

export interface InsightV2ProjectEvolution extends InsightV2Envelope {
  project_id: string;
  evolution: {
    plans: number;
    evolved_plans: number;
    evolution_rate: number | null;
    generation_count: number;
    rework_count: number;
    rework_ratio: number | null;
    recovery_attempts: number;
    recovery_successes: number;
    recovery_effectiveness: number | null;
    max_loop_depth: number;
    stale_orphan_residue: number;
    anomaly_drilldowns: {
      rework: Record<string, unknown>;
      recovery: Record<string, unknown>;
      loop_depth: Record<string, unknown>;
      residue: Record<string, unknown>;
    };
    [key: string]: unknown;
  };
}

export interface InsightV2Generation {
  generation: number;
  created_at: string;
  triggered_by: string;
  reason: string;
  evidence: Record<string, unknown>[];
  node_changes: Record<string, unknown>[];
  recovery_duration_ms: number | null;
  recovery_outcome: string;
  delivery_branch: string;
  delivery_sha: string;
  acceptance_verdict: 'pass' | 'reject' | 'pending' | string;
}

export interface InsightV2PlanLineage extends InsightV2Envelope {
  project_id: string;
  plan_id: string;
  generations: InsightV2Generation[];
}

export interface InsightExecutionFilters {
  agent_ref?: string;
  project_id?: string;
  cursor?: string;
  limit?: number;
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

function recordArrayOrEmpty(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.filter(isRecord) : [];
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
    recovery_finalized_executions: numberOrZero(source.recovery_finalized_executions),
    failure_rate: numberOrNull(source.failure_rate),
    slot_utilization: numberOrNull(source.slot_utilization),
    slot_coverage_ratio: numberOrNull(source.slot_coverage_ratio),
    queue_wait_ms: normalizePercentiles(source.queue_wait_ms),
    execution_duration_ms: normalizePercentiles(source.execution_duration_ms),
  };
}

function normalizeTrendPoint(value: unknown): InsightTrendPoint {
  const source = isRecord(value) ? value : {};
  return {
    bucket_start: stringOrEmpty(source.bucket_start),
    completed_executions: numberOrZero(source.completed_executions),
    failed_executions: numberOrZero(source.failed_executions),
    recovery_finalized_executions: numberOrZero(source.recovery_finalized_executions),
    avg_duration_ms: numberOrNull(source.avg_duration_ms),
  };
}

function normalizeUsageTrendPoint(value: unknown): InsightUsageTrendPoint {
  const source = isRecord(value) ? value : {};
  return {
    bucket_start: stringOrEmpty(source.bucket_start),
    input_tokens: numberOrZero(source.input_tokens),
    output_tokens: numberOrZero(source.output_tokens),
    cache_read_tokens: numberOrZero(source.cache_read_tokens),
    cache_write_tokens: numberOrZero(source.cache_write_tokens),
    total_tokens: numberOrZero(source.total_tokens),
    cost_micros: numberOrZero(source.cost_micros),
  };
}

function normalizeUsageModel(value: unknown): InsightUsageModelSummary {
  const source = isRecord(value) ? value : {};
  return {
    model: stringOrEmpty(source.model) || 'unknown',
    events: numberOrZero(source.events),
    total_tokens: numberOrZero(source.total_tokens),
    cost_micros: numberOrZero(source.cost_micros),
  };
}

function normalizeUsage(value: unknown): InsightUsageSummary {
  const source = isRecord(value) ? value : {};
  return {
    input_tokens: numberOrZero(source.input_tokens),
    output_tokens: numberOrZero(source.output_tokens),
    cache_read_tokens: numberOrZero(source.cache_read_tokens),
    cache_write_tokens: numberOrZero(source.cache_write_tokens),
    total_tokens: numberOrZero(source.total_tokens),
    cost_micros: numberOrZero(source.cost_micros),
    events: numberOrZero(source.events),
    trend: arrayOrEmpty(source.trend as InsightUsageTrendPoint[] | null | undefined).map(normalizeUsageTrendPoint),
    by_model: arrayOrEmpty(source.by_model as InsightUsageModelSummary[] | null | undefined).map(normalizeUsageModel),
  };
}

function normalizePlanScale(value: unknown): InsightPlanScaleSummary {
  const source = isRecord(value) ? value : {};
  const planID = stringOrEmpty(source.plan_id) || 'unknown';
  return {
    plan_id: planID,
    plan_name: stringOrEmpty(source.plan_name) || planID,
    project_id: stringOrEmpty(source.project_id) || 'unknown',
    project_name: stringOrNull(source.project_name),
    status: stringOrEmpty(source.status) || 'unknown',
    task_count: numberOrZero(source.task_count),
    edge_count: numberOrZero(source.edge_count),
    generation_count: numberOrZero(source.generation_count),
    evolution_count: numberOrZero(source.evolution_count),
    active_task_count: numberOrZero(source.active_task_count),
    blocked_task_count: numberOrZero(source.blocked_task_count),
    failed_task_count: numberOrZero(source.failed_task_count),
    done_task_count: numberOrZero(source.done_task_count),
  };
}

function normalizeProjectLifecycleTrendPoint(value: unknown): InsightProjectLifecycleTrendPoint {
  const source = isRecord(value) ? value : {};
  return {
    bucket_start: stringOrEmpty(source.bucket_start),
    issue_created: numberOrZero(source.issue_created),
    issue_done: numberOrZero(source.issue_done),
    issue_canceled: numberOrZero(source.issue_canceled),
    plan_created: numberOrZero(source.plan_created),
    plan_done: numberOrZero(source.plan_done),
    plan_failed: numberOrZero(source.plan_failed),
    plan_canceled: numberOrZero(source.plan_canceled),
    task_created: numberOrZero(source.task_created),
    task_done: numberOrZero(source.task_done),
    task_failed: numberOrZero(source.task_failed),
    task_canceled: numberOrZero(source.task_canceled),
  };
}

function normalizeDurationHistogramBucket(value: unknown): InsightDurationHistogramBucket {
  const source = isRecord(value) ? value : {};
  return {
    label: stringOrEmpty(source.label),
    min_ms: numberOrZero(source.min_ms),
    max_ms: numberOrNull(source.max_ms),
    count: numberOrZero(source.count),
  };
}

function normalizeProjectLifecycle(value: unknown): InsightProjectLifecycleSummary {
  const source = isRecord(value) ? value : {};
  const projectId = stringOrEmpty(source.project_id) || 'unknown';
  return {
    project_id: projectId,
    project_name: stringOrNull(source.project_name),
    trend: arrayOrEmpty(source.trend as InsightProjectLifecycleTrendPoint[] | null | undefined).map(normalizeProjectLifecycleTrendPoint),
    task_duration_histogram: arrayOrEmpty(source.task_duration_histogram as InsightDurationHistogramBucket[] | null | undefined).map(normalizeDurationHistogramBucket),
    plan_duration_histogram: arrayOrEmpty(source.plan_duration_histogram as InsightDurationHistogramBucket[] | null | undefined).map(normalizeDurationHistogramBucket),
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

function normalizeV2Health(value: unknown): InsightV2Health {
  const source = isRecord(value) ? value : {};
  const status = source.status;
  return {
    status: status === 'healthy' || status === 'elevated' || status === 'degraded' || status === 'unknown'
      ? status
      : 'unknown',
    reason_codes: Array.isArray(source.reason_codes) ? source.reason_codes.filter((v): v is string => typeof v === 'string') : [],
    evidence: recordArrayOrEmpty(source.evidence),
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

function normalizeV2Envelope(value: unknown): InsightV2Envelope {
  const source = isRecord(value) ? value : {};
  return {
    metric_version: 'insight.metrics.v2',
    time_window: normalizeWindow(source.time_window),
    as_of: stringOrEmpty(source.as_of),
    health: normalizeV2Health(source.health),
    meta: normalizeV2Meta(source.meta),
  };
}

function normalizeV2CountMetric(value: unknown): InsightV2CountMetric {
  const source = isRecord(value) ? value : {};
  return {
    value: numberOrNull(source.value),
    meta: normalizeV2Meta(source.meta),
  };
}

function normalizeV2ProjectSummary(value: unknown): InsightV2ProjectSummary {
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

function normalizeV2Projects(value: unknown): InsightV2Projects {
  return Array.isArray(value) ? value.map(normalizeV2ProjectSummary) : [];
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

function normalizeV2FunnelBreak(value: unknown): InsightV2FunnelBreak {
  const source = isRecord(value) ? value : {};
  return {
    kind: stringOrEmpty(source.kind) || 'unknown',
    count: normalizeV2CountMetric(source.count),
    drilldown: isRecord(source.drilldown) ? source.drilldown : {},
  };
}

function normalizeV2Funnel(value: unknown): InsightV2Funnel {
  const source = isRecord(value) ? value : {};
  return {
    issues: normalizeV2CountMetric(source.issues),
    tasks: normalizeV2CountMetric(source.tasks),
    plans: normalizeV2CountMetric(source.plans),
    done: normalizeV2CountMetric(source.done),
    breaks: arrayOrEmpty(source.breaks as InsightV2FunnelBreak[] | null | undefined).map(normalizeV2FunnelBreak),
  };
}

function normalizeV2Delivery(value: unknown): InsightV2ProjectDelivery {
  const source = isRecord(value) ? value : {};
  return {
    ...normalizeV2Envelope(source),
    project_id: stringOrEmpty(source.project_id),
    funnel: normalizeV2Funnel(source.funnel),
  };
}

function normalizeV2Evolution(value: unknown): InsightV2ProjectEvolution {
  const source = isRecord(value) ? value : {};
  const evolution = isRecord(source.evolution) ? source.evolution : {};
  const drilldowns = isRecord(evolution.anomaly_drilldowns) ? evolution.anomaly_drilldowns : {};
  return {
    ...normalizeV2Envelope(source),
    project_id: stringOrEmpty(source.project_id),
    evolution: {
      ...evolution,
      plans: numberOrZero(evolution.plans),
      evolved_plans: numberOrZero(evolution.evolved_plans),
      evolution_rate: numberOrNull(evolution.evolution_rate),
      generation_count: numberOrZero(evolution.generation_count),
      rework_count: numberOrZero(evolution.rework_count),
      rework_ratio: numberOrNull(evolution.rework_ratio),
      recovery_attempts: numberOrZero(evolution.recovery_attempts),
      recovery_successes: numberOrZero(evolution.recovery_successes),
      recovery_effectiveness: numberOrNull(evolution.recovery_effectiveness),
      max_loop_depth: numberOrZero(evolution.max_loop_depth),
      stale_orphan_residue: numberOrZero(evolution.stale_orphan_residue),
      anomaly_drilldowns: {
        rework: isRecord(drilldowns.rework) ? drilldowns.rework : {},
        recovery: isRecord(drilldowns.recovery) ? drilldowns.recovery : {},
        loop_depth: isRecord(drilldowns.loop_depth) ? drilldowns.loop_depth : {},
        residue: isRecord(drilldowns.residue) ? drilldowns.residue : {},
      },
    },
  };
}

function normalizeV2Generation(value: unknown): InsightV2Generation {
  const source = isRecord(value) ? value : {};
  return {
    generation: numberOrZero(source.generation),
    created_at: stringOrEmpty(source.created_at),
    triggered_by: stringOrEmpty(source.triggered_by),
    reason: stringOrEmpty(source.reason) || 'unknown',
    evidence: recordArrayOrEmpty(source.evidence),
    node_changes: recordArrayOrEmpty(source.node_changes),
    recovery_duration_ms: numberOrNull(source.recovery_duration_ms),
    recovery_outcome: stringOrEmpty(source.recovery_outcome) || 'unknown',
    delivery_branch: stringOrEmpty(source.delivery_branch),
    delivery_sha: stringOrEmpty(source.delivery_sha),
    acceptance_verdict: stringOrEmpty(source.acceptance_verdict) || 'pending',
  };
}

function normalizeV2Lineage(value: unknown): InsightV2PlanLineage {
  const source = isRecord(value) ? value : {};
  return {
    ...normalizeV2Envelope(source),
    project_id: stringOrEmpty(source.project_id),
    plan_id: stringOrEmpty(source.plan_id),
    generations: arrayOrEmpty(source.generations as InsightV2Generation[] | null | undefined).map(normalizeV2Generation),
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

function normalizeCollaborationNode(value: unknown): CollaborationNode {
  const source = isRecord(value) ? value : {};
  const kind = source.kind === 'agent' || source.kind === 'task' ? source.kind : 'task';
  const id = stringOrEmpty(source.id) || (kind === 'agent' ? 'agent:unknown' : 'task:unknown');
  return {
    id,
    kind,
    label: stringOrEmpty(source.label) || id,
    task_id: stringOrEmpty(source.task_id) || undefined,
  };
}

function normalizeCollaborationRelation(value: unknown): CollaborationRelation {
  return RELATION_VALUES.includes(value as CollaborationRelation) ? value as CollaborationRelation : 'assign';
}

function normalizeCollaborationPolarity(value: unknown): CollaborationPolarity {
  return POLARITY_VALUES.includes(value as CollaborationPolarity) ? value as CollaborationPolarity : 'neutral';
}

const RELATION_VALUES: CollaborationRelation[] = ['assign', 'reassign', 'complete', 'block', 'unblock', 'dependency_release', 'review_accept', 'review_reject'];
const POLARITY_VALUES: CollaborationPolarity[] = ['positive', 'negative', 'neutral', 'mixed'];

function normalizeMagnitude(value: unknown): 1 | 2 | 3 {
  return value === 2 || value === 3 ? value : 1;
}

function normalizeCollaborationEdge(value: unknown): CollaborationEdge {
  const source = isRecord(value) ? value : {};
  const id = stringOrEmpty(source.id) || stringOrEmpty(source.effect_id) || 'ce_unknown';
  return {
    id,
    source: stringOrEmpty(source.source) || stringOrEmpty(source.source_agent_ref) || 'agent:unknown',
    target: stringOrEmpty(source.target) || `task:${stringOrEmpty(source.target_task_id) || 'unknown'}`,
    relation_type: normalizeCollaborationRelation(source.relation_type),
    polarity: normalizeCollaborationPolarity(source.polarity),
    magnitude: normalizeMagnitude(source.magnitude),
    effect_id: stringOrEmpty(source.effect_id) || id,
  };
}

function normalizeCollaborationEffect(value: unknown): CollaborationEffect {
  const source = isRecord(value) ? value : {};
  const edge = normalizeCollaborationEdge(source);
  return {
    ...edge,
    project_id: stringOrEmpty(source.project_id),
    target_task_id: stringOrEmpty(source.target_task_id),
    source_agent_ref: stringOrEmpty(source.source_agent_ref),
    target_agent_ref: stringOrEmpty(source.target_agent_ref),
    confidence: stringOrEmpty(source.confidence) || 'unknown',
    occurred_at: stringOrEmpty(source.occurred_at),
    rule_version: stringOrEmpty(source.rule_version),
    evidence_event_ids: Array.isArray(source.evidence_event_ids) ? source.evidence_event_ids.filter((item): item is string => typeof item === 'string') : [],
    before_state: isRecord(source.before_state) ? source.before_state : {},
    after_state: isRecord(source.after_state) ? source.after_state : {},
    explanation_key: stringOrEmpty(source.explanation_key),
  };
}

function normalizeCollaborationGraphResponse(value: unknown): CollaborationGraphResponse {
  const source = isRecord(value) ? value : {};
  const graph = isRecord(source.graph) ? source.graph : {};
  const effects = arrayOrEmpty(source.effects as unknown[] | null | undefined).map(normalizeCollaborationEffect);
  const summary = isRecord(source.summary) ? source.summary : {};
  return {
    graph: {
      nodes: arrayOrEmpty(graph.nodes as unknown[] | null | undefined).map(normalizeCollaborationNode),
      edges: arrayOrEmpty(graph.edges as unknown[] | null | undefined).map(normalizeCollaborationEdge),
    },
    effects,
    summary: {
      positive_count: numberOrZero(summary.positive_count),
      negative_count: numberOrZero(summary.negative_count),
      neutral_count: numberOrZero(summary.neutral_count),
      mixed_count: numberOrZero(summary.mixed_count),
      affected_task_count: numberOrZero(summary.affected_task_count),
    },
    next_cursor: stringOrEmpty(source.next_cursor),
  };
}

function normalizeCollaborationEvidenceEvent(value: unknown): CollaborationEvidenceEvent {
  const source = isRecord(value) ? value : {};
  return {
    event_id: stringOrEmpty(source.event_id) || 'event:unknown',
    event_type: stringOrEmpty(source.event_type),
    occurred_at: stringOrEmpty(source.occurred_at),
    actor_ref: stringOrEmpty(source.actor_ref),
    refs: isRecord(source.refs) ? Object.fromEntries(Object.entries(source.refs).filter((entry): entry is [string, string] => typeof entry[1] === 'string')) : {},
    payload: isRecord(source.payload) ? source.payload : {},
  };
}

function normalizeCollaborationEvidenceResponse(value: unknown): CollaborationEvidenceResponse {
  const source = isRecord(value) ? value : {};
  return {
    effect_id: stringOrEmpty(source.effect_id),
    evidence: arrayOrEmpty(source.evidence as unknown[] | null | undefined).map(normalizeCollaborationEvidenceEvent),
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
    trend: arrayOrEmpty(source.trend as InsightTrendPoint[] | null | undefined).map(normalizeTrendPoint),
    usage: normalizeUsage(source.usage),
    plan_scale: arrayOrEmpty(source.plan_scale as InsightPlanScaleSummary[] | null | undefined).map(normalizePlanScale),
    project_lifecycle: arrayOrEmpty(source.project_lifecycle as InsightProjectLifecycleSummary[] | null | undefined).map(normalizeProjectLifecycle),
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

export function useInsightV2Projects() {
  return useQuery({
    queryKey: qk.insightV2Projects(),
    queryFn: async () => normalizeV2Projects(await api.get<unknown>('/insights/v2/projects?window=24h')),
  });
}

export function useInsightV2Project(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.insightV2Project(projectId ?? ''),
    queryFn: async () => normalizeV2ProjectSummary(await api.get<unknown>(`/insights/v2/projects/${encodeURIComponent(projectId ?? '')}?window=24h`)),
    enabled: Boolean(projectId),
  });
}

export function useInsightV2ProjectDelivery(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.insightV2ProjectDelivery(projectId ?? ''),
    queryFn: async () => normalizeV2Delivery(await api.get<unknown>(`/insights/v2/projects/${encodeURIComponent(projectId ?? '')}/delivery?window=24h`)),
    enabled: Boolean(projectId),
  });
}

export function useInsightV2ProjectLifecycle(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.insightV2ProjectLifecycle(projectId ?? ''),
    queryFn: async () => normalizeProjectLifecycle(await api.get<unknown>(`/insights/v2/projects/${encodeURIComponent(projectId ?? '')}/lifecycle?window=24h`)),
    enabled: Boolean(projectId),
  });
}

export function useInsightV2ProjectEvolution(projectId: string | undefined) {
  return useQuery({
    queryKey: qk.insightV2ProjectEvolution(projectId ?? ''),
    queryFn: async () => normalizeV2Evolution(await api.get<unknown>(`/insights/v2/projects/${encodeURIComponent(projectId ?? '')}/evolution?window=24h`)),
    enabled: Boolean(projectId),
  });
}

export function useInsightV2PlanLineage(projectId: string | undefined, planId: string | undefined) {
  return useQuery({
    queryKey: qk.insightV2PlanLineage(projectId ?? '', planId ?? ''),
    queryFn: async () => normalizeV2Lineage(await api.get<unknown>(`/insights/v2/projects/${encodeURIComponent(projectId ?? '')}/plans/${encodeURIComponent(planId ?? '')}/lineage?window=24h`)),
    enabled: Boolean(projectId && planId),
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

function collaborationParams(filters: CollaborationFilters): string {
  const params = new URLSearchParams({ project_id: filters.project_id, limit: String(Math.min(filters.limit ?? 100, 500)) });
  for (const key of ['task_id', 'agent_ref', 'relation_type', 'polarity', 'since', 'until', 'cursor'] as const) {
    const value = filters[key];
    if (value) params.set(key, String(value));
  }
  return params.toString();
}

export function useCollaborationEffects(filters: CollaborationFilters, enabled = true) {
  return useQuery({
    queryKey: qk.collaborationEffects(filters),
    queryFn: async () => normalizeCollaborationGraphResponse(await api.get<unknown>(`/insights/collaboration-effects?${collaborationParams(filters)}`)),
    enabled: enabled && Boolean(filters.project_id),
  });
}

export function useCollaborationEvidence(effectId: string | null) {
  return useQuery({
    queryKey: qk.collaborationEvidence(effectId ?? ''),
    queryFn: async () => normalizeCollaborationEvidenceResponse(await api.get<unknown>(`/insights/collaboration-effects/${encodeURIComponent(effectId ?? '')}/evidence`)),
    enabled: Boolean(effectId),
  });
}

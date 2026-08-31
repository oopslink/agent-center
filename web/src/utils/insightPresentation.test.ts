import { describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import type { InsightExecutionRow } from '@/api/insights';
import {
  classifyInsightCoverage,
  formatInsightDuration,
  formatInsightFailure,
  formatInsightPercentiles,
  formatInsightRatio,
  insightExecutionStatus,
  insightFailureMessage,
  insightQualityLabel,
  presentInsightEnum,
} from './insightPresentation';

const t = i18n.getFixedT('en', 'insights');

const baseExecution: InsightExecutionRow = {
  execution_id: 'exec-1',
  command_id: 'cmd-1',
  task_id: 'task-1',
  task_ref: 'task-1',
  task_title: 'Task',
  agent_ref: 'agent:a',
  agent_name: 'Agent',
  project_id: 'proj-1',
  project_name: 'Project',
  worker_id: 'worker-1',
  outcome: null,
  failure_reason: null,
  failure_message: null,
  command_status: null,
  status_reason: null,
  status_message: null,
  queued_at: '2026-08-27T00:00:00Z',
  started_at: null,
  finished_at: null,
  queue_wait_ms: null,
  duration_ms: null,
  recovered: false,
  quality: 'valid',
};

describe('insight presentation semantics', () => {
  it.each([
    { coverage: null, utilization: 0, kind: 'unknown', value: 'Cannot determine', sub: 'No computable capacity baseline', showUtilization: false },
    { coverage: 0, utilization: 0, kind: 'unknown', value: 'Cannot determine', sub: 'No valid slot observations in the past 24 hours', showUtilization: false },
    { coverage: 0.001, utilization: 0, kind: 'insufficient', value: 'Insufficient data', sub: 'Observation coverage 0.1%; utilization is hidden', showUtilization: false },
    { coverage: 0.499, utilization: 0.4, kind: 'insufficient', value: 'Insufficient data', sub: 'Observation coverage 49.9%; utilization is hidden', showUtilization: false },
    { coverage: 0.5, utilization: 0, kind: 'partial', value: '0% (partial observation)', sub: 'Observation coverage 50%; may not represent the full 24 hours', showUtilization: true },
    { coverage: 0.899, utilization: 0.4, kind: 'partial', value: '40% (partial observation)', sub: 'Observation coverage 89.9%; may not represent the full 24 hours', showUtilization: true },
    { coverage: 0.9, utilization: 0, kind: 'representative', value: '0%', sub: 'Observation coverage 90%', showUtilization: true },
    { coverage: 0.0009, utilization: 0, kind: 'insufficient', value: 'Insufficient data', sub: 'Observation coverage <0.1%; utilization is hidden', showUtilization: false },
    { coverage: 0.9, utilization: null, kind: 'unknown', value: 'Cannot determine', sub: 'No valid available slot duration', showUtilization: false },
  ])('classifies coverage boundary %#', ({ coverage, utilization, kind, value, sub, showUtilization }) => {
    const got = classifyInsightCoverage(coverage, utilization, t);
    expect(got).toMatchObject({ kind, value, sub, showUtilization });
  });

  it.each([
    { value: null, want: '—' },
    { value: -1, want: 'Invalid time data' },
    { value: 250, want: '250 ms' },
    { value: 1200, want: '1.2 s' },
    { value: 484000, want: '8 min 04 s' },
    { value: 7380000, want: '2 h 03 min' },
    { value: 100800000, want: '1 d 4 h' },
  ])('formats durations %#', ({ value, want }) => {
    expect(formatInsightDuration(value, t)).toBe(want);
  });

  it('distinguishes zero from null for rates and percentiles', () => {
    expect(formatInsightRatio(0)).toBe('0%');
    expect(formatInsightRatio(null)).toBe('—');
    expect(formatInsightFailure({
      completed_executions: 1,
      failed_executions: 0,
      failure_rate: 0,
      slot_utilization: null,
      slot_coverage_ratio: null,
      queue_wait_ms: { p50: null, p95: null, samples: 0 },
      execution_duration_ms: { p50: null, p95: null, samples: 0 },
    }, t)).toBe('0/1 (0%)');
    expect(formatInsightFailure({
      completed_executions: 0,
      failed_executions: 0,
      failure_rate: null,
      slot_utilization: null,
      slot_coverage_ratio: null,
      queue_wait_ms: { p50: null, p95: null, samples: 0 },
      execution_duration_ms: { p50: null, p95: null, samples: 0 },
    }, t)).toBe('— (0 completed)');
    expect(formatInsightPercentiles({ p50: 0, p95: 0, samples: 1 }, t)).toBe('P50 0 ms / P95 0 ms · 1 samples');
    expect(formatInsightPercentiles({ p50: null, p95: null, samples: 0 }, t)).toBe('— (0 valid samples)');
  });

  it.each([
    { row: { outcome: 'succeeded', finished_at: '2026-08-27T00:01:00Z' }, label: 'Completed', tone: 'success' },
    { row: { outcome: 'failed', finished_at: '2026-08-27T00:01:00Z' }, label: 'Failed', tone: 'danger' },
    { row: { outcome: 'crashed', finished_at: '2026-08-27T00:01:00Z' }, label: 'Interrupted', tone: 'danger' },
    { row: { outcome: 'quiet_finalized', finished_at: '2026-08-27T00:01:00Z' }, label: 'Ended during recovery', tone: 'danger' },
    { row: { outcome: 'new_state', finished_at: '2026-08-27T00:01:00Z' }, label: 'Outcome unavailable', tone: 'warn' },
    { row: { started_at: '2026-08-27T00:00:01Z' }, label: 'Running', tone: 'info' },
    { row: { command_status: 'rejected' }, label: 'Did not start', tone: 'danger' },
    { row: { command_status: 'failed' }, label: 'Did not start', tone: 'danger' },
    { row: { command_status: 'expired' }, label: 'Did not start', tone: 'warn' },
    { row: { queued_at: '2026-08-27T00:00:00Z' }, label: 'Waiting to start', tone: 'neutral' },
    { row: { queued_at: null }, label: 'Status unavailable', tone: 'warn' },
  ])('maps execution status %#', ({ row, label, tone }) => {
    expect(insightExecutionStatus({ ...baseExecution, ...row }, t)).toMatchObject({ label, tone });
  });

  it.each([
    { quality: 'valid', label: null },
    { quality: 'invalid_time_order', label: 'Invalid time data' },
    { quality: 'future_quality', label: 'Data needs review' },
  ])('maps quality %#', ({ quality, label }) => {
    expect(insightQualityLabel(quality, t)).toBe(label);
  });

  it.each([
    { reason: 'nonzero_exit', want: 'The execution process returned an error.' },
    { reason: 'output_failure', want: 'The executor reported failure.' },
    { reason: 'status_failed', want: 'The executor reported failure.' },
    { reason: 'process_gone', want: 'The execution process exited unexpectedly.' },
    { reason: 'clean_exit_no_output', want: 'Execution ended without a valid result.' },
    { reason: 'done_no_output', want: 'Execution ended without a valid result.' },
    { reason: 'stalled', want: 'Execution stopped after a long period without progress.' },
    { reason: 'non_delivery', want: 'The execution result was not delivered successfully.' },
    { reason: 'evidence_persistence', want: 'Execution evidence could not be saved.' },
    { reason: 'repo_source_unavailable', want: 'The repository source was unavailable.' },
    { reason: 'no_backfill_guard', want: 'Recovery could not safely confirm the earlier execution result.' },
    { reason: 'unknown_future_reason', want: 'The execution was not successful.' },
  ])('maps failure reasons %#', ({ reason, want }) => {
    expect(insightFailureMessage({ ...baseExecution, failure_reason: reason }, t)).toBe(want);
  });

  it('prefers human messages over reason fallback', () => {
    expect(insightFailureMessage({ ...baseExecution, failure_reason: 'nonzero_exit', failure_message: 'Runtime said no.' }, t)).toBe('Runtime said no.');
    expect(insightFailureMessage({ ...baseExecution, command_status: 'rejected', status_reason: 'repo_source_unavailable', status_message: 'No worker matched.' }, t)).toBe('No worker matched.');
  });

  it.each([
    ['health', 'unknown_status', 'Unknown'],
    ['freshness', 'raw_future_enum', 'Freshness unavailable'],
    ['lineageReason', 'arbitrary_future_token', 'Unknown'],
    ['recoveryOutcome', 'future_outcome', 'Unknown'],
    ['verdict', 'future_outcome', 'Unknown value'],
    ['reasonCode', 'raw_future_enum', 'Backend-defined reason'],
    ['breakKind', 'raw_future_enum', 'Backend-defined break'],
    ['anomaly', 'raw_future_enum', 'Backend-defined anomaly'],
  ] as const)('hides unknown %s enum tokens', (kind, raw, want) => {
    const rendered = presentInsightEnum(kind, raw, t);
    expect(rendered).toBe(want);
    expect(rendered).not.toContain(raw);
  });
});

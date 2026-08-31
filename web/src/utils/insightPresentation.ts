import type { TFunction } from 'i18next';
import type { InsightExecutionRow, InsightFreshness, InsightPercentiles, InsightSummary } from '@/api/insights';

export const INSIGHT_EMPTY = '—';

export type InsightCoverageKind = 'unknown' | 'insufficient' | 'partial' | 'representative';
export type InsightTone = 'normal' | 'unknown' | 'warn' | 'danger' | 'success' | 'info' | 'neutral';

export interface InsightCoveragePresentation {
  kind: InsightCoverageKind;
  value: string;
  sub: string;
  tone: 'normal' | 'unknown' | 'warn';
  showUtilization: boolean;
}

export function classifyInsightCoverage(
  coverage: number | null,
  utilization: number | null,
  t: TFunction<'insights'>,
): InsightCoveragePresentation {
  const coverageLabel = formatInsightCoverage(coverage);
  if (coverage === null) {
    return { kind: 'unknown', value: t('insight.coverage.unknown'), sub: t('insight.coverage.noBaseline'), tone: 'unknown', showUtilization: false };
  }
  if (coverage === 0) {
    return { kind: 'unknown', value: t('insight.coverage.unknown'), sub: t('insight.coverage.noObservation'), tone: 'unknown', showUtilization: false };
  }
  if (utilization === null) {
    return { kind: 'unknown', value: t('insight.coverage.unknown'), sub: t('insight.coverage.noAvailable'), tone: 'unknown', showUtilization: false };
  }
  if (coverage < 0.5) {
    return { kind: 'insufficient', value: t('insight.coverage.insufficient'), sub: t('insight.coverage.insufficientSub', { coverage: coverageLabel }), tone: 'warn', showUtilization: false };
  }
  if (coverage < 0.9) {
    return { kind: 'partial', value: `${formatInsightRatio(utilization)} ${t('insight.coverage.partialSuffix')}`, sub: t('insight.coverage.partialSub', { coverage: coverageLabel }), tone: 'warn', showUtilization: true };
  }
  return { kind: 'representative', value: formatInsightRatio(utilization), sub: t('insight.coverage.representativeSub', { coverage: coverageLabel }), tone: 'normal', showUtilization: true };
}

export function formatInsightRatio(value: number | null): string {
  if (value === null) return INSIGHT_EMPTY;
  return `${Math.round(value * 1000) / 10}%`;
}

export function formatInsightCoverage(value: number | null): string {
  if (value === null) return INSIGHT_EMPTY;
  if (value > 0 && value < 0.001) return '<0.1%';
  return `${Math.round(value * 1000) / 10}%`;
}

export function formatInsightDuration(value: number | null, t: TFunction<'insights'>): string {
  if (value === null) return INSIGHT_EMPTY;
  if (value < 0) return t('insight.duration.invalid');
  if (value < 1000) return t('insight.duration.ms', { count: Math.round(value) });
  const seconds = value / 1000;
  if (seconds < 60) {
    const rounded = `${Math.round(seconds * 10) / 10}`.replace(/\.0$/, '');
    return t('insight.duration.seconds', { value: rounded });
  }
  const minutes = Math.floor(seconds / 60);
  const wholeSeconds = Math.round(seconds % 60);
  if (minutes < 60) {
    if (wholeSeconds === 0) return t('insight.duration.minutesOnly', { minutes });
    return t('insight.duration.minutesSeconds', { minutes, seconds: String(wholeSeconds).padStart(2, '0') });
  }
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  if (hours < 24) return t('insight.duration.hoursMinutes', { hours, minutes: String(remMinutes).padStart(2, '0') });
  const days = Math.floor(hours / 24);
  return t('insight.duration.daysHours', { days, hours: hours % 24 });
}

export function formatInsightWindowTime(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  const nowYear = new Date().getFullYear();
  const options: Intl.DateTimeFormatOptions = d.getFullYear() === nowYear
    ? { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }
    : { year: 'numeric', month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' };
  return new Intl.DateTimeFormat(undefined, options).format(d);
}

export function formatInsightClock(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(d);
}

export function formatInsightTimeOrLabel(value: string | null, label: string): string {
  return value ? formatInsightClock(value) : label;
}

export function localInsightUTCOffset(): string {
  const minutes = -new Date().getTimezoneOffset();
  const sign = minutes >= 0 ? '+' : '-';
  const abs = Math.abs(minutes);
  return `UTC${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`;
}

export function insightFreshnessTone(freshness: InsightFreshness): 'success' | 'warn' | 'danger' | 'unknown' {
  if (freshness.state === 'fresh') return 'success';
  if (freshness.state === 'stale') return 'warn';
  if (freshness.state === 'rebuilding' || freshness.state === 'unavailable') return 'danger';
  return 'unknown';
}

export function formatInsightFailure(summary: InsightSummary, t: TFunction<'insights'>): string {
  if (summary.completed_executions === 0 || summary.failure_rate === null) return t('insight.table.noCompletedFailure');
  return t('insight.table.failureValue', {
    failed: summary.failed_executions,
    completed: summary.completed_executions,
    rate: formatInsightRatio(summary.failure_rate),
  });
}

export function formatInsightPercentiles(data: InsightPercentiles, t: TFunction<'insights'>): string {
  if (data.samples === 0) return t('insight.table.noPercentileSamples');
  return t('insight.table.percentileValue', {
    p50: formatInsightDuration(data.p50, t),
    p95: formatInsightDuration(data.p95, t),
    samples: data.samples,
  });
}

export interface InsightExecutionStatusPresentation {
  label: string;
  tone: 'success' | 'danger' | 'warn' | 'info' | 'neutral';
  failureRateTerminal: boolean;
}

export function insightExecutionStatus(row: InsightExecutionRow, t: TFunction<'insights'>): InsightExecutionStatusPresentation {
  if (row.outcome === 'succeeded') return { label: t('insight.status.completed'), tone: 'success', failureRateTerminal: false };
  if (row.outcome === 'failed') return { label: t('insight.status.failed'), tone: 'danger', failureRateTerminal: true };
  if (row.outcome === 'crashed') return { label: t('insight.status.interrupted'), tone: 'danger', failureRateTerminal: true };
  if (row.outcome === 'quiet_finalized') return { label: t('insight.status.quietFinalized'), tone: 'danger', failureRateTerminal: true };
  if (row.finished_at) return { label: t('insight.status.outcomeUnknown'), tone: 'warn', failureRateTerminal: false };
  if (row.started_at) return { label: t('insight.status.running'), tone: 'info', failureRateTerminal: false };
  if (row.command_status === 'rejected' || row.command_status === 'failed' || row.command_status === 'expired') {
    return { label: t('insight.status.didNotStart'), tone: row.command_status === 'expired' ? 'warn' : 'danger', failureRateTerminal: false };
  }
  if (row.queued_at) return { label: t('insight.status.waiting'), tone: 'neutral', failureRateTerminal: false };
  return { label: t('insight.status.unknown'), tone: 'warn', failureRateTerminal: false };
}

export function insightQualityLabel(quality: string, t: TFunction<'insights'>): string | null {
  if (quality === 'valid') return null;
  if (quality === 'invalid_time_order') return t('insight.quality.invalidTime');
  return t('insight.quality.check');
}

export function insightFailureMessage(row: InsightExecutionRow, t: TFunction<'insights'>): string | null {
  if (row.failure_message) return row.failure_message;
  if (row.status_message && !row.started_at) return row.status_message;
  const reason = row.failure_reason || row.status_reason;
  if (!reason) return null;
  const key = insightReasonMap[reason] ?? 'generic';
  return t(`insight.reason.${key}`);
}

const insightReasonMap: Record<string, string> = {
  nonzero_exit: 'nonzeroExit',
  output_failure: 'outputFailure',
  status_failed: 'outputFailure',
  process_gone: 'processGone',
  clean_exit_no_output: 'cleanExitNoOutput',
  done_no_output: 'cleanExitNoOutput',
  stalled: 'stalled',
  non_delivery: 'nonDelivery',
  evidence_persistence: 'evidencePersistence',
  repo_source_unavailable: 'repoSourceUnavailable',
  no_backfill_guard: 'noBackfillGuard',
};

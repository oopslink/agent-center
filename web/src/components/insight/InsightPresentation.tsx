import type React from 'react';
import { useTranslation } from 'react-i18next';
import type { InsightExecutionRow, InsightFreshness } from '@/api/insights';
import {
  formatInsightDuration,
  insightExecutionStatus,
  insightFreshnessTone,
  insightQualityLabel,
  presentInsightEnum,
  type InsightTone,
} from '@/utils/insightPresentation';

export function InsightStatePanel({
  testId,
  title,
  body,
  tone = 'neutral',
}: {
  testId: string;
  title: string;
  body?: string;
  tone?: Extract<InsightTone, 'neutral' | 'unknown' | 'warn' | 'danger'>;
}): React.ReactElement {
  const cls = tone === 'danger'
    ? 'border-danger/30 bg-danger/5 text-danger'
    : tone === 'warn'
      ? 'border-warning/30 bg-warning/5 text-warning'
      : 'border-border-base bg-bg-elevated text-text-secondary';
  return (
    <div className={`rounded border p-3 text-sm ${cls}`} data-testid={testId}>
      <div className="font-medium">{title}</div>
      {body && <p className="mt-1 text-xs">{body}</p>}
    </div>
  );
}

export function InsightFreshnessBadge({ freshness }: { freshness: InsightFreshness }): React.ReactElement {
  const { t } = useTranslation('insights');
  const tone = insightFreshnessTone(freshness);
  const cls = tone === 'success'
    ? 'border-success/30 bg-success/10 text-success'
    : tone === 'warn'
      ? 'border-warning/30 bg-warning/10 text-warning'
      : tone === 'danger'
        ? 'border-danger/30 bg-danger/10 text-danger'
        : 'border-text-muted/30 bg-bg-subtle text-text-muted';
  return (
    <span
      className={`rounded-full border px-2 py-0.5 font-medium ${cls}`}
      title={`${formatInsightDuration(freshness.age_ms, t)} / ${formatInsightDuration(freshness.threshold_ms, t)}`}
      data-testid="insight-freshness"
    >
      {presentInsightEnum('freshness', freshness.state, t)}
    </span>
  );
}

export function InsightExecutionStatusBadge({ row }: { row: InsightExecutionRow }): React.ReactElement {
  const { t } = useTranslation('insights');
  const status = insightExecutionStatus(row, t);
  const cls = status.tone === 'success'
    ? 'border-success/30 bg-success/10 text-success'
    : status.tone === 'danger'
      ? 'border-danger/30 bg-danger/10 text-danger'
      : status.tone === 'warn'
        ? 'border-warning/30 bg-warning/10 text-warning'
        : status.tone === 'info'
          ? 'border-brand/30 bg-brand/10 text-brand'
          : 'border-border-base bg-bg-subtle text-text-secondary';
  return <span className={`rounded-full border px-2 py-0.5 text-xs font-medium ${cls}`}>{status.label}</span>;
}

export function InsightQualityBadge({ quality }: { quality: string }): React.ReactElement | null {
  const { t } = useTranslation('insights');
  const label = insightQualityLabel(quality, t);
  if (!label) return null;
  return <span className="rounded-full border border-warning/30 bg-warning/10 px-2 py-0.5 text-xs font-medium text-warning">{label}</span>;
}

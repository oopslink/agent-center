import type React from 'react';
import { useTranslation } from 'react-i18next';
import type { CardData } from '@/utils/analyticsWindows';
import { formatTokens, formatCostMicros, formatDelta } from '@/utils/format';

// I28/F6 overview cards (1:1 with docs/design/v2.15.0/mockups/i28-analytics-en.png).
// Five fixed THIS-MONTH cards derived entirely from the per-day heatmap series
// (see deriveCards). TOKENS/COST carry a percent delta vs the prior 30 days;
// TASKS DONE an absolute count delta; ACTIVE DAYS and CURRENT STREAK show
// sub-text instead of a delta chip.
//
// NOTE (spec-vs-mockup, surfaced for Review): the Build Spec text says
// "今日/周/月" but the approved English mockup fixes the cards to THIS MONTH with
// no range selector — we follow the mockup. A multi-window selector can be a
// later follow-up without reworking these cards.

// TriangleIcon is an inline up/down delta arrow — inline SVG per ux-standards §12
// (never an emoji/pictograph ▲▼ glyph).
function TriangleIcon({ up }: { up: boolean }): React.ReactElement {
  return (
    <svg viewBox="0 0 10 10" className="h-2.5 w-2.5 fill-current" aria-hidden="true">
      {up ? <path d="M5 1l4 7H1z" /> : <path d="M5 9L1 2h8z" />}
    </svg>
  );
}

// DeltaChip renders a signed delta with a directional arrow + color token. Up =
// success, down = danger (uniform with the mockup, which greens an increase
// regardless of metric). `text` is preformatted (e.g. "+12.4%" or "-3").
function DeltaChip({ value, text, testId }: { value: number; text: string; testId: string }): React.ReactElement {
  const up = value > 0;
  const flat = value === 0;
  const cls = flat ? 'text-text-muted' : up ? 'text-success' : 'text-danger';
  return (
    <span className={['inline-flex items-center gap-1 text-xs font-medium', cls].join(' ')} data-testid={testId}>
      {!flat && <TriangleIcon up={up} />}
      {text}
    </span>
  );
}

// Card is the shared shell: uppercase label, big value, and a fixed-height meta
// row so every card aligns even when one (TASKS DONE) omits its delta chip.
function Card({
  label,
  value,
  meta,
  testId,
}: {
  label: string;
  value: string;
  meta: React.ReactNode;
  testId: string;
}): React.ReactElement {
  return (
    <div
      className="flex min-h-[8rem] flex-col justify-center gap-1 rounded-lg border border-border-base bg-bg-elevated px-5 py-4"
      data-testid={testId}
    >
      <span className="text-xs uppercase tracking-wide text-text-muted">{label}</span>
      <span className="text-3xl font-semibold leading-none tabular-nums text-text-primary" data-testid={`${testId}-value`}>
        {value}
      </span>
      <span className="flex min-h-[1.25rem] items-center text-xs text-text-muted">{meta}</span>
    </div>
  );
}

export function OverviewCards({ cards }: { cards: CardData }): React.ReactElement {
  const { t } = useTranslation('insights');
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5" data-testid="analytics-overview-cards">
      <Card
        label={t('analytics.overview.tokens')}
        value={formatTokens(cards.tokens)}
        meta={<DeltaChip value={cards.tokensDeltaPct} text={formatDelta(cards.tokensDeltaPct)} testId="card-tokens-delta" />}
        testId="card-tokens"
      />
      <Card
        label={t('analytics.overview.cost')}
        value={formatCostMicros(cards.costMicros)}
        meta={<DeltaChip value={cards.costDeltaPct} text={formatDelta(cards.costDeltaPct)} testId="card-cost-delta" />}
        testId="card-cost"
      />
      <Card
        label={t('analytics.overview.tasksDone')}
        value={String(cards.tasksDone)}
        meta={
          <DeltaChip
            value={cards.tasksDoneDelta}
            text={cards.tasksDoneDelta > 0 ? `+${cards.tasksDoneDelta}` : String(cards.tasksDoneDelta)}
            testId="card-tasks-delta"
          />
        }
        testId="card-tasks"
      />
      <Card
        label={t('analytics.overview.activeDays')}
        value={`${cards.activeDays}/${cards.activeDenom}`}
        meta={<span data-testid="card-active-rate">{t('analytics.overview.activeRate', { pct: cards.activeRatePct })}</span>}
        testId="card-active"
      />
      <Card
        label={t('analytics.overview.currentStreak')}
        value={t('analytics.overview.streakDays', { count: cards.streakCurrent })}
        meta={<span data-testid="card-streak-longest">{t('analytics.overview.longest', { count: cards.streakLongest })}</span>}
        testId="card-streak"
      />
    </div>
  );
}

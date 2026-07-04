import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import type React from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import type { HeatmapCell } from '@/api/types';

const MONTH_KEYS = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

// AgentHeatmap (I28/F5, issue-a7ff560e v2.15.0) — the GitHub-style activity
// contribution graph for the per-agent analytics dashboard. 53 weeks × 7 days
// ending today (UTC), one square per UTC calendar day, with a switchable 口径
// (Activity / Tokens / Cost). Per the English mockup
// (docs/design/v2.15.0/mockups/i28-analytics-en.png) each 口径 owns a hue —
// Activity=green, Tokens=blue, Cost=amber — and the SAME 5-step less→more
// intensity ramp re-colors off that metric's field. F7 assembles this into the
// AgentDetail analytics tab; this component is pure (cells as a prop) so it
// unit-tests in isolation.
//
// All colours are semantic tokens (success / accent / warning / bg-subtle) so
// the no-raw-colors-spa lint passes; the legend + cells are CSS squares and the
// title marker is an inline SVG (never emoji) per the no-emoji-icons a11y rule.

export type HeatmapMetric = 'activity' | 'tokens' | 'cost';

interface AgentHeatmapProps {
  cells: HeatmapCell[];
  /** Override "now" for deterministic rendering/tests; defaults to the real now. */
  today?: Date;
  /** Initial 口径; defaults to 'activity'. */
  initialMetric?: HeatmapMetric;
}

const WEEKS = 53;
const DAYS_PER_WEEK = 7;

// Per-口径 hue + label + the solid swatch shown in the switch (mockup: green /
// blue / amber). The intensity ramp (level 1..4) is opacity on the same token;
// level 0 (empty) is the neutral surface so "no activity" ≠ "low activity".
const METRICS: { id: HeatmapMetric; labelKey: string; swatch: string }[] = [
  { id: 'activity', labelKey: 'agentRuntime.heatmap.metric.activity', swatch: 'bg-success' },
  { id: 'tokens', labelKey: 'agentRuntime.heatmap.metric.tokens', swatch: 'bg-accent' },
  { id: 'cost', labelKey: 'agentRuntime.heatmap.metric.cost', swatch: 'bg-warning' },
];

// T474: the intensity ramp. Level 0 (empty) is the neutral surface TOKEN (visible
// via the cell ring). Levels 1..4 are a data-viz hue ramp applied via INLINE STYLE
// — the old `bg-success/30` etc. were alpha-on-token, which renders TRANSPARENT in
// this SPA (the documented trap), so levels 1-3 were invisible and only level 4
// (solid) showed (@oopslink: Less/More 不对). raw-color-ok: data-visualization ramp.
const RAMP_EMPTY = 'bg-bg-subtle';
// Ordered LIGHT (level 1, least) → DARK (level 4, most) so the Less▢▢▢▢▢More legend
// darkens toward "More", matching the GitHub-contribution convention (more activity =
// deeper hue). The arrays were previously dark→light, which inverted the legend — the
// darkest swatch sat at the "Less" end (@oopslink).
const RAMP_HEX: Record<HeatmapMetric, [string, string, string, string]> = {
  activity: ['#39d353', '#26a641', '#006d32', '#0e4429'], // GitHub-style greens, light→dark
  tokens: ['#54aeff', '#218bff', '#0969da', '#0a3069'], // blues, light→dark
  cost: ['#f5c518', '#e3a008', '#bb8009', '#7a4f01'], // ambers, light→dark
};

/** rampStyle returns the inline backgroundColor for a level (1..4), or undefined
 * for level 0 (which uses the neutral RAMP_EMPTY token class instead). */
function rampStyle(metric: HeatmapMetric, level: number): React.CSSProperties | undefined {
  return level > 0 ? { backgroundColor: RAMP_HEX[metric][level - 1] } : undefined;
}

/** metricValue extracts the coloured field for a cell under the active 口径. */
function metricValue(cell: HeatmapCell, metric: HeatmapMetric): number {
  switch (metric) {
    case 'tokens':
      return cell.tokens_in + cell.tokens_out;
    case 'cost':
      return cell.cost_micros;
    case 'activity':
    default:
      return cell.events;
  }
}

/** intensityLevel buckets a value into 0..4 relative to the window max. */
function intensityLevel(value: number, max: number): number {
  if (value <= 0 || max <= 0) return 0;
  const r = value / max;
  if (r > 0.75) return 4;
  if (r > 0.5) return 3;
  if (r > 0.25) return 2;
  return 1;
}

/** utcDay formats a Date as its UTC "YYYY-MM-DD" calendar date. */
function utcDay(d: Date): string {
  return d.toISOString().slice(0, 10);
}

/** addUTCDays returns a new Date shifted by n days, preserving UTC midnight. */
function addUTCDays(d: Date, n: number): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate() + n));
}

/** formatValue renders the active 口径's value for the tooltip. */
function formatValue(cell: HeatmapCell, metric: HeatmapMetric, t: TFunction): string {
  switch (metric) {
    case 'tokens':
      return t('agentRuntime.heatmap.value.tokens', {
        count: cell.tokens_in + cell.tokens_out,
        formattedCount: (cell.tokens_in + cell.tokens_out).toLocaleString(),
      });
    case 'cost': {
      const usd = cell.cost_micros / 1_000_000;
      // sub-cent costs matter on a per-turn dashboard; show up to 4 decimals.
      const amount = `$${usd.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 4 })}`;
      return t('agentRuntime.heatmap.value.cost', { amount });
    }
    case 'activity':
    default:
      return t('agentRuntime.heatmap.value.event', { count: cell.events });
  }
}

/** prettyDay turns "YYYY-MM-DD" into "Jun 23, 2026" for the tooltip. */
function prettyDay(day: string, t: TFunction): string {
  const [y, m, d] = day.split('-').map(Number);
  if (!y || !m || !d) return day;
  return t('agentRuntime.heatmap.prettyDay', { month: t(`agentRuntime.heatmap.month.${MONTH_KEYS[m - 1]}`), day: d, year: y });
}

interface GridCell {
  date: string; // "YYYY-MM-DD"
  inRange: boolean; // false for future days in the trailing week (rendered blank)
  cell: HeatmapCell | null;
}

export function AgentHeatmap({ cells, today, initialMetric = 'activity' }: AgentHeatmapProps): React.ReactElement {
  const { t } = useTranslation('members');
  const [metric, setMetric] = useState<HeatmapMetric>(initialMetric);

  const byDay = useMemo(() => {
    const m = new Map<string, HeatmapCell>();
    for (const c of cells) m.set(c.day, c);
    return m;
  }, [cells]);

  const max = useMemo(() => {
    let mx = 0;
    for (const c of cells) {
      const v = metricValue(c, metric);
      if (v > mx) mx = v;
    }
    return mx;
  }, [cells, metric]);

  // Build the 53×7 grid: columns are weeks (Sun-started), the last column is the
  // week containing today, earlier days fill backwards. Work entirely in UTC so
  // the day buckets line up with the rollup's UTC calendar dates.
  const { columns, monthLabels } = useMemo(() => {
    const now = today ?? new Date();
    const todayUTC = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
    const lastSunday = addUTCDays(todayUTC, -todayUTC.getUTCDay());
    const firstSunday = addUTCDays(lastSunday, -(WEEKS - 1) * DAYS_PER_WEEK);

    const cols: GridCell[][] = [];
    const labels: { col: number; monthIndex: number }[] = [];
    let prevMonth = -1;
    for (let w = 0; w < WEEKS; w++) {
      const col: GridCell[] = [];
      const colSunday = addUTCDays(firstSunday, w * DAYS_PER_WEEK);
      const colMonth = colSunday.getUTCMonth();
      if (colMonth !== prevMonth) {
        labels.push({ col: w, monthIndex: colMonth });
        prevMonth = colMonth;
      }
      for (let r = 0; r < DAYS_PER_WEEK; r++) {
        const date = addUTCDays(firstSunday, w * DAYS_PER_WEEK + r);
        const ds = utcDay(date);
        col.push({ date: ds, inRange: date.getTime() <= todayUTC.getTime(), cell: byDay.get(ds) ?? null });
      }
      cols.push(col);
    }
    return { columns: cols, monthLabels: labels };
  }, [byDay, today]);

  // T-heatmap-scroll (@oopslink 2026-07-04): the 53-week grid is a fixed ~800px
  // wide strip inside an overflow-x-auto scroller. Activity lives in the MOST
  // RECENT (rightmost) weeks, but a fresh scroller sits at scrollLeft=0 — the
  // oldest, empty weeks. When the panel is narrower than the grid (≈100% zoom on
  // the two-column analytics layout) the populated right edge is scrolled out of
  // view and the visible level-0 cells read as "no data" (@oopslink: 100% 就是空的,
  // 90% 才有). Anchor the initial scroll to the right end so the newest activity
  // is what you see, matching the GitHub contribution graph. useLayoutEffect runs
  // before paint (no left→right flash); a resize listener re-anchors on zoom.
  const scrollerRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (el) el.scrollLeft = el.scrollWidth;
  }, [columns]);
  useEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const anchorRight = (): void => {
      el.scrollLeft = el.scrollWidth;
    };
    window.addEventListener('resize', anchorRight);
    return () => window.removeEventListener('resize', anchorRight);
  }, []);

  const active = METRICS.find((m) => m.id === metric) ?? METRICS[0];
  const activeLabel = t(active.labelKey);

  const tip = (g: GridCell): string =>
    t('agentRuntime.heatmap.tooltip', {
      day: prettyDay(g.date, t),
      value: formatValue(g.cell ?? emptyCell(g.date), metric, t),
    });
  const levelOf = (g: GridCell): number => (g.cell ? intensityLevel(metricValue(g.cell, metric), max) : 0);

  return (
    <section className="flex min-h-[18rem] w-full min-w-0 flex-col rounded-lg border border-border-base bg-bg-elevated p-5" data-testid="agent-heatmap">
      <header className="mb-5 flex flex-wrap items-center justify-between gap-3">
        <h3 className="flex items-center gap-1.5 text-sm font-semibold text-text-primary">
          {t('agentRuntime.heatmap.title')}
        </h3>
        <div
          className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1"
          role="tablist"
          aria-label={t('agentRuntime.heatmap.metricSwitchLabel')}
          data-testid="heatmap-metric-switch"
        >
          {METRICS.map((m) => {
            const on = m.id === metric;
            return (
              <button
                key={m.id}
                type="button"
                role="tab"
                aria-selected={on}
                data-testid={`heatmap-metric-${m.id}`}
                data-active={on}
                onClick={() => setMetric(m.id)}
                className={[
                  'flex items-center gap-1.5 rounded px-0.5 py-0.5 text-xs font-medium',
                  on ? 'text-text-primary' : 'text-text-secondary hover:text-text-primary',
                ].join(' ')}
              >
                <span className={['h-2.5 w-2.5 rounded-[2px]', m.swatch].join(' ')} aria-hidden="true" />
                {t(m.labelKey)}
              </button>
            );
          })}
        </div>
      </header>

      <div ref={scrollerRef} data-testid="heatmap-scroller" className="flex flex-1 flex-col justify-center overflow-x-auto">
        {/* month labels row, aligned to the week columns (15px pitch = 12px cell + 3px gap) */}
        <div className="ml-8 flex min-w-max" aria-hidden="true" data-testid="heatmap-months">
          {columns.map((_, w) => {
            const lbl = monthLabels.find((l) => l.col === w);
            return (
              <div key={w} className="w-[15px] shrink-0 whitespace-nowrap text-[0.625rem] text-text-muted">
                {lbl ? t(`agentRuntime.heatmap.month.${MONTH_KEYS[lbl.monthIndex]}`) : ''}
              </div>
            );
          })}
        </div>

        <div className="flex min-w-max">
          {/* weekday row labels: Mon / Wed / Fri (rows 1/3/5; 0=Sun) */}
          <div className="mr-1 flex w-7 flex-col gap-[3px] text-[0.625rem] text-text-muted" aria-hidden="true">
            {['', t('agentRuntime.heatmap.weekday.mon'), '', t('agentRuntime.heatmap.weekday.wed'), '', t('agentRuntime.heatmap.weekday.fri'), ''].map((lbl, i) => (
              <div key={i} className="h-3 leading-3">
                {lbl}
              </div>
            ))}
          </div>

          {/* the grid: one column per week */}
          <div className="flex gap-[3px]" role="grid" aria-label={t('agentRuntime.heatmap.gridLabel', { metric: activeLabel })}>
            {columns.map((col, w) => (
              <div key={w} className="flex flex-col gap-[3px]" role="row">
                {col.map((g) =>
                  g.inRange ? (
                    <div
                      key={g.date}
                      role="gridcell"
                      data-testid="heatmap-cell"
                      data-date={g.date}
                      data-level={levelOf(g)}
                      // T473: a subtle inset ring outlines EVERY in-range day so the
                      // full GitHub-style grid is visible — empty (level-0) cells
                      // were bg-bg-subtle ≈ the card bg in dark mode and vanished
                      // (@oopslink: 每个块都要展示出来).
                      className={['h-3 w-3 rounded-[2px] ring-1 ring-inset ring-border-base', levelOf(g) === 0 ? RAMP_EMPTY : ''].join(' ')}
                      style={rampStyle(metric, levelOf(g))}
                      title={tip(g)}
                      aria-label={tip(g)}
                    />
                  ) : (
                    <div key={g.date} className="h-3 w-3 rounded-[2px]" aria-hidden="true" />
                  ),
                )}
              </div>
            ))}
          </div>
        </div>

        {/* legend: Less ▢▢▢▢▢ More — CSS squares in the active 口径's hue, no emoji */}
        <div className="mt-3 flex items-center justify-end gap-1 text-[0.625rem] text-text-muted" data-testid="heatmap-legend">
          <span>{t('agentRuntime.heatmap.less')}</span>
          {[0, 1, 2, 3, 4].map((lvl) => (
            <span
              key={lvl}
              data-testid={`heatmap-legend-${lvl}`}
              className={['h-3 w-3 rounded-[2px] ring-1 ring-inset ring-border-base', lvl === 0 ? RAMP_EMPTY : ''].join(' ')}
              style={rampStyle(metric, lvl)}
              aria-hidden="true"
            />
          ))}
          <span>{t('agentRuntime.heatmap.more')}</span>
        </div>
      </div>
    </section>
  );
}

/** emptyCell is a zero-valued cell for a day with no rollup row (tooltip text). */
function emptyCell(day: string): HeatmapCell {
  return { day, events: 0, tokens_in: 0, tokens_out: 0, cache_tokens: 0, cost_micros: 0 };
}

export default AgentHeatmap;

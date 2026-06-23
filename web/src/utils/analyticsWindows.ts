// Client-side window math for the F6 overview cards. The month boundary here is
// a BYTE-FOR-BYTE port of the F4 Go Overview (internal/usage/sqlite/analytics.go):
// "this month" = the rolling last 30 days inclusive of today, i.e. day strings in
// [utcDay(now,-29), utcDay(now,0)]. The previous period for the delta is the 30
// days immediately before that, [utcDay(now,-59), utcDay(now,-30)]. Keeping these
// identical to Overview guarantees the card's displayed total and its delta base
// never disagree (PD hard constraint).

import type { AnalyticsHeatmapCell } from '../api/types';

// utcDay shifts now by delta calendar days and returns "YYYY-MM-DD" in UTC,
// matching Go's dayString(now, delta) = now.UTC().AddDate(0,0,delta).
export function utcDay(now: Date, delta: number): string {
  const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + delta));
  return d.toISOString().slice(0, 10);
}

// headlineTokens is the F6 "tokens" measure for a cell: input + output. Cache is a
// separate efficiency metric and is intentionally NOT folded into the headline
// token volume (documented口径; one place to change if Review wants it included).
export function headlineTokens(c: AnalyticsHeatmapCell): number {
  return c.tokens_in + c.tokens_out;
}

// cellIsActive matches the Go streak/active definition: any nonzero measure.
export function cellIsActive(c: AnalyticsHeatmapCell): boolean {
  return c.events > 0 || c.tokens_in > 0 || c.tokens_out > 0 || c.cache_tokens > 0 || c.cost_micros > 0;
}

export interface WindowSum {
  tokens: number; // headline (in+out)
  costMicros: number;
  completed: number; // task completions in the window
}

// sumWindow totals headline tokens + cost + completions over cells with
// fromDay <= day <= toDay (inclusive string compare, valid for the fixed ISO
// date format).
export function sumWindow(cells: AnalyticsHeatmapCell[], fromDay: string, toDay: string): WindowSum {
  let tokens = 0;
  let costMicros = 0;
  let completed = 0;
  for (const c of cells) {
    if (c.day >= fromDay && c.day <= toDay) {
      tokens += headlineTokens(c);
      costMicros += c.cost_micros;
      completed += c.completed;
    }
  }
  return { tokens, costMicros, completed };
}

// pctChange mirrors utils/format.pctChange (kept local to avoid a cross-import
// cycle): 0 baseline → 0 (flat), else (curr-prev)/prev*100.
function pctChange(curr: number, prev: number): number {
  if (prev === 0) return 0;
  return ((curr - prev) / prev) * 100;
}

// activeDaysInWindow counts distinct active days in [fromDay, toDay].
export function activeDaysInWindow(cells: AnalyticsHeatmapCell[], fromDay: string, toDay: string): number {
  let n = 0;
  for (const c of cells) {
    if (c.day >= fromDay && c.day <= toDay && cellIsActive(c)) n++;
  }
  return n;
}

// MONTH_DAYS is the rolling-window denominator shown as "X/30" on the ACTIVE DAYS
// card (matches the 30-day Overview.month window).
export const MONTH_DAYS = 30;

// currentStreak counts consecutive active days ending at today (UTC), walking
// backward from now until a gap. 0 when today is inactive.
export function currentStreak(cells: AnalyticsHeatmapCell[], now: Date): number {
  const active = new Set(cells.filter(cellIsActive).map((c) => c.day));
  let n = 0;
  for (let d = 0; active.has(utcDay(now, d)); d--) n++;
  return n;
}

// CardData is the derived value set for the five THIS-MONTH overview cards. All
// five come from the single per-day heatmap series (PD's "single口径源"):
// TOKENS/COST carry a percent delta vs the prior 30 days; TASKS DONE an absolute
// count delta; ACTIVE DAYS a count/denominator + rate; STREAK current + longest.
export interface CardData {
  tokens: number;
  tokensDeltaPct: number;
  costMicros: number;
  costDeltaPct: number;
  tasksDone: number;
  tasksDoneDelta: number; // absolute (current − prior), per mockup "▼ −3"
  activeDays: number;
  activeDenom: number;
  activeRatePct: number;
  streakCurrent: number;
  streakLongest: number;
}

// deriveCards computes the THIS-MONTH card values from the per-day series. The
// month window is the rolling last 30 days (matching F4 Overview.month); the
// delta baseline is the prior 30 days.
export function deriveCards(cells: AnalyticsHeatmapCell[], now: Date): CardData {
  const today = utcDay(now, 0);
  const curr = sumWindow(cells, utcDay(now, -29), today);
  const prev = sumWindow(cells, utcDay(now, -59), utcDay(now, -30));
  const active = activeDaysInWindow(cells, utcDay(now, -29), today);
  return {
    tokens: curr.tokens,
    tokensDeltaPct: pctChange(curr.tokens, prev.tokens),
    costMicros: curr.costMicros,
    costDeltaPct: pctChange(curr.costMicros, prev.costMicros),
    tasksDone: curr.completed,
    tasksDoneDelta: curr.completed - prev.completed,
    activeDays: active,
    activeDenom: MONTH_DAYS,
    activeRatePct: Math.round((active / MONTH_DAYS) * 100),
    streakCurrent: currentStreak(cells, now),
    streakLongest: longestStreak(cells),
  };
}

// epochDay converts a "YYYY-MM-DD" UTC date to an integer day index.
function epochDay(day: string): number {
  return Math.round(Date.parse(`${day}T00:00:00Z`) / 86_400_000);
}

// longestStreak is the longest run of consecutive active calendar days across the
// whole cell series (the "longest 14 days" figure). Heatmap cells are active days
// only (zero days have no rollup row), so we walk the sorted active-day indices.
export function longestStreak(cells: AnalyticsHeatmapCell[]): number {
  const days = cells
    .filter(cellIsActive)
    .map((c) => epochDay(c.day))
    .sort((a, b) => a - b);
  if (days.length === 0) return 0;
  let best = 1;
  let run = 1;
  for (let i = 1; i < days.length; i++) {
    if (days[i] === days[i - 1]) continue; // de-dup same day
    if (days[i] === days[i - 1] + 1) {
      run++;
      best = Math.max(best, run);
    } else {
      run = 1;
    }
  }
  return best;
}

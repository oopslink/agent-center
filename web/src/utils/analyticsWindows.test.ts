import { describe, it, expect } from 'vitest';
import {
  utcDay,
  sumWindow,
  deriveCards,
  currentStreak,
  longestStreak,
  MONTH_DAYS,
} from './analyticsWindows';
import type { AnalyticsHeatmapCell } from '../api/types';

function cell(
  day: string,
  tokensIn: number,
  tokensOut = 0,
  cost = 0,
  completed = 0,
  events = 0,
): AnalyticsHeatmapCell {
  return { day, events, completed, tokens_in: tokensIn, tokens_out: tokensOut, cache_tokens: 0, cost_micros: cost };
}

const now = new Date(Date.UTC(2026, 5, 22, 12, 0, 0)); // 2026-06-22

describe('utcDay', () => {
  it('shifts UTC calendar days, ISO format', () => {
    expect(utcDay(now, 0)).toBe('2026-06-22');
    expect(utcDay(now, -29)).toBe('2026-05-24');
    expect(utcDay(now, -30)).toBe('2026-05-23');
    expect(utcDay(now, -59)).toBe('2026-04-24');
  });
});

describe('sumWindow', () => {
  it('sums headline tokens (in+out) + cost + completed inclusive of bounds', () => {
    const cells = [cell('2026-06-20', 100, 50, 900, 2), cell('2026-06-21', 10, 0, 100, 1), cell('2026-07-01', 999, 0, 9999, 9)];
    const s = sumWindow(cells, '2026-06-01', '2026-06-30');
    expect(s.tokens).toBe(160); // 150 + 10, July excluded
    expect(s.costMicros).toBe(1000);
    expect(s.completed).toBe(3); // 2 + 1, July excluded
  });
});

describe('deriveCards', () => {
  it('derives all five cards from the per-day series', () => {
    const cells = [
      // current 30d window [05-24 .. 06-22]
      cell('2026-06-22', 100, 0, 2000, 2), // today active
      cell('2026-06-21', 50, 0, 1000, 1),
      // prior 30d window [04-24 .. 05-23]
      cell('2026-05-10', 75, 0, 1500, 4),
    ];
    const c = deriveCards(cells, now);
    // tokens: current 150 vs prior 75 → +100%
    expect(c.tokens).toBe(150);
    expect(c.tokensDeltaPct).toBeCloseTo(100);
    // cost: 3000 vs 1500 → +100%
    expect(c.costMicros).toBe(3000);
    expect(c.costDeltaPct).toBeCloseTo(100);
    // tasks done: current 3 vs prior 4 → absolute delta −1
    expect(c.tasksDone).toBe(3);
    expect(c.tasksDoneDelta).toBe(-1);
    // active days: 06-22 + 06-21 in window → 2 of 30
    expect(c.activeDays).toBe(2);
    expect(c.activeDenom).toBe(MONTH_DAYS);
    expect(c.activeRatePct).toBe(Math.round((2 / 30) * 100)); // 7
    // streak: 06-21,06-22 consecutive ending today → current 2; longest 2
    expect(c.streakCurrent).toBe(2);
    expect(c.streakLongest).toBe(2);
  });

  it('flat 0% delta when no prior baseline', () => {
    const c = deriveCards([cell('2026-06-22', 100, 0, 500, 1)], now);
    expect(c.tokensDeltaPct).toBe(0);
    expect(c.costDeltaPct).toBe(0);
    expect(c.tasksDoneDelta).toBe(1); // 1 − 0
  });
});

describe('currentStreak', () => {
  it('counts consecutive active days ending today', () => {
    const cells = [cell('2026-06-20', 1), cell('2026-06-21', 1), cell('2026-06-22', 1)];
    expect(currentStreak(cells, now)).toBe(3);
  });
  it('is 0 when today is inactive', () => {
    const cells = [cell('2026-06-20', 1), cell('2026-06-21', 1)]; // no 06-22
    expect(currentStreak(cells, now)).toBe(0);
  });
});

describe('longestStreak', () => {
  it('finds the longest consecutive active-day run across the series', () => {
    const cells = [
      cell('2026-06-01', 1),
      cell('2026-06-02', 1),
      cell('2026-06-03', 1), // run of 3
      cell('2026-06-10', 1),
      cell('2026-06-11', 1), // run of 2
    ];
    expect(longestStreak(cells)).toBe(3);
  });
  it('ignores inactive cells and handles empty', () => {
    expect(longestStreak([])).toBe(0);
    expect(longestStreak([cell('2026-06-01', 0, 0, 0, 0, 0)])).toBe(0);
  });
});

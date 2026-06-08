import { describe, expect, it } from 'vitest';
import { formatChatDate, formatChatTime, formatLocalTime, localDateToRFC3339 } from './time';

describe('formatLocalTime', () => {
  const iso = '2026-06-04T07:34:21.874846Z';

  it('formats an ISO timestamp into a human-readable local string (not raw ISO)', () => {
    const out = formatLocalTime(iso);
    expect(out).not.toBe(iso);
    expect(out).not.toMatch(/T\d\d:\d\d/); // no raw "T07:34"
    expect(out).toContain('2026');
  });

  it('includes a timezone indicator (abbr or GMT offset)', () => {
    expect(formatLocalTime(iso)).toMatch(/GMT|UTC|[A-Z]{2,5}|[+-]\d/);
  });

  it('returns invalid / empty input unchanged (fail-safe)', () => {
    expect(formatLocalTime('not-a-date')).toBe('not-a-date');
    expect(formatLocalTime('')).toBe('');
  });

  it('is stable for the same input', () => {
    expect(formatLocalTime(iso)).toBe(formatLocalTime(iso));
  });
});

// @oopslink locked (DM mockup): per-message formatChatTime renders 24-hr local
// "HH:MM" (e.g. "13:00"). tz-tolerant: assert the shape via regex, not a fixed
// wall-clock value (the actual digits depend on the runner's local tz).
describe('formatChatTime', () => {
  const iso = '2026-06-08T12:01:02.500Z';

  it('formats a known ISO into 24-hr local "HH:MM" (e.g. "13:00")', () => {
    expect(formatChatTime(iso)).toMatch(/^\d{2}:\d{2}$/);
  });

  it('is the short HH:MM form — NOT the long formatLocalTime form (no date / no tz)', () => {
    const out = formatChatTime(iso);
    expect(out).not.toBe(formatLocalTime(iso));
    expect(out).not.toMatch(/\d{4}/); // no year
    expect(out).not.toMatch(/GMT|UTC/); // no tz label
  });

  it('returns invalid / empty input unchanged (fail-safe)', () => {
    expect(formatChatTime('not-a-date')).toBe('not-a-date');
    expect(formatChatTime('')).toBe('');
  });

  it('is stable for the same input', () => {
    expect(formatChatTime(iso)).toBe(formatChatTime(iso));
  });
});

// @oopslink locked (DM mockup): formatChatDate renders the Chinese local date
// "YYYY年MM月DD日" (e.g. "2026年6月4日"). Used for the chat date separators.
// tz-tolerant: assert the shape via regex (month/day may be 1 or 2 digits).
describe('formatChatDate', () => {
  const iso = '2026-06-04T12:01:02.500Z';

  it('formats a known ISO into Chinese local "YYYY年MM月DD日"', () => {
    expect(formatChatDate(iso)).toMatch(/^\d{4}年\d{1,2}月\d{1,2}日$/);
  });

  it('returns invalid / empty input unchanged (fail-safe)', () => {
    expect(formatChatDate('not-a-date')).toBe('not-a-date');
    expect(formatChatDate('')).toBe('');
  });

  it('is stable for the same input', () => {
    expect(formatChatDate(iso)).toBe(formatChatDate(iso));
  });
});

// The off-by-one 命门: a date-picker value "YYYY-MM-DD" must become an RFC3339
// ABSOLUTE instant carrying the viewer's LOCAL timezone offset — never a naive
// date, never UTC midnight (Z). start = local 00:00:00, end = local 23:59:59.
describe('localDateToRFC3339', () => {
  it('empty input → undefined (param omitted)', () => {
    expect(localDateToRFC3339('', 'start')).toBeUndefined();
    expect(localDateToRFC3339('', 'end')).toBeUndefined();
  });

  it('start edge = local start-of-day with a [+-]HH:MM offset (NOT Z, NOT bare date)', () => {
    const out = localDateToRFC3339('2026-06-08', 'start');
    expect(out).toBeDefined();
    expect(out).toMatch(/^2026-06-08T00:00:00[+-]\d{2}:\d{2}$/);
    // explicitly NOT UTC Z, NOT a naive/bare date
    expect(out).not.toMatch(/Z$/);
    expect(out).not.toBe('2026-06-08');
  });

  it('end edge = local end-of-day (23:59:59) with a [+-]HH:MM offset', () => {
    const out = localDateToRFC3339('2026-06-08', 'end');
    expect(out).toMatch(/^2026-06-08T23:59:59[+-]\d{2}:\d{2}$/);
    expect(out).not.toMatch(/Z$/);
  });

  it('start vs end differ only in the time-of-day (00:00:00 vs 23:59:59), same offset', () => {
    const start = localDateToRFC3339('2026-06-08', 'start')!;
    const end = localDateToRFC3339('2026-06-08', 'end')!;
    expect(start).toContain('T00:00:00');
    expect(end).toContain('T23:59:59');
    expect(start.slice(0, 10)).toBe(end.slice(0, 10)); // same date
    expect(start.slice(19)).toBe(end.slice(19)); // same offset suffix
  });

  it('the offset matches the runtime local offset derived from getTimezoneOffset', () => {
    const out = localDateToRFC3339('2026-06-08', 'start')!;
    const offMin = -new Date(2026, 5, 8, 0, 0, 0).getTimezoneOffset();
    const sign = offMin >= 0 ? '+' : '-';
    const abs = Math.abs(offMin);
    const hh = String(Math.floor(abs / 60)).padStart(2, '0');
    const mm = String(abs % 60).padStart(2, '0');
    expect(out.slice(19)).toBe(`${sign}${hh}:${mm}`);
  });
});

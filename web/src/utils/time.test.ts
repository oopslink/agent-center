import { describe, expect, it } from 'vitest';
import { formatLocalTime } from './time';

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

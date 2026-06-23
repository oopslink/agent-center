import { describe, it, expect } from 'vitest';
import { formatTokens, formatCostMicros, formatPercent, formatDelta, pctChange } from './format';

describe('formatTokens', () => {
  it('renders raw under 1000', () => {
    expect(formatTokens(0)).toBe('0');
    expect(formatTokens(950)).toBe('950');
  });
  it('renders K/M/B with one decimal, trimming .0', () => {
    expect(formatTokens(1_000)).toBe('1K');
    expect(formatTokens(1_500)).toBe('1.5K');
    expect(formatTokens(48_200_000)).toBe('48.2M');
    expect(formatTokens(2_300_000_000)).toBe('2.3B');
  });
  it('preserves sign', () => {
    expect(formatTokens(-1_500)).toBe('-1.5K');
  });
});

describe('formatCostMicros', () => {
  it('converts micros to USD with 2 decimals', () => {
    expect(formatCostMicros(0)).toBe('$0.00');
    expect(formatCostMicros(312_740_000)).toBe('$312.74');
    expect(formatCostMicros(1_000_000)).toBe('$1.00');
  });
  it('preserves sign', () => {
    expect(formatCostMicros(-1_230_000)).toBe('-$1.23');
  });
});

describe('formatPercent', () => {
  it('renders percent units', () => {
    expect(formatPercent(77)).toBe('77%');
    expect(formatPercent(77.25, 1)).toBe('77.3%');
  });
});

describe('formatDelta', () => {
  it('renders signed deltas, flat 0%', () => {
    expect(formatDelta(12.4)).toBe('+12.4%');
    expect(formatDelta(-3.2)).toBe('-3.2%');
    expect(formatDelta(0)).toBe('0%');
  });
});

describe('pctChange', () => {
  it('computes period-over-period change', () => {
    expect(pctChange(110, 100)).toBeCloseTo(10);
    expect(pctChange(80, 100)).toBeCloseTo(-20);
  });
  it('returns 0 when prev is 0 (no baseline, not Infinity)', () => {
    expect(pctChange(50, 0)).toBe(0);
  });
});

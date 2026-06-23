// Number / cost / percent formatting for the analytics dashboard (I28/F6).
// Locale-agnostic, dependency-free — mirrors the codebase's hand-rolled toFixed
// style. Cost is stored as micros (millionths of a USD; cost_micros / 1e6 = USD),
// matching usage_events.cost_micros and the model_prices *_per_mtok_micros units.

// formatTokens renders a token count compactly: 950 → "950", 1_500 → "1.5K",
// 48_200_000 → "48.2M", 2_300_000_000 → "2.3B". One decimal, trailing ".0"
// trimmed (1_000 → "1K", not "1.0K").
export function formatTokens(n: number): string {
  const sign = n < 0 ? '-' : '';
  const v = Math.abs(n);
  if (v < 1_000) return `${sign}${v}`;
  const units: Array<[number, string]> = [
    [1_000_000_000, 'B'],
    [1_000_000, 'M'],
    [1_000, 'K'],
  ];
  for (const [base, suffix] of units) {
    if (v >= base) return `${sign}${trimZero(v / base)}${suffix}`;
  }
  return `${sign}${v}`;
}

// trimZero formats to one decimal but drops a trailing ".0" (1 → "1", 1.5 → "1.5").
function trimZero(x: number): string {
  const s = x.toFixed(1);
  return s.endsWith('.0') ? s.slice(0, -2) : s;
}

// formatCostMicros renders a micros cost as USD with 2 decimals: 312_740_000 →
// "$312.74", 0 → "$0.00". Negative preserved as "-$1.23".
export function formatCostMicros(micros: number): string {
  const sign = micros < 0 ? '-' : '';
  const usd = Math.abs(micros) / 1_000_000;
  return `${sign}$${usd.toFixed(2)}`;
}

// formatPercent renders a ratio-as-percent already in percent units: 77 → "77%".
// `decimals` controls precision (default 0).
export function formatPercent(pct: number, decimals = 0): string {
  return `${pct.toFixed(decimals)}%`;
}

// formatDelta renders a signed period-over-period percentage for the overview
// cards: +12.4 → "+12.4%", -3.2 → "-3.2%", 0 → "0%". Used with directional
// color tokens by the card component.
export function formatDelta(pct: number, decimals = 1): string {
  if (pct === 0) return '0%';
  const sign = pct > 0 ? '+' : '';
  return `${sign}${pct.toFixed(decimals)}%`;
}

// pctChange computes a period-over-period percent change, guarding divide-by-zero:
// (curr - prev) / prev * 100. When prev is 0, returns 0 (no baseline → no delta
// rather than Infinity), so the card shows a flat "0%" instead of a broken value.
export function pctChange(curr: number, prev: number): number {
  if (prev === 0) return 0;
  return ((curr - prev) / prev) * 100;
}

// formatLocalTime — render an ISO-8601 (UTC) timestamp in the viewer's LOCAL
// timezone with a timezone abbreviation, e.g. "Jun 4, 2026, 7:34 AM GMT+8".
// Replaces raw UTC-ISO displays site-wide (@oopslink convention: default local
// tz + show the tz abbreviation, never the raw "2026-06-04T07:34:21Z"). Invalid
// or empty input is returned unchanged (fail-safe — never throw on bad data).
export function formatLocalTime(iso: string): string {
  if (!iso) return iso;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    timeZoneName: 'short',
  }).format(d);
}

// formatChatTime — chat-only timestamp helper (Chat UX 2 #4). Renders an
// ISO-8601 (UTC) timestamp in the viewer's LOCAL timezone with the FULL date,
// seconds, and a tz abbreviation/offset, e.g. "2026-06-08 20:01:02 GMT+8".
// This longer "YYYY-MM-DD HH:MM:SS GMT+N" form is intentionally chat-ONLY — do
// NOT reuse it for site-wide displays (those keep formatLocalTime's short form).
// Built from Intl.DateTimeFormat parts (local tz) so the date/time digits and
// the tz name come from the SAME local-tz computation. Invalid or empty input is
// returned unchanged (fail-safe — never throw on bad data, like formatLocalTime).
export function formatChatTime(iso: string): string {
  if (!iso) return iso;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const fmt = new Intl.DateTimeFormat('en-CA', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
    timeZoneName: 'short',
  });
  const parts = fmt.formatToParts(d);
  const get = (t: Intl.DateTimeFormatPartTypes): string =>
    parts.find((p) => p.type === t)?.value ?? '';
  // en-CA gives YYYY-MM-DD for the date order and 24h time; assemble explicitly
  // so the shape is stable across engines (no locale-dependent separators).
  let hour = get('hour');
  if (hour === '24') hour = '00'; // some engines emit 24 for midnight
  const date = `${get('year')}-${get('month')}-${get('day')}`;
  const time = `${hour}:${get('minute')}:${get('second')}`;
  const tz = get('timeZoneName') || 'GMT'; // e.g. "GMT+8"
  return `${date} ${time} ${tz}`;
}

// localDateToRFC3339 — convert a `<input type="date">` value ("YYYY-MM-DD") into
// an RFC3339 ABSOLUTE instant in the viewer's LOCAL timezone (#258 date-range
// filter, PR #224 backend). This is the off-by-one 命门: the backend compares
// absolute instants and does NOT guess a timezone, so we must emit the LOCAL
// offset — never a naive date, never UTC midnight (Z).
//
//   edge 'start' → local 00:00:00 (start-of-day)
//   edge 'end'   → local 23:59:59 (end-of-day)
//
// e.g. for a GMT+8 viewer: "2026-06-08" → "2026-06-08T00:00:00+08:00" (start) /
// "2026-06-08T23:59:59+08:00" (end). Empty input → undefined (omit the param).
//
// We build a LOCAL Date and format the offset from -getTimezoneOffset(); we do
// NOT use Date.toISOString() (that converts to UTC and emits "Z").
export function localDateToRFC3339(value: string, edge: 'start' | 'end'): string | undefined {
  if (!value) return undefined;
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!m) return undefined;
  const [, ys, ms, ds] = m;
  const y = Number(ys);
  const mo = Number(ms);
  const d = Number(ds);
  const [hh, mm, ss] = edge === 'start' ? [0, 0, 0] : [23, 59, 59];
  // LOCAL Date — getTimezoneOffset() then reflects this wall-clock's offset
  // (correct across DST for the picked day, not just "now").
  const local = new Date(y, mo - 1, d, hh, mm, ss, 0);
  if (Number.isNaN(local.getTime())) return undefined;
  const pad = (n: number) => String(n).padStart(2, '0');
  // getTimezoneOffset() is minutes to ADD to local to get UTC (e.g. GMT+8 → -480);
  // negate so +480 → "+08:00".
  const offMin = -local.getTimezoneOffset();
  const sign = offMin >= 0 ? '+' : '-';
  const absMin = Math.abs(offMin);
  const offset = `${sign}${pad(Math.floor(absMin / 60))}:${pad(absMin % 60)}`;
  return `${ys}-${ms}-${ds}T${pad(hh)}:${pad(mm)}:${pad(ss)}${offset}`;
}

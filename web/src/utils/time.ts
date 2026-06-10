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

// formatChatTime — per-message chat timestamp (@oopslink locked, DM mockup).
// Renders an ISO-8601 (UTC) timestamp as 24-hr "HH:MM" in the viewer's LOCAL
// timezone, e.g. "13:00". Overrides the old long "YYYY-MM-DD HH:MM:SS GMT+N"
// form. Built from Intl.DateTimeFormat parts (local tz, hour12:false, 2-digit
// hour+minute) so the digits come from the local-tz computation. Invalid or
// empty input is returned unchanged (fail-safe — never throw on bad data).
export function formatChatTime(iso: string): string {
  if (!iso) return iso;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const fmt = new Intl.DateTimeFormat('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
  const parts = fmt.formatToParts(d);
  const get = (t: Intl.DateTimeFormatPartTypes): string =>
    parts.find((p) => p.type === t)?.value ?? '';
  let hour = get('hour');
  if (hour === '24') hour = '00'; // some engines emit 24 for midnight
  const minute = get('minute');
  return `${hour}:${minute}`;
}

// formatChatDate — chat date-separator label (@oopslink locked, DM mockup).
// Renders an ISO-8601 (UTC) timestamp as a Chinese "YYYY年MM月DD日" date in the
// viewer's LOCAL timezone, e.g. "2026年6月4日". Used by the 7th-DM date
// separators. Built from local-tz date parts so the day matches the viewer's
// wall clock (NOT UTC). Invalid or empty input is returned unchanged (fail-safe).
export function formatChatDate(iso: string): string {
  if (!iso) return iso;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  // 'en-CA' yields a stable YYYY-MM-DD shape in the local tz; parse the parts
  // and reassemble into the Chinese form (month/day NOT zero-padded, matching
  // the "2026年6月4日" mockup example).
  const fmt = new Intl.DateTimeFormat('en-CA', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
  const parts = fmt.formatToParts(d);
  const get = (t: Intl.DateTimeFormatPartTypes): string =>
    parts.find((p) => p.type === t)?.value ?? '';
  const year = Number(get('year'));
  const month = Number(get('month'));
  const day = Number(get('day'));
  return `${year}年${month}月${day}日`;
}

// formatStatusDuration — how long the item has been in its current status,
// computed FE-side as (now − sinceIso). Renders the LARGEST TWO non-zero units
// (d/h/m), e.g. "3d 5h", "2h 14m", "5m". Under a minute → "<1m". Invalid/empty
// input → null (caller omits the duration gracefully). Pure compute on render —
// no ticking timer; it refreshes whenever the component re-renders.
//   "3d 5h"  (days + hours)   · "2h 14m" (hours + minutes)
//   "45m"    (minutes only)   · "<1m"    (under a minute)
export function formatStatusDuration(
  sinceIso: string | undefined | null,
  now: number = Date.now(),
): string | null {
  if (!sinceIso) return null;
  const start = new Date(sinceIso).getTime();
  if (Number.isNaN(start)) return null;
  let secs = Math.floor((now - start) / 1000);
  if (secs < 0) secs = 0; // clock skew → clamp (never show a negative duration)
  if (secs < 60) return '<1m';
  const days = Math.floor(secs / 86400);
  const hours = Math.floor((secs % 86400) / 3600);
  const mins = Math.floor((secs % 3600) / 60);
  // Largest two units: pick the top non-zero unit, then the next one down.
  if (days > 0) return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
  if (hours > 0) return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
  return `${mins}m`;
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

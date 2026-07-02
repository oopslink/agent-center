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

// tzOffsetLabel — the viewer's LOCAL UTC offset for an instant, formatted as a
// compact "UTC+8" / "UTC-7" / "UTC+5:30" / "UTC+0" label. Computed from the
// Date's getTimezoneOffset() so it is correct across DST for THAT instant (not
// just "now"). getTimezoneOffset() returns minutes to ADD to local to reach UTC
// (GMT+8 → -480), so we negate to get minutes EAST of UTC.
function tzOffsetLabel(d: Date): string {
  const offMin = -d.getTimezoneOffset();
  const sign = offMin >= 0 ? '+' : '-';
  const abs = Math.abs(offMin);
  const h = Math.floor(abs / 60);
  const m = abs % 60;
  return m === 0 ? `UTC${sign}${h}` : `UTC${sign}${h}:${String(m).padStart(2, '0')}`;
}

// formatChatTime — per-message chat timestamp (T751 cross-day rule).
// Renders an ISO-8601 (UTC) timestamp in the viewer's LOCAL timezone:
//   • same local calendar day as `now` → 24-hr "HH:MM" only          (e.g. "13:21")
//   • a different day, same year       → "MM-DD HH:MM (UTC±N)"       (e.g. "07-01 13:21 (UTC+8)")
//   • a different year                 → "YYYY-MM-DD HH:MM (UTC±N)"  (e.g. "2025-12-31 13:21 (UTC+8)")
// This lets yesterday's and today's messages be told apart (previously both
// rendered as bare "HH:MM"). The date + tz are only shown when they add signal.
// `now` is injectable for deterministic tests. All digits come from a LOCAL-tz
// Intl computation. Invalid/empty input is returned unchanged (fail-safe).
export function formatChatTime(iso: string, now: number = Date.now()): string {
  if (!iso) return iso;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;

  // 24-hr local "HH:MM" — always part of the output.
  const timeFmt = new Intl.DateTimeFormat('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
  const tParts = timeFmt.formatToParts(d);
  const tGet = (t: Intl.DateTimeFormatPartTypes): string =>
    tParts.find((p) => p.type === t)?.value ?? '';
  let hour = tGet('hour');
  if (hour === '24') hour = '00'; // some engines emit 24 for midnight
  const time = `${hour}:${tGet('minute')}`;

  // Local calendar-day comparison of the message vs `now`. 'en-CA' gives a
  // stable YYYY-MM-DD shape in the local tz; compare the parts, not the instant.
  const dateFmt = new Intl.DateTimeFormat('en-CA', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
  const partsOf = (val: Date) => {
    const p = dateFmt.formatToParts(val);
    const g = (t: Intl.DateTimeFormatPartTypes): string => p.find((x) => x.type === t)?.value ?? '';
    return { year: g('year'), month: g('month'), day: g('day') };
  };
  const msg = partsOf(d);
  const today = partsOf(new Date(now));

  // Same local day → time only.
  if (msg.year === today.year && msg.month === today.month && msg.day === today.day) {
    return time;
  }

  // Different day → date + time + timezone. Include the year only when it differs.
  const datePart =
    msg.year === today.year ? `${msg.month}-${msg.day}` : `${msg.year}-${msg.month}-${msg.day}`;
  return `${datePart} ${time} (${tzOffsetLabel(d)})`;
}

// formatLocalDateTimeSeconds — render an ISO-8601 (UTC) timestamp as a full
// "yyyy-MM-dd HH:mm:ss" wall-clock in the viewer's LOCAL timezone (24-hr), e.g.
// "2026-06-20 13:45:09". Used by the channel-list recent-message previews (T234)
// where a compact, sortable absolute timestamp is wanted (NOT the long locale
// form). Built from Intl.DateTimeFormat parts (local tz, hour12:false, 2-digit
// everything) so the digits come from the local-tz computation. Invalid/empty
// input is returned unchanged (fail-safe — never throw on bad data).
export function formatLocalDateTimeSeconds(iso: string): string {
  if (!iso) return iso;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  // 'en-CA' yields a stable YYYY-MM-DD date shape in the local tz; combined with
  // 2-digit 24-hr time parts we reassemble the canonical "YYYY-MM-DD HH:mm:ss".
  const fmt = new Intl.DateTimeFormat('en-CA', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  const parts = fmt.formatToParts(d);
  const get = (t: Intl.DateTimeFormatPartTypes): string =>
    parts.find((p) => p.type === t)?.value ?? '';
  let hour = get('hour');
  if (hour === '24') hour = '00'; // some engines emit 24 for midnight
  return `${get('year')}-${get('month')}-${get('day')} ${hour}:${get('minute')}:${get('second')}`;
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

// formatRelativeTime — a GitHub-style coarse "time ago" label for a past instant,
// computed FE-side as (now − iso): "just now", "N minutes ago", "N hours ago",
// "yesterday", "N days ago", then falls back to an absolute local date ("Jun 30,
// 2026") once it is older than ~30 days. Singular/plural handled ("1 minute ago").
// A FUTURE instant (clock skew) clamps to "just now". Invalid/empty input → '' so
// the caller can omit it gracefully. now is injectable for deterministic tests.
export function formatRelativeTime(iso: string | undefined | null, now: number = Date.now()): string {
  if (!iso) return '';
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  let secs = Math.floor((now - t) / 1000);
  if (secs < 0) secs = 0; // future / clock skew → "just now"
  if (secs < 45) return 'just now';
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins} ${mins === 1 ? 'minute' : 'minutes'} ago`;
  const hours = Math.round(secs / 3600);
  if (hours < 24) return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`;
  const days = Math.round(secs / 86400);
  if (days === 1) return 'yesterday';
  if (days < 30) return `${days} days ago`;
  return formatDayLabel(iso);
}

// formatDayLabel — an absolute, no-time day label in the viewer's LOCAL timezone,
// e.g. "Jun 30, 2026". Used for the commit-list date-group headers ("Commits on
// {label}") and as formatRelativeTime's old-instant fallback. Invalid/empty input
// is returned unchanged (fail-safe — never throw on bad data).
export function formatDayLabel(iso: string): string {
  if (!iso) return iso;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
  }).format(d);
}

// localDayKey — a stable, sortable "YYYY-MM-DD" key for the viewer's LOCAL calendar
// day of an instant (en-CA yields that shape directly). Used to GROUP a commit list
// by day before rendering each group's header. Invalid/empty input → '' (its own
// bucket; the header then renders the raw value via formatDayLabel's fail-safe).
export function localDayKey(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(d);
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

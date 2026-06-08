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

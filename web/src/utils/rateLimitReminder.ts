// rateLimitReminder — detect an LLM quota / session-limit message in an agent's
// recent activity and turn its "resets <time>" clock into a one-shot reminder
// prefill (T474). The signal looks like:
//
//   "You've hit your session limit · resets 12:10pm (Asia/Shanghai)"
//
// It can arrive either as a dedicated `rate_limit` event (payload.message) or
// embedded in an `assistant_text` event (payload.text / payload.raw.text). We
// scan the last 3 days of activity, take the MOST RECENT match, parse the reset
// wall-clock (+ optional IANA timezone), and compute the NEXT absolute instant
// that clock occurs. The result carries LOCAL-tz `<input type=date|time>` strings
// so the create-modal (which interprets its once inputs in the viewer's local tz)
// reproduces the exact same instant.

import type { AgentActivityEvent } from '@/api/types';

const THREE_DAYS_MS = 3 * 24 * 60 * 60 * 1000;

// "resets 12:10pm (Asia/Shanghai)" / "resets at 9am" / "resets 14:00".
// g1 hour · g2 minute? · g3 am/pm? · g4 parenthesised timezone?
const RESET_RE =
  /\bresets?\b\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(a\.?m\.?|p\.?m\.?)?\s*(?:\(([^)]+)\))?/i;

export interface RateLimitReset {
  /** The reset clock as shown in the source, e.g. "12:10pm". */
  clock: string;
  /** IANA timezone captured from the message (e.g. "Asia/Shanghai"), if any. */
  timezone?: string;
  /** The next absolute instant the limit resets (ISO-8601). */
  resetAt: string;
  /** LOCAL-tz "YYYY-MM-DD" for an `<input type="date">`. */
  onceDate: string;
  /** LOCAL-tz "HH:MM" for an `<input type="time">`. */
  onceTime: string;
  /** The matched source text (for the prefilled reminder content). */
  sourceText: string;
}

function parsePayload(raw: string): Record<string, unknown> {
  if (!raw) return {};
  try {
    const v = JSON.parse(raw);
    return v && typeof v === 'object' ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

// candidateText pulls the human-readable strings a limit message could live in:
// a dedicated rate_limit event's `message`, or the `text` / nested `raw.text` of
// an assistant_text event.
function candidateText(payload: Record<string, unknown>): string {
  const parts: string[] = [];
  if (typeof payload.message === 'string') parts.push(payload.message);
  if (typeof payload.text === 'string') parts.push(payload.text);
  const raw = payload.raw;
  if (raw && typeof raw === 'object' && typeof (raw as Record<string, unknown>).text === 'string') {
    parts.push((raw as Record<string, unknown>).text as string);
  }
  return parts.join('\n');
}

// tzOffsetMs returns (wall-clock-of-instant-in-tz interpreted as UTC) − instant,
// i.e. the tz's UTC offset in ms at that instant. Standard Intl formatToParts
// trick; throws for an unknown tz (caller guards).
function tzOffsetMs(instant: number, tz: string): number {
  const dtf = new Intl.DateTimeFormat('en-US', {
    timeZone: tz,
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
  const map: Record<string, number> = {};
  for (const p of dtf.formatToParts(new Date(instant))) {
    if (p.type !== 'literal') map[p.type] = Number(p.value);
  }
  const asUTC = Date.UTC(map.year, map.month - 1, map.day, map.hour, map.minute, map.second);
  return asUTC - instant;
}

// zonedWallToInstant — the absolute instant at which the given wall-clock occurs
// in `tz` (offset-correction; one pass is exact away from DST seams).
function zonedWallToInstant(
  y: number,
  mo: number,
  d: number,
  h: number,
  mi: number,
  tz: string,
): number {
  const utcGuess = Date.UTC(y, mo - 1, d, h, mi);
  const offset = tzOffsetMs(utcGuess, tz);
  return utcGuess - offset;
}

// wallDateInTz — the y/m/d the given instant falls on in `tz`.
function wallDateInTz(instant: number, tz: string): { y: number; mo: number; d: number } {
  const dtf = new Intl.DateTimeFormat('en-CA', {
    timeZone: tz,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
  const map: Record<string, number> = {};
  for (const p of dtf.formatToParts(new Date(instant))) {
    if (p.type !== 'literal') map[p.type] = Number(p.value);
  }
  return { y: map.year, mo: map.month, d: map.day };
}

// nextOccurrenceInTz — the next instant strictly after `now` whose wall-clock in
// `tz` reads h:mi. Starts from "today in tz" and advances a day if already past.
function nextOccurrenceInTz(h: number, mi: number, tz: string, now: number): number {
  const { y, mo, d } = wallDateInTz(now, tz);
  let when = zonedWallToInstant(y, mo, d, h, mi, tz);
  if (when <= now) {
    const tomorrow = zonedWallToInstant(y, mo, d + 1, h, mi, tz);
    when = tomorrow;
  }
  return when;
}

// nextOccurrenceLocal — same, but interpreting h:mi in the viewer's LOCAL tz
// (used when the message carried no parenthesised timezone).
function nextOccurrenceLocal(h: number, mi: number, now: number): number {
  const base = new Date(now);
  const cand = new Date(base.getFullYear(), base.getMonth(), base.getDate(), h, mi, 0, 0);
  if (cand.getTime() <= now) cand.setDate(cand.getDate() + 1);
  return cand.getTime();
}

const pad = (n: number) => String(n).padStart(2, '0');

function localDate(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
function localTime(d: Date): string {
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// looksLikeTimezone — accept an IANA-shaped tz ("Area/City", "UTC"); reject
// free-form parentheticals so a stray "(soon)" isn't fed to Intl.
function looksLikeTimezone(tz: string): boolean {
  return /^[A-Za-z]+\/[A-Za-z0-9_+/-]+$/.test(tz) || /^(UTC|GMT)$/i.test(tz);
}

// parseResetClock turns a matched text into the next reset instant. Returns null
// when no "resets <time>" clock is present (the text isn't a limit-reset signal).
function parseResetClock(text: string, now: number): RateLimitReset | null {
  const m = RESET_RE.exec(text);
  if (!m) return null;
  let hour = Number(m[1]);
  const minute = m[2] ? Number(m[2]) : 0;
  const ap = (m[3] ?? '').toLowerCase().replace(/\./g, '');
  if (ap === 'pm' && hour < 12) hour += 12;
  if (ap === 'am' && hour === 12) hour = 0;
  if (hour > 23 || minute > 59) return null;

  const rawTz = m[4]?.trim();
  let timezone: string | undefined;
  let resetMs: number;
  if (rawTz && looksLikeTimezone(rawTz)) {
    try {
      resetMs = nextOccurrenceInTz(hour, minute, rawTz, now);
      timezone = rawTz;
    } catch {
      resetMs = nextOccurrenceLocal(hour, minute, now);
    }
  } else {
    resetMs = nextOccurrenceLocal(hour, minute, now);
  }

  const resetAt = new Date(resetMs);
  // The displayed clock: prefer the source's own rendering (g0 sans the leading
  // "resets"); fall back to a normalized HH:MM.
  const clock = m[0].replace(/^\s*resets?\s+(?:at\s+)?/i, '').trim() || `${pad(hour)}:${pad(minute)}`;
  return {
    clock,
    timezone,
    resetAt: resetAt.toISOString(),
    onceDate: localDate(resetAt),
    onceTime: localTime(resetAt),
    sourceText: text.trim(),
  };
}

// extractRateLimitReset scans the agent's activity for the most-recent quota /
// session-limit reset signal within the last 3 days and returns its reminder
// prefill, or null when none is found.
export function extractRateLimitReset(
  events: AgentActivityEvent[],
  now: number = Date.now(),
): RateLimitReset | null {
  let best: { at: number; reset: RateLimitReset } | null = null;
  for (const ev of events) {
    const at = new Date(ev.occurred_at).getTime();
    if (Number.isNaN(at)) continue;
    // Within the last 3 days (ignore future-dated rows defensively).
    if (now - at > THREE_DAYS_MS || at > now) continue;
    const text = candidateText(parsePayload(ev.payload));
    if (!/\blimit\b/i.test(text) || !/\bresets?\b/i.test(text)) continue;
    const reset = parseResetClock(text, now);
    if (!reset) continue;
    if (!best || at > best.at) best = { at, reset };
  }
  return best?.reset ?? null;
}

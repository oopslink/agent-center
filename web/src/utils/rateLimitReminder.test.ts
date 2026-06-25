import { describe, expect, it } from 'vitest';
import { extractRateLimitReset } from './rateLimitReminder';
import type { AgentActivityEvent } from '@/api/types';

// Build an activity event with a given payload object + age.
function ev(
  payload: Record<string, unknown>,
  occurred_at: string,
  event_type = 'assistant_text',
): AgentActivityEvent {
  return {
    id: `e-${occurred_at}`,
    agent_id: 'agent-1',
    event_type,
    payload: JSON.stringify(payload),
    occurred_at,
  };
}

// A fixed "now": 2026-06-25T02:00:00Z (= 10:00 Asia/Shanghai, +08:00).
const NOW = Date.parse('2026-06-25T02:00:00Z');

describe('extractRateLimitReset', () => {
  it('returns null on empty activity', () => {
    expect(extractRateLimitReset([], NOW)).toBeNull();
  });

  it('returns null when no limit message is present', () => {
    const events = [ev({ text: 'just some normal output' }, '2026-06-25T01:00:00Z')];
    expect(extractRateLimitReset(events, NOW)).toBeNull();
  });

  it('extracts the reset clock + timezone from the canonical session-limit text', () => {
    const text = "You've hit your session limit · resets 12:10pm (Asia/Shanghai)";
    const events = [ev({ raw: { type: 'text', text }, text, type: 'assistant_text' }, '2026-06-25T01:55:00Z')];
    const r = extractRateLimitReset(events, NOW);
    expect(r).not.toBeNull();
    expect(r!.timezone).toBe('Asia/Shanghai');
    expect(r!.clock).toBe('12:10pm (Asia/Shanghai)');
    // 12:10 Asia/Shanghai today (+08:00) == 04:10Z, which is after NOW (02:00Z) →
    // fires TODAY, not tomorrow.
    expect(r!.resetAt).toBe('2026-06-25T04:10:00.000Z');
    expect(r!.sourceText).toContain('session limit');
  });

  it('reads a dedicated rate_limit event (payload.message)', () => {
    const events = [
      ev(
        { message: "You've hit your usage limit · resets 9am (Asia/Shanghai)" },
        '2026-06-25T01:00:00Z',
        'rate_limit',
      ),
    ];
    const r = extractRateLimitReset(events, NOW);
    expect(r).not.toBeNull();
    expect(r!.timezone).toBe('Asia/Shanghai');
    // 9am Asia/Shanghai today == 01:00Z, already past NOW (02:00Z) → tomorrow.
    expect(r!.resetAt).toBe('2026-06-26T01:00:00.000Z');
  });

  it('rolls a same-day-but-past reset to the next day', () => {
    // 8am Asia/Shanghai == 00:00Z, before NOW → next day.
    const text = 'session limit reached · resets 8am (Asia/Shanghai)';
    const r = extractRateLimitReset([ev({ text }, '2026-06-25T01:00:00Z')], NOW);
    expect(r!.resetAt).toBe('2026-06-26T00:00:00.000Z');
  });

  it('handles a 24-hour clock with no am/pm and no timezone (local interpretation)', () => {
    // No tz → interpreted in the test runner's LOCAL tz; assert the local HH:MM
    // round-trips into onceTime rather than a hard-coded instant.
    const r = extractRateLimitReset([ev({ text: 'usage limit · resets 23:30' }, '2026-06-25T01:00:00Z')], NOW);
    expect(r).not.toBeNull();
    expect(r!.timezone).toBeUndefined();
    expect(r!.onceTime).toBe('23:30');
    // onceDate/onceTime must reconstruct resetAt when parsed as local wall time.
    expect(new Date(`${r!.onceDate}T${r!.onceTime}:00`).toISOString()).toBe(r!.resetAt);
  });

  it('ignores limit messages older than 3 days', () => {
    const text = "You've hit your session limit · resets 12:10pm (Asia/Shanghai)";
    const old = '2026-06-21T01:00:00Z'; // >3 days before NOW
    expect(extractRateLimitReset([ev({ text }, old)], NOW)).toBeNull();
  });

  it('picks the MOST RECENT limit message when several are present', () => {
    const a = ev({ text: 'session limit · resets 9am (Asia/Shanghai)' }, '2026-06-24T05:00:00Z');
    const b = ev({ text: 'session limit · resets 3pm (Asia/Shanghai)' }, '2026-06-25T01:30:00Z');
    const r = extractRateLimitReset([a, b], NOW);
    // 3pm Asia/Shanghai today == 07:00Z (after NOW) → today.
    expect(r!.clock).toContain('3pm');
    expect(r!.resetAt).toBe('2026-06-25T07:00:00.000Z');
  });

  it('rejects a non-IANA parenthetical and falls back to local time', () => {
    const r = extractRateLimitReset(
      [ev({ text: 'usage limit · resets 10:00 (soon)' }, '2026-06-25T01:00:00Z')],
      NOW,
    );
    expect(r).not.toBeNull();
    expect(r!.timezone).toBeUndefined();
    expect(r!.onceTime).toBe('10:00');
  });

  it('produces a date/time prefill local round-trip for the tz case', () => {
    const text = "You've hit your session limit · resets 12:10pm (Asia/Shanghai)";
    const r = extractRateLimitReset([ev({ text }, '2026-06-25T01:55:00Z')], NOW)!;
    // The local onceDate/onceTime, parsed as local wall time, equal resetAt.
    expect(new Date(`${r.onceDate}T${r.onceTime}:00`).toISOString()).toBe(r.resetAt);
  });
});

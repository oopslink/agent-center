import { describe, expect, it } from 'vitest';
import {
  displayNameFallback,
  isResolvedName,
  normalizeIdentityRef,
} from './members';

// v2.7 #160: members carry BARE ids ("user-x"); message sender_identity_id and
// conversation participants carry PREFIXED refs ("user:user-x" / "agent:y").
// normalizeIdentityRef strips the prefix so both key to the same value for the
// display-name lookup.
describe('normalizeIdentityRef (#160)', () => {
  it('strips user:/agent: prefixes to the bare id', () => {
    expect(normalizeIdentityRef('user:user-ab12')).toBe('user-ab12');
    expect(normalizeIdentityRef('agent:agent-cd34')).toBe('agent-cd34');
  });
  it('leaves bare ids unchanged', () => {
    expect(normalizeIdentityRef('user-ab12')).toBe('user-ab12');
    expect(normalizeIdentityRef('agent-cd34')).toBe('agent-cd34');
  });
  it('only strips the leading scheme prefix', () => {
    // a single strip — does not recurse / mangle ids that contain colons later.
    expect(normalizeIdentityRef('user:weird:id')).toBe('weird:id');
  });
});

// F1 (v2.8.1 #192): an UNRESOLVED ref (e.g. a force-deleted agent) must never
// surface the raw `agent:agent-xxx` prefixed form. The resolver's fallback +
// the isResolvedName predicate gate that.
describe('displayNameFallback (#192 F1)', () => {
  it('strips the scheme prefix → a clean tail handle (NOT the raw ref)', () => {
    const raw = 'agent:agent-8d1126f6';
    const fallback = displayNameFallback(raw);
    expect(fallback).toBe('agent-8d1126f6');
    // the load-bearing guarantee: never the raw prefixed form.
    expect(fallback).not.toBe(raw);
    expect(fallback.startsWith('agent:')).toBe(false);
    expect(fallback).not.toContain('agent:agent-');
  });
  it('strips a user: prefix too', () => {
    expect(displayNameFallback('user:user-ab12')).toBe('user-ab12');
  });
});

describe('isResolvedName (#192 F1)', () => {
  it('treats a name equal to the RAW ref as UNRESOLVED (deleted)', () => {
    // the resolver returns the raw ref unchanged on a miss (the #192/#215
    // sentinel) — that equality means "unresolved".
    const ref = 'agent:agent-8d1126f6';
    expect(isResolvedName(ref, ref)).toBe(false);
  });
  it('treats a real member display name as RESOLVED', () => {
    expect(isResolvedName('agent:agent-8d1126f6', 'builder-bot')).toBe(true);
  });
  it('an empty ref is unresolved', () => {
    expect(isResolvedName('', '')).toBe(false);
  });
});

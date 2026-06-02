import { describe, expect, it } from 'vitest';
import { normalizeIdentityRef } from './members';

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

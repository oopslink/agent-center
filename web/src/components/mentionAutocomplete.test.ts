// @vitest-environment node
import { describe, expect, it } from 'vitest';
import { detectTrigger, insertToken, mentionToken } from './mentionAutocomplete';

describe('detectTrigger', () => {
  it('detects @ at line start', () => {
    expect(detectTrigger('@al', 3)).toEqual({ trigger: '@', query: 'al', start: 0 });
  });

  it('detects @ after whitespace with empty query', () => {
    expect(detectTrigger('hi @', 4)).toEqual({ trigger: '@', query: '', start: 3 });
  });

  it('detects # (channel) after whitespace', () => {
    expect(detectTrigger('see #gen', 8)).toEqual({ trigger: '#', query: 'gen', start: 4 });
  });

  it('returns null when a space follows the trigger (token committed)', () => {
    expect(detectTrigger('@alice ', 7)).toBeNull();
  });

  it('returns null with no trigger', () => {
    expect(detectTrigger('hello world', 11)).toBeNull();
  });

  it('returns null for a mid-word @ (e.g. email — not a word-start trigger)', () => {
    expect(detectTrigger('bob@host', 8)).toBeNull();
  });

  it('uses the cursor position, not end of string', () => {
    // cursor right after "@al" inside a longer string
    expect(detectTrigger('@al rest', 3)).toEqual({ trigger: '@', query: 'al', start: 0 });
  });
});

describe('mentionToken', () => {
  it('builds @name with exactly one trailing space', () => {
    expect(mentionToken('@', 'Alice')).toBe('@Alice ');
  });
  it('builds #channel with trailing space', () => {
    expect(mentionToken('#', 'general')).toBe('#general ');
  });
  it('preserves multi-word display names (continuous substring matches wake)', () => {
    expect(mentionToken('@', 'John Smith')).toBe('@John Smith ');
  });
});

describe('insertToken', () => {
  it('replaces the trigger+query with the token and returns cursor after it', () => {
    // "hi @al" cursor=6, trigger start=3 → insert "@Alice "
    const r = insertToken('hi @al', 3, 6, '@Alice ');
    expect(r.value).toBe('hi @Alice ');
    expect(r.cursor).toBe(10);
  });

  it('keeps text after the cursor intact', () => {
    // "@al end" cursor at 3 (after @al), start 0
    const r = insertToken('@al end', 0, 3, '@Alice ');
    expect(r.value).toBe('@Alice  end'); // token has its trailing space; " end" preserved
    expect(r.cursor).toBe(7);
  });

  it('inserted token ends with a trailing space (wake word-boundary)', () => {
    const r = insertToken('@a', 0, 2, mentionToken('@', 'Alice'));
    expect(r.value.endsWith(' ')).toBe(true);
  });
});

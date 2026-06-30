import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { installMemoryLocalStorage } from '@/test/localStorageMock';
import { mergeOrder, moveId, readOrder, writeOrder } from './listOrder';

describe('mergeOrder', () => {
  it('keeps the saved relative order for ids that still exist', () => {
    expect(mergeOrder(['c', 'a', 'b'], ['a', 'b', 'c'])).toEqual(['c', 'a', 'b']);
  });

  it('appends new (unsaved) ids in their natural order, after the saved ones', () => {
    expect(mergeOrder(['b', 'a'], ['a', 'b', 'c', 'd'])).toEqual(['b', 'a', 'c', 'd']);
  });

  it('drops saved ids that no longer exist', () => {
    expect(mergeOrder(['x', 'b', 'y', 'a'], ['a', 'b'])).toEqual(['b', 'a']);
  });

  it('ignores duplicate saved ids', () => {
    expect(mergeOrder(['a', 'a', 'b'], ['a', 'b'])).toEqual(['a', 'b']);
  });

  it('no saved order = natural order', () => {
    expect(mergeOrder([], ['a', 'b', 'c'])).toEqual(['a', 'b', 'c']);
  });
});

describe('moveId', () => {
  it('moves a row down to just before the drop target', () => {
    expect(moveId(['a', 'b', 'c', 'd'], 'a', 'c')).toEqual(['b', 'a', 'c', 'd']);
  });

  it('moves a row up to just before the drop target', () => {
    expect(moveId(['a', 'b', 'c', 'd'], 'd', 'b')).toEqual(['a', 'd', 'b', 'c']);
  });

  it('is a no-op when dropping onto itself', () => {
    expect(moveId(['a', 'b', 'c'], 'b', 'b')).toEqual(['a', 'b', 'c']);
  });

  it('is a no-op when an id is missing', () => {
    expect(moveId(['a', 'b'], 'z', 'a')).toEqual(['a', 'b']);
    expect(moveId(['a', 'b'], 'a', 'z')).toEqual(['a', 'b']);
  });

  it('returns a new array (does not mutate the input)', () => {
    const input = ['a', 'b', 'c'];
    const out = moveId(input, 'a', 'c');
    expect(input).toEqual(['a', 'b', 'c']);
    expect(out).not.toBe(input);
  });
});

describe('readOrder / writeOrder', () => {
  beforeAll(installMemoryLocalStorage);
  beforeEach(() => localStorage.clear());
  afterEach(() => localStorage.clear());

  it('round-trips an id list under the namespaced key', () => {
    writeOrder('org-1/channels', ['c', 'a', 'b']);
    expect(readOrder('org-1/channels')).toEqual(['c', 'a', 'b']);
    expect(localStorage.getItem('ac.navorder.org-1/channels')).toBe(JSON.stringify(['c', 'a', 'b']));
  });

  it('returns [] for an unknown key', () => {
    expect(readOrder('nope')).toEqual([]);
  });

  it('returns [] for corrupt JSON', () => {
    localStorage.setItem('ac.navorder.bad', '{not json');
    expect(readOrder('bad')).toEqual([]);
  });

  it('filters non-string entries', () => {
    localStorage.setItem('ac.navorder.mixed', JSON.stringify(['a', 1, null, 'b']));
    expect(readOrder('mixed')).toEqual(['a', 'b']);
  });
});

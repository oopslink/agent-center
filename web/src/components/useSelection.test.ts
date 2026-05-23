import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, renderHook } from '@testing-library/react';
import { useSelection } from './useSelection';

describe('useSelection', () => {
  afterEach(() => cleanup());

  it('starts not in select mode with an empty selection', () => {
    const { result } = renderHook(() => useSelection());
    expect(result.current.selectMode).toBe(false);
    expect(result.current.count).toBe(0);
  });

  it('toggleSelectMode flips the flag and clears selection', () => {
    const { result } = renderHook(() => useSelection());
    act(() => result.current.toggleSelectMode());
    expect(result.current.selectMode).toBe(true);
    act(() => result.current.toggle('M1'));
    expect(result.current.count).toBe(1);
    act(() => result.current.toggleSelectMode()); // exit
    expect(result.current.selectMode).toBe(false);
    expect(result.current.count).toBe(0);
  });

  it('toggle adds, removes, and dedupes ids', () => {
    const { result } = renderHook(() => useSelection());
    act(() => result.current.toggle('M1'));
    act(() => result.current.toggle('M2'));
    expect(result.current.count).toBe(2);
    expect(result.current.isSelected('M1')).toBe(true);
    act(() => result.current.toggle('M1')); // remove
    expect(result.current.isSelected('M1')).toBe(false);
    expect(result.current.count).toBe(1);
  });

  it('exitSelectMode clears + leaves select mode', () => {
    const { result } = renderHook(() => useSelection());
    act(() => result.current.toggleSelectMode());
    act(() => result.current.toggle('M1'));
    act(() => result.current.exitSelectMode());
    expect(result.current.selectMode).toBe(false);
    expect(result.current.count).toBe(0);
  });

  it('clear empties selection without leaving select mode', () => {
    const { result } = renderHook(() => useSelection());
    act(() => result.current.toggleSelectMode());
    act(() => result.current.toggle('M1'));
    act(() => result.current.clear());
    expect(result.current.selectMode).toBe(true);
    expect(result.current.count).toBe(0);
  });
});

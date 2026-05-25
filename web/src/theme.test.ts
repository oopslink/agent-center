import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { applyTheme, readStoredTheme, writeTheme } from './theme';

// Polyfill localStorage for tests that need a working storage backend.
function installLocalStorage(): Record<string, string> {
  const store: Record<string, string> = {};
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => { store[k] = String(v); },
      removeItem: (k: string) => { delete store[k]; },
      clear: () => { for (const k of Object.keys(store)) delete store[k]; },
    },
    configurable: true,
  });
  return store;
}

describe('theme', () => {
  beforeEach(() => {
    installLocalStorage();
    document.documentElement.classList.remove('dark');
  });
  afterEach(() => {
    document.documentElement.classList.remove('dark');
  });

  it('readStoredTheme returns null when nothing stored', () => {
    expect(readStoredTheme()).toBeNull();
  });

  it('writeTheme persists + applies the dark class', () => {
    writeTheme('dark');
    expect(readStoredTheme()).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });

  it('writeTheme(light) removes the dark class', () => {
    writeTheme('dark');
    writeTheme('light');
    expect(readStoredTheme()).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('applyTheme is idempotent', () => {
    applyTheme('dark');
    applyTheme('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});

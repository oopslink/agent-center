// Theme toggle — v2.3 P6. Persists the user's choice in localStorage
// and applies it to <html class="dark">. Initial paint runs from
// main.tsx via applyInitialTheme() BEFORE React mounts so there's no
// flash of light theme.
//
// We deliberately keep this dependency-free + outside Zustand —
// it's a single boolean read on mount, no reactive store needed.

const STORAGE_KEY = 'ac.theme';

export type Theme = 'light' | 'dark';

function prefersDark(): boolean {
  return typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia('(prefers-color-scheme: dark)').matches
    : false;
}

export function readStoredTheme(): Theme | null {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return null;
    const v = localStorage.getItem(STORAGE_KEY);
    return v === 'light' || v === 'dark' ? v : null;
  } catch {
    return null;
  }
}

export function readTheme(): Theme {
  return readStoredTheme() ?? (prefersDark() ? 'dark' : 'light');
}

export function writeTheme(theme: Theme): void {
  try {
    if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
      localStorage.setItem(STORAGE_KEY, theme);
    }
  } catch {
    // ignore (private mode / SSR)
  }
  applyTheme(theme);
}

export function applyTheme(theme: Theme): void {
  if (typeof document === 'undefined') return;
  const root = document.documentElement;
  if (theme === 'dark') root.classList.add('dark');
  else root.classList.remove('dark');
}

/** Called from main.tsx before React renders to avoid FOUC. */
export function applyInitialTheme(): void {
  applyTheme(readTheme());
}

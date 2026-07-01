// Language persistence — v2.25 F0. Mirrors theme.ts: persists the user's
// language choice in localStorage and reflects it on <html lang="…">. The
// initial value is applied from main.tsx via applyInitialLang() BEFORE React
// mounts (and before i18next reads it) so the first paint is in the right
// language — no flash of fallback copy.
//
// Kept dependency-free + outside Zustand, exactly like theme.ts: it's a single
// string read on mount, no reactive store needed. i18next owns reactivity at
// runtime; this module owns persistence + the pre-mount/SSR-safe reads.

const STORAGE_KEY = 'ac.lang';

export type Lang = 'zh' | 'en';

export const SUPPORTED_LANGS: readonly Lang[] = ['en', 'zh'];

export const DEFAULT_LANG: Lang = 'en';

function isLang(v: unknown): v is Lang {
  return v === 'zh' || v === 'en';
}

/** Map a raw navigator/BCP-47 tag (e.g. "zh-CN", "en-US") to a supported Lang. */
function normalizeTag(tag: string | undefined): Lang | null {
  if (!tag) return null;
  const base = tag.toLowerCase().split('-')[0];
  return isLang(base) ? base : null;
}

function navigatorLang(): Lang | null {
  if (typeof navigator === 'undefined') return null;
  const langs = Array.isArray(navigator.languages) && navigator.languages.length
    ? navigator.languages
    : [navigator.language];
  for (const l of langs) {
    const m = normalizeTag(l);
    if (m) return m;
  }
  return null;
}

export function readStoredLang(): Lang | null {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return null;
    const v = localStorage.getItem(STORAGE_KEY);
    return isLang(v) ? v : null;
  } catch {
    return null;
  }
}

export function readLang(): Lang {
  return readStoredLang() ?? navigatorLang() ?? DEFAULT_LANG;
}

export function writeLang(lang: Lang): void {
  try {
    if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
      localStorage.setItem(STORAGE_KEY, lang);
    }
  } catch {
    // ignore (private mode / SSR)
  }
  applyLang(lang);
}

export function applyLang(lang: Lang): void {
  if (typeof document === 'undefined') return;
  document.documentElement.lang = lang;
}

/** Called from main.tsx before React renders to avoid a flash of fallback copy. */
export function applyInitialLang(): void {
  applyLang(readLang());
}

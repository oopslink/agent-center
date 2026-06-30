import type React from 'react';
import { useTranslation } from 'react-i18next';
import { SUPPORTED_LANGS, writeLang, type Lang } from '@/i18n/lang';

// v2.25 F0 — the Language panel under System → Settings. A segmented
// EN | 中文 control mirroring ThemeSegmented's pattern. Selecting a language:
//   1. i18n.changeLanguage(l) — re-renders the whole tree in the new language
//      immediately (react-i18next subscription), and
//   2. writeLang(l) — persists to localStorage `ac.lang` + sets <html lang>,
//      so a reload keeps the choice (and applyInitialLang avoids any flash).
// The two are the runtime + persistence halves of the same switch.
export function LanguagePanel(): React.ReactElement {
  const { t, i18n } = useTranslation('common');
  // i18n.language can be a region tag (e.g. 'en-US') under some detectors; we
  // only ever store the base, so compare on the base for the active segment.
  const current = (i18n.language?.split('-')[0] as Lang) ?? 'en';

  const setLang = (l: Lang) => {
    if (l === current) return;
    void i18n.changeLanguage(l);
    writeLang(l);
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    const idx = SUPPORTED_LANGS.indexOf(current);
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault();
      setLang(SUPPORTED_LANGS[Math.min(idx + 1, SUPPORTED_LANGS.length - 1)]);
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault();
      setLang(SUPPORTED_LANGS[Math.max(idx - 1, 0)]);
    }
  };

  return (
    <div
      className="max-w-md rounded-lg border border-border-base bg-bg-elevated p-4"
      data-testid="language-panel"
    >
      <h2 className="text-sm font-semibold text-text-primary">{t('settings.language.title')}</h2>
      <p className="mt-1 text-xs text-text-muted">{t('settings.language.description')}</p>
      <div
        role="radiogroup"
        aria-label={t('settings.language.title')}
        data-testid="language-toggle"
        onKeyDown={onKeyDown}
        className="mt-3 flex gap-1 rounded-md border border-border-base bg-bg-base p-0.5"
      >
        {SUPPORTED_LANGS.map((l) => {
          const selected = current === l;
          return (
            <button
              key={l}
              type="button"
              role="radio"
              aria-checked={selected}
              data-testid={`language-segment-${l}`}
              tabIndex={selected ? 0 : -1}
              onClick={() => setLang(l)}
              className={[
                'flex flex-1 items-center justify-center rounded px-3 py-1.5 text-xs font-medium motion-safe:transition-colors',
                selected ? 'bg-brand-hover text-white shadow-sm' : 'text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              {t(`settings.language.${l}`)}
            </button>
          );
        })}
      </div>
    </div>
  );
}

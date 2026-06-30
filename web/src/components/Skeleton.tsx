import type React from 'react';
import { useTranslation } from 'react-i18next';

// Skeleton — replaces the "Loading…" plain-text fallback (skill rule
// `progressive-loading`: anything that takes >300ms should reserve
// visual space rather than blank-and-pop). Width is parameterised so
// callers can match it to the eventual content shape (e.g. table row,
// card, chip).
//
// We use a CSS pulse rather than a moving shimmer to satisfy
// prefers-reduced-motion via the global media-query in index.css.
export function Skeleton({
  className,
  width,
  height,
}: {
  className?: string;
  width?: string;
  height?: string;
}): React.ReactElement {
  const style: React.CSSProperties = {
    width: width ?? '100%',
    height: height ?? '1rem',
  };
  return (
    <span
      aria-hidden="true"
      className={[
        'inline-block animate-pulse rounded bg-bg-subtle',
        className ?? '',
      ].join(' ')}
      style={style}
    />
  );
}

// PageSkeleton — composite shell used as the Suspense fallback for any
// lazily loaded page. Mirrors the rough shape of a "list + sidebar"
// page so route transitions don't flash blank.
export function PageSkeleton(): React.ReactElement {
  const { t } = useTranslation('common');
  return (
    <div
      role="status"
      aria-live="polite"
      aria-busy="true"
      data-testid="page-fallback"
      className="space-y-4"
    >
      <Skeleton width="12rem" height="1.5rem" />
      <div className="space-y-2">
        <Skeleton height="2rem" />
        <Skeleton height="2rem" />
        <Skeleton height="2rem" />
      </div>
      <span className="sr-only">{t('skeleton.loading')}</span>
    </div>
  );
}

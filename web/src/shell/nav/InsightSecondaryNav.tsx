import type React from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink } from 'react-router-dom';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';

function InsightIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="M4 14.5 8 10l3 2 5-7" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M4 5v10h12" strokeLinecap="round" />
    </svg>
  );
}

export function InsightSecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  const { t } = useTranslation('insights');
  const overviewPath = `${orgBase}/insights/overview`;
  return (
    <ul className="space-y-4" data-testid="insight-secondary-nav">
      <li>
        <h2 className="px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
          {t('insight.nav.section')}
        </h2>
        <ul className="space-y-0.5">
          <li>
            <NavLink
              to={overviewPath}
              className={({ isActive }) => [
                'flex items-center gap-2 rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                isActive ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
              ].join(' ')}
            >
              <span aria-hidden="true" className="inline-flex h-4 w-4"><InsightIcon /></span>
              <span>{t('insight.overview.title')}</span>
            </NavLink>
          </li>
        </ul>
      </li>
    </ul>
  );
}

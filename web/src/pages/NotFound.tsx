import type React from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';


// NotFound — 404 with a navigation link back to the home so users don't
// get stuck. Per x9527 #6 open question 5.
export default function NotFound(): React.ReactElement {
  const { t } = useTranslation('common');
  return (
    <section className="space-y-4" data-testid="page-NotFound">
      <h2 className="text-xl font-semibold">{t('notFound.title')}</h2>
      <p className="text-sm text-text-muted">
        {t('notFound.description')}
      </p>
      <div className="space-x-3">
        <OrgLink
          to="/"
          className="text-accent hover:underline"
          data-testid="nav-home"
        >
          {t('notFound.home')}
        </OrgLink>
      </div>
    </section>
  );
}

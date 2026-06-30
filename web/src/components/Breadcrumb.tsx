import type React from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';

export interface BreadcrumbItem {
  /** Visible text — a display NAME, never a raw id (#192). */
  label: string;
  /** Org-scoped path; when set (and not the current/last segment) the segment links. */
  to?: string;
}

// Breadcrumb (v2.7.1 #238) — a generic detail-page trail rendered above the
// header (oopslink dogfood: detail pages had empty space + no "where am I").
// Shape: `Section / … / Current`, separated by "/". Segments with `to` link
// (org-scoped OrgLink); section labels without `to` are plain muted text; the
// last segment is the current page — bold, non-clickable, aria-current.
// Callers pass display NAMES, never raw ids (#192 chrome rule).
export function Breadcrumb({ items }: { items: BreadcrumbItem[] }): React.ReactElement {
  const { t } = useTranslation('common');
  return (
    <nav
      className="flex flex-wrap items-center gap-1.5 text-xs text-text-muted"
      aria-label={t('breadcrumb.ariaLabel')}
      data-testid="breadcrumb"
    >
      {items.map((item, i) => {
        const isLast = i === items.length - 1;
        return (
          <span key={`${i}-${item.label}`} className="flex items-center gap-1.5">
            {i > 0 && <span aria-hidden="true">/</span>}
            {isLast ? (
              <span
                className="font-semibold text-text-secondary"
                data-testid={`breadcrumb-segment-${i}`}
                aria-current="page"
              >
                {item.label}
              </span>
            ) : item.to ? (
              <OrgLink
                to={item.to}
                className="hover:text-text-primary hover:underline"
                data-testid={`breadcrumb-segment-${i}`}
              >
                {item.label}
              </OrgLink>
            ) : (
              <span data-testid={`breadcrumb-segment-${i}`}>{item.label}</span>
            )}
          </span>
        );
      })}
    </nav>
  );
}

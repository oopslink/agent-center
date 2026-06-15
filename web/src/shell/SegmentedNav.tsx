import type React from 'react';
import { useLocation } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';

// ============================================================================
// v2.10.1 SegmentedNav — the reusable mobile (<768) "二级段控" (secondary
// segmented nav). On mobile a module's col② second-level nav (e.g. System's
// Environment | Settings, Conversations' Channels | DMs) reflows to a row of
// pill segments at the top of the screen (mockup `.mseg`), navigating between
// the module's sibling routes. Desktop (≥768) keeps the real col② nav, so this
// is `md:hidden`.
//
// Each segment links to an org-scoped route (via OrgLink). The active segment
// is detected by matching the current path's tail; segments are ≥44px touch
// targets and the row scrolls horizontally if it overflows.
// ============================================================================
export interface Segment {
  label: string;
  /** Org-relative route, e.g. "/environment". */
  to: string;
  testId?: string;
}

export function SegmentedNav({
  items,
  ariaLabel,
}: {
  items: ReadonlyArray<Segment>;
  ariaLabel: string;
}): React.ReactElement {
  const { pathname } = useLocation();
  return (
    <nav
      aria-label={ariaLabel}
      data-testid="segmented-nav"
      className="-mx-1 flex gap-1.5 overflow-x-auto px-1 md:hidden"
    >
      {items.map((s) => {
        const active = pathname === s.to || pathname.endsWith(s.to);
        return (
          <OrgLink
            key={s.to}
            to={s.to}
            data-testid={s.testId ?? `segment-${s.to.replace(/^\//, '')}`}
            data-active={active}
            aria-current={active ? 'page' : undefined}
            className={[
              'inline-flex min-h-[44px] shrink-0 items-center whitespace-nowrap rounded-full px-4 text-sm motion-safe:transition-colors',
              active
                ? 'bg-brand font-semibold text-white'
                : 'bg-bg-subtle text-text-secondary hover:bg-bg-base',
            ].join(' ')}
          >
            {s.label}
          </OrgLink>
        );
      })}
    </nav>
  );
}

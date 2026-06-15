import type React from 'react';
import { Link } from 'react-router-dom';
import { useOptionalOrgContext } from '@/OrgContext';

// MembersSegmentControl (v2.10.1 M6) — the mobile-only (md:hidden) Humans/Agents
// switch shown atop the Members list pages. On desktop the col② secondary nav
// owns this switch, but col② is hidden on mobile, so each Members list surfaces
// this segmented control instead (mockup `docs/design/v2.10.1/v2.10.1-mobile` —
// Members frame `.mseg`). Segments are full-width, ≥44px tall (touch baseline).
export function MembersSegmentControl({
  active,
}: {
  active: 'humans' | 'agents';
}): React.ReactElement {
  const orgCtx = useOptionalOrgContext();
  const base = orgCtx ? `/organizations/${orgCtx.slug}` : '';

  const seg = (id: 'humans' | 'agents', label: string, to: string): React.ReactElement => {
    const on = active === id;
    return (
      <Link
        to={to}
        role="tab"
        aria-selected={on}
        aria-current={on ? 'page' : undefined}
        data-testid={`members-seg-${id}`}
        data-active={on}
        className={[
          'flex min-h-[44px] flex-1 items-center justify-center rounded-md text-sm font-medium',
          on
            ? 'border border-border-base bg-bg-elevated text-text-primary'
            : 'text-text-secondary hover:text-text-primary',
        ].join(' ')}
      >
        {label}
      </Link>
    );
  };

  return (
    <div
      className="flex gap-1 rounded-lg bg-bg-subtle p-1 md:hidden"
      role="tablist"
      aria-label="Members type"
      data-testid="members-segment"
    >
      {seg('humans', 'Humans', `${base}/members/humans`)}
      {seg('agents', 'Agents', `${base}/agents`)}
    </div>
  );
}

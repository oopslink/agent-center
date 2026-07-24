import type React from 'react';

export function SystemDmBadge({ className = '' }: { className?: string }): React.ReactElement {
  return (
    <span
      data-testid="system-dm-badge"
      className={[
        'rounded bg-status-amber-bg px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-status-amber-fg',
        className,
      ]
        .filter(Boolean)
        .join(' ')}
    >
      SYSTEM
    </span>
  );
}
